package render

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FlexEbat/Netscribe/internal/model"
)

func TestHTMLIsOneSelfContainedFileWithoutScriptsOrRequests(t *testing.T) {
	f := export(t, sample(), FormatHTML)
	if f.Name != "netscribe-2026-10-15.html" || !strings.HasPrefix(f.ContentType, "text/html") {
		t.Errorf("file = %s %s", f.Name, f.ContentType)
	}
	html := string(f.Body)
	for _, forbidden := range []string{"<script", "<link", "<img", "<iframe", "<object", "<embed", "@import", "url(", " src=", " href=", "javascript:"} {
		if strings.Contains(strings.ToLower(html), forbidden) {
			t.Errorf("the file contains %q", forbidden)
		}
	}
	if !strings.Contains(html, `content="`+HTMLCSP+`"`) {
		t.Error("the file does not carry its own policy")
	}
	for _, want := range []string{
		"Generated: 2026-10-15T12:30:00Z", "Devices: 3 (2 online)", "<svg ", "@media print", "break-inside: avoid",
		"<td>gw</td>", "<td>192.168.1.1</td>", "<td>web-app</td>", "<td>nginx:1.27</td>", "<td>8080-&gt;80/tcp</td>", "<td>ssh</td>",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("the file lacks %q", want)
		}
	}
	// The only address in the file is the SVG namespace, a name and not a request.
	for _, m := range regexp.MustCompile(`https?://[^"' <)]+`).FindAllString(html, -1) {
		if m != "http://www.w3.org/2000/svg" {
			t.Errorf("the file mentions %q", m)
		}
	}
}

func TestHTMLShowsAScriptNameAsText(t *testing.T) {
	src := sample()
	src.topo.Devices[0].Hostname = "<script>alert(1)</script>"
	src.topo.Containers[0].Name = `"><img src=x onerror=alert(1)>`
	src.topo.Containers[0].Image = "a&b"
	html := string(export(t, src, FormatHTML).Body)

	if strings.Contains(html, "<script") || strings.Contains(html, "<img") {
		t.Errorf("markup survived:\n%s", html)
	}
	if !strings.Contains(html, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Error("the host name is not shown as text in the table")
	}
	if strings.Count(html, "&lt;script&gt;") < 2 {
		t.Error("the host name is not escaped in the diagram as well")
	}
	if !strings.Contains(html, "a&amp;b") {
		t.Error("an ampersand is not escaped")
	}
}

func TestDiagramShowsTheNodesWhereThePanelDrawsThem(t *testing.T) {
	src := sample()
	laid := Layout(src.topo.Devices)
	svg := Diagram(Data{Devices: laid, Links: src.topo.Links, Containers: src.topo.Containers})

	for _, d := range laid {
		want := fmt.Sprintf(`<rect x="%s" y="%s" width="160" height="56"`, num(*d.X), num(*d.Y))
		if !strings.Contains(svg, want) {
			t.Errorf("device %d is not drawn at (%v, %v)", d.ID, *d.X, *d.Y)
		}
	}
	c := LayoutContainers(laid, src.topo.Containers)[0]
	if !strings.Contains(svg, fmt.Sprintf(`<rect x="%s" y="%s" width="144" height="56"`, num(c.X), num(c.Y))) {
		t.Error("the container is not drawn under its host")
	}
	if !strings.Contains(svg, "Gi0/1 - eth0") {
		t.Error("the link has no port label")
	}
	// The offline device is faded, the online ones are not.
	if n := strings.Count(svg, `opacity="0.5"`); n != 1 {
		t.Errorf("%d faded nodes, want 1 (device 3 is offline)", n)
	}
}

var viewBox = regexp.MustCompile(`viewBox="(-?[0-9.]+) (-?[0-9.]+) ([0-9.]+) ([0-9.]+)"`)

func TestDiagramViewBoxIsTheBoundingBoxOfTheNodes(t *testing.T) {
	x1, y1, x2, y2 := 100.0, 50.0, 700.0, 300.0
	devs := []model.Device{
		{ID: 1, IP: "10.0.0.1", X: &x1, Y: &y1, Online: true},
		{ID: 2, IP: "10.0.0.2", X: &x2, Y: &y2, Online: true},
	}
	m := viewBox.FindStringSubmatch(Diagram(Data{Devices: devs}))
	if m == nil {
		t.Fatal("no viewBox")
	}
	// From the left edge of the first node to the right edge of the second, plus the margin.
	want := []string{num(100 - svgPadding), num(50 - svgPadding), num(700 + 160 - 100 + 2*svgPadding), num(300 + 56 - 50 + 2*svgPadding)}
	for i, w := range want {
		if m[i+1] != w {
			t.Errorf("viewBox part %d = %s, want %s (%v)", i, m[i+1], w, m[1:])
		}
	}

	// Containers count as nodes: a host with many of them is taller.
	var ctrs []model.Container
	for i := 0; i < 13; i++ {
		ctrs = append(ctrs, model.Container{ID: fmt.Sprint("c", i), DeviceID: 1, Name: fmt.Sprintf("n%02d", i)})
	}
	m2 := viewBox.FindStringSubmatch(Diagram(Data{Devices: devs[:1], Containers: ctrs}))
	if m2 == nil || m2[4] == m[4] {
		t.Errorf("containers do not widen the view: %v", m2)
	}
}

