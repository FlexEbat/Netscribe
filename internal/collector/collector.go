// Package collector gathers facts about the network and hands them to the scanner.
// It never imports store: a collector returns a Result and the scanner writes it.
package collector

import (
	"context"
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
	FDBPolled  []model.DeviceKey
	Services   []model.ServiceInput
	Containers []model.ContainerInput
}

// Collector is one source of facts. Collect must return when ctx is canceled.
type Collector interface {
	Name() string
	Collect(ctx context.Context, in Input) (Result, error)
}
