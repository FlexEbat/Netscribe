package api

import (
	"context"
	"errors"
	"mime"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/FlexEbat/Netscribe/internal/render"
)

const (
	// pdfSendTime is what a finished PDF may take to reach the client.
	pdfSendTime = 15 * time.Second

	noChromeText = "PDF export needs Chrome or Chromium on the server. Install it or set export.chromePath, " +
		"or download the HTML export and print it to PDF from your browser."
)

// export answers GET /api/export/{format} with the file `netscribe export` writes. A
// download is an event worth remembering, so it goes to the audit log.
func (s *server) export(w http.ResponseWriter, r *http.Request) {
	format := chi.URLParam(r, "format")
	if format == render.FormatPDF {
		// A PDF outlives the server's write timeout. It gets its own deadline: the render
		// limit plus room to send the file.
		rc := http.NewResponseController(w)
		if err := rc.SetWriteDeadline(time.Now().Add(render.PDFTimeout + pdfSendTime)); err != nil && !errors.Is(err, http.ErrNotSupported) {
			s.internalError(w, "extend write deadline", err)
			return
		}
	}
	f, err := render.Export(r.Context(), s.repo, format, s.now(), s.exportOpts)
	switch {
	case errors.Is(err, render.ErrUnknownFormat):
		writeError(w, http.StatusNotFound, "not found")
		return
	case errors.Is(err, render.ErrNoChrome):
		s.record(r, "export", "error", format)
		writeError(w, http.StatusServiceUnavailable, noChromeText)
		return
	case errors.Is(err, context.DeadlineExceeded):
		s.record(r, "export", "error", format)
		writeError(w, http.StatusServiceUnavailable, "the PDF took too long to render, try again later")
		return
	case err != nil:
		s.record(r, "export", "error", format)
		s.internalError(w, "export", err)
		return
	}
	s.record(r, "export", "ok", format)
	h := w.Header()
	h.Set("Content-Type", f.ContentType)
	h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": f.Name}))
	h.Set("Cache-Control", "no-store")
	if format == render.FormatHTML {
		// The file carries its own policy: it needs styles and nothing else.
		h.Set("Content-Security-Policy", render.HTMLCSP)
		h.Set("X-Content-Type-Options", "nosniff")
	}
	_, _ = w.Write(f.Body)
}
