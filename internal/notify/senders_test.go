package notify

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FlexEbat/Netscribe/internal/config"
)

func TestWebhookPostsJSONAndRetriesOnA500(t *testing.T) {
	var hits atomic.Int32
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	if err := (webhook{url: srv.URL + "/secret-path", client: newClient(time.Second)}).Send(context.Background(), "s", "b"); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 1 || gotBody["subject"] != "s" || gotBody["text"] != "b" {
		t.Errorf("hits %d, body %v", hits.Load(), gotBody)
	}

	var bad atomic.Int32
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		bad.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failing.Close()
	n := New(fakeSource{}, nil, nil)
	n.retryDelay = time.Millisecond
	err := n.deliver(context.Background(), cfgChannel(failing.URL+"/secret-path"), "s", "b")
	if err == nil || bad.Load() != 2 {
		t.Errorf("err %v after %d calls, want an error after exactly 2", err, bad.Load())
	}
	if err != nil && (strings.Contains(err.Error(), "secret-path") || strings.Contains(err.Error(), failing.URL)) {
		t.Errorf("the error holds the address: %v", err)
	}
}

func TestWebhookDoesNotFollowARedirect(t *testing.T) {
	var elsewhere atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { elsewhere.Add(1) }))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusFound)
	}))
	defer srv.Close()
	err := (webhook{url: srv.URL, client: newClient(time.Second)}).Send(context.Background(), "s", "b")
	if err == nil || elsewhere.Load() != 0 {
		t.Errorf("err %v, the redirect target was hit %d times, want an error and 0", err, elsewhere.Load())
	}
}

func TestWebhookTimesOutWithoutNamingTheAddress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { time.Sleep(300 * time.Millisecond) }))
	defer srv.Close()
	start := time.Now()
	err := (webhook{url: srv.URL + "/secret-path", client: newClient(50 * time.Millisecond)}).Send(context.Background(), "s", "b")
	if err == nil || err.Error() != "timeout" || time.Since(start) > 250*time.Millisecond {
		t.Errorf("err %v after %v, want a plain timeout quickly", err, time.Since(start))
	}
}

func TestTelegramUsesPlainTextAndHidesTheTokenInErrors(t *testing.T) {
	var path string
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&got)
	}))
	tg := telegram{base: srv.URL, token: "123:SECRET", chat: "42", client: newClient(time.Second)}
	if err := tg.Send(context.Background(), "s", strings.Repeat("x", 5000)); err != nil {
		t.Fatal(err)
	}
	if path != "/bot123:SECRET/sendMessage" || got["chat_id"] != "42" || len([]rune(got["text"])) != telegramRunes {
		t.Errorf("path %q, body chat %q, text of %d runes", path, got["chat_id"], len([]rune(got["text"])))
	}
	if _, has := got["parse_mode"]; has {
		t.Error("a parse mode was sent")
	}
	srv.Close()
	if err := tg.Send(context.Background(), "s", "b"); err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Errorf("error after the server is gone: %v", err)
	}
}

// fakeSMTP greets, answers EHLO without STARTTLS and records every command after that.
func fakeSMTP(t *testing.T) (port int, commands func() []string) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	var seen atomic.Value
	seen.Store([]string{})
	go func() {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		_, _ = io.WriteString(conn, "220 test ESMTP\r\n")
		r := bufio.NewReader(conn)
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimSpace(line)
			seen.Store(append(seen.Load().([]string), line))
			switch {
			case strings.HasPrefix(line, "EHLO"):
				_, _ = io.WriteString(conn, "250 test\r\n") // no STARTTLS line
			case strings.HasPrefix(line, "QUIT"):
				_, _ = io.WriteString(conn, "221 bye\r\n")
				return
			default:
				_, _ = io.WriteString(conn, "250 ok\r\n")
			}
		}
	}()
	return l.Addr().(*net.TCPAddr).Port, func() []string { return seen.Load().([]string) }
}

func TestEmailWithoutStartTLSSendsNothing(t *testing.T) {
	port, commands := fakeSMTP(t)
	e := email{host: "127.0.0.1", port: port, user: "u", password: "SECRET-PW", from: "n@example.org", to: []string{"a@example.org"},
		dial: (&net.Dialer{Timeout: time.Second}).DialContext}
	err := e.Send(context.Background(), "s", "b")
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("err = %v, want a refusal because of STARTTLS", err)
	}
	time.Sleep(50 * time.Millisecond)
	for _, c := range commands() {
		if strings.HasPrefix(c, "MAIL") || strings.HasPrefix(c, "AUTH") || strings.HasPrefix(c, "RCPT") || strings.HasPrefix(c, "DATA") {
			t.Errorf("the client sent %q to a server without STARTTLS", c)
		}
	}
}

func TestMessageHasSafeHeaders(t *testing.T) {
	from, err := mail.ParseAddress("Net Scribe <n@example.org>")
	if err != nil {
		t.Fatal(err)
	}
	to, err := mail.ParseAddress("a@example.org")
	if err != nil {
		t.Fatal(err)
	}
	m := message(from, []*mail.Address{to}, "Subject\r\nBcc: evil@example.org", "line1\nline2")
	head := strings.SplitN(m, "\r\n\r\n", 2)[0]
	if strings.Contains(head, "\r\nBcc:") {
		t.Errorf("the subject injected a header:\n%s", head)
	}
	if !strings.Contains(m, "line1\r\nline2\r\n") {
		t.Errorf("body not CRLF: %q", m)
	}
}

func cfgChannel(url string) config.ChannelConfig {
	return config.ChannelConfig{Name: "x", Type: config.ChannelWebhook, URL: config.Secret(url)}
}
