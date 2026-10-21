package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/FlexEbat/Netscribe/internal/collector"
	"github.com/FlexEbat/Netscribe/internal/model"
)

const recentScans = 50

// listScans answers GET /api/scans with {"scans": [...]}, newest first.
func (s *server) listScans(w http.ResponseWriter, r *http.Request) {
	scans, err := s.repo.ListScans(r.Context(), recentScans)
	if err != nil {
		s.internalError(w, "list scans", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string][]model.Scan{"scans": scans})
}

// startScan answers POST /api/scans: 202 with the new scan, 409 while another one runs.
//
// The body is optional. {"nmap": true} runs this scan with nmap switched on, which
// needs a scanner that supports it and nmap to be found.
func (s *server) startScan(w http.ResponseWriter, r *http.Request) {
	var opts struct {
		Nmap bool `json:"nmap"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&opts); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "the request body is not valid")
		return
	}
	start := s.scanner.Start
	if opts.Nmap {
		ns, ok := s.scanner.(NmapScanner)
		if !ok || s.nmap == nil || !s.nmap.NmapState().Available {
			writeError(w, http.StatusBadRequest, "nmap is not available")
			return
		}
		start = ns.StartWithNmap
	}
	scan, err := start()
	if errors.Is(err, collector.ErrBusy) {
		writeError(w, http.StatusConflict, "a scan is already running")
		return
	}
	if errors.Is(err, collector.ErrNoTargets) {
		writeError(w, http.StatusBadRequest, "no scan targets are configured")
		return
	}
	if err != nil {
		s.internalError(w, "start scan", err)
		return
	}
	detail := "scan " + strconv.FormatInt(scan.ID, 10)
	if opts.Nmap {
		detail += " with nmap"
	}
	s.record(r, "scan_start", "ok", detail)
	writeJSON(w, http.StatusAccepted, scan)
}

// getScan answers GET /api/scans/{id}. A malformed id is a missing scan: section 7.2 of
// the spec wants 404 there, not 400.
func (s *server) getScan(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	scan, err := s.repo.GetScan(r.Context(), id)
	if errors.Is(err, model.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		s.internalError(w, "get scan", err)
		return
	}
	writeJSON(w, http.StatusOK, scan)
}

// getNmap answers GET /api/nmap with whether nmap is switched on and can be found.
func (s *server) getNmap(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.nmap.NmapState())
}
