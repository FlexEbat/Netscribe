package api

import (
	"errors"
	"fmt"
	"mime"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/FlexEbat/Netscribe/internal/model"
	"github.com/FlexEbat/Netscribe/internal/store"
)

const (
	maxBulkIDs      = 200
	maxIDsInAudit   = 20
	maxImportErrors = 50
)

type bulkBody struct {
	IDs     []int64 `json:"ids"`
	Trusted *bool   `json:"trusted"`
	Kind    *string `json:"kind"`
	GroupID *int64  `json:"groupId"`
}

// bulkEdit answers POST /api/devices/bulk with {"updated": n}. It changes every listed
// device or none.
func (s *server) bulkEdit(w http.ResponseWriter, r *http.Request) {
	var body bulkBody
	if !decode(r, &body) {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(body.IDs) < 1 || len(body.IDs) > maxBulkIDs {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("ids must hold 1 to %d devices", maxBulkIDs))
		return
	}
	seen := map[int64]bool{}
	for _, id := range body.IDs {
		if id < 1 || seen[id] {
			writeError(w, http.StatusBadRequest, "ids must be distinct positive numbers")
			return
		}
		seen[id] = true
	}
	if body.Trusted == nil && body.Kind == nil && body.GroupID == nil {
		writeError(w, http.StatusBadRequest, "nothing to change: send trusted, kind or groupId")
		return
	}
	change := store.BulkChange{Trusted: body.Trusted, GroupID: body.GroupID}
	var fields []string
	if body.Trusted != nil {
		fields = append(fields, "trusted")
	}
	if body.Kind != nil {
		k := model.DeviceKind(*body.Kind)
		if k == "" || !validKind(k) {
			writeError(w, http.StatusBadRequest, errKindUnknown.Error())
			return
		}
		change.Kind = &k
		fields = append(fields, "kind")
	}
	if body.GroupID != nil {
		if *body.GroupID < 0 {
			writeError(w, http.StatusBadRequest, "groupId must be 0 or a group id")
			return
		}
		fields = append(fields, "groupId")
	}
	p, _ := principal(r)
	n, err := s.repo.BulkEdit(r.Context(), body.IDs, change, p.User.ID)
	switch {
	case errors.Is(err, model.ErrNotFound):
		writeError(w, http.StatusNotFound, "a listed device does not exist, nothing was changed")
		return
	case errors.Is(err, store.ErrNoSuchGroup):
		writeError(w, http.StatusBadRequest, "the group does not exist")
		return
	case errors.Is(err, model.ErrVersionConflict):
		writeError(w, http.StatusConflict, "a device was changed by someone else, try again")
		return
	case err != nil:
		s.internalError(w, "bulk edit", err)
		return
	}
	detail := fmt.Sprintf("%d devices (%s)", n, strings.Join(fields, ", "))
	if n <= maxIDsInAudit {
		ids := slices.Clone(body.IDs)
		slices.Sort(ids)
		parts := make([]string, len(ids))
		for i, id := range ids {
			parts[i] = strconv.FormatInt(id, 10)
		}
		detail += " ids " + strings.Join(parts, ",")
	}
	s.record(r, "device_bulk_edit", "ok", detail)
	s.changed()
	writeJSON(w, http.StatusOK, map[string]int{"updated": n})
}

type importProblem struct {
	Line  int    `json:"line"`
	Error string `json:"error"`
}

// importDevices answers POST /api/devices/import. The body is a CSV file. A good file
// creates its devices (201 {"created": n}), a dry run only says what would happen (200
// {"wouldCreate": n}). A bad file creates nothing and lists the problem lines.
func (s *server) importDevices(w http.ResponseWriter, r *http.Request) {
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "text/csv" {
		writeError(w, http.StatusBadRequest, "send the file with Content-Type: text/csv")
		return
	}
	dryRun := r.URL.Query().Get("dryRun") == "true"
	rows, err := model.ParseDeviceCSV(r.Body)
	var tooBig *http.MaxBytesError
	switch {
	case errors.As(err, &tooBig):
		writeError(w, http.StatusRequestEntityTooLarge, "the file is larger than 1 MiB")
		return
	case errors.Is(err, model.ErrCSVTooMany):
		writeError(w, http.StatusRequestEntityTooLarge, err.Error())
		return
	case err != nil:
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(rows) == 0 {
		writeError(w, http.StatusBadRequest, "the file has no rows")
		return
	}

	groups, err := s.repo.ListGroups(r.Context())
	if err != nil {
		s.internalError(w, "import devices", err)
		return
	}
	byName := map[string]int64{}
	for _, g := range groups {
		byName[strings.ToLower(g.Name)] = g.ID
	}

	var problems []importProblem
	items := make([]store.ImportItem, 0, len(rows))
	firstLine := map[string]int{} // a MAC or an address -> the line that had it first
	for _, row := range rows {
		in, err := (deviceBody{MAC: row.MAC, IP: row.IP, Hostname: row.Hostname, Label: row.Label, Notes: row.Notes, Kind: row.Kind}).manualDevice()
		var group *int64
		if err == nil && row.Group != "" {
			id, ok := byName[strings.ToLower(row.Group)]
			if !ok {
				err = fmt.Errorf("the group %q does not exist", row.Group)
			}
			group = &id
		}
		if err == nil {
			for _, key := range []string{"mac:" + in.MAC, "ip:" + in.IP} {
				if key == "mac:" || key == "ip:" {
					continue
				}
				if first, dup := firstLine[key]; dup {
					err = fmt.Errorf("the same %s as line %d", key[:strings.Index(key, ":")], first)
					break
				}
			}
		}
		if err != nil {
			problems = append(problems, importProblem{Line: row.Line, Error: err.Error()})
			continue
		}
		for _, key := range []string{"mac:" + in.MAC, "ip:" + in.IP} {
			if key != "mac:" && key != "ip:" {
				firstLine[key] = row.Line
			}
		}
		items = append(items, store.ImportItem{Line: row.Line, Device: in, GroupID: group})
	}
	if len(problems) > 0 {
		total := len(problems)
		if total > maxImportErrors {
			problems = problems[:maxImportErrors]
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": fmt.Sprintf("%d rows have problems", total), "rows": problems})
		return
	}

	p, _ := principal(r)
	n, err := s.repo.ImportDevices(r.Context(), items, dryRun, p.User.ID)
	var conflict *store.ImportConflict
	switch {
	case errors.As(err, &conflict):
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": conflict.Error(),
			"rows":  []importProblem{{Line: conflict.Line, Error: "a device with this MAC or address already exists"}},
		})
		return
	case err != nil:
		s.internalError(w, "import devices", err)
		return
	}
	if dryRun {
		writeJSON(w, http.StatusOK, map[string]int{"wouldCreate": n})
		return
	}
	s.record(r, "device_import", "ok", fmt.Sprintf("%d devices", n))
	s.changed()
	writeJSON(w, http.StatusCreated, map[string]int{"created": n})
}
