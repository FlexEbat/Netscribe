package collector

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/FlexEbat/Netscribe/internal/config"
	"github.com/FlexEbat/Netscribe/internal/model"
)

// dockerList is a real answer of GET /containers/json, cut to what matters.
const dockerList = `[
 {"Id":"4f1e","Names":["/web"],"Image":"nginx:1.27","State":"running",
  "Labels":{"com.docker.compose.project":"shop"},
  "Ports":[{"IP":"::","PrivatePort":80,"PublicPort":8080,"Type":"tcp"},
           {"IP":"0.0.0.0","PrivatePort":80,"PublicPort":8080,"Type":"tcp"},
           {"PrivatePort":443,"Type":"tcp"}],
  "NetworkSettings":{"Networks":{"shop_default":{"IPAddress":"172.18.0.3"},"bridge":{"IPAddress":"172.17.0.2"}}}},
 {"Id":"9a2c","Names":["/db"],"Image":"postgres:16","State":"exited","Labels":{},"Ports":[],
  "NetworkSettings":{"Networks":{}}}
]`

type fakeDockerAPI struct {
	mu      sync.Mutex
	list    []dockerContainer
	listErr error
	byID    map[string]dockerContainer
	events  chan dockerEvent
	asked   int
}

func (f *fakeDockerAPI) Containers(context.Context) ([]dockerContainer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked++
	return f.list, f.listErr
}

func (f *fakeDockerAPI) Container(_ context.Context, id string) (dockerContainer, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.byID[id]
	return c, ok, nil
}

func (f *fakeDockerAPI) Events(ctx context.Context, _ time.Time, fn func(dockerEvent)) error {
	for {
		select {
		case ev, ok := <-f.events:
			if !ok {
				return errors.New("the event stream ended")
			}
			fn(ev)
		case <-ctx.Done():
			return nil
		}
	}
}

func (f *fakeDockerAPI) set(id string, c dockerContainer) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.byID == nil {
		f.byID = map[string]dockerContainer{}
	}
	f.byID[id] = c
}

func event(action, id string) dockerEvent {
	ev := dockerEvent{Action: action}
	ev.Actor.ID = id
	return ev
}

func container(id, name, state string) dockerContainer {
	return dockerContainer{ID: id, Names: []string{"/" + name}, Image: "img:" + name, State: state}
}

func testDocker(api dockerAPI, log *slog.Logger) *Docker {
	d := NewDocker(config.DockerConfig{Enabled: true, Socket: "/run/docker.sock"}, log)
	d.newAPI = func(string) dockerAPI { return api }
	d.local = func() []netip.Addr {
		return []netip.Addr{netip.MustParseAddr("172.17.0.1"), netip.MustParseAddr("192.168.1.5"), netip.MustParseAddr("127.0.0.1")}
	}
	d.hostname = func() string { return "dockerhost" }
	d.retry = 10 * time.Millisecond
	return d
}

func dockerInput(devices ...model.Device) Input {
	cfg := testConfig()
	cfg.Docker = config.DockerConfig{Enabled: true, Socket: "/run/docker.sock"}
	return Input{Targets: []netip.Prefix{netip.MustParsePrefix("192.168.1.0/24")}, Devices: devices, Config: cfg}
}

type fakeSink struct {
	mu       sync.Mutex
	hosts    []model.DeviceInput
	replaced [][]model.ContainerInput
	saved    map[string]model.ContainerInput
	deleted  []string
}

func (s *fakeSink) ReplaceContainers(_ context.Context, host model.DeviceInput, list []model.ContainerInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hosts = append(s.hosts, host)
	s.replaced = append(s.replaced, list)
	return nil
}

func (s *fakeSink) SaveContainer(_ context.Context, in model.ContainerInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.saved == nil {
		s.saved = map[string]model.ContainerInput{}
	}
	s.saved[in.ID] = in
	return nil
}

func (s *fakeSink) DeleteContainer(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleted = append(s.deleted, id)
	return nil
}