func TestDiagramOfNothingHasAFixedViewBox(t *testing.T) {
	svg := Diagram(Data{})
	if !strings.Contains(svg, `viewBox="`+svgEmptyView+`"`) || !strings.Contains(svg, "No devices") {
		t.Errorf("svg = %s", svg)
	}
}

func TestLongLabelsAreCut(t *testing.T) {
	x, y := 0.0, 0.0
	svg := Diagram(Data{Devices: []model.Device{{ID: 1, Hostname: strings.Repeat("w", 80), X: &x, Y: &y}}})
	if strings.Contains(svg, strings.Repeat("w", 30)) || !strings.Contains(svg, "…") {
		t.Errorf("label not cut: %s", svg)
	}
}

func TestPDFIsPrintedFromTheSameHTML(t *testing.T) {
	var got []byte
	opts := Options{PDF: func(_ context.Context, html []byte) ([]byte, error) {
		got = html
		return []byte("%PDF-1.7 fake"), nil
	}}
	f, err := Export(context.Background(), sample(), FormatPDF, at, opts)
	if err != nil {
		t.Fatal(err)
	}
	if f.Name != "netscribe-2026-10-15.pdf" || f.ContentType != "application/pdf" || !strings.HasPrefix(string(f.Body), "%PDF") {
		t.Errorf("file = %s %s %q", f.Name, f.ContentType, f.Body)
	}
	html := export(t, sample(), FormatHTML)
	if string(got) != string(html.Body) {
		t.Error("the printer got other HTML than the HTML export")
	}
}

func TestPDFWithoutChromeFailsBeforeReadingAnything(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	boom := errors.New("the database must not be read")
	if _, err := Export(context.Background(), fakeSource{err: boom}, FormatPDF, at, Options{}); !errors.Is(err, ErrNoChrome) {
		t.Errorf("no PATH: err = %v, want ErrNoChrome", err)
	}
	opts := Options{ChromePath: "/nonexistent/chromium"}
	_, err := Export(context.Background(), fakeSource{err: boom}, FormatPDF, at, opts)
	if !errors.Is(err, ErrNoChrome) || !strings.Contains(err.Error(), "export.chromePath") {
		t.Errorf("bad chromePath: err = %v", err)
	}
}

func TestFindChromeUsesTheConfiguredPathOrPATH(t *testing.T) {
	dir := t.TempDir()
	exe := dir + "/chromium"
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o700); err != nil { //nolint:gosec // a fake program, it has to be executable
		t.Fatal(err)
	}
	if p, err := FindChrome(exe); err != nil || p != exe {
		t.Errorf("configured: %q, %v", p, err)
	}
	t.Setenv("PATH", dir)
	if p, err := FindChrome(""); err != nil || p != exe {
		t.Errorf("PATH: %q, %v", p, err)
	}
}

func TestOnlyOnePDFIsPrintedAtATime(t *testing.T) {
	old := printer
	defer func() { printer = old }()
	var now, peak atomic.Int32
	printer = func(ctx context.Context, _ string, _ []byte) ([]byte, error) {
		n := now.Add(1)
		defer now.Add(-1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		return []byte("%PDF"), nil
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	pdf := ChromePDF(exe)

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := pdf(context.Background(), []byte("<html></html>")); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if p := peak.Load(); p != 1 {
		t.Errorf("%d PDFs were printed at the same time, want 1", p)
	}
}

func TestAPDFWaitingForItsTurnGivesUpWithItsContext(t *testing.T) {
	old := printer
	defer func() { printer = old }()
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	printer = func(context.Context, string, []byte) ([]byte, error) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		return []byte("%PDF"), nil
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	pdf := ChromePDF(exe)
	done := make(chan struct{})
	go func() {
		_, _ = pdf(context.Background(), nil)
		close(done)
	}()
	<-entered // the first render holds the slot; a fixed sleep could let the second one in first

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := pdf(ctx, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want the deadline", err)
	}
	close(release)
	<-done
}

func TestRealChromePrintsAValidPDF(t *testing.T) {
	if _, err := FindChrome(""); err != nil {
		t.Skip("no Chrome here")
	}
	f, err := Export(context.Background(), sample(), FormatPDF, at, Options{})
	if err != nil {
		t.Fatalf("print: %v", err)
	}
	if !strings.HasPrefix(string(f.Body), "%PDF") || len(f.Body) < 1000 {
		t.Errorf("not a PDF: %d bytes, starts %q", len(f.Body), f.Body[:min(8, len(f.Body))])
	}
}
