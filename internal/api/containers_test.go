package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/collector"
	"github.com/FlexEbat/Netscribe/internal/model"
)

type fakeDocker struct{ state collector.DockerState }

func (f fakeDocker) State() collector.DockerState { return f.state }

func seedContainers(t *testing.T, e *env) model.DeviceKey {
	t.Helper()
	host := model.DeviceInput{Key: model.DeviceKey{IP: "192.168.1.5"}, Hostname: "docker-host", Source: "local"}
	err := e.store.ReplaceContainers(context.Background(), host, []model.ContainerInput{
		{
			Device: host.Key, ID: "id-web", Name: "web", Image: "nginx:1.27", State: "running", ComposeProject: "shop",
			Ports:    []model.PortMapping{{HostIP: "0.0.0.0", HostPort: 8080, ContainerPort: 80, Proto: "tcp"}},
			Networks: []model.ContainerNetwork{{Name: "shop_default", IP: "172.18.0.3"}},
		},
		{Device: host.Key, ID: "id-db", Name: "db", Image: "postgres:16", State: "exited"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return host.Key
}

type containersBody struct {
	Containers []model.Container     `json:"containers"`
	Docker     collector.DockerState `json:"docker"`
}

func getContainers(t *testing.T, e *env) (int, containersBody) {
	t.Helper()
	rec := e.as(e.mustLogin("admin"), http.MethodGet, "/api/containers", nil)
	var body containersBody
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
	}
	return rec.Code, body
}

func TestContainersNeedASession(t *testing.T) {
	e := newEnv(t)
	if rec := e.do(http.MethodGet, "/api/containers", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestListContainersWithTheirHost(t *testing.T) {
	e := newEnv(t)
	seedContainers(t, e)

	code, body := getContainers(t, e)
	if code != http.StatusOK || len(body.Containers) != 2 {
		t.Fatalf("status %d, containers %+v", code, body.Containers)
	}
	db, web := body.Containers[0], body.Containers[1] // ordered by name within the host
	if db.Name != "db" || db.State != "exited" || len(db.Ports) != 0 || len(db.Networks) != 0 {
		t.Errorf("db = %+v", db)
	}
	if web.Name != "web" || web.Image != "nginx:1.27" || web.State != "running" || web.ComposeProject != "shop" || web.DeviceID == 0 {
		t.Errorf("web = %+v", web)
	}
	if len(web.Ports) != 1 || web.Ports[0].HostPort != 8080 || web.Ports[0].ContainerPort != 80 || web.Ports[0].Proto != "tcp" {
		t.Errorf("ports = %+v", web.Ports)
	}
	if len(web.Networks) != 1 || web.Networks[0].Name != "shop_default" || web.Networks[0].IP != "172.18.0.3" {
		t.Errorf("networks = %+v", web.Networks)
	}
	if !body.Docker.Available {
		t.Errorf("docker = %+v, a daemon nobody reports on counts as available", body.Docker)
	}
}

func TestListContainersIsAnEmptyArrayWithoutData(t *testing.T) {
	e := newEnv(t)
	rec := e.as(e.mustLogin("admin"), http.MethodGet, "/api/containers", nil)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw["containers"]) != "[]" {
		t.Errorf("containers = %s, want []", raw["containers"])
	}
}

func TestContainersReportAnUnavailableDaemon(t *testing.T) {
	state := collector.DockerState{Enabled: true, Reason: "the Docker socket does not exist"}
	e := newEnv(t, func(o *envOptions) { o.docker = fakeDocker{state} })
	seedContainers(t, e)

	code, body := getContainers(t, e)
	if code != http.StatusOK {
		t.Fatalf("status = %d, the page must still load", code)
	}
	if body.Docker != state {
		t.Errorf("docker = %+v, want %+v", body.Docker, state)
	}
}

func TestTopologyCarriesTheContainers(t *testing.T) {
	e := newEnv(t)
	seedContainers(t, e)

	rec := e.as(e.mustLogin("admin"), http.MethodGet, "/api/topology", nil)
	var topo model.Topology
	if err := json.Unmarshal(rec.Body.Bytes(), &topo); err != nil {
		t.Fatal(err)
	}
	if len(topo.Containers) != 2 || len(topo.Devices) != 1 || topo.Containers[0].DeviceID != topo.Devices[0].ID {
		t.Errorf("topology = %+v", topo)
	}
}
