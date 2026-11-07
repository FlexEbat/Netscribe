package render

import (
	"fmt"
	"testing"
	"time"

	"github.com/FlexEbat/Netscribe/internal/model"
)

// largeData is n devices in a tree, four to a parent, with six services each. The numbers are
// the ones of slice 43: 500 devices, a link for each but the first, 3000 services.
func largeData(n int) Data {
	var devs []model.Device
	for i := 1; i <= n; i++ {
		kind := model.KindHost
		switch {
		case i == 1:
			kind = model.KindRouter
		case i%20 == 0:
			kind = model.KindSwitch
		case i%10 == 5:
			kind = model.KindServer
		}
		devs = append(devs, model.Device{
			ID: int64(i), IP: fmt.Sprintf("10.0.%d.%d", i/250, i%250+1), MAC: fmt.Sprintf("02:00:00:00:%02x:%02x", i>>8&0xff, i&0xff),
			Hostname: fmt.Sprintf("dev-%04d", i), Kind: kind, Online: i%7 != 0,
		})
	}
	d := Data{Devices: Layout(devs)}
	for i := 2; i <= n; i++ {
		d.Links = append(d.Links, model.Link{ID: int64(i), ADeviceID: int64((i-2)/4 + 1), APort: fmt.Sprintf("p%d", i), BDeviceID: int64(i), BPort: "up", Source: "lldp"})
	}
	for j := 0; j < n*6; j++ {
		d.Services = append(d.Services, model.Service{ID: int64(j + 1), DeviceID: int64(j%n + 1), Port: 1000 + j/n, Proto: "tcp", Name: "svc"})
	}
	return d
}

// within runs f and fails when it takes longer than the budget. The budgets are a wide
// multiple of what a laptop needs, so a slow runner does not fail them: they are there to
// catch a change that makes a step quadratic, not to time it.
func within(t *testing.T, name string, budget time.Duration, f func()) {
	t.Helper()
	start := time.Now()
	f()
	if took := time.Since(start); took > budget {
		t.Errorf("%s took %v, budget %v", name, took, budget)
	}
}

func TestLayoutOfFiveHundredAndTwoThousandDevices(t *testing.T) {
	for _, n := range []int{500, 2000} {
		var devs []model.Device
		for i := 1; i <= n; i++ {
			devs = append(devs, model.Device{ID: int64(i), IP: fmt.Sprintf("10.0.%d.%d", i/250, i%250+1), Kind: model.KindHost})
		}
		var out []model.Device
		within(t, fmt.Sprintf("layout of %d", n), 10*time.Second, func() { out = Layout(devs) })
		if len(out) != n {
			t.Fatalf("layout of %d returned %d devices", n, len(out))
		}
		at := map[[2]float64]int64{}
		for _, d := range out {
			if d.X == nil || d.Y == nil {
				t.Fatalf("device %d has no position", d.ID)
			}
			p := [2]float64{*d.X, *d.Y}
			if other, ok := at[p]; ok {
				t.Fatalf("devices %d and %d are at the same place %v", other, d.ID, p)
			}
			at[p] = d.ID
		}
	}
}

func TestExportsOfFiveHundredDevicesFinishAndStayInSize(t *testing.T) {
	d := largeData(500)
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	const limit = 32 << 20
	outputs := map[string]func() int{
		"svg":      func() int { return len(Diagram(d)) },
		"markdown": func() int { return len(Markdown(d, now)) },
		"drawio":   func() int { return len(Drawio(d)) },
		"html": func() int {
			b, err := HTML(d, now)
			if err != nil {
				t.Errorf("html: %v", err)
			}
			return len(b)
		},
	}
	for name, run := range outputs {
		var size int
		within(t, name, 30*time.Second, func() { size = run() })
		if size == 0 || size > limit {
			t.Errorf("%s is %d bytes, want 1 to %d", name, size, limit)
		}
	}
}