func TestDockerCollectMapsContainersPortsAndNetworks(t *testing.T) {
	api := &fakeDockerAPI{list: []dockerContainer{
		{
			ID: "4f1e", Names: []string{"/web"}, Image: "nginx:1.27", State: "running",
			Labels: map[string]string{composeLabel: "shop"},
		},
		container("9a2c", "db", "exited"),
	}}
	api.list[0].Ports = []struct {
		IP          string
		PrivatePort int
		PublicPort  int
		Type        string
	}{{IP: "::", PrivatePort: 80, PublicPort: 8080, Type: "tcp"}, {IP: "0.0.0.0", PrivatePort: 80, PublicPort: 8080, Type: "tcp"}, {PrivatePort: 443, Type: "tcp"}}
	api.list[0].NetworkSettings.Networks = map[string]struct{ IPAddress string }{
		"shop_default": {IPAddress: "172.18.0.3"}, "bridge": {IPAddress: "172.17.0.2"},
	}
	d := testDocker(api, quietLogger())

	res, err := d.Collect(context.Background(), dockerInput())
	if err != nil {
		t.Fatal(err)
	}
	host := model.DeviceKey{IP: "192.168.1.5"}
	if len(res.Devices) != 1 || res.Devices[0].Key != host || res.Devices[0].Source != "local" || res.Devices[0].Hostname != "dockerhost" {
		t.Errorf("devices = %+v, want the local host created", res.Devices)
	}
	if len(res.ContainersPolled) != 1 || res.ContainersPolled[0] != host {
		t.Errorf("polled = %+v", res.ContainersPolled)
	}
	if len(res.Containers) != 2 {
		t.Fatalf("containers = %+v", res.Containers)
	}
	web := res.Containers[0]
	if web.Device != host || web.ID != "4f1e" || web.Name != "web" || web.Image != "nginx:1.27" || web.State != "running" || web.ComposeProject != "shop" {
		t.Errorf("web = %+v", web)
	}
	wantPorts := "[{0.0.0.0 8080 80 tcp} {:: 8080 80 tcp} { 0 443 tcp}]"
	if got := fmt.Sprint(web.Ports); got != wantPorts {
		t.Errorf("ports = %s, want %s", got, wantPorts)
	}
	if got := fmt.Sprint(web.Networks); got != "[{bridge 172.17.0.2} {shop_default 172.18.0.3}]" {
		t.Errorf("networks = %s", got)
	}
	if db := res.Containers[1]; db.State != "exited" || db.Ports == nil || db.Networks == nil {
		t.Errorf("db = %+v, an empty container still has empty lists", db)
	}
	if st := d.State(); !st.Enabled || !st.Available || st.Reason != "" {
		t.Errorf("state = %+v", st)
	}
}

func TestDockerHostIsADeviceWithALocalAddress(t *testing.T) {
	d := testDocker(&fakeDockerAPI{}, quietLogger())
	known := model.Device{ID: 7, MAC: "aa:aa:aa:aa:aa:05", IP: "192.168.1.5", Hostname: "nas"}

	res, err := d.Collect(context.Background(), dockerInput(known))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Devices) != 1 || res.Devices[0].Key != (model.DeviceKey{MAC: known.MAC, IP: known.IP}) {
		t.Errorf("devices = %+v, want the device that is already known", res.Devices)
	}
}

func TestDockerHostIsNeverADockerBridgeAddress(t *testing.T) {
	d := testDocker(&fakeDockerAPI{}, quietLogger())
	d.local = func() []netip.Addr { return []netip.Addr{netip.MustParseAddr("172.17.0.1")} }

	res, err := d.Collect(context.Background(), dockerInput())
	if err != nil || len(res.Containers) != 0 || len(res.ContainersPolled) != 0 {
		t.Errorf("res = %+v, err = %v, want nothing without a local address in the targets", res, err)
	}
	if st := d.State(); st.Available || st.Reason != reasonNoLocalIP {
		t.Errorf("state = %+v", st)
	}
}

func TestDockerDisabledDoesNothing(t *testing.T) {
	api := &fakeDockerAPI{list: []dockerContainer{container("a", "web", "running")}}
	d := testDocker(api, quietLogger())
	in := dockerInput()
	in.Config.Docker.Enabled = false

	res, err := d.Collect(context.Background(), in)
	if err != nil || len(res.Devices) != 0 || len(res.Containers) != 0 || len(res.ContainersPolled) != 0 {
		t.Errorf("res = %+v, err = %v", res, err)
	}
	if api.asked != 0 {
		t.Errorf("the daemon was asked %d times", api.asked)
	}
	if st := d.State(); st.Enabled || st.Available {
		t.Errorf("state = %+v, want disabled", st)
	}
}

