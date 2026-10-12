package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/FlexEbat/Netscribe/internal/model"
	"github.com/FlexEbat/Netscribe/internal/store"
)

const (
	defaultAuditLimit = 50
	maxAuditLimit     = 500
)

// auditPeriodLayouts are the accepted forms of the from and to parameters.
var auditPeriodLayouts = []string{time.RFC3339, "2006-01-02"}

func parsePeriod(s string) (time.Time, bool) {
	for _, layout := range auditPeriodLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

// listAudit answers GET /api/audit?limit=&before=&user=&action=&result=&from=&to= with
// {"entries": [...]}, newest first. before is the id of the oldest entry already seen.
// The log can be read here and nowhere changed.
func (s *server) listAudit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.AuditFilter{Limit: defaultAuditLimit, Username: q.Get("user"), Action: q.Get("action"), Result: q.Get("result")}

	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxAuditLimit {
			writeError(w, http.StatusBadRequest, "limit is out of range")
			return
		}
		f.Limit = n
	}
	if v := q.Get("before"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "before is not valid")
			return
		}
		f.BeforeID = n
	}
	switch f.Result {
	case "", "ok", "denied", "error":
	default:
		writeError(w, http.StatusBadRequest, "result is unknown")
		return
	}
	if len(f.Username) > maxUsernameBytes || len(f.Action) > 64 {
		writeError(w, http.StatusBadRequest, "filter is too long")
		return
	}
	for name, dst := range map[string]*time.Time{"from": &f.From, "to": &f.To} {
		if v := q.Get(name); v != "" {
			t, ok := parsePeriod(v)
			if !ok {
				writeError(w, http.StatusBadRequest, name+" is not a date")
				return
			}
			*dst = t
		}
	}

	entries, err := s.auditLog.ListAudit(r.Context(), f)
	if err != nil {
		s.internalError(w, "list audit", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string][]model.AuditEntry{"entries": entries})
}
