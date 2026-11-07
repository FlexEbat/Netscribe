// Package render turns stored data into things to draw and export.
package render

import (
	"math"
	"net/netip"
	"slices"
	"strings"

	"github.com/FlexEbat/Netscribe/internal/model"
)

const (
	// ColumnStep and RowStep are the distances between nodes on the canvas.
	ColumnStep = 220.0
	RowStep    = 140.0
	// RowCapacity is how many nodes one row takes before it wraps to the next. A layer with
	// many devices gets more columns (see columnsFor), so it stays about as wide as a screen.
	RowCapacity = 8
	// wideRatio is the width over the height a large layer is laid out in.
	wideRatio = 1.6

	// ContainersPerRow is how many containers stand in one row under their host. The
	// steps and the offset are the ones the web canvas uses (web/src/lib/topology.ts).
	ContainersPerRow    = 6
	ContainerColumnStep = 150.0
	ContainerRowStep    = 76.0
	ContainerOffsetY    = 110.0
)

// kindOrder is the top-to-bottom order of the layers.
var kindOrder = []model.DeviceKind{
	model.KindRouter, model.KindFirewall, model.KindSwitch, model.KindAP, model.KindServer, model.KindNAS,
	model.KindPrinter, model.KindCamera, model.KindIoT, model.KindHost, model.KindUnknown,
}

// Layout returns the devices with a canvas position on every one of them. A position
// that was saved is kept exactly. The rest are placed in layers by kind, in IP order
// inside a layer, ColumnStep by RowStep apart, wrapping after columnsFor nodes (RowCapacity
// for a small layer). The
// result depends only on the input, never on map order or time.
func Layout(devices []model.Device) []model.Device {
	out := slices.Clone(devices)
	byKind := make(map[model.DeviceKind][]int, len(kindOrder))
	for i, d := range out {
		if d.X != nil && d.Y != nil {
			continue
		}
		k := d.Kind
		if !known(k) {
			k = model.KindUnknown
		}
		byKind[k] = append(byKind[k], i)
	}

	row := 0
	for _, kind := range kindOrder {
		idx := byKind[kind]
		if len(idx) == 0 {
			continue
		}
		slices.SortFunc(idx, func(a, b int) int { return compareDevices(out[a], out[b]) })
		cols := columnsFor(len(idx))
		for n, i := range idx {
			x := float64(n%cols) * ColumnStep
			y := float64(row+n/cols) * RowStep
			out[i].X, out[i].Y = &x, &y
		}
		row += (len(idx) + cols - 1) / cols
	}
	return out
}

func known(k model.DeviceKind) bool {
	return slices.Contains(kindOrder, k)
}

// compareDevices orders by IP value, devices without a valid IP last, then by id.
func compareDevices(a, b model.Device) int {
	ia, errA := netip.ParseAddr(a.IP)
	ib, errB := netip.ParseAddr(b.IP)
	switch {
	case errA != nil && errB == nil:
		return 1
	case errA == nil && errB != nil:
		return -1
	case errA == nil && errB == nil:
		if c := ia.Compare(ib); c != 0 {
			return c
		}
	}
	switch {
	case a.ID < b.ID:
		return -1
	case a.ID > b.ID:
		return 1
	}
	return 0
}

// columnsFor is how many nodes a layer of n devices has in a row: RowCapacity, or more when
// that would make the layer much taller than wide. At 600 hosts eight columns are 75 rows,
// a strip too narrow for the canvas to fit at a readable zoom; this gives a block of about
// 25 columns and 24 rows. It depends on n only, so the layout stays deterministic.
func columnsFor(n int) int {
	wide := int(math.Ceil(math.Sqrt(float64(n) * wideRatio * RowStep / ColumnStep)))
	return max(RowCapacity, wide)
}

// ContainerPosition is a container's place on the canvas.
type ContainerPosition struct {
	ID   string
	X, Y float64
}

// LayoutContainers places the containers under their host devices: sorted by name, then
// by id, ContainersPerRow to a row, the first row ContainerOffsetY below the host. A
// container whose host has no position, or is not in devices, is left out. The result
// depends only on the input.
func LayoutContainers(devices []model.Device, containers []model.Container) []ContainerPosition {
	hosts := make(map[int64]model.Device, len(devices))
	for _, d := range devices {
		if d.X != nil && d.Y != nil {
			hosts[d.ID] = d
		}
	}
	byHost := make(map[int64][]model.Container)
	for _, c := range containers {
		if _, ok := hosts[c.DeviceID]; ok {
			byHost[c.DeviceID] = append(byHost[c.DeviceID], c)
		}
	}
	ids := make([]int64, 0, len(byHost))
	for id := range byHost {
		ids = append(ids, id)
	}
	slices.Sort(ids)

	var out []ContainerPosition
	for _, id := range ids {
		list := byHost[id]
		slices.SortFunc(list, func(a, b model.Container) int {
			if c := strings.Compare(a.Name, b.Name); c != 0 {
				return c
			}
			return strings.Compare(a.ID, b.ID)
		})
		host := hosts[id]
		for n, c := range list {
			out = append(out, ContainerPosition{
				ID: c.ID,
				X:  *host.X + float64(n%ContainersPerRow)*ContainerColumnStep,
				Y:  *host.Y + ContainerOffsetY + float64(n/ContainersPerRow)*ContainerRowStep,
			})
		}
	}
	return out
}
