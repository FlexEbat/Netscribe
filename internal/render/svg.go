package render

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
)

const (
	svgPadding   = 24.0
	svgMaxLabel  = 24
	svgEmptyView = "0 0 480 120"
)

// svgText escapes a label for markup. It is the XML escaper, which is also safe in HTML.
func svgText(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(oneLine(s))) // a bytes.Buffer does not fail
	return b.String()
}

func shorten(s string) string {
	r := []rune(s)
	if len(r) <= svgMaxLabel {
		return s
	}
	return string(r[:svgMaxLabel-1]) + "…"
}

type box struct{ x, y, w, h float64 }

func (b box) cx() float64 { return b.x + b.w/2 }
func (b box) cy() float64 { return b.y + b.h/2 }

// Diagram draws the topology as an SVG element. The nodes stand where the panel draws
// them, the viewBox is the bounding box of the nodes plus a margin. The text comes from
// the network, so every label is escaped.
func Diagram(d Data) string {
	names := deviceNames(d.Devices)
	boxes := make(map[int64]box, len(d.Devices))
	minX, minY, maxX, maxY := 0.0, 0.0, 0.0, 0.0
	first := true
	grow := func(b box) {
		if first {
			minX, minY, maxX, maxY, first = b.x, b.y, b.x+b.w, b.y+b.h, false
			return
		}
		minX, minY = min(minX, b.x), min(minY, b.y)
		maxX, maxY = max(maxX, b.x+b.w), max(maxY, b.y+b.h)
	}
	for _, dev := range d.Devices {
		if dev.X == nil || dev.Y == nil {
			continue
		}
		b := box{*dev.X, *dev.Y, drawioDeviceW, drawioDeviceH}
		boxes[dev.ID] = b
		grow(b)
	}
	places := LayoutContainers(d.Devices, d.Containers)
	cboxes := make(map[string]box, len(places))
	for _, p := range places {
		b := box{p.X, p.Y, drawioContainerW, drawioContainerH}
		cboxes[p.ID] = b
		grow(b)
	}

	var s strings.Builder
	if first {
		s.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="` + svgEmptyView + `" role="img" aria-label="Network topology">`)
		s.WriteString(`<text x="240" y="64" text-anchor="middle" font-size="14" fill="#666666">No devices</text></svg>`)
		return s.String()
	}
	fmt.Fprintf(&s, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="%s %s %s %s" role="img" aria-label="Network topology" font-family="system-ui, sans-serif">`,
		num(minX-svgPadding), num(minY-svgPadding), num(maxX-minX+2*svgPadding), num(maxY-minY+2*svgPadding))
	s.WriteString("\n")

	// Lines first, so the boxes cover their ends.
	for _, l := range d.Links {
		a, okA := boxes[l.ADeviceID]
		b, okB := boxes[l.BDeviceID]
		if !okA || !okB {
			continue
		}
		fmt.Fprintf(&s, `<line x1="%s" y1="%s" x2="%s" y2="%s" stroke="#666666" stroke-width="1.5"/>`+"\n",
			num(a.cx()), num(a.cy()), num(b.cx()), num(b.cy()))
		if label := portLabel(l.APort, l.BPort); label != "" {
			fmt.Fprintf(&s, `<text x="%s" y="%s" text-anchor="middle" font-size="10" fill="#444444">%s</text>`+"\n",
				num((a.cx()+b.cx())/2), num((a.cy()+b.cy())/2-4), svgText(shorten(label)))
		}
	}
	for _, c := range d.Containers {
		cb, okC := cboxes[c.ID]
		hb, okH := boxes[c.DeviceID]
		if !okC || !okH {
			continue
		}
		fmt.Fprintf(&s, `<line x1="%s" y1="%s" x2="%s" y2="%s" stroke="%s" stroke-width="1.5"/>`+"\n",
			num(hb.cx()), num(hb.y+hb.h), num(cb.cx()), num(cb.y), containerColors(c.State)[1])
	}

	for _, dev := range d.Devices {
		b, ok := boxes[dev.ID]
		if !ok {
			continue
		}
		colors, known := kindColors[dev.Kind]
		if !known {
			colors = [2]string{"#ffffff", "#666666"}
		}
		attrs := ""
		if !dev.Online {
			attrs = ` opacity="0.5" stroke-dasharray="4 3"`
		}
		fmt.Fprintf(&s, `<g%s><rect x="%s" y="%s" width="%s" height="%s" rx="8" fill="%s" stroke="%s" stroke-width="1.5"/>`,
			attrs, num(b.x), num(b.y), num(b.w), num(b.h), colors[0], colors[1])
		fmt.Fprintf(&s, `<text x="%s" y="%s" text-anchor="middle" font-size="12" font-weight="600" fill="#111111">%s</text>`,
			num(b.cx()), num(b.y+22), svgText(shorten(names[dev.ID])))
		if dev.IP != "" && dev.IP != names[dev.ID] {
			fmt.Fprintf(&s, `<text x="%s" y="%s" text-anchor="middle" font-size="10" fill="#444444">%s</text>`,
				num(b.cx()), num(b.y+40), svgText(shorten(dev.IP)))
		}
		s.WriteString("</g>\n")
	}

	for _, c := range d.Containers {
		b, ok := cboxes[c.ID]
		if !ok {
			continue
		}
		colors := containerColors(c.State)
		fmt.Fprintf(&s, `<g><rect x="%s" y="%s" width="%s" height="%s" rx="6" fill="%s" stroke="%s" stroke-width="1.5"/>`,
			num(b.x), num(b.y), num(b.w), num(b.h), colors[0], colors[1])
		fmt.Fprintf(&s, `<text x="%s" y="%s" text-anchor="middle" font-size="11" font-weight="600" fill="#111111">%s</text>`,
			num(b.cx()), num(b.y+20), svgText(shorten(c.Name)))
		fmt.Fprintf(&s, `<text x="%s" y="%s" text-anchor="middle" font-size="9" fill="#444444">%s</text>`,
			num(b.cx()), num(b.y+34), svgText(shorten(c.Image)))
		fmt.Fprintf(&s, `<text x="%s" y="%s" text-anchor="middle" font-size="9" fill="#444444">%s</text></g>`+"\n",
			num(b.cx()), num(b.y+47), svgText(c.State))
	}
	s.WriteString("</svg>")
	return s.String()
}
