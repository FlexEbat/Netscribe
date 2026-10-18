package collector

import (
	"bufio"
	_ "embed" // the default rules are part of the binary
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"

	"github.com/FlexEbat/Netscribe/internal/model"
)

//go:embed classify_rules.csv
var defaultRules string

// Facts are the things a rule can look at besides the device itself.
type Facts struct {
	Services int      // sysServices, -1 when unknown
	Ports    []int    // open TCP ports
	HTTP     string   // the HTTP banners of the device, one per line, lower case
	MDNS     []string // the mDNS service types it announced, such as _ipp._tcp
	SSDP     []string // the SERVER and ST values of its SSDP answers, lower case
}

type rule struct {
	field string // descr, http, mdns, ssdp, services, port, vendor
	match string // lower case text, "bridge", "router", or the port number
	port  int
	kind  model.DeviceKind
}

// ruleOrder is the order in which the fields are tried (section 5.2 of the spec: SNMP
// text, then what the device says over HTTP, mDNS and SSDP, then services, ports, vendor).
var ruleOrder = []string{"descr", "http", "mdns", "ssdp", "services", "port", "vendor"}

// Classifier sets the kind of devices by rules that live in data, not in code.
type Classifier struct {
	byField map[string][]rule
}

var validKinds = map[model.DeviceKind]bool{
	model.KindRouter: true, model.KindSwitch: true, model.KindAP: true, model.KindFirewall: true,
	model.KindServer: true, model.KindNAS: true, model.KindPrinter: true, model.KindCamera: true,
	model.KindIoT: true, model.KindHost: true,
}

// NewClassifier reads rules in the format of classify_rules.csv: "field,match,kind" per
// line, "#" starts a comment line. Any bad line is an error, a typo must not silently
// switch a rule off.
func NewClassifier(r io.Reader) (*Classifier, error) {
	c := &Classifier{byField: map[string][]rule{}}
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, ",")
		if len(parts) != 3 {
			return nil, fmt.Errorf("classify rules line %d: want field,match,kind", n)
		}
		field, match, kind := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), model.DeviceKind(strings.TrimSpace(parts[2]))
		if !validKinds[kind] {
			return nil, fmt.Errorf("classify rules line %d: unknown kind %q", n, kind)
		}
		ru := rule{field: field, match: strings.ToLower(match), kind: kind}
		switch field {
		case "descr", "http", "ssdp", "vendor":
			if match == "" {
				return nil, fmt.Errorf("classify rules line %d: empty match", n)
			}
		case "mdns":
			if !strings.HasPrefix(ru.match, "_") || strings.ContainsAny(ru.match, " ,") {
				return nil, fmt.Errorf("classify rules line %d: mdns match must be a service type such as _ipp._tcp", n)
			}
		case "services":
			if ru.match != "bridge" && ru.match != "router" {
				return nil, fmt.Errorf("classify rules line %d: services match must be bridge or router", n)
			}
		case "port":
			p, err := strconv.Atoi(match)
			if err != nil || p < 1 || p > 65535 {
				return nil, fmt.Errorf("classify rules line %d: bad port %q", n, match)
			}
			ru.port = p
		default:
			return nil, fmt.Errorf("classify rules line %d: unknown field %q", n, field)
		}
		c.byField[field] = append(c.byField[field], ru)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read classify rules: %w", err)
	}
	return c, nil
}

var (
	defaultOnce       sync.Once
	defaultClassifier *Classifier
	defaultErr        error
)

// DefaultClassifier returns the classifier built from the embedded classify_rules.csv.
func DefaultClassifier() (*Classifier, error) {
	defaultOnce.Do(func() {
		defaultClassifier, defaultErr = NewClassifier(strings.NewReader(defaultRules))
	})
	return defaultClassifier, defaultErr
}

// Kind returns the kind the rules give to a device, or unknown.
func (c *Classifier) Kind(description, vendor string, f Facts) model.DeviceKind {
	description, vendor = strings.ToLower(description), strings.ToLower(vendor)
	ssdp := strings.Join(f.SSDP, "\n")
	for _, field := range ruleOrder {
		for _, r := range c.byField[field] {
			var hit bool
			switch field {
			case "descr":
				hit = strings.Contains(description, r.match)
			case "vendor":
				hit = strings.Contains(vendor, r.match)
			case "http":
				hit = strings.Contains(strings.ToLower(f.HTTP), r.match)
			case "mdns":
				for _, t := range f.MDNS {
					if strings.EqualFold(t, r.match) {
						hit = true
					}
				}
			case "ssdp":
				hit = strings.Contains(strings.ToLower(ssdp), r.match)
			case "services":
				switch r.match {
				case "bridge":
					hit = f.Services >= 0 && f.Services&sysServicesBridge != 0
				case "router":
					hit = f.Services >= 0 && f.Services&sysServicesRouter != 0
				}
			case "port":
				for _, p := range f.Ports {
					if p == r.port {
						hit = true
					}
				}
			}
			if hit {
				return r.kind
			}
		}
	}
	return model.KindUnknown
}

// Apply sets Kind on the devices whose kind nothing has said yet. It looks at all inputs
// of one device together (the ARP, SNMP and LLDP inputs of a scan are separate entries),
// and writes the result to the first one. It makes no network requests.
func (c *Classifier) Apply(devs []model.DeviceInput, facts func(model.DeviceKey) Facts) {
	type merged struct {
		first       int
		known       bool
		description string
		vendor      string
	}
	byKey := map[model.DeviceKey]*merged{}
	var order []model.DeviceKey
	for i, d := range devs {
		m, ok := byKey[d.Key]
		if !ok {
			m = &merged{first: i}
			byKey[d.Key] = m
			order = append(order, d.Key)
		}
		if d.Kind != "" && d.Kind != model.KindUnknown {
			m.known = true
		}
		if m.description == "" {
			m.description = d.Description
		}
		if m.vendor == "" {
			m.vendor = d.Vendor
		}
	}
	for _, k := range order {
		m := byKey[k]
		if m.known {
			continue
		}
		f := Facts{Services: -1}
		if facts != nil {
			f = facts(k)
		}
		if kind := c.Kind(m.description, m.vendor, f); kind != model.KindUnknown {
			devs[m.first].Kind = kind
		}
	}
}
