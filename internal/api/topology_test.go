package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/model"
)

func getTopology(t *testing.T, e *env, s session) model.Topology {
	t.Helper()
	rec := e.as(s, http.MethodGet, "/api/topology", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/topology: %d %s", rec.Code, rec.Body)
	}
	var out model.Topology
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestTopologyOfAnEmptyDatabaseHasEmptyLists(t *testing.T) {
	e := newEnv(t)
	rec := e.as(e.mustLogin("admin"), http.MethodGet, "/api/topology", nil)
	want := "{\"devices\":[],\"links\":[],\"containers\":[]}\n"
	if rec.Code != http.StatusOK || rec.Body.String() != want {
		t.Errorf("body = %q, want %q", rec.Body, want)
	}
}

func TestTopologyGivesEveryDeviceAPositionAndKeepsSavedOnes(t *testing.T) {
	e := newEnv(t)
	seedScan(t, e,
		device("aa:aa:aa:aa:aa:01", "192.168.1.1", "gw", model.KindRouter),
		device("aa:aa:aa:aa:aa:02", "192.168.1.2", "sw", model.KindSwitch),
	)
	s := e.mustLogin("admin")
	before := getTopology(t, e, s)
	if len(before.Devices) != 2 {
		t.Fatalf("devices = %d, want 2", len(before.Devices))
	}
	for _, d := range before.Devices {
		if d.X == nil || d.Y == nil {
			t.Fatalf("device %d has no position", d.ID)
		}
	}

	moved := before.Devices[1]
	rec := e.as(s, http.MethodPut, "/api/devices/"+strconv.FormatInt(moved.ID, 10)+"/position", map[string]float64{"x": 12.5, "y": -40})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("PUT position: %d %s", rec.Code, rec.Body)
	}
	after := getTopology(t, e, s)
	for _, d := range after.Devices {
		if d.ID == moved.ID && (*d.X != 12.5 || *d.Y != -40) {
			t.Errorf("saved position came back as (%v,%v)", *d.X, *d.Y)
		}
		if d.ID != moved.ID {
			for _, b := range before.Devices {
				if b.ID == d.ID && (*b.X != *d.X || *b.Y != *d.Y) {
					t.Errorf("device %d moved without being touched", d.ID)
				}
			}
		}
	}
}

func TestSetPositionRejectsBadInput(t *testing.T) {
	e := newEnv(t)
	seedScan(t, e, device("aa:aa:aa:aa:aa:01", "192.168.1.1", "gw", model.KindRouter))
	s := e.mustLogin("admin")
	id := strconv.FormatInt(getTopology(t, e, s).Devices[0].ID, 10)

	for name, body := range map[string]string{
		"string":        `{"x":"1","y":2}`,
		"missing y":     `{"x":1}`,
		"null":          `{"x":null,"y":null}`,
		"too large":     `{"x":100001,"y":0}`,
		"too small":     `{"x":0,"y":-100001}`,
		"overflow":      `{"x":1e999,"y":0}`,
		"unknown field": `{"x":1,"y":2,"z":3}`,
		"not json":      `x`,
		"empty body":    ``,
	} {
		t.Run(name, func(t *testing.T) {
			if rec := e.as(s, http.MethodPut, "/api/devices/"+id+"/position", body); rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400 (%s)", rec.Code, rec.Body)
			}
		})
	}
	if rec := e.as(s, http.MethodPut, "/api/devices/"+id+"/position", `{"x":100000,"y":-100000}`); rec.Code != http.StatusNoContent {
		t.Errorf("the limits themselves: %d, want 204", rec.Code)
	}
}

func TestSetPositionUnknownDevice(t *testing.T) {
	e := newEnv(t)
	s := e.mustLogin("admin")
	for _, id := range []string{"999", "abc", "0", "-3"} {
		if rec := e.as(s, http.MethodPut, "/api/devices/"+id+"/position", `{"x":1,"y":2}`); rec.Code != http.StatusNotFound {
			t.Errorf("PUT /api/devices/%s/position = %d, want 404", id, rec.Code)
		}
	}
}

func TestOnlyWritersMayMoveDevices(t *testing.T) {
	e := newEnv(t)
	seedScan(t, e, device("aa:aa:aa:aa:aa:01", "192.168.1.1", "gw", model.KindRouter))
	e.addUser("vera", model.RoleViewer)
	e.addUser("olga", model.RoleOperator)
	id := strconv.FormatInt(getTopology(t, e, e.mustLogin("admin")).Devices[0].ID, 10)

	if rec := e.as(e.mustLogin("vera"), http.MethodPut, "/api/devices/"+id+"/position", `{"x":1,"y":2}`); rec.Code != http.StatusForbidden {
		t.Errorf("viewer: %d, want 403", rec.Code)
	}
	if rec := e.as(e.mustLogin("olga"), http.MethodPut, "/api/devices/"+id+"/position", `{"x":1,"y":2}`); rec.Code != http.StatusNoContent {
		t.Errorf("operator: %d, want 204", rec.Code)
	}
	if rec := e.as(e.mustLogin("vera"), http.MethodGet, "/api/topology", nil); rec.Code != http.StatusOK {
		t.Errorf("viewer reading the topology: %d, want 200", rec.Code)
	}
}
