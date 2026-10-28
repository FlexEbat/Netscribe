package collector

import (
	"context"
	"log/slog"
	"net"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/FlexEbat/Netscribe/internal/model"
)

// The defaults of scan.portTimeout and scan.concurrency, used when a config leaves them out.
const (
	servicesTimeout     = 500 * time.Millisecond // per port
	servicesConcurrency = 256                    // connections at one moment, for the whole scan
)

// Services is a TCP connect scan of the configured ports on every live device. It does
// not read banners, guess versions or send anything after the handshake.
type Services struct {
	dial dialer
	log  *slog.Logger

	mu    sync.Mutex
	ports map[string][]int // ip -> open ports of the last Collect
}

// NewServices returns a scanner that opens plain TCP connections.
func NewServices(log *slog.Logger) *Services {
	return &Services{
		dial:  (&net.Dialer{}).DialContext, // the context of every connection carries scan.portTimeout
		log:   log,
		ports: map[string][]int{},
	}
}

func (c *Services) Name() string { return "services" }

// openPorts returns the open ports found on ip.
func (c *Services) openPorts(ip string) []int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ports[ip]
}

// Collect scans scan.ports. An empty list switches the stage off.
func (c *Services) Collect(ctx context.Context, in Input) (Result, error) {
	ports := in.Config.Scan.Ports
	if len(ports) == 0 {
		return Result{}, nil
	}

	timeout, limit := in.Config.Scan.PortTimeout.Std(), in.Config.Scan.Concurrency
	if timeout <= 0 {
		timeout = servicesTimeout
	}
	if limit <= 0 {
		limit = servicesConcurrency
	}
	var (
		wg  sync.WaitGroup
		mu  sync.Mutex
		res Result
		sem = make(chan struct{}, limit)
	)
	open := map[string][]int{}
loop:
	for _, d := range in.Devices {
		if d.IP == "" {
			continue
		}
		res.ServicesPolled = append(res.ServicesPolled, model.DeviceKey{MAC: d.MAC, IP: d.IP})
		for _, port := range ports {
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				break loop
			}
			wg.Add(1)
			go func(d model.Device, port int) {
				defer wg.Done()
				defer func() { <-sem }()
				cctx, cancel := context.WithTimeout(ctx, timeout)
				conn, err := c.dial(cctx, "tcp", net.JoinHostPort(d.IP, strconv.Itoa(port)))
				cancel()
				if err != nil {
					return // closed, filtered or unreachable: not a service
				}
				_ = conn.Close() // the handshake is all that is wanted
				mu.Lock()
				res.Services = append(res.Services, model.ServiceInput{
					Device: model.DeviceKey{MAC: d.MAC, IP: d.IP}, Port: port, Proto: "tcp", Name: portNameOf(port),
				})
				open[d.IP] = append(open[d.IP], port)
				mu.Unlock()
			}(d, port)
		}
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	sort.Slice(res.Services, func(i, j int) bool {
		a, b := res.Services[i], res.Services[j]
		if a.Device.IP != b.Device.IP {
			return a.Device.IP < b.Device.IP
		}
		return a.Port < b.Port
	})
	for _, p := range open {
		sort.Ints(p)
	}
	c.mu.Lock()
	c.ports = open
	c.mu.Unlock()
	return res, nil
}
