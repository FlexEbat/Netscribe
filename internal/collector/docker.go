package collector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/FlexEbat/Netscribe/internal/config"
	"github.com/FlexEbat/Netscribe/internal/model"
)

const (
	dockerListTimeout = 10 * time.Second
	dockerMaxList     = 16 << 20 // bytes of one container list
	dockerRetry       = 5 * time.Second
	composeLabel      = "com.docker.compose.project"
)

// dockerEvents are the container events that change what the page shows.
var dockerEvents = []string{"create", "start", "stop", "die", "destroy"}

// Reasons shown on the page when the daemon cannot be read.
const (
	reasonNoSocket   = "the Docker socket does not exist"
	reasonNoAccess   = "no permission to use the Docker socket"
	reasonNoAnswer   = "the Docker daemon does not answer on the socket"
	reasonNoLocalIP  = "no local address of this host lies inside the scan targets"
	reasonNotChecked = "the Docker daemon was not asked yet"
)

// dockerContainer is the part of an entry of GET /containers/json the collector reads.
type dockerContainer struct {
	ID     string   `json:"Id"`
	Names  []string `json:"Names"`
	Image  string   `json:"Image"`
	State  string   `json:"State"`
	Labels map[string]string
	Ports  []struct {
		IP          string
		PrivatePort int
		PublicPort  int
		Type        string
	}
	NetworkSettings struct {
		Networks map[string]struct{ IPAddress string }
	}
}

// dockerEvent is one entry of the event stream.
type dockerEvent struct {
	Action string
	Actor  struct{ ID string }
}

// dockerAPI is the read-only part of the Docker Engine API the collector uses. The
// real one talks over the socket, tests use a fake.
type dockerAPI interface {
	// Containers lists every container, in any state.
	Containers(ctx context.Context) ([]dockerContainer, error)
	// Container returns one container, false when the daemon does not know it.
	Container(ctx context.Context, id string) (dockerContainer, bool, error)
	// Events calls fn for every container event since the given time until the stream
	// ends or ctx is canceled.
	Events(ctx context.Context, since time.Time, fn func(dockerEvent)) error
}

// ContainerSink is where the watcher keeps the containers between scans. *store.Store
// implements it.
type ContainerSink interface {
	ReplaceContainers(ctx context.Context, host model.DeviceInput, list []model.ContainerInput) error
	SaveContainer(ctx context.Context, in model.ContainerInput) error
	DeleteContainer(ctx context.Context, id string) error
}

// DockerState tells the page whether the daemon can be read, and if not, why.
type DockerState struct {
	Enabled   bool   `json:"enabled"`
	Available bool   `json:"available"`
	Reason    string `json:"reason"`
}

// Docker reads the containers of the host netscribe runs on. It only lists and listens:
// it never starts, stops or changes a container and never reads logs.
type Docker struct {
	log      *slog.Logger
	newAPI   func(socket string) dockerAPI
	local    func() []netip.Addr
	hostname func() string
	retry    time.Duration

	mu       sync.Mutex
	cfg      config.DockerConfig
	state    DockerState
	reported string // the reason already written to the log

	// managed is set by Reconfigure. From then on the server owns cfg and a scan, which
	// may carry an older config, does not write it back.
	managed bool
	// stopWatch ends the connection Watch holds, wake ends its wait before a retry.
	stopWatch context.CancelFunc
	wake      chan struct{}
}

// NewDocker returns a collector for the daemon behind cfg.Socket.
func NewDocker(cfg config.DockerConfig, log *slog.Logger) *Docker {
	d := &Docker{
		log:      log,
		newAPI:   func(socket string) dockerAPI { return newSocketAPI(socket) },
		local:    localAddrs,
		hostname: func() string { h, _ := os.Hostname(); return h },
		retry:    dockerRetry,
		cfg:      cfg,
		wake:     make(chan struct{}, 1),
	}
	if cfg.Enabled {
		d.state = DockerState{Enabled: true, Reason: reasonNotChecked}
	}
	return d
}

func (d *Docker) Name() string { return "docker" }

