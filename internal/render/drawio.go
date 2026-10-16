package render

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"

	"github.com/FlexEbat/Netscribe/internal/model"
)

const (
	drawioDeviceW    = 160
	drawioDeviceH    = 56
	drawioContainerW = 144
	drawioContainerH = 56
)

// kindColors are the fill and stroke of a device kind in the diagram.
var kindColors = map[model.DeviceKind][2]string{
	model.KindRouter:   {"#dae8fc", "#6c8ebf"},
	model.KindSwitch:   {"#d5e8d4", "#82b366"},
	model.KindAP:       {"#e1d5e7", "#9673a6"},
	model.KindFirewall: {"#f8cecc", "#b85450"},
	model.KindServer:   {"#fff2cc", "#d6b656"},
	model.KindNAS:      {"#ffe6cc", "#d79b00"},
	model.KindPrinter:  {"#f5f5f5", "#666666"},
	model.KindCamera:   {"#f5f5f5", "#666666"},
	model.KindIoT:      {"#e6f7f7", "#3a8f8f"},
	model.KindHost:     {"#f5f5f5", "#666666"},
}

// stateColors are the fill and stroke of a container state.
func containerColors(state string) [2]string {
	switch state {
	case "running":
		return [2]string{"#d5e8d4", "#10b981"}
	case "restarting", "paused", "removing":
		return [2]string{"#fff2cc", "#f59e0b"}
	case "exited", "dead":
		return [2]string{"#f8cecc", "#ef4444"}
	}
	return [2]string{"#f5f5f5", "#94a3b8"}
}

// xmlAttr escapes a string for an XML attribute. Characters XML cannot hold become
// U+FFFD, a line break is kept as a character reference.
func xmlAttr(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s)) // a bytes.Buffer does not fail
	return b.String()
}

func num(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// Drawio renders an uncompressed mxGraph file: a vertex for every device and container
// at the position the panel draws it, an edge for every link and every container.
func Drawio(d Data) []byte {
	var b strings.Builder
	names := deviceNames(d.Devices)

	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	b.WriteString("<mxfile host=\"netscribe\">\n")
	b.WriteString("  <diagram id=\"network\" name=\"Network\">\n")
	b.WriteString("    <mxGraphModel dx=\"1200\" dy=\"800\" grid=\"1\" gridSize=\"10\" guides=\"1\" tooltips=\"1\" connect=\"1\" arrows=\"1\" fold=\"1\" page=\"0\" pageScale=\"1\" math=\"0\" shadow=\"0\">\n")
	b.WriteString("      <root>\n")
	b.WriteString("        <mxCell id=\"0\"/>\n")
	b.WriteString("        <mxCell id=\"1\" parent=\"0\"/>\n")

	for _, dev := range d.Devices {
		colors, ok := kindColors[dev.Kind]
		if !ok {
			colors = [2]string{"#ffffff", "#666666"}
		}
		style := fmt.Sprintf("rounded=1;whiteSpace=wrap;fillColor=%s;strokeColor=%s;", colors[0], colors[1])
		if !dev.Online {
			style += "dashed=1;opacity=50;"
		}
		label := names[dev.ID]
		if dev.IP != "" && dev.IP != label {
			label += "\n" + dev.IP
		}
		var x, y float64
		if dev.X != nil && dev.Y != nil {
			x, y = *dev.X, *dev.Y
		}
		fmt.Fprintf(&b, "        <mxCell id=\"d%d\" value=\"%s\" style=\"%s\" vertex=\"1\" parent=\"1\">\n", dev.ID, xmlAttr(label), style)
		fmt.Fprintf(&b, "          <mxGeometry x=\"%s\" y=\"%s\" width=\"%d\" height=\"%d\" as=\"geometry\"/>\n", num(x), num(y), drawioDeviceW, drawioDeviceH)
		b.WriteString("        </mxCell>\n")
	}

	for _, p := range LayoutContainers(d.Devices, d.Containers) {
		for _, c := range d.Containers {
			if c.ID != p.ID {
				continue
			}
			colors := containerColors(c.State)
			style := fmt.Sprintf("rounded=1;whiteSpace=wrap;fillColor=%s;strokeColor=%s;fontSize=11;", colors[0], colors[1])
			label := c.Name + "\n" + c.Image + "\n" + c.State
			fmt.Fprintf(&b, "        <mxCell id=\"c%s\" value=\"%s\" style=\"%s\" vertex=\"1\" parent=\"1\">\n", xmlAttr(c.ID), xmlAttr(label), style)
			fmt.Fprintf(&b, "          <mxGeometry x=\"%s\" y=\"%s\" width=\"%d\" height=\"%d\" as=\"geometry\"/>\n", num(p.X), num(p.Y), drawioContainerW, drawioContainerH)
			b.WriteString("        </mxCell>\n")
			fmt.Fprintf(&b, "        <mxCell id=\"ce%s\" value=\"\" style=\"endArrow=none;strokeColor=%s;\" edge=\"1\" parent=\"1\" source=\"d%d\" target=\"c%s\">\n", xmlAttr(c.ID), colors[1], c.DeviceID, xmlAttr(c.ID))
			b.WriteString("          <mxGeometry relative=\"1\" as=\"geometry\"/>\n")
			b.WriteString("        </mxCell>\n")
			break
		}
	}

	for _, l := range d.Links {
		if _, ok := names[l.ADeviceID]; !ok {
			continue
		}
		if _, ok := names[l.BDeviceID]; !ok {
			continue
		}
		fmt.Fprintf(&b, "        <mxCell id=\"l%d\" value=\"%s\" style=\"endArrow=none;\" edge=\"1\" parent=\"1\" source=\"d%d\" target=\"d%d\">\n",
			l.ID, xmlAttr(portLabel(l.APort, l.BPort)), l.ADeviceID, l.BDeviceID)
		b.WriteString("          <mxGeometry relative=\"1\" as=\"geometry\"/>\n")
		b.WriteString("        </mxCell>\n")
	}

	b.WriteString("      </root>\n")
	b.WriteString("    </mxGraphModel>\n")
	b.WriteString("  </diagram>\n")
	b.WriteString("</mxfile>\n")
	return []byte(b.String())
}
