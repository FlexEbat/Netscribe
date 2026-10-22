package collector

import (
	"context"
	"net/netip"
	"sync"
	"testing"
)

// fixedTargets returns a targets function that always answers with the given CIDRs.
func fixedTargets(cidrs ...string) func() []netip.Prefix {
	list := make([]netip.Prefix, 0, len(cidrs))
	for _, c := range cidrs {
		list = append(list, netip.MustParsePrefix(c))
	}
	return func() []netip.Prefix { return list }
}

func TestDockerWatchFollowsAChangeOfTheScanTargets(t *testing.T) {
	api := &fakeDockerAPI{list: []dockerContainer{container("a", "web", "running")}, events: make(chan dockerEvent)}
	sink := &fakeSink{}
	d := testDocker(api, quietLogger())

	var mu sync.Mutex
	current := []netip.Prefix{netip.MustParsePrefix("10.9.0.0/16")} // no local address lies in it
	targets := func() []netip.Prefix { mu.Lock(); defer mu.Unlock(); return current }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Watch(ctx, sink, targets, func() {})

	waitDocker(t, "the missing local address to be reported", func() bool { return d.State().Reason == reasonNoLocalIP })

	mu.Lock()
	current = []netip.Prefix{netip.MustParsePrefix("192.168.1.0/24")}
	mu.Unlock()
	d.Retarget()

	waitDocker(t, "the containers to be listed after the change", func() bool {
		sink.mu.Lock()
		defer sink.mu.Unlock()
		return len(sink.replaced) == 1
	})
}
