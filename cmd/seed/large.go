package main

import (
	"context"
	"fmt"

	"github.com/FlexEbat/Netscribe/internal/collector"
	"github.com/FlexEbat/Netscribe/internal/model"
	"github.com/FlexEbat/Netscribe/internal/store"
)

// largeSpec is the size of a generated network. There is no random source in it: the same
// spec always gives the same devices, links and services, so a measurement can be repeated.
type largeSpec struct {
	Devices  int
	Links    int
	Services int
	Scans    int
}

// largePorts are the ports the generated services use, one more of them for every pass over
// the devices, so a device never gets the same port twice.
var largePorts = []struct {
	port int
	name string
}{
	{22, "ssh"}, {80, "http"}, {443, "https"}, {3306, "mysql"}, {5432, "postgresql"},
	{8080, "http-alt"}, {53, "domain"}, {445, "microsoft-ds"}, {6379, "redis"}, {9100, "jetdirect"},
}

func largeKey(n int) model.DeviceKey {
	return model.DeviceKey{
		MAC: fmt.Sprintf("02:%02x:%02x:%02x:%02x:%02x", n>>32&0xff, n>>24&0xff, n>>16&0xff, n>>8&0xff, n&0xff),
		IP:  fmt.Sprintf("10.%d.%d.%d", n/62500, n/250%250, n%250+1),
	}
}

// largeKind makes the first device a router, one in twenty a switch, one in ten a server,
// and the rest hosts.
func largeKind(n int) model.DeviceKind {
	switch {
	case n == 1:
		return model.KindRouter
	case n%20 == 0:
		return model.KindSwitch
	case n%10 == 5:
		return model.KindServer
	default:
		return model.KindHost
	}
}

func largeNetwork(s largeSpec) collector.Result {
	var res collector.Result
	for n := 1; n <= s.Devices; n++ {
		res.Devices = append(res.Devices, model.DeviceInput{
			Key: largeKey(n), Hostname: fmt.Sprintf("dev-%04d", n), Kind: largeKind(n), Source: "arp",
		})
	}

	ports := make([]int, s.Devices+1)
	pairs := map[[2]int]bool{}
	addLink := func(a, b int) bool {
		if a == b || a < 1 || b < 1 || a > s.Devices || b > s.Devices {
			return false
		}
		if a > b {
			a, b = b, a
		}
		if pairs[[2]int{a, b}] || len(res.Links) >= s.Links {
			return false
		}
		pairs[[2]int{a, b}] = true
		ports[a]++
		ports[b]++
		res.Links = append(res.Links, model.LinkInput{
			A: largeKey(a), B: largeKey(b),
			APort: fmt.Sprintf("Gi%d", ports[a]), BPort: fmt.Sprintf("Gi%d", ports[b]), Source: "lldp",
		})
		return true
	}
	// a tree first, four children to a parent, so every device is reachable ...
	for n := 2; n <= s.Devices; n++ {
		addLink((n-2)/4+1, n)
	}
	// ... then cross links, by a fixed step, until the wanted count is reached
	for n := 1; len(res.Links) < s.Links && n <= s.Devices*4; n++ {
		addLink(n%s.Devices+1, (n*31+7)%s.Devices+1)
	}

	for j := 0; j < s.Services && s.Devices > 0; j++ {
		p := largePorts[j/s.Devices%len(largePorts)]
		res.Services = append(res.Services, model.ServiceInput{
			Device: largeKey(j%s.Devices + 1), Port: p.port, Proto: "tcp", Name: p.name,
		})
	}
	return res
}

// seedLarge stores a generated network as Scans scans in a row. Every odd scan lacks the
// first service, so the history holds changes and not only repeats.
func seedLarge(ctx context.Context, st store.Repo, s largeSpec) (int, error) {
	full := largeNetwork(s)
	for i := 0; i < max(s.Scans, 1); i++ {
		res := full
		if i%2 == 1 && len(full.Services) > 0 {
			res.Services = full.Services[1:]
		}
		scan, err := st.StartScan(ctx, "10.0.0.0/8")
		if err != nil {
			return 0, err
		}
		if err := st.ApplyResult(ctx, scan.ID, res); err != nil {
			_ = st.FinishScan(ctx, scan.ID, "failed", err.Error())
			return 0, err
		}
		if err := st.FinishScan(ctx, scan.ID, "done", ""); err != nil {
			return 0, err
		}
	}
	return len(full.Devices), nil
}
