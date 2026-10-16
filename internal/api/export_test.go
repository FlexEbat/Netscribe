package api

import (
	"context"
	"encoding/xml"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/render"
)

func TestExportNeedsASession(t *testing.T) {
	e := newEnv(t)
	for _, format := range []string{"md", "drawio"} {
		if rec := e.do(http.MethodGet, "/api/export/"+format, nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want 401", format, rec.Code)
		}
	}
}

func TestExportMarkdownIsADownloadWithTheDateInItsName(t *testing.T) {
	e := newEnv(t)
	seedContainers(t, e)

	rec := e.as(e.mustLogin("admin"), http.MethodGet, "/api/export/md", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Content-Disposition"); got != "attachment; filename=netscribe-2026-10-09.md" {
		t.Errorf("Content-Disposition = %q", got)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/markdown") {
		t.Errorf("Content-Type = %q", got)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q", rec.Header().Get("Cache-Control"))
	}
	body := rec.Body.String()
	for _, want := range []string{"```mermaid", "docker-host", "| web | nginx:1.27 | running"} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q:\n%s", want, body)
		}
	}
}

func TestExportDrawioIsWellFormedXML(t *testing.T) {
	e := newEnv(t)
	seedContainers(t, e)

	rec := e.as(e.mustLogin("admin"), http.MethodGet, "/api/export/drawio", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Disposition"), "netscribe-2026-10-09.drawio") {
		t.Fatalf("status = %d, disposition = %q", rec.Code, rec.Header().Get("Content-Disposition"))
	}
	var doc struct{}
	if err := xml.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Errorf("not XML: %v", err)
	}
}

func TestExportOfAnEmptyDatabaseIsNotAnError(t *testing.T) {
	e := newEnv(t)
	rec := e.as(e.mustLogin("admin"), http.MethodGet, "/api/export/md", nil)
	if rec.Code != http.StatusOK || !strings.Contains(strings.ToLower(rec.Body.String()), "no devices") {
		t.Errorf("status = %d, body = %s", rec.Code, rec.Body)
	}
}

func TestExportUnknownFormatsAreNotFound(t *testing.T) {
	e := newEnv(t)
	s := e.mustLogin("admin")
	for _, format := range []string{"docx", "txt", "exe", "MD"} {
		if rec := e.as(s, http.MethodGet, "/api/export/"+format, nil); rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", format, rec.Code)
		}
	}
}

func TestExportIsAuditedAndGivesWhatTheCLIWrites(t *testing.T) {
	e := newEnv(t)
	seedContainers(t, e)

	rec := e.as(e.mustLogin("admin"), http.MethodGet, "/api/export/md", nil)
	entries := auditActions(t, e, "export")
	if len(entries) != 1 || entries[0].Result != "ok" || entries[0].Detail != "md" || entries[0].Username != "admin" {
		t.Errorf("audit = %+v", entries)
	}

	want, err := render.Export(context.Background(), e.store, render.FormatMarkdown, e.clk.now(), render.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Body.String() != string(want.Body) {
		t.Error("the API and render.Export (what the CLI calls) differ")
	}
}

func TestExportHTMLCarriesItsOwnPolicy(t *testing.T) {
	e := newEnv(t)
	seedContainers(t, e)

	rec := e.as(e.mustLogin("admin"), http.MethodGet, "/api/export/html", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	h := rec.Header()
	if h.Get("Content-Security-Policy") != "default-src 'none'; style-src 'unsafe-inline'; img-src data:" {
		t.Errorf("Content-Security-Policy = %q", h.Get("Content-Security-Policy"))
	}
	if h.Get("X-Content-Type-Options") != "nosniff" || !strings.HasPrefix(h.Get("Content-Type"), "text/html") {
		t.Errorf("headers = %v", h)
	}
	if h.Get("Content-Disposition") != "attachment; filename=netscribe-2026-10-09.html" {
		t.Errorf("Content-Disposition = %q", h.Get("Content-Disposition"))
	}
	if strings.Contains(rec.Body.String(), "<script") {
		t.Error("the file has a script")
	}
}

func fakePDF(body string, err error) render.Options {
	return render.Options{PDF: func(context.Context, []byte) ([]byte, error) { return []byte(body), err }}
}

func TestExportPDFIsAValidFile(t *testing.T) {
	e := newEnv(t, func(o *envOptions) { o.export = fakePDF("%PDF-1.7 fake", nil) })
	seedContainers(t, e)

	rec := e.as(e.mustLogin("admin"), http.MethodGet, "/api/export/pdf", nil)
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Body.String(), "%PDF") {
		t.Fatalf("status = %d, body %q", rec.Code, rec.Body)
	}
	if rec.Header().Get("Content-Type") != "application/pdf" || rec.Header().Get("Content-Disposition") != "attachment; filename=netscribe-2026-10-09.pdf" {
		t.Errorf("headers = %v", rec.Header())
	}
}

func TestExportPDFWithoutChromeIs503WithAnExplanation(t *testing.T) {
	e := newEnv(t, func(o *envOptions) { o.export = fakePDF("", render.ErrNoChrome) })
	rec := e.as(e.mustLogin("admin"), http.MethodGet, "/api/export/pdf", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Chrome") || !strings.Contains(body, "export.chromePath") || !strings.Contains(body, "print") {
		t.Errorf("the text does not say how to get a PDF: %s", body)
	}
	if got := auditActions(t, e, "export"); len(got) != 1 || got[0].Result != "error" {
		t.Errorf("audit = %+v", got)
	}
}

func TestExportPDFThatTakesTooLongIs503AndOtherFailuresAre500(t *testing.T) {
	e := newEnv(t, func(o *envOptions) { o.export = fakePDF("", context.DeadlineExceeded) })
	rec := e.as(e.mustLogin("admin"), http.MethodGet, "/api/export/pdf", nil)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "too long") {
		t.Errorf("status = %d, body %s", rec.Code, rec.Body)
	}

	e = newEnv(t, func(o *envOptions) { o.export = fakePDF("", errors.New("chrome crashed")) })
	rec = e.as(e.mustLogin("admin"), http.MethodGet, "/api/export/pdf", nil)
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "crashed") {
		t.Errorf("status = %d, body %s: the cause stays in the log", rec.Code, rec.Body)
	}
}
