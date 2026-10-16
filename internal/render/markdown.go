package render

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/FlexEbat/Netscribe/internal/model"
)

// mdCell makes a string safe inside a table cell: a bar cannot start a new column, markup
// characters are written as text and angle brackets and ampersands as entities.
func mdCell(s string) string {
	var b strings.Builder
	for _, r := range oneLine(s) {
		switch r {
		case '|', '\\', '`', '*', '_', '[', ']', '~':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '&':
			b.WriteString("&amp;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func mdOrDash(s string) string {
	if s == "" {
		return "-"
	}
	return mdCell(s)
}

func state(online bool) string {
	if online {
		return "online"
	}
	return "offline"
}

func mdTable(b *strings.Builder, head []string, rows [][]string) {
	b.WriteString("| " + strings.Join(head, " | ") + " |\n")
	b.WriteString("|" + strings.Repeat(" --- |", len(head)) + "\n")
	for _, r := range rows {
		b.WriteString("| " + strings.Join(r, " | ") + " |\n")
	}
	b.WriteString("\n")
}

// Markdown renders the documentation of the network. The line that starts with
// "Generated:" is the only one that depends on now.
func Markdown(d Data, now time.Time) []byte {
	var b strings.Builder
	names := deviceNames(d.Devices)
	online := 0
	for _, dev := range d.Devices {
		if dev.Online {
			online++
		}
	}

	b.WriteString("# Netscribe network documentation\n\n")
	fmt.Fprintf(&b, "Generated: %s\n\n", now.UTC().Format(time.RFC3339))
	b.WriteString("## Summary\n\n")
	fmt.Fprintf(&b, "- Devices: %d (%d online)\n- Links: %d\n- Services: %d\n- Containers: %d\n\n",
		len(d.Devices), online, len(d.Links), len(d.Services), len(d.Containers))

	if len(d.Devices) == 0 {
		b.WriteString("_There are no devices yet. Run a scan first._\n")
		return []byte(b.String())
	}

	b.WriteString("## Topology\n\n```mermaid\n" + mermaidGraph(d) + "```\n\n")

	b.WriteString("## Devices\n\n")
	rows := make([][]string, 0, len(d.Devices))
	for _, dev := range d.Devices {
		rows = append(rows, []string{
			mdCell(names[dev.ID]), mdOrDash(dev.IP), mdOrDash(dev.MAC), mdCell(string(dev.Kind)), mdOrDash(dev.Vendor), state(dev.Online),
		})
	}
	mdTable(&b, []string{"Name", "IP", "MAC", "Kind", "Vendor", "State"}, rows)

	if len(d.Interfaces) > 0 {
		b.WriteString("## Interfaces\n\n")
		rows = rows[:0]
		for _, i := range d.Interfaces {
			rows = append(rows, []string{mdCell(names[i.DeviceID]), mdCell(i.Name), mdOrDash(i.IP), mdOrDash(i.MAC), state(i.Up)})
		}
		mdTable(&b, []string{"Device", "Interface", "IP", "MAC", "State"}, rows)
	}

	if len(d.Services) > 0 {
		b.WriteString("## Services\n\n")
		rows = rows[:0]
		for _, s := range d.Services {
			rows = append(rows, []string{mdCell(names[s.DeviceID]), strconv.Itoa(s.Port), mdCell(s.Proto), mdOrDash(s.Name)})
		}
		mdTable(&b, []string{"Device", "Port", "Proto", "Service"}, rows)
	}

	if len(d.Containers) > 0 {
		b.WriteString("## Containers\n\n")
		rows = rows[:0]
		for _, c := range d.Containers {
			rows = append(rows, []string{
				mdCell(names[c.DeviceID]), mdCell(c.Name), mdCell(c.Image), mdCell(c.State), mdOrDash(c.ComposeProject), mdOrDash(portList(c.Ports)),
			})
		}
		mdTable(&b, []string{"Host", "Name", "Image", "State", "Project", "Ports"}, rows)
	}
	return []byte(b.String())
}

// portList writes published ports as "8080->80/tcp" and exposed ones as "443/tcp", once
// per number even when the daemon lists IPv4 and IPv6.
func portList(ports []model.PortMapping) string {
	var out []string
	seen := map[string]bool{}
	for _, p := range ports {
		s := fmt.Sprintf("%d/%s", p.ContainerPort, p.Proto)
		if p.HostPort > 0 {
			s = fmt.Sprintf("%d->%d/%s", p.HostPort, p.ContainerPort, p.Proto)
		}
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return strings.Join(out, ", ")
}
