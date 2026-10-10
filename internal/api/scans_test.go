package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/model"
)

func TestGetScan(t *testing.T) {
	e := newEnv(t)
	scan := seedScan(t, e, device("aa:aa:aa:aa:aa:02", "192.168.1.2", "gw", model.KindRouter))
	s := e.mustLogin("admin")

	rec := e.as(s, http.MethodGet, "/api/scans/"+strconv.FormatInt(scan.ID, 10), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	var got model.Scan
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != scan.ID || got.Status != "done" || got.DeviceCount != 1 || got.Target != "192.168.1.0/24" {
		t.Errorf("scan = %+v", got)
	}

	for name, tc := range map[string]struct {
		id   string
		code int
	}{
		"unknown scan": {"9999", http.StatusNotFound},
		"not a number": {"abc", http.StatusNotFound},
		"zero":         {"0", http.StatusNotFound},
		"negative":     {"-4", http.StatusNotFound},
		"overflow":     {"99999999999999999999", http.StatusNotFound},
	} {
		t.Run(name, func(t *testing.T) {
			if rec := e.as(s, http.MethodGet, "/api/scans/"+tc.id, nil); rec.Code != tc.code {
				t.Errorf("GET /api/scans/%s = %d, want %d", tc.id, rec.Code, tc.code)
			}
		})
	}
}
