package render

import (
	"context"
	"encoding/xml"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/FlexEbat/Netscribe/internal/model"
)

type fakeSource struct {
	topo     model.Topology
	ifaces   map[int64][]model.Interface
	services []model.Service
	err      error
}

func (f fakeSource) Topology(context.Context) (model.Topology, error) { return f.topo, f.err }
func (f fakeSource) ListInterfaces(_ context.Context, id int64) ([]model.Interface, error) {
	return f.ifaces[id], nil
}
func (f fakeSource) ListServices(context.Context, int64) ([]model.Service, error) {
	return f.services, nil
}

var at = time.Date(2026, 10, 15, 12, 30, 0, 0, time.UTC)

func sample() fakeSource {
	return fakeSource{
		topo: model.Topology{
			Devices: []model.Device{
				{ID: 2, IP: "192.168.1.10", MAC: "aa:bb:cc:00:00:02", Hostname: "web", Kind: model.KindServer, Vendor: "Dell", Online: true},
				{ID: 1, IP: "192.168.1.1", MAC: "aa:bb:cc:00:00:01", Hostname: "gw", Kind: model.KindRouter, Online: true},
				{ID: 3, IP: "192.168.1.9", Kind: model.KindUnknown},
			},
			Links: []model.Link{{ID: 5, ADeviceID: 1, APort: "Gi0/1", BDeviceID: 2, BPort: "eth0"}},
			Containers: []model.Container{
				{ID: "abcd", DeviceID: 2, Name: "web-app", Image: "nginx:1.27", State: "running", ComposeProject: "shop",
					Ports: []model.PortMapping{{HostIP: "0.0.0.0", HostPort: 8080, ContainerPort: 80, Proto: "tcp"}, {HostIP: "::", HostPort: 8080, ContainerPort: 80, Proto: "tcp"}}},
			},
		},
		ifaces:   map[int64][]model.Interface{2: {{ID: 1, DeviceID: 2, Name: "eth0", IP: "192.168.1.10", Up: true}}},
		services: []model.Service{{ID: 1, DeviceID: 2, Port: 22, Proto: "tcp", Name: "ssh"}},
	}
}