func TestDockerMissingSocketKeepsTheServiceRunningAndLogsOnce(t *testing.T) {
	var logs bytes.Buffer
	d := testDocker(nil, slog.New(slog.NewTextHandler(&logs, nil)))
	d.newAPI = func(socket string) dockerAPI { return newSocketAPI(socket) }
	in := dockerInput()
	in.Config.Docker.Socket = filepath.Join(t.TempDir(), "absent.sock")

	for i := 0; i < 3; i++ {
		res, err := d.Collect(context.Background(), in)
		if err != nil {
			t.Fatalf("scan %d: a missing socket failed the scan: %v", i, err)
		}
		if len(res.Containers) != 0 || len(res.ContainersPolled) != 0 {
			t.Errorf("scan %d: res = %+v, want nothing so stored containers stay", i, res)
		}
	}
	if st := d.State(); !st.Enabled || st.Available || st.Reason != reasonNoSocket {
		t.Errorf("state = %+v", st)
	}
	if n := strings.Count(logs.String(), "docker is not available"); n != 1 {
		t.Errorf("the error was logged %d times, want once:\n%s", n, logs.String())
	}
}

func TestDockerReasons(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{fmt.Errorf("dial: %w", fs.ErrNotExist), reasonNoSocket},
		{fmt.Errorf("dial: %w", fs.ErrPermission), reasonNoAccess},
		{&net.OpError{Op: "dial", Err: fs.ErrPermission}, reasonNoAccess},
		{errors.New("docker answered 500\nsecond line"), "the Docker daemon could not be read: docker answered 500second line"},
	} {
		if got := dockerReason(tc.err); got != tc.want {
			t.Errorf("dockerReason(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

func TestDockerFailureIsLoggedAgainAfterARecovery(t *testing.T) {
	var logs bytes.Buffer
	api := &fakeDockerAPI{listErr: fs.ErrPermission}
	d := testDocker(api, slog.New(slog.NewTextHandler(&logs, nil)))
	collect := func() {
		if _, err := d.Collect(context.Background(), dockerInput()); err != nil {
			t.Fatal(err)
		}
	}
	collect()
	collect()
	api.mu.Lock()
	api.listErr = nil
	api.mu.Unlock()
	collect()
	api.mu.Lock()
	api.listErr = fs.ErrPermission
	api.mu.Unlock()
	collect()
	if n := strings.Count(logs.String(), "docker is not available"); n != 2 {
		t.Errorf("logged %d times, want twice: once per outage:\n%s", n, logs.String())
	}
}

func TestDockerEventHandlerAppliesEachAction(t *testing.T) {
	api := &fakeDockerAPI{}
	api.set("a", container("a", "web", "running"))
	sink := &fakeSink{}
	d := testDocker(api, quietLogger())
	host := model.DeviceKey{IP: "192.168.1.5"}
	changed := 0
	handle := func(ev dockerEvent) { d.handle(context.Background(), api, sink, host, ev, func() { changed++ }) }

	handle(event("start", "a"))
	if got := sink.saved["a"]; got.State != "running" || got.Device != host || got.Name != "web" {
		t.Errorf("saved = %+v", sink.saved)
	}
	handle(event("die", "gone")) // the daemon no longer knows it
	handle(event("destroy", "a"))
	handle(event("start", ""))
	if fmt.Sprint(sink.deleted) != "[gone a]" {
		t.Errorf("deleted = %v", sink.deleted)
	}
	if changed != 3 {
		t.Errorf("changed called %d times, want 3 (an event without an id changes nothing)", changed)
	}
}

func waitDocker(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestDockerWatchFollowsEventsWithoutAScan(t *testing.T) {
	api := &fakeDockerAPI{list: []dockerContainer{container("a", "web", "running")}, events: make(chan dockerEvent, 8)}
	sink := &fakeSink{}
	d := testDocker(api, quietLogger())
	var mu sync.Mutex
	changed := 0
	count := func() int { mu.Lock(); defer mu.Unlock(); return changed }

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		d.Watch(ctx, sink, []netip.Prefix{netip.MustParsePrefix("192.168.1.0/24")}, func() { mu.Lock(); changed++; mu.Unlock() })
		close(done)
	}()

	waitDocker(t, "the first list", func() bool { return count() == 1 })
	sink.mu.Lock()
	if len(sink.replaced) != 1 || len(sink.replaced[0]) != 1 || sink.hosts[0].Source != "local" || sink.hosts[0].Key.IP != "192.168.1.5" {
		t.Errorf("first list = %+v, hosts = %+v", sink.replaced, sink.hosts)
	}
	sink.mu.Unlock()

	api.set("b", container("b", "cache", "running"))
	api.events <- event("start", "b")
	waitDocker(t, "the start event", func() bool { return count() == 2 })
	sink.mu.Lock()
	if sink.saved["b"].State != "running" {
		t.Errorf("saved = %+v", sink.saved)
	}
	sink.mu.Unlock()

	api.events <- event("destroy", "b")
	waitDocker(t, "the destroy event", func() bool { return count() == 3 })
	sink.mu.Lock()
	if fmt.Sprint(sink.deleted) != "[b]" {
		t.Errorf("deleted = %v", sink.deleted)
	}
	sink.mu.Unlock()

	if !d.State().Available {
		t.Errorf("state = %+v", d.State())
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Watch did not return after cancel")
	}
}

func TestDockerWatchReconnectsAndListsAgain(t *testing.T) {
	api := &fakeDockerAPI{list: []dockerContainer{container("a", "web", "running")}, events: make(chan dockerEvent)}
	sink := &fakeSink{}
	d := testDocker(api, quietLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Watch(ctx, sink, []netip.Prefix{netip.MustParsePrefix("192.168.1.0/24")}, func() {})

	waitDocker(t, "the first list", func() bool { sink.mu.Lock(); defer sink.mu.Unlock(); return len(sink.replaced) == 1 })
	// The daemon restarts: the stream ends, the watcher lists again.
	close(api.events)
	waitDocker(t, "the second list", func() bool { sink.mu.Lock(); defer sink.mu.Unlock(); return len(sink.replaced) >= 2 })
}

// socketServer serves fixtures on a unix socket the way the daemon does and refuses
// everything that is not a GET.
func socketServer(t *testing.T) (string, *[]string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "d.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Skipf("no unix sockets here: %v", err)
	}
	var mu sync.Mutex
	var seen []string
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()
		if strings.Contains(r.URL.Query().Get("filters"), "4f1e") {
			_, _ = w.Write([]byte(`[` + dockerList[2:strings.Index(dockerList, "},\n {")+1] + `]`))
			return
		}
		_, _ = w.Write([]byte(dockerList))
	})
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()
		_, _ = w.Write([]byte(`{"Type":"container","Action":"start","Actor":{"ID":"4f1e"}}` + "\n" +
			`{"Type":"container","Action":"destroy","Actor":{"ID":"9a2c"}}` + "\n"))
	})
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("the collector sent %s %s, it must only read", r.Method, r.URL.Path)
			http.Error(w, "read only", http.StatusMethodNotAllowed)
			return
		}
		mux.ServeHTTP(w, r)
	}), ReadHeaderTimeout: time.Second}
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(func() { _ = srv.Close() })
	return path, &seen
}

