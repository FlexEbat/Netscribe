package render

import (
	"bytes"
	_ "embed"
	"fmt"
	"html/template"
	"strconv"
	"time"
)

// HTMLCSP is the policy the export carries, in a header and in a meta tag: no script, no
// request to anywhere, inline styles and data images only.
const HTMLCSP = "default-src 'none'; style-src 'unsafe-inline'; img-src data:"

//go:embed templates/report.html.tmpl
var reportSource string

var reportTemplate = template.Must(template.New("report").Parse(reportSource))

type htmlDevice struct {
	Name, IP, MAC, Kind, Vendor, State string
	Online                             bool
}

type htmlInterface struct{ Device, Name, IP, MAC, State string }
type htmlService struct{ Device, Port, Proto, Name string }
type htmlContainer struct{ Host, Name, Image, State, Project, Ports string }
type htmlChange struct{ Kind, Summary string }

type htmlView struct {
	Generated                        string
	Devices, Online, Links, Services int
	Containers                       int
	Diagram                          template.HTML
	DeviceRows                       []htmlDevice
	InterfaceRows                    []htmlInterface
	ServiceRows                      []htmlService
	ContainerRows                    []htmlContainer
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// HTML renders one self-contained file: styles, the diagram and the tables, no script and
// no external reference. Every value goes through html/template, so a host name such as
// "<script>" is shown as text.
func HTML(d Data, now time.Time) ([]byte, error) {
	names := deviceNames(d.Devices)
	v := htmlView{
		Generated:  now.UTC().Format(time.RFC3339),
		Devices:    len(d.Devices),
		Links:      len(d.Links),
		Services:   len(d.Services),
		Containers: len(d.Containers),
		// The diagram is built from escaped text only, see Diagram.
		Diagram: template.HTML(Diagram(d)), //nolint:gosec // every label is XML-escaped in Diagram
	}
	for _, dev := range d.Devices {
		if dev.Online {
			v.Online++
		}
		v.DeviceRows = append(v.DeviceRows, htmlDevice{
			Name: names[dev.ID], IP: dash(dev.IP), MAC: dash(dev.MAC), Kind: string(dev.Kind), Vendor: dash(dev.Vendor),
			State: state(dev.Online), Online: dev.Online,
		})
	}
	for _, i := range d.Interfaces {
		v.InterfaceRows = append(v.InterfaceRows, htmlInterface{names[i.DeviceID], i.Name, dash(i.IP), dash(i.MAC), state(i.Up)})
	}
	for _, s := range d.Services {
		v.ServiceRows = append(v.ServiceRows, htmlService{names[s.DeviceID], strconv.Itoa(s.Port), s.Proto, dash(s.Name)})
	}
	for _, c := range d.Containers {
		v.ContainerRows = append(v.ContainerRows, htmlContainer{names[c.DeviceID], c.Name, c.Image, c.State, dash(c.ComposeProject), dash(portList(c.Ports))})
	}
	if d.LastScan != nil {
		v.HasScan = true
		v.LastScan = fmt.Sprintf("#%d, %s", d.LastScan.ID, scanTime(d.LastScan))
		for _, c := range d.Changes {
			v.ChangeRows = append(v.ChangeRows, htmlChange{c.Kind, c.Summary})
		}
	}
	var b bytes.Buffer
	if err := reportTemplate.Execute(&b, v); err != nil {
		return nil, fmt.Errorf("render html: %w", err)
	}
	return b.Bytes(), nil
}
