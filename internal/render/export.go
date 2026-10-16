package render

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/FlexEbat/Netscribe/internal/model"
)

// Formats an export can have. The web panel and `netscribe export` both go through Export,
// so they hand out the same bytes.
const (
	FormatMarkdown = "md"
	FormatHTML     = "html"
	FormatPDF      = "pdf"
	FormatDrawio   = "drawio"
)

// ErrUnknownFormat means Export has no such format.
var ErrUnknownFormat = errors.New("unknown export format")

// Source is what an export reads. *store.Store implements it.
type Source interface {
	Topology(ctx context.Context) (model.Topology, error)
	ListInterfaces(ctx context.Context, deviceID int64) ([]model.Interface, error)
	ListServices(ctx context.Context, deviceID int64) ([]model.Service, error)
	LatestChanges(ctx context.Context) (scan model.Scan, changes []model.Change, ok bool, err error)
}

// Data is the state of the database an export shows. Devices have the positions the web
// panel draws them at.
type Data struct {
	Devices    []model.Device
	Links      []model.Link
	Containers []model.Container
	Interfaces []model.Interface
	Services   []model.Service
	// LastScan is the last completed scan, nil before the first one. Changes are what it
	// changed, oldest first.
	LastScan *model.Scan
	Changes  []model.Change
}

// File is a finished export.
type File struct {
	Name        string
	ContentType string
	Body        []byte
}

// Load reads everything an export needs and puts it in a fixed order, so that the same
// database always gives the same Data.
func Load(ctx context.Context, src Source) (Data, error) {
	t, err := src.Topology(ctx)
	if err != nil {
		return Data{}, fmt.Errorf("read topology: %w", err)
	}
	d := Data{Devices: Layout(t.Devices), Links: t.Links, Containers: t.Containers}
	sortDevices(d.Devices)
	for _, dev := range d.Devices {
		ifaces, err := src.ListInterfaces(ctx, dev.ID)
		if err != nil {
			return Data{}, fmt.Errorf("read interfaces: %w", err)
		}
		d.Interfaces = append(d.Interfaces, ifaces...)
	}
	if d.Services, err = src.ListServices(ctx, 0); err != nil {
		return Data{}, fmt.Errorf("read services: %w", err)
	}
	scan, changes, ok, err := src.LatestChanges(ctx)
	if err != nil {
		return Data{}, fmt.Errorf("read changes: %w", err)
	}
	if ok {
		d.LastScan, d.Changes = &scan, changes
		sort.SliceStable(d.Changes, func(i, j int) bool { return d.Changes[i].ID < d.Changes[j].ID })
	}
	sort.SliceStable(d.Links, func(i, j int) bool { return d.Links[i].ID < d.Links[j].ID })
	sort.SliceStable(d.Interfaces, func(i, j int) bool {
		a, b := d.Interfaces[i], d.Interfaces[j]
		if a.DeviceID != b.DeviceID {
			return a.DeviceID < b.DeviceID
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.IP < b.IP
	})
	sort.SliceStable(d.Services, func(i, j int) bool {
		a, b := d.Services[i], d.Services[j]
		if a.DeviceID != b.DeviceID {
			return a.DeviceID < b.DeviceID
		}
		if a.Port != b.Port {
			return a.Port < b.Port
		}
		return a.Proto < b.Proto
	})
	sort.SliceStable(d.Containers, func(i, j int) bool {
		a, b := d.Containers[i], d.Containers[j]
		if a.DeviceID != b.DeviceID {
			return a.DeviceID < b.DeviceID
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.ID < b.ID
	})
	return d, nil
}

// Options tune an export.
type Options struct {
	// ChromePath is the browser that prints PDFs, empty to look in PATH.
	ChromePath string
	// PDF replaces the PDF printer, for tests. Nil means Chrome.
	PDF PDFFunc
}

// Export renders the database in the given format. now is the generation time, the only
// part of the output that changes between two runs on the same data. A PDF needs Chrome:
// without it the error is ErrNoChrome.
func Export(ctx context.Context, src Source, format string, now time.Time, opts Options) (File, error) {
	switch format {
	case FormatMarkdown, FormatHTML, FormatPDF, FormatDrawio:
	default:
		return File{}, ErrUnknownFormat
	}
	if format == FormatPDF && opts.PDF == nil {
		// Look for the browser before reading the database: no Chrome, no work.
		if _, err := FindChrome(opts.ChromePath); err != nil {
			return File{}, err
		}
		opts.PDF = ChromePDF(opts.ChromePath)
	}
	d, err := Load(ctx, src)
	if err != nil {
		return File{}, err
	}
	name := "netscribe-" + now.UTC().Format("2006-01-02") + "."
	switch format {
	case FormatMarkdown:
		return File{Name: name + "md", ContentType: "text/markdown; charset=utf-8", Body: Markdown(d, now)}, nil
	case FormatDrawio:
		return File{Name: name + "drawio", ContentType: "application/xml; charset=utf-8", Body: Drawio(d)}, nil
	}
	html, err := HTML(d, now)
	if err != nil {
		return File{}, err
	}
	if format == FormatHTML {
		return File{Name: name + "html", ContentType: "text/html; charset=utf-8", Body: html}, nil
	}
	pdf, err := opts.PDF(ctx, html)
	if err != nil {
		return File{}, err
	}
	return File{Name: name + "pdf", ContentType: "application/pdf", Body: pdf}, nil
}

// deviceName is what the panel calls a device: its label, else its host name, else its IP.
func deviceName(d model.Device) string {
	for _, s := range []string{d.Label, d.Hostname, d.IP, d.MAC} {
		if s != "" {
			return s
		}
	}
	return "device"
}

// sortDevices orders by IP address as numbers, then by MAC and id.
func sortDevices(devs []model.Device) {
	sort.SliceStable(devs, func(i, j int) bool {
		a, b := devs[i], devs[j]
		ai, aerr := netip.ParseAddr(a.IP)
		bi, berr := netip.ParseAddr(b.IP)
		switch {
		case aerr == nil && berr == nil && ai != bi:
			return ai.Less(bi)
		case (aerr == nil) != (berr == nil):
			return aerr == nil
		case a.MAC != b.MAC:
			return a.MAC < b.MAC
		}
		return a.ID < b.ID
	})
}

func deviceNames(devs []model.Device) map[int64]string {
	out := make(map[int64]string, len(devs))
	for _, d := range devs {
		out[d.ID] = deviceName(d)
	}
	return out
}

// oneLine turns control characters and line breaks into spaces.
func oneLine(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '\u2028' || r == '\u2029' {
			return ' '
		}
		return r
	}, s)
}