func TestSocketAPIReadsTheDaemonWithGetRequestsOnly(t *testing.T) {
	path, seen := socketServer(t)
	api := newSocketAPI(path)
	ctx := context.Background()

	list, err := api.Containers(ctx)
	if err != nil || len(list) != 2 || list[0].ID != "4f1e" || list[0].Labels[composeLabel] != "shop" {
		t.Fatalf("list = %+v, err = %v", list, err)
	}
	if len(list[0].Ports) != 3 || len(list[0].NetworkSettings.Networks) != 2 {
		t.Errorf("web = %+v", list[0])
	}
	one, found, err := api.Container(ctx, "4f1e")
	if err != nil || !found || one.Names[0] != "/web" {
		t.Errorf("container = %+v, %v, %v", one, found, err)
	}
	if _, found, err := api.Container(ctx, "zzzz"); err != nil || found {
		t.Errorf("unknown container: found = %v, err = %v", found, err)
	}

	var got []dockerEvent
	err = api.Events(ctx, time.Unix(100, 0), func(ev dockerEvent) { got = append(got, ev) })
	if err == nil || !strings.Contains(err.Error(), "ended") {
		t.Errorf("err = %v, want the stream-ended error", err)
	}
	if len(got) != 2 || got[0].Action != "start" || got[0].Actor.ID != "4f1e" || got[1].Action != "destroy" {
		t.Errorf("events = %+v", got)
	}

	all, err := url.QueryUnescape(strings.Join(*seen, "\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"GET /containers/json?all=1", `"id":["4f1e"]`, "GET /events?", "since=100", "destroy"} {
		if !strings.Contains(all, want) {
			t.Errorf("requests lack %q:\n%s", want, all)
		}
	}
}
