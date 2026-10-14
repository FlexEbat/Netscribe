package collector

import (
	"context"
	"errors"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/model"
)

// stepCollector records the devices it was given and returns a fixed result.
type stepCollector struct {
	name string
	res  Result
	err  error
	seen []model.Device
}

func (c *stepCollector) Name() string { return c.name }

func (c *stepCollector) Collect(_ context.Context, in Input) (Result, error) {
	c.seen = append([]model.Device(nil), in.Devices...)
	return c.res, c.err
}

func enrichScanner(classify bool, steps ...Collector) *Scanner {
	s := &Scanner{log: quietLogger(), enrichers: steps}
	if classify {
		s.classifier, _ = DefaultClassifier()
		s.facts = func(k model.DeviceKey) Facts {
			if k.IP == "10.0.0.9" {
				return Facts{Services: -1, Ports: []int{9100}}
			}
			return Facts{Services: -1}
		}
	}
	return s
}

func TestEnrichersSeeWhatEarlierOnesFound(t *testing.T) {
	found := model.DeviceKey{IP: "10.1.0.5"}
	first := &stepCollector{name: "probe", res: Result{Devices: []model.DeviceInput{{Key: found, Source: "probe"}}}}
	second := &stepCollector{name: "snmp"}
	s := enrichScanner(false, first, second)

	s.enrich(context.Background(), nil, Result{Devices: []model.DeviceInput{
		{Key: model.DeviceKey{MAC: "aa:aa:aa:aa:aa:01", IP: "192.168.1.1"}},
		{Key: model.DeviceKey{MAC: "aa:aa:aa:aa:aa:02"}}, // no address, nothing to ask
	}})

	if len(first.seen) != 1 || first.seen[0].IP != "192.168.1.1" {
		t.Errorf("first enricher saw %+v", first.seen)
	}
	if len(second.seen) != 2 || second.seen[1].IP != "10.1.0.5" {
		t.Errorf("second enricher saw %+v, want the discovered device and the probed one", second.seen)
	}
}

func TestEnrichSurvivesAFailingCollectorAndMergesTheRest(t *testing.T) {
	a := model.DeviceKey{IP: "192.168.1.1"}
	b := model.DeviceKey{IP: "192.168.1.2"}
	broken := &stepCollector{name: "snmp", err: errors.New("boom")}
	good := &stepCollector{name: "lldp", res: Result{
		Links:       []model.LinkInput{{A: a, B: b, Source: "lldp"}},
		LinksPolled: []model.DeviceKey{a},
		FDBPolled:   []model.DeviceKey{b},
		Interfaces:  []model.InterfaceInput{{Device: a, Name: "eth0"}},
	}}
	res := enrichScanner(false, broken, good).enrich(context.Background(), nil, Result{})
	if len(res.Links) != 1 || len(res.LinksPolled) != 1 || len(res.FDBPolled) != 1 || len(res.Interfaces) != 1 {
		t.Errorf("res = %+v", res)
	}
}

func TestEnrichSetsVendorAndKind(t *testing.T) {
	printer := model.DeviceKey{MAC: "00:00:0c:00:00:09", IP: "10.0.0.9"}
	other := model.DeviceKey{MAC: "aa:aa:aa:aa:aa:01", IP: "10.0.0.10"}
	locked := model.DeviceKey{MAC: "aa:aa:aa:aa:aa:02", IP: "10.0.0.11"}
	res := enrichScanner(true).enrich(context.Background(), nil, Result{Devices: []model.DeviceInput{
		{Key: printer}, {Key: other}, {Key: locked, Kind: model.KindAP},
	}})
	if res.Devices[0].Vendor != "Cisco Systems" || res.Devices[0].Kind != model.KindPrinter {
		t.Errorf("printer = %+v", res.Devices[0])
	}
	if res.Devices[1].Kind != "" {
		t.Errorf("nothing matches, kind = %q, want it left for the store to call unknown", res.Devices[1].Kind)
	}
	if res.Devices[2].Kind != model.KindAP {
		t.Errorf("a kind that was already set was changed to %q", res.Devices[2].Kind)
	}
}

func TestEnrichStopsOnCancel(t *testing.T) {
	c := &stepCollector{name: "snmp"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	enrichScanner(false, c).enrich(ctx, nil, Result{})
	if c.seen != nil {
		t.Error("a collector ran after the scan was canceled")
	}
}
