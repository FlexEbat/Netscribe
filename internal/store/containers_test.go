package store

import (
	"context"
	"errors"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/collector"
	"github.com/FlexEbat/Netscribe/internal/model"
)

var dockerHost = model.DeviceInput{Key: model.DeviceKey{IP: "192.168.1.5"}, Hostname: "docker-host", Source: "local"}

func ctr(id, name, state string) model.ContainerInput {
	return model.ContainerInput{Device: dockerHost.Key, ID: id, Name: name, Image: "img:" + name, State: state}
}

func listContainers(t *testing.T, s *Store) []model.Container {
	t.Helper()
	out, err := s.ListContainers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func names(cs []model.Container) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.Name)
	}
	return out
}

func TestReplaceContainersCreatesTheHostAndStoresEveryState(t *testing.T) {
	s := newTestStore(t)
	web := ctr("id-web", "web", "running")
	web.ComposeProject = "shop"
	web.Ports = []model.PortMapping{{HostIP: "0.0.0.0", HostPort: 8080, ContainerPort: 80, Proto: "tcp"}, {ContainerPort: 443, Proto: "tcp"}}
	web.Networks = []model.ContainerNetwork{{Name: "shop_default", IP: "172.18.0.3"}}

	if err := s.ReplaceContainers(context.Background(), dockerHost, []model.ContainerInput{web, ctr("id-db", "db", "exited")}); err != nil {
		t.Fatal(err)
	}
	devs := list(t, s, DeviceFilter{})
	if len(devs) != 1 || devs[0].IP != "192.168.1.5" || devs[0].Source != "local" || devs[0].Hostname != "docker-host" {
		t.Fatalf("devices = %+v, want the host created with source local", devs)
	}
	got := listContainers(t, s)
	if len(got) != 2 || got[0].Name != "db" || got[1].Name != "web" {
		t.Fatalf("containers = %v", names(got))
	}
	w := got[1]
	if w.DeviceID != devs[0].ID || w.State != "running" || w.ComposeProject != "shop" || w.Image != "img:web" {
		t.Errorf("web = %+v", w)
	}
	if len(w.Ports) != 2 || w.Ports[0].HostPort != 8080 || w.Ports[1].HostPort != 0 || w.Ports[1].ContainerPort != 443 {
		t.Errorf("ports = %+v", w.Ports)
	}
	if len(w.Networks) != 1 || w.Networks[0].IP != "172.18.0.3" {
		t.Errorf("networks = %+v", w.Networks)
	}
	if got[0].Ports == nil || got[0].Networks == nil {
		t.Error("empty ports and networks must be empty lists, not null")
	}
}

func TestReplaceContainersRemovesTheOnesTheDaemonNoLongerLists(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.ReplaceContainers(ctx, dockerHost, []model.ContainerInput{ctr("a", "web", "running"), ctr("b", "db", "running")}); err != nil {
		t.Fatal(err)
	}
	first := listContainers(t, s)[1].FirstSeenAt // web

	if err := s.ReplaceContainers(ctx, dockerHost, []model.ContainerInput{ctr("a", "web", "exited")}); err != nil {
		t.Fatal(err)
	}
	got := listContainers(t, s)
	if len(got) != 1 || got[0].ID != "a" || got[0].State != "exited" {
		t.Fatalf("containers = %+v, want only web, exited", got)
	}
	if !got[0].FirstSeenAt.Equal(first) || !got[0].LastSeenAt.After(first) {
		t.Errorf("first seen %v, last seen %v: first must stay, last must move", got[0].FirstSeenAt, got[0].LastSeenAt)
	}

	if err := s.ReplaceContainers(ctx, dockerHost, nil); err != nil {
		t.Fatal(err)
	}
	if got := listContainers(t, s); len(got) != 0 {
		t.Errorf("containers = %+v, want none", got)
	}
}