func export(t *testing.T, src Source, format string) File {
	t.Helper()
	f, err := Export(context.Background(), src, format, at, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestMarkdownHoldsEverySection(t *testing.T) {
	f := export(t, sample(), FormatMarkdown)
	if f.Name != "netscribe-2026-10-15.md" || !strings.HasPrefix(f.ContentType, "text/markdown") {
		t.Errorf("file = %s %s", f.Name, f.ContentType)
	}
	md := string(f.Body)
	for _, want := range []string{
		"# Netscribe network documentation",
		"Generated: 2026-10-15T12:30:00Z",
		"- Devices: 3 (2 online)", "- Links: 1", "- Services: 1", "- Containers: 1",
		"```mermaid\ngraph LR\n",
		"n1[\"gw<br/>192.168.1.1\"]",
		"n1 -- \"Gi0/1 - eth0\" --- n2",
		"| Name | IP | MAC | Kind | Vendor | State |",
		"| gw | 192.168.1.1 | aa:bb:cc:00:00:01 | router | - | online |",
		"## Interfaces", "| web | eth0 | 192.168.1.10 | - | online |",
		"## Services", "| web | 22 | tcp | ssh |",
		"## Containers", "| web | web-app | nginx:1.27 | running | shop | 8080-&gt;80/tcp |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
	// Devices are in IP order, as numbers.
	if strings.Index(md, "| gw |") > strings.Index(md, "| web |") {
		t.Error("devices are not in IP order")
	}
}

func TestExportOfAnEmptyDatabaseSaysSo(t *testing.T) {
	for _, format := range []string{FormatMarkdown, FormatHTML, FormatDrawio} {
		f := export(t, fakeSource{topo: model.Topology{}}, format)
		if len(f.Body) == 0 {
			t.Errorf("%s: empty body", format)
		}
	}
	md := string(export(t, fakeSource{}, FormatMarkdown).Body)
	if !strings.Contains(strings.ToLower(md), "no devices") || strings.Contains(md, "```mermaid") {
		t.Errorf("markdown = %s", md)
	}
}

var generated = regexp.MustCompile(`(?m)^Generated: .*$`)

func TestTheSameStateGivesTheSameBytesExceptTheDate(t *testing.T) {
	for _, format := range []string{FormatMarkdown, FormatHTML, FormatDrawio} {
		a, err := Export(context.Background(), sample(), format, at, Options{})
		if err != nil {
			t.Fatal(err)
		}
		// Shuffle what the store returns: the order must not leak into the file.
		src := sample()
		src.topo.Devices[0], src.topo.Devices[2] = src.topo.Devices[2], src.topo.Devices[0]
		b, err := Export(context.Background(), src, format, at.Add(48*time.Hour), Options{})
		if err != nil {
			t.Fatal(err)
		}
		na := generated.ReplaceAllString(string(a.Body), "")
		nb := generated.ReplaceAllString(string(b.Body), "")
		if na != nb {
			t.Errorf("%s: two exports of one state differ", format)
		}
		if format == FormatMarkdown && string(a.Body) == string(b.Body) {
			t.Error("the generation date does not show")
		}
	}
}

func TestSpecialCharactersAreEscapedInEveryFormat(t *testing.T) {
	src := sample()
	src.topo.Devices[0].Hostname = `a<b>&"c"|d_e*[x]` + "\nnext"
	src.topo.Devices[0].Label = ""
	src.topo.Containers[0].Name = `x|y<script>`

	md := string(export(t, src, FormatMarkdown).Body)
	if strings.Contains(md, "<b>") || strings.Contains(md, "<script>") {
		t.Errorf("markup survived in the markdown:\n%s", md)
	}
	if !strings.Contains(md, `a&lt;b&gt;&amp;"c"\|d\_e\*\[x\] next`) {
		t.Errorf("table cell is not escaped:\n%s", md)
	}
	if !strings.Contains(md, `x\|y&lt;script&gt;`) {
		t.Errorf("container cell is not escaped:\n%s", md)
	}
	// In Mermaid the quote, the bar and the angle brackets are entity codes, so the label
	// stays inside its quotes.
	if !strings.Contains(md, `a#lt;b#gt;#amp;#quot;c#quot;#124;d_e*[x] next`) {
		t.Errorf("mermaid label is not escaped:\n%s", md)
	}
	for _, line := range strings.Split(md, "\n") {
		if strings.Contains(line, "script") && !strings.Contains(line, "web-app") {
			// Host, name, image, state, project, ports: seven bars, the escaped one does not count.
			if n := strings.Count(strings.ReplaceAll(line, `\|`, ""), "|"); n != 7 {
				t.Errorf("a bar opened a new column (%d bars): %s", n, line)
			}
		}
	}

	drawio := string(export(t, src, FormatDrawio).Body)
	if strings.Contains(drawio, "<b>") || strings.Contains(drawio, "<script>") {
		t.Errorf("markup survived in the XML:\n%s", drawio)
	}
	var doc struct{}
	if err := xml.Unmarshal([]byte(drawio), &doc); err != nil {
		t.Errorf("the XML is not well-formed: %v", err)
	}
	if !strings.Contains(drawio, "a&lt;b&gt;&amp;&#34;c&#34;|d_e*[x]&#xA;next") {
		t.Errorf("attribute is not escaped:\n%s", drawio)
	}
}

type cell struct {
	ID       string `xml:"id,attr"`
	Value    string `xml:"value,attr"`
	Style    string `xml:"style,attr"`
	Vertex   string `xml:"vertex,attr"`
	Edge     string `xml:"edge,attr"`
	Source   string `xml:"source,attr"`
	Target   string `xml:"target,attr"`
	Geometry struct {
		X float64 `xml:"x,attr"`
		Y float64 `xml:"y,attr"`
	} `xml:"mxGeometry"`
}

func TestDrawioHasAVertexPerDeviceAndContainerAndAnEdgePerLink(t *testing.T) {
	src := sample()
	f := export(t, src, FormatDrawio)
	if f.Name != "netscribe-2026-10-15.drawio" || !strings.Contains(f.ContentType, "xml") {
		t.Errorf("file = %s %s", f.Name, f.ContentType)
	}
	var doc struct {
		Cells []cell `xml:"diagram>mxGraphModel>root>mxCell"`
	}
	if err := xml.Unmarshal(f.Body, &doc); err != nil {
		t.Fatal(err)
	}
	byID := map[string]cell{}
	vertices, edges := 0, 0
	for _, c := range doc.Cells {
		byID[c.ID] = c
		if c.Vertex == "1" {
			vertices++
		}
		if c.Edge == "1" {
			edges++
		}
	}
	if vertices != 4 || edges != 2 { // 3 devices and 1 container; 1 link and 1 container edge
		t.Errorf("vertices = %d, edges = %d", vertices, edges)
	}

	// The positions are the ones the panel draws.
	laid := Layout(src.topo.Devices)
	for _, d := range laid {
		c := byID["d"+strconv.FormatInt(d.ID, 10)]
		if c.Geometry.X != *d.X || c.Geometry.Y != *d.Y {
			t.Errorf("device %d at (%v, %v), the panel has (%v, %v)", d.ID, c.Geometry.X, c.Geometry.Y, *d.X, *d.Y)
		}
	}
	box := byID["cabcd"]
	want := LayoutContainers(laid, src.topo.Containers)[0]
	if box.Geometry.X != want.X || box.Geometry.Y != want.Y || !strings.Contains(box.Style, "#10b981") {
		t.Errorf("container = %+v, want (%v, %v) in the running colour", box, want.X, want.Y)
	}
	link := byID["l5"]
	if link.Source != "d1" || link.Target != "d2" || link.Value != "Gi0/1 - eth0" {
		t.Errorf("link = %+v", link)
	}
	if router := byID["d1"]; !strings.Contains(router.Style, "#dae8fc") || !strings.Contains(router.Value, "gw") {
		t.Errorf("router = %+v", router)
	}
	if offline := byID["d3"]; !strings.Contains(offline.Style, "dashed=1") {
		t.Errorf("an offline device is not dashed: %+v", offline)
	}
}

func TestExportRefusesAnUnknownFormatAndReportsReadErrors(t *testing.T) {
	if _, err := Export(context.Background(), sample(), "exe", at, Options{}); !errors.Is(err, ErrUnknownFormat) {
		t.Errorf("err = %v, want ErrUnknownFormat", err)
	}
	boom := errors.New("boom")
	if _, err := Export(context.Background(), fakeSource{err: boom}, FormatMarkdown, at, Options{}); !errors.Is(err, boom) {
		t.Errorf("err = %v, want the read error", err)
	}
}
