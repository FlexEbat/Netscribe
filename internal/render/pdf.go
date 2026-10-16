package render

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/chromedp/cdproto/page"
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
		select {
		case pdfSlot <- struct{}{}:
			defer func() { <-pdfSlot }()
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return printer(ctx, exe, html)
	}
}

// printer prints one document. Tests replace it, a real Chrome is not always there.
var printer = printPDF

// printPDF opens the document from a private temporary file and prints it. The browser
// gets no network: the document needs none.
func printPDF(ctx context.Context, exe string, html []byte) ([]byte, error) {
	dir, err := os.MkdirTemp("", "netscribe-pdf-")
	if err != nil {
		return nil, fmt.Errorf("make temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }() // best effort, the directory is private
	file := filepath.Join(dir, "report.html")
	if err := os.WriteFile(file, html, 0o600); err != nil {
		return nil, fmt.Errorf("write report: %w", err)
	}

	opts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	opts = append(opts,
		chromedp.ExecPath(exe),
		chromedp.UserDataDir(filepath.Join(dir, "profile")),
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("host-resolver-rules", "MAP * ~NOTFOUND"),
	)
	if os.Geteuid() == 0 {
		// Chrome refuses its sandbox as root, which is how a container runs it.
		opts = append(opts, chromedp.NoSandbox)
	}
	alloc, cancelAlloc := chromedp.NewExecAllocator(ctx, opts...)
	defer cancelAlloc()
	tab, cancelTab := chromedp.NewContext(alloc)
	defer cancelTab()

	var pdf []byte
	err = chromedp.Run(tab,
		chromedp.Navigate("file://"+filepath.ToSlash(file)),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			pdf, _, err = page.PrintToPDF().WithPrintBackground(true).WithPreferCSSPageSize(true).Do(ctx)
			return err
		}),
	)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("print pdf: %w", err)
	}
	return pdf, nil
}
