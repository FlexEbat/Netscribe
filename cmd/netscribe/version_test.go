package main

import (
	"bytes"
	"io"
	"runtime"
	"strings"
	"testing"
)

func TestVersionCommandPrintsOneLineOnStdout(t *testing.T) {
	for _, arg := range []string{"version", "--version"} {
		var out, errOut bytes.Buffer
		code := run([]string{arg}, streams{in: strings.NewReader(""), out: &out, err: &errOut})
		if code != 0 {
			t.Fatalf("%s: exit code = %d, want 0", arg, code)
		}
		line := strings.TrimSpace(out.String())
		if !strings.HasPrefix(line, "netscribe ") || strings.Contains(line, "\n") || !strings.Contains(line, runtime.Version()) {
			t.Errorf("%s: output %q, want one line that names the version and the Go toolchain", arg, line)
		}
		if errOut.Len() != 0 {
			t.Errorf("%s: wrote %q to stderr", arg, errOut.String())
		}
	}
}

func TestVersionCommandRejectsArguments(t *testing.T) {
	code := run([]string{"version", "extra"}, streams{in: strings.NewReader(""), out: io.Discard, err: io.Discard})
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}
