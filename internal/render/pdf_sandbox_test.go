package render

import (
	"context"
	"errors"
	"os"
	"testing"
)

// A system that forbids the sandbox costs one failed try on the first print only: the next
// prints go straight to the way that works.
func TestAPrintThatNeededNoSandboxMakesTheNextOnesSkipTheFailingTry(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("as root the sandbox is never tried")
	}
	old, oldFlag := printOnceFn, noSandboxFromNowOn.Load()
	noSandboxFromNowOn.Store(false)
	defer func() { printOnceFn = old; noSandboxFromNowOn.Store(oldFlag) }()

	var tries []bool
	printOnceFn = func(_ context.Context, _, _, _ string, noSandbox bool) ([]byte, error) {
		tries = append(tries, noSandbox)
		if !noSandbox {
			return nil, errors.New("chrome failed: No usable sandbox")
		}
		return []byte("%PDF-1.4"), nil
	}
	for i := 0; i < 2; i++ {
		if _, err := printPDF(context.Background(), "chrome", []byte("<html></html>")); err != nil {
			t.Fatalf("print %d: %v", i+1, err)
		}
	}
	want := []bool{false, true, true} // sandbox, the retry, then straight to no sandbox
	if len(tries) != len(want) {
		t.Fatalf("tries = %v, want %v", tries, want)
	}
	for i := range want {
		if tries[i] != want[i] {
			t.Fatalf("tries = %v, want %v", tries, want)
		}
	}
}

// Another failure is not a reason to give up the sandbox.
func TestAFailureThatIsNotTheSandboxKeepsTheSandbox(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("as root the sandbox is never tried")
	}
	old, oldFlag := printOnceFn, noSandboxFromNowOn.Load()
	noSandboxFromNowOn.Store(false)
	defer func() { printOnceFn = old; noSandboxFromNowOn.Store(oldFlag) }()

	calls := 0
	printOnceFn = func(context.Context, string, string, string, bool) ([]byte, error) {
		calls++
		return nil, errors.New("print pdf: the page crashed")
	}
	if _, err := printPDF(context.Background(), "chrome", []byte("<html></html>")); err == nil {
		t.Fatal("the error was swallowed")
	}
	if calls != 1 || noSandboxFromNowOn.Load() {
		t.Fatalf("calls = %d, sticky = %v; a failure that is not the sandbox must not retry or switch it off", calls, noSandboxFromNowOn.Load())
	}
}
