// Package collector gathers facts about the network and hands them to the scanner.
// It never imports store: a collector returns a Result and the scanner writes it.
package collector

import (
	"context"
	"fmt"
	"net/netip"

	"github.com/FlexEbat/Netscribe/internal/config"
	"github.com/FlexEbat/Netscribe/internal/model"
)

// Input is what every collector receives.
type Input struct {
	Targets []netip.Prefix
	Devices []model.Device // live devices after the discovery stage
	Config  config.Config
}

// Result is what a collector found.
type Result struct {
	Devices    []model.DeviceInput
	Interfaces []model.InterfaceInput
	Links      []model.LinkInput
	// LinksPolled lists the devices whose neighbor table was read completely. A link of
	// source lldp that touches one of them and was not seen again is removed.
	LinksPolled []model.DeviceKey
	// FDBPolled is the same for links of source fdb: switches whose forwarding table
	// was read.
	FDBPolled []model.DeviceKey
	Services  []model.ServiceInput
	// ServicesPolled lists the devices whose ports were scanned. A service of such a
	// device that did not answer again is removed.
	ServicesPolled []model.DeviceKey
	Containers     []model.ContainerInput
	// ContainersPolled lists the hosts whose Docker daemon was read completely. A
	// container of such a host that the daemon no longer lists is removed.
	ContainersPolled []model.DeviceKey
}

// Collector is one source of facts. Collect must return when ctx is canceled.
type Collector interface {
	Name() string
	Collect(ctx context.Context, in Input) (Result, error)
}

// safeCollect runs a collector and turns a panic into an error. A collector parses bytes
// that devices on the network send, and a bug in one parser must cost one source of
// facts and not the process.
func safeCollect(ctx context.Context, c Collector, in Input) (res Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			res, err = Result{}, fmt.Errorf("%s collector panicked: %v", c.Name(), r)
		}
	}()
	return c.Collect(ctx, in)
}