func TestSaveContainerNeedsAKnownHost(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.SaveContainer(ctx, ctr("a", "web", "running")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if err := s.ReplaceContainers(ctx, dockerHost, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveContainer(ctx, ctr("a", "web", "created")); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveContainer(ctx, ctr("a", "web", "running")); err != nil {
		t.Fatal(err)
	}
	got := listContainers(t, s)
	if len(got) != 1 || got[0].State != "running" {
		t.Errorf("containers = %+v, want one that is running", got)
	}
}

func TestDeleteContainerIgnoresAnUnknownID(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.ReplaceContainers(ctx, dockerHost, []model.ContainerInput{ctr("a", "web", "running")}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteContainer(ctx, "nope"); err != nil {
		t.Errorf("unknown id: %v", err)
	}
	if err := s.DeleteContainer(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if got := listContainers(t, s); len(got) != 0 {
		t.Errorf("containers = %+v, want none", got)
	}
}

func TestContainersGoWithTheirDevice(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.ReplaceContainers(ctx, dockerHost, []model.ContainerInput{ctr("a", "web", "running")}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteDevice(ctx, list(t, s, DeviceFilter{})[0].ID); err != nil {
		t.Fatal(err)
	}
	if got := listContainers(t, s); len(got) != 0 {
		t.Errorf("containers = %+v, want none after the host was deleted", got)
	}
}

func TestScanStoresContainersAndCountsThem(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	run := func(res collector.Result) model.Scan {
		sc, err := s.StartScan(ctx, "192.168.1.0/24")
		if err != nil {
			t.Fatal(err)
		}
		if err := s.ApplyResult(ctx, sc.ID, res); err != nil {
			t.Fatalf("ApplyResult: %v", err)
		}
		if err := s.FinishScan(ctx, sc.ID, "done", ""); err != nil {
			t.Fatal(err)
		}
		got, err := s.GetScan(ctx, sc.ID)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	sc := run(collector.Result{
		Devices:          []model.DeviceInput{dockerHost},
		Containers:       []model.ContainerInput{ctr("a", "web", "running"), ctr("b", "db", "running")},
		ContainersPolled: []model.DeviceKey{dockerHost.Key},
	})
	if sc.ContainerCount != 2 {
		t.Errorf("container count = %d, want 2", sc.ContainerCount)
	}

	// The daemon could not be read: nothing is polled, the stored containers stay.
	sc = run(collector.Result{Devices: []model.DeviceInput{dockerHost}})
	if sc.ContainerCount != 2 || len(listContainers(t, s)) != 2 {
		t.Errorf("count = %d, containers = %d, want both untouched", sc.ContainerCount, len(listContainers(t, s)))
	}

	// One container is gone from the daemon.
	sc = run(collector.Result{
		Devices:          []model.DeviceInput{dockerHost},
		Containers:       []model.ContainerInput{ctr("a", "web", "running")},
		ContainersPolled: []model.DeviceKey{dockerHost.Key},
	})
	if got := listContainers(t, s); sc.ContainerCount != 1 || len(got) != 1 || got[0].ID != "a" {
		t.Errorf("count = %d, containers = %v, want only web", sc.ContainerCount, names(got))
	}
}

func TestContainerTextFromTheDaemonIsCleaned(t *testing.T) {
	s := newTestStore(t)
	bad := ctr("a", "we\x1b[31mb\n", "RUNNING")
	bad.Image = "img\x00:1"
	if err := s.ReplaceContainers(context.Background(), dockerHost, []model.ContainerInput{bad}); err != nil {
		t.Fatal(err)
	}
	got := listContainers(t, s)[0]
	if got.Name != "we[31mb" || got.Image != "img:1" || got.State != "running" {
		t.Errorf("container = %+v", got)
	}
}

func TestContainerWithoutIDIsRefused(t *testing.T) {
	s := newTestStore(t)
	if err := s.ReplaceContainers(context.Background(), dockerHost, []model.ContainerInput{ctr(" ", "web", "running")}); err == nil {
		t.Error("a container without an id was stored")
	}
	if devs := list(t, s, DeviceFilter{}); len(devs) != 0 {
		t.Errorf("devices = %+v, the failed replace must change nothing", devs)
	}
}

func TestTopologyHoldsContainers(t *testing.T) {
	s := newTestStore(t)
	empty, err := s.Topology(context.Background())
	if err != nil || empty.Containers == nil || len(empty.Containers) != 0 {
		t.Fatalf("empty topology = %+v, %v", empty, err)
	}
	if err := s.ReplaceContainers(context.Background(), dockerHost, []model.ContainerInput{ctr("a", "web", "running")}); err != nil {
		t.Fatal(err)
	}
	topo, err := s.Topology(context.Background())
	if err != nil || len(topo.Containers) != 1 || len(topo.Devices) != 1 {
		t.Fatalf("topology = %+v, %v", topo, err)
	}
}