// State returns what the last contact with the daemon showed.
func (d *Docker) State() DockerState {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.state
}

func (d *Docker) config() config.DockerConfig {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.cfg
}

func (d *Docker) setConfig(cfg config.DockerConfig) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cfg = cfg
	if !cfg.Enabled {
		d.state = DockerState{}
		d.reported = ""
	} else if !d.state.Enabled {
		d.state = DockerState{Enabled: true, Reason: reasonNotChecked}
	}
}

// Reconfigure applies the docker section of a reloaded config. When the socket or the
// switch changed, Watch drops its connection and connects again with the new settings,
// or stops watching when Docker is off.
func (d *Docker) Reconfigure(cfg config.DockerConfig) {
	d.mu.Lock()
	changed := d.cfg != cfg
	d.managed = true
	stop := d.stopWatch
	d.mu.Unlock()

	d.setConfig(cfg)
	if !changed {
		return
	}
	if stop != nil {
		stop()
	}
	select {
	case d.wake <- struct{}{}:
	default: // a wake-up is already waiting
	}
}

func (d *Docker) isManaged() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.managed
}

// ok records that the daemon answered, so the next failure is reported again.
func (d *Docker) ok() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.state = DockerState{Enabled: true, Available: true}
	d.reported = ""
}

// fail records why the daemon cannot be read. The log gets the reason once, however
// many scans and reconnects run into it.
func (d *Docker) fail(reason string) {
	d.mu.Lock()
	d.state = DockerState{Enabled: true, Reason: reason}
	first := d.reported != reason
	d.reported = reason
	d.mu.Unlock()
	if first {
		d.log.Warn("docker is not available, containers are not updated", "reason", reason)
	}
}

// Collect reads the containers of the local daemon. A daemon that cannot be read costs
// one log line and leaves the stored containers as they are.
func (d *Docker) Collect(ctx context.Context, in Input) (Result, error) {
	cfg := in.Config.Docker
	if !d.isManaged() {
		d.setConfig(cfg)
	}
	if !cfg.Enabled {
		return Result{}, nil
	}
	host, ok := d.host(in.Targets, in.Devices)
	if !ok {
		d.fail(reasonNoLocalIP)
		return Result{}, nil
	}
	list, err := d.newAPI(cfg.Socket).Containers(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return Result{}, ctx.Err()
		}
		d.fail(dockerReason(err))
		return Result{}, nil
	}
	d.ok()
	res := Result{
		Devices:          []model.DeviceInput{host},
		ContainersPolled: []model.DeviceKey{host.Key},
		Containers:       toContainerInputs(host.Key, list),
	}
	return res, nil
}

// host picks the device that holds the containers: the one whose IP is a local address
// of this machine. Addresses that belong to Docker's own bridges are not in the scan
// targets, so they are never taken. A device that is not stored yet is created with the
// source "local".
func (d *Docker) host(targets []netip.Prefix, devices []model.Device) (model.DeviceInput, bool) {
	addrs := d.local()
	sort.Slice(addrs, func(i, j int) bool { return addrs[i].Less(addrs[j]) })
	pick := func(ok func(netip.Addr) bool) (netip.Addr, bool) {
		for _, a := range addrs {
			if a.IsValid() && !a.IsLoopback() && !a.IsLinkLocalUnicast() && !a.IsUnspecified() && ok(a) {
				return a, true
			}
		}
		return netip.Addr{}, false
	}
	known := map[string]model.Device{}
	for _, dev := range devices {
		known[dev.IP] = dev
	}
	if a, ok := pick(func(a netip.Addr) bool { _, found := known[a.String()]; return found }); ok {
		dev := known[a.String()]
		return model.DeviceInput{Key: model.DeviceKey{MAC: dev.MAC, IP: dev.IP}, Hostname: dev.Hostname, Source: "local"}, true
	}
	inTargets := func(a netip.Addr) bool {
		for _, t := range targets {
			if t.Contains(a) {
				return true
			}
		}
		return false
	}
	if a, ok := pick(inTargets); ok {
		return model.DeviceInput{Key: model.DeviceKey{IP: a.String()}, Hostname: cleanHostname(d.hostname()), Source: "local"}, true
	}
	return model.DeviceInput{}, false
}

