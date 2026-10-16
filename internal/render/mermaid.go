package render

import (
	"fmt"
	"strings"
)

// mermaidText makes a string safe inside a quoted Mermaid label. Quotes, angle brackets,
// ampersands and bars are written as Mermaid entity codes, so a host name cannot close
// the label or inject markup.
func mermaidText(s string) string {
	var b strings.Builder
	for _, r := range oneLine(s) {
		switch r {
		case '"':
			b.WriteString("#quot;")
		case '<':
			b.WriteString("#lt;")
		case '>':
			b.WriteString("#gt;")
		case '&':
			b.WriteString("#amp;")
		case '|':
			b.WriteString("#124;")
		case '#':
			b.WriteString("#35;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// mermaidGraph draws the topology as `graph LR`: a node per device, an edge per link
// with the port names.
func mermaidGraph(d Data) string {
	var b strings.Builder
	b.WriteString("graph LR\n")
	names := deviceNames(d.Devices)
	for _, dev := range d.Devices {
		label := mermaidText(names[dev.ID])
		if dev.IP != "" && dev.IP != names[dev.ID] {
			label += "<br/>" + mermaidText(dev.IP)
		}
		fmt.Fprintf(&b, "  n%d[\"%s\"]\n", dev.ID, label)
	}
	for _, l := range d.Links {
		if _, ok := names[l.ADeviceID]; !ok {
			continue
		}
		if _, ok := names[l.BDeviceID]; !ok {
			continue
		}
		if l.APort == "" && l.BPort == "" {
			fmt.Fprintf(&b, "  n%d --- n%d\n", l.ADeviceID, l.BDeviceID)
			continue
		}
		fmt.Fprintf(&b, "  n%d -- \"%s\" --- n%d\n", l.ADeviceID, mermaidText(portLabel(l.APort, l.BPort)), l.BDeviceID)
	}
	return b.String()
}

func portLabel(a, b string) string {
	switch {
	case a != "" && b != "":
		return a + " - " + b
	case a != "":
		return a
	}
	return b
}
