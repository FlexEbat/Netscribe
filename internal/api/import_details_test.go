package api

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
)

func TestImportReadsTheDetailColumns(t *testing.T) {
	e := newEnv(t)
	s := e.mustLogin("admin")

	body := "label,ip,owner,location,serial,warranty,tags\n" +
		"cam1,192.168.9.5,Anna,Rack 2,SN-1,2030-05-31,Camera ; Lobby;;\n" +
		"cam2,192.168.9.6,,,,,\n"
	code, out := postCSV(e, s, "/api/devices/import", body)
	if code != http.StatusCreated || string(out["created"]) != "2" {
		t.Fatalf("%d %v", code, out)
	}
	for _, d := range listDevices(t, e, s, "") {
		switch d.Label {
		case "cam1":
			if d.Owner != "Anna" || d.Location != "Rack 2" || d.Serial != "SN-1" || d.WarrantyUntil != "2030-05-31" {
				t.Errorf("cam1 = %+v", d)
			}
			if !reflect.DeepEqual(d.Tags, []string{"camera", "lobby"}) {
				t.Errorf("cam1 tags = %v, want [camera lobby]", d.Tags)
			}
		case "cam2":
			if d.Owner != "" || len(d.Tags) != 0 {
				t.Errorf("cam2 = %+v, empty cells must stay empty", d)
			}
		}
	}
}

func TestImportJudgesTheDetailColumnsLikeTheForm(t *testing.T) {
	e := newEnv(t)
	s := e.mustLogin("admin")

	body := "label,ip,warranty,tags\n" +
		"good,192.168.9.5,2030-05-31,a\n" +
		"badwarranty,192.168.9.6,tomorrow,\n" +
		"badtag,192.168.9.7,,UPPER!\n"
	code, out := postCSV(e, s, "/api/devices/import", body)
	if code != http.StatusBadRequest {
		t.Fatalf("%d %v, want 400", code, out)
	}
	var rows []importProblem
	if err := json.Unmarshal(out["rows"], &rows); err != nil || len(rows) != 2 || rows[0].Line != 3 || rows[1].Line != 4 {
		t.Errorf("rows = %+v, %v, want lines 3 and 4", rows, err)
	}
	if got := listDevices(t, e, s, ""); len(got) != 0 {
		t.Errorf("%d devices were created by a bad file", len(got))
	}
}
