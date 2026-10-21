package main

import (
	"bytes"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// busyPort holds a port on loopback until the test ends and returns its address.
func busyPort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := ln.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	return ln.Addr().String()
}

func portOf(t *testing.T, ln net.Listener) int {
	t.Helper()
	_, p, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.Atoi(p)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// answers returns an ask function that replies with the given lines in order and then
// reports the end of input, and counts the prompts it saw.
func answers(lines ...string) (func(string) (string, error), *int) {
	asked := 0
	return func(string) (string, error) {
		asked++
		if asked > len(lines) {
			return "", io.EOF
		}
		return lines[asked-1], nil
	}, &asked
}

func TestBindListenerUsesAFreePortAsIs(t *testing.T) {
	ln, err := bindListener("127.0.0.1:0", bindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestBindListenerFailsWithAHintWhenNobodyCanBeAsked(t *testing.T) {
	busy := busyPort(t)
	var warned []string
	_, err := bindListener(busy, bindOptions{warn: func(msg string, _ ...any) { warned = append(warned, msg) }})
	if err == nil {
		t.Fatal("no error for a port in use")
	}
	for _, want := range []string{busy, "already in use", "--listen", "--auto-port"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	if len(warned) == 0 {
		t.Error("no warning for a port in use")
	}
}

func TestBindListenerAutoTakesTheNextFreePort(t *testing.T) {
	busy := busyPort(t)
	ln, err := bindListener(busy, bindOptions{auto: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }() // the test ends here, a failed close changes nothing
	host, busyPortText, _ := net.SplitHostPort(busy)
	got, _, _ := net.SplitHostPort(ln.Addr().String())
	if got != host {
		t.Errorf("host = %q, want %q", got, host)
	}
	busyNumber, _ := strconv.Atoi(busyPortText)
	if p := portOf(t, ln); p <= busyNumber || p > busyNumber+maxPortSearch {
		t.Errorf("port = %d, want one of the %d above %d", p, maxPortSearch, busyNumber)
	}
}

func TestBindListenerAsksAgainAfterAnAnswerThatDoesNotWork(t *testing.T) {
	busy := busyPort(t)
	taken := busyPort(t)
	_, takenPort, _ := net.SplitHostPort(taken)

	// A free port number is found by opening and closing a listener.
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	free := portOf(t, probe)
	if err := probe.Close(); err != nil {
		t.Fatal(err)
	}

	ask, asked := answers("nonsense", "0", "70000", takenPort, strconv.Itoa(free))
	ln, err := bindListener(busy, bindOptions{ask: ask})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }() // the test ends here, a failed close changes nothing
	if got := portOf(t, ln); got != free {
		t.Errorf("port = %d, want %d", got, free)
	}
	if *asked != 5 {
		t.Errorf("asked %d times, want 5", *asked)
	}
}

func TestBindListenerAskedForAuto(t *testing.T) {
	ask, _ := answers("auto")
	ln, err := bindListener(busyPort(t), bindOptions{ask: ask})
	if err != nil {
		t.Fatal(err)
	}
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestBindListenerEmptyAnswerQuits(t *testing.T) {
	ask, _ := answers("")
	if _, err := bindListener(busyPort(t), bindOptions{ask: ask}); err == nil {
		t.Fatal("no error after an empty answer")
	}
}

func TestBindListenerEndOfInputQuits(t *testing.T) {
	ask, _ := answers()
	_, err := bindListener(busyPort(t), bindOptions{ask: ask})
	if !errors.Is(err, io.EOF) {
		t.Fatalf("error = %v, want the end of input", err)
	}
}

func TestBindListenerKeepsOtherErrors(t *testing.T) {
	ask, asked := answers("auto")
	if _, err := bindListener("192.0.2.1:8080", bindOptions{auto: true, ask: ask}); err == nil {
		t.Fatal("no error for an address that cannot be bound")
	}
	if *asked != 0 {
		t.Error("asked about a problem that is not a busy port")
	}
}

func TestServeStopsAtABusyPortBeforeAnythingElseStarts(t *testing.T) {
	busy := busyPort(t)
	dir := t.TempDir()
	db := filepath.Join(dir, "netscribe.db")
	cfg := filepath.Join(dir, "netscribe.yaml")
	body := "listen: \"" + busy + "\"\ndatabase: \"" + db + "\"\nscan:\n  targets: [192.168.1.0/24]\n"
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	var stderr bytes.Buffer
	code := run([]string{"serve", "--config", cfg}, streams{in: strings.NewReader(""), out: io.Discard, err: &stderr})
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	for _, want := range []string{"already in use", "--auto-port"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr %q does not mention %q", stderr.String(), want)
		}
	}
	if _, err := os.Stat(db); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the database was opened before the port was taken: %v", err)
	}
}

func TestServeRejectsABadListenFlag(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "netscribe.yaml")
	if err := os.WriteFile(cfg, []byte("scan:\n  targets: [192.168.1.0/24]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	code := run([]string{"serve", "--config", cfg, "--listen", "8080"}, streams{in: strings.NewReader(""), out: io.Discard, err: &stderr})
	if code != 1 || !strings.Contains(stderr.String(), "--listen") {
		t.Errorf("exit code %d, stderr %q, want 1 and a message about --listen", code, stderr.String())
	}
}
