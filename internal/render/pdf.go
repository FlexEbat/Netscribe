package render

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/chromedp/chromedp"
)

// PDFTimeout is how long one PDF may take, waiting for its turn included.
const PDFTimeout = 60 * time.Second

// ErrNoChrome means no Chrome or Chromium was found to print the PDF.
var ErrNoChrome = errors.New("chrome or chromium not found")

// chromeNames are the programs looked up in PATH when export.chromePath is empty.
var chromeNames = []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable", "chrome"}

// PDFFunc prints an HTML document as PDF.
type PDFFunc func(ctx context.Context, html []byte) ([]byte, error)

// FindChrome returns the browser to print with: the configured path, or the first of the
// usual names in PATH.
func FindChrome(configured string) (string, error) {
	if configured != "" {
		p, err := exec.LookPath(configured)
		if err != nil {
			return "", fmt.Errorf("%w: %s", ErrNoChrome, "export.chromePath does not point to a program")
		}
		return p, nil
	}
	for _, n := range chromeNames {
		if p, err := exec.LookPath(n); err == nil {
			return p, nil
		}
	}
	return "", ErrNoChrome
}

// pdfSlot lets one render run at a time: Chrome is heavy, and a download is no reason to
// start several.
var pdfSlot = make(chan struct{}, 1)

// ChromePDF returns a PDFFunc that prints with the Chrome found at path (or in PATH).
// It waits for its turn, then renders, and gives up when ctx ends or PDFTimeout passes.
func ChromePDF(path string) PDFFunc {
	return func(ctx context.Context, html []byte) ([]byte, error) {
		exe, err := FindChrome(path)
		if err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(ctx, PDFTimeout)
		defer cancel()
		asked := time.Now()
		select {
		case pdfSlot <- struct{}{}:
			defer func() { <-pdfSlot }()
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		waited := time.Since(asked)
		pdf, err := printer(ctx, exe, html)
		slog.Info("pdf done", "wait_ms", waited.Milliseconds(), "total_ms", time.Since(asked).Milliseconds(), "ok", err == nil)
		return pdf, err
	}
}

// printer prints one document. Tests replace it, a real Chrome is not always there.
var printer = printPDF

// printPDF opens the document from a private temporary file and prints it. The browser
// gets no network and no script: the document needs neither.
func printPDF(ctx context.Context, exe string, html []byte) ([]byte, error) {
	dir, err := os.MkdirTemp("", "netscribe-pdf-")
	if err != nil {
		return nil, fmt.Errorf("make temp dir: %w", err)
	}
	defer func() {
		removing := time.Now()
		_ = os.RemoveAll(dir) // best effort, the directory is private
		slog.Info("pdf temp files removed", "ms", time.Since(removing).Milliseconds())
	}()
	file := filepath.Join(dir, "report.html")
	if err := os.WriteFile(file, html, 0o600); err != nil {
		return nil, fmt.Errorf("write report: %w", err)
	}

	// Chrome runs with its sandbox. Where the system forbids it (as root, in a container, or
	// with unprivileged user namespaces switched off) the browser says so at start, and the
	// print is tried once more without it: the document is made by this program, every
	// value in it is escaped, and the browser runs without script or network.
	noSandbox := os.Geteuid() == 0 || noSandboxFromNowOn.Load()
	tried := time.Now()
	pdf, err := printOnceFn(ctx, exe, dir, file, noSandbox)
	if err != nil && !noSandbox && ctx.Err() == nil && strings.Contains(err.Error(), "sandbox") {
		// The failed try is the slow, uneven part of a first print on such a system (in CI it
		// took from 8 s to more than 40 s), so its time and its error are logged, and a print
		// that then works without the sandbox makes the next prints skip the failing try.
		slog.Info("pdf sandbox is not usable, printing without it", "failed_after_ms", time.Since(tried).Milliseconds(), "error", firstLine(err.Error()))
		pdf, err = printOnceFn(ctx, exe, dir, file, true)
		if err == nil {
			noSandboxFromNowOn.Store(true)
		}
	}
	return pdf, err
}

// noSandboxFromNowOn is set once a print has failed for the sandbox and worked without it:
// the system does not change while the program runs.
var noSandboxFromNowOn atomic.Bool

// printOnceFn is printOnce; tests replace it, a real Chrome is not always there.
var printOnceFn = printOnce

func printOnce(ctx context.Context, exe, dir, file string, noSandbox bool) ([]byte, error) {
	profile, err := os.MkdirTemp(dir, "profile-")
	if err != nil {
		return nil, fmt.Errorf("make profile dir: %w", err)
	}
	opts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	opts = append(opts,
		chromedp.ExecPath(exe),
		chromedp.UserDataDir(profile),
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("host-resolver-rules", "MAP * ~NOTFOUND"),
		chromedp.Flag("blink-settings", "scriptEnabled=false"),
	)
	if noSandbox {
		opts = append(opts, chromedp.Flag("no-sandbox", true))
	}
	alloc, cancelAlloc := chromedp.NewExecAllocator(ctx, opts...)
	tab, cancelTab := chromedp.NewContext(alloc)
	defer func() {
		// Closing the browser waits for the process to end: its time is logged because it
		// may be the part of a print that no other timer holds.
		closing := time.Now()
		cancelTab()
		cancelAlloc()
		slog.Info("pdf chrome closed", "ms", time.Since(closing).Milliseconds())
	}()

	started := time.Now()
	if err := chromedp.Do(tab, chromedp.Navigate("about:blank")); err != nil { // starts the browser and nothing else
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("start chrome: %w", err)
	}
	launched := time.Since(started)
	began := time.Now()
	if err := chromedp.Do(tab, chromedp.Navigate("file://"+filepath.ToSlash(file))); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("open report: %w", err)
	}
	opened := time.Since(began)
	pdf, err := chromedp.Run(tab, chromedp.PrintToPDF(chromedp.PDFPrintBackground(), chromedp.PDFPreferCSSPageSize()))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("print pdf: %w", err)
	}
	// One line per print: where the seconds go (starting Chrome and loading the page, or the
	// print itself) and how big the document was. Amendment 60: the PDF of 600 devices took
	// 7.5 s, 18.7 s and 45.9 s in CI and nothing explained the spread.
	var size int64
	if st, err := os.Stat(file); err == nil {
		size = st.Size()
	}
	slog.Info("pdf printed", "html_kb", size/1024, "launch_ms", launched.Milliseconds(), "open_ms", opened.Milliseconds(),
		"print_ms", (time.Since(began) - opened).Milliseconds(), "pdf_kb", len(pdf)/1024, "no_sandbox", noSandbox)
	return pdf, nil
}

// firstLine is the first line of a message, for a log: Chrome follows its one line of cause
// with a page of stack.
func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	if len(line) > 300 {
		line = line[:300]
	}
	return line
}