// localAddrs lists the IPv4 addresses of this machine.
func localAddrs() []netip.Addr {
	list, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var out []netip.Addr
	for _, a := range list {
		p, err := netip.ParsePrefix(a.String())
		if err != nil || !p.Addr().Is4() {
			continue
		}
		out = append(out, p.Addr())
	}
	return out
}

// Watch keeps the stored containers equal to the daemon between scans: it lists them,
// then follows the event stream and re-reads the container each event names. changed is
// called after every change. Watch returns when ctx is canceled, a lost connection is
// retried.
func (d *Docker) Watch(ctx context.Context, sink ContainerSink, targets []netip.Prefix, changed func()) {
	for ctx.Err() == nil {
		cfg := d.config()
		if cfg.Enabled {
			wctx, stop := context.WithCancel(ctx)
			d.mu.Lock()
			d.stopWatch = stop
			d.mu.Unlock()
			// Reconfigure may have run before stop was registered, so look again.
			if d.config() == cfg {
				if err := d.watchOnce(wctx, sink, cfg, targets, changed); err != nil && wctx.Err() == nil {
					d.fail(dockerReason(err))
				}
			}
			stop()
		}
		select {
		case <-ctx.Done():
		case <-d.wake:
		case <-time.After(d.retry):
		}
	}
}

// watchOnce lists the containers, then follows events until the stream ends.
func (d *Docker) watchOnce(ctx context.Context, sink ContainerSink, cfg config.DockerConfig, targets []netip.Prefix, changed func()) error {
	host, ok := d.host(targets, nil)
	if !ok {
		d.fail(reasonNoLocalIP)
		return nil
	}
	api := d.newAPI(cfg.Socket)
	// Events from this moment on are replayed, so a container that changes while the list
	// is read is not missed.
	since := time.Now()
	list, err := api.Containers(ctx)
	if err != nil {
		return err
	}
	d.ok()
	if err := sink.ReplaceContainers(ctx, host, toContainerInputs(host.Key, list)); err != nil {
		d.log.Warn("store containers", "err", err)
	} else {
		changed()
	}
	return api.Events(ctx, since, func(ev dockerEvent) {
		d.handle(ctx, api, sink, host.Key, ev, changed)
	})
}

// handle applies one event: a destroyed container is deleted, every other one is read
// again and stored.
func (d *Docker) handle(ctx context.Context, api dockerAPI, sink ContainerSink, host model.DeviceKey, ev dockerEvent, changed func()) {
	id := ev.Actor.ID
	if id == "" {
		return
	}
	var err error
	if ev.Action == "destroy" {
		err = sink.DeleteContainer(ctx, id)
	} else {
		var c dockerContainer
		var found bool
		c, found, err = api.Container(ctx, id)
		switch {
		case err != nil:
		case found:
			err = sink.SaveContainer(ctx, toContainerInput(host, c))
		default:
			err = sink.DeleteContainer(ctx, id)
		}
	}
	if err != nil {
		if ctx.Err() == nil {
			d.log.Warn("apply docker event", "action", ev.Action, "err", err)
		}
		return
	}
	changed()
}

func toContainerInputs(host model.DeviceKey, list []dockerContainer) []model.ContainerInput {
	out := make([]model.ContainerInput, 0, len(list))
	for _, c := range list {
		out = append(out, toContainerInput(host, c))
	}
	return out
}

