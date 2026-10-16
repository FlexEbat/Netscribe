package api

import (
	"errors"
	"mime"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/FlexEbat/Netscribe/internal/render"
)

// export answers GET /api/export/{format} with the file `netscribe export` writes. A
// download is an event worth remembering, so it goes to the audit log.
func (s *server) export(w http.ResponseWriter, r *http.Request) {
	format := chi.URLParam(r, "format")
	f, err := render.Export(r.Context(), s.repo, format, s.now())
	if errors.Is(err, render.ErrUnknownFormat) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		s.record(r, "export", "error", format)
		s.internalError(w, "export", err)
		return
	}
	s.record(r, "export", "ok", format)
	h := w.Header()
	h.Set("Content-Type", f.ContentType)
	h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": f.Name}))
	h.Set("Cache-Control", "no-store")
	_, _ = w.Write(f.Body)
}
