package api

import (
	"context"
	"encoding/xml"
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
	for _, format := range []string{"html", "pdf", "exe", "MD"} {
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

	want, err := render.Export(context.Background(), e.store, render.FormatMarkdown, e.clk.now())
	if err != nil {
		t.Fatal(err)
	}
	if rec.Body.String() != string(want.Body) {
		t.Error("the API and render.Export (what the CLI calls) differ")
	}
}