// toContainerInput maps one daemon entry. Ports and networks are sorted so the same
// container always looks the same.
func toContainerInput(host model.DeviceKey, c dockerContainer) model.ContainerInput {
	in := model.ContainerInput{
		Device: host, ID: c.ID, Image: c.Image, State: c.State, ComposeProject: c.Labels[composeLabel],
		Ports: []model.PortMapping{}, Networks: []model.ContainerNetwork{},
	}
	if len(c.Names) > 0 {
		in.Name = strings.TrimPrefix(c.Names[0], "/")
	}
	for _, p := range c.Ports {
		in.Ports = append(in.Ports, model.PortMapping{
			HostIP: p.IP, HostPort: p.PublicPort, ContainerPort: p.PrivatePort, Proto: p.Type,
		})
	}
	sort.Slice(in.Ports, func(i, j int) bool {
		a, b := in.Ports[i], in.Ports[j]
		if a.ContainerPort != b.ContainerPort {
			return a.ContainerPort < b.ContainerPort
		}
		if a.Proto != b.Proto {
			return a.Proto < b.Proto
		}
		if a.HostPort != b.HostPort {
			return a.HostPort < b.HostPort
		}
		return a.HostIP < b.HostIP
	})
	for name, n := range c.NetworkSettings.Networks {
		in.Networks = append(in.Networks, model.ContainerNetwork{Name: name, IP: n.IPAddress})
	}
	sort.Slice(in.Networks, func(i, j int) bool { return in.Networks[i].Name < in.Networks[j].Name })
	return in
}

// dockerReason turns an error of the socket into a sentence for the page.
func dockerReason(err error) string {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return reasonNoSocket
	case errors.Is(err, fs.ErrPermission):
		return reasonNoAccess
	case errors.Is(err, syscall.ECONNREFUSED):
		return reasonNoAnswer
	}
	return "the Docker daemon could not be read: " + cleanLine(err.Error())
}

// socketAPI talks to the daemon over a unix socket. Every request is a GET, the daemon
// is never asked to change anything.
type socketAPI struct {
	client *http.Client
}

func newSocketAPI(socket string) *socketAPI {
	tr := &http.Transport{
		// A client lives for one scan and is never closed, so an idle connection would
		// stay open until the process ends.
		DisableKeepAlives: true,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
	}
	return &socketAPI{client: &http.Client{Transport: tr}}
}

// get sends a GET request. The caller closes the body.
func (a *socketAPI) get(ctx context.Context, path string, query url.Values) (*http.Response, error) {
	u := url.URL{Scheme: "http", Host: "docker", Path: path, RawQuery: query.Encode()}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("docker answered %d", resp.StatusCode)
	}
	return resp, nil
}

func (a *socketAPI) list(ctx context.Context, filters string) ([]dockerContainer, error) {
	ctx, cancel := context.WithTimeout(ctx, dockerListTimeout)
	defer cancel()
	q := url.Values{"all": {"1"}}
	if filters != "" {
		q.Set("filters", filters)
	}
	resp, err := a.get(ctx, "/containers/json", q)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	var out []dockerContainer
	if err := json.NewDecoder(io.LimitReader(resp.Body, dockerMaxList)).Decode(&out); err != nil {
		return nil, fmt.Errorf("read container list: %w", err)
	}
	return out, nil
}

func (a *socketAPI) Containers(ctx context.Context) ([]dockerContainer, error) {
	return a.list(ctx, "")
}

func (a *socketAPI) Container(ctx context.Context, id string) (dockerContainer, bool, error) {
	filters, err := json.Marshal(map[string][]string{"id": {id}})
	if err != nil {
		return dockerContainer{}, false, err
	}
	list, err := a.list(ctx, string(filters))
	if err != nil {
		return dockerContainer{}, false, err
	}
	for _, c := range list {
		if c.ID == id {
			return c, true, nil
		}
	}
	return dockerContainer{}, false, nil
}

func (a *socketAPI) Events(ctx context.Context, since time.Time, fn func(dockerEvent)) error {
	filters, err := json.Marshal(map[string][]string{"type": {"container"}, "event": dockerEvents})
	if err != nil {
		return err
	}
	resp, err := a.get(ctx, "/events", url.Values{
		"filters": {string(filters)},
		"since":   {strconv.FormatInt(since.Unix(), 10)},
	})
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	dec := json.NewDecoder(resp.Body)
	for {
		var ev dockerEvent
		if err := dec.Decode(&ev); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if errors.Is(err, io.EOF) {
				return errors.New("the event stream ended")
			}
			return fmt.Errorf("read event: %w", err)
		}
		fn(ev)
	}
}
