package api

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/FlexEbat/Netscribe/internal/collector"
	"github.com/FlexEbat/Netscribe/internal/model"
)

// seedLargeNetwork stores n devices in a tree, four to a parent, with six services each, as
// one finished scan: the shape of slice 43.
func seedLargeNetwork(t *testing.T, e *env, n int) {
	t.Helper()
	ctx := context.Background()
	key := func(i int) model.DeviceKey {
		return model.DeviceKey{MAC: fmt.Sprintf("02:00:00:00:%02x:%02x", i>>8&0xff, i&0xff), IP: fmt.Sprintf("10.0.%d.%d", i/250, i%250+1)}
	}
	var res collector.Result
	for i := 1; i <= n; i++ {
		res.Devices = append(res.Devices, model.DeviceInput{Key: key(i), Hostname: fmt.Sprintf("dev-%04d", i), Kind: model.KindHost, Source: "arp"})
		for p := 0; p < 6; p++ {
			res.Services = append(res.Services, model.ServiceInput{Device: key(i), Port: 1000 + p, Proto: "tcp", Name: "svc"})
		}
		if i > 1 {
			res.Links = append(res.Links, model.LinkInput{A: key((i-2)/4 + 1), B: key(i), APort: fmt.Sprintf("p%d", i), BPort: "up", Source: "lldp"})
		}
	}
	scan, err := e.store.StartScan(ctx, "10.0.0.0/16")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.store.ApplyResult(ctx, scan.ID, res); err != nil {
		t.Fatal(err)
	}
	if err := e.store.FinishScan(ctx, scan.ID, "done", ""); err != nil {
		t.Fatal(err)
	}
}

// The budgets are wide multiples of what a laptop needs: they catch a read that has become
// quadratic or that sends far more than the page can use, they do not time the server.
func TestReadsOfFiveHundredDevicesAnswerInTimeAndInSize(t *testing.T) {
	e := newEnv(t)
	seedLargeNetwork(t, e, 500)
	s := e.mustLogin("admin")
	paths := map[string]int{
		"/api/devices":              4 << 20,
		"/api/devices?q=dev-04":     1 << 20,
		"/api/topology":             4 << 20,
		"/api/overview":             256 << 10,
		"/api/devices/1/history":    256 << 10,
		"/api/services":             4 << 20,
		"/api/changes":              4 << 20,
		"/api/scans":                1 << 20,
		"/api/devices?trusted=true": 1 << 20,
	}
	for path, limit := range paths {
		start := time.Now()
		rec := e.as(s, http.MethodGet, path, nil)
		took := time.Since(start)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d %.200s", path, rec.Code, rec.Body)
			continue
		}
		if took > 10*time.Second {
			t.Errorf("GET %s took %v, budget 10s", path, took)
		}
		if rec.Body.Len() > limit {
			t.Errorf("GET %s answered %d bytes, budget %d", path, rec.Body.Len(), limit)
		}
	}
}
