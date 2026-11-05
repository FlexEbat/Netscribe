package collector

import (
	"bufio"
	_ "embed" // the default rules are part of the binary
	"fmt"
	"io"
	"sort"
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
	Nmap     []string // what nmap guessed: "type=router", "os=linux 5.x", lower case
	TLS      []string // the subject, alternative names and issuer of its certificates, lower case
	DHCP     []string // the vendor class and the host name it sent in a DHCP request, lower case
}

type rule struct {
	field  string // descr, http, mdns, ssdp, nmap, services, port, vendor, tls, dhcp
	match  string // lower case text, "bridge", "router", or the port number
	port   int
	kind   model.DeviceKind
	weight int // 1 to 100, what a match adds to its kind
}

// defaultWeight is the weight of a rule whose line has no weight column, so a rules file
// written before scores existed still loads.
const defaultWeight = 50

// The scores that decide a verdict (section 5.2 of the spec).
const (
	minScore = 40 // the winner needs at least this
	minGap   = 10 // and this much more than the next kind
	maxScore = 100
	maxWhy   = 5  // reasons kept per device
	whyLen   = 80 // characters of one reason
)

// ruleOrder is the order in which the fields are tried (section 5.2 of the spec: SNMP
// text, then what the device says over HTTP, mDNS and SSDP, then services, ports, vendor).
var ruleOrder = []string{"descr", "http", "mdns", "ssdp", "nmap", "tls", "dhcp", "services", "port", "vendor"}

// Classifier sets the kind of devices by rules that live in data, not in code.
type Classifier struct {
	byField map[string][]rule
}

var validKinds = map[model.DeviceKind]bool{
	model.KindRouter: true, model.KindSwitch: true, model.KindAP: true, model.KindFirewall: true,
	model.KindServer: true, model.KindNAS: true, model.KindPrinter: true, model.KindCamera: true,
	model.KindIoT: true, model.KindHost: true,
}

// NewClassifier reads rules in the format of classify_rules.csv: "field,match,kind" or
// "field,match,kind,weight" per line, "#" starts a comment line. A line without a weight
// gets 50. Any bad line is an error, a typo must not silently switch a rule off.
func NewClassifier(r io.Reader) (*Classifier, error) {
	c := &Classifier{byField: map[string][]rule{}}
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, ",")
		if len(parts) != 3 && len(parts) != 4 {
			return nil, fmt.Errorf("classify rules line %d: want field,match,kind or field,match,kind,weight", n)
		}
		field, match, kind := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), model.DeviceKind(strings.TrimSpace(parts[2]))
		weight := defaultWeight
		if len(parts) == 4 {
			w, err := strconv.Atoi(strings.TrimSpace(parts[3]))
			if err != nil || w < 1 || w > maxScore {
				return nil, fmt.Errorf("classify rules line %d: the weight must be a whole number from 1 to %d", n, maxScore)
			}
			weight = w
		}
		if !validKinds[kind] {
			return nil, fmt.Errorf("classify rules line %d: unknown kind %q", n, kind)
		}
		ru := rule{field: field, match: strings.ToLower(match), kind: kind, weight: weight}
		switch field {
		case "descr", "http", "ssdp", "nmap", "vendor", "tls", "dhcp":
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

// Verdict is what the rules say about one device.
type Verdict struct {
	Kind    model.DeviceKind
	Score   int      // 0 to 100: the total of the winning kind, capped; 0 when unknown
	Reasons []string // at most 5 short phrases; for unknown, the candidates
}

// Kind returns the kind the rules give to a device, or unknown.
func (c *Classifier) Kind(description, vendor string, f Facts) model.DeviceKind {
	return c.Classify(description, vendor, f).Kind
}

// match is one rule that hit.
type match struct {
	kind   model.DeviceKind
	weight int
	why    string
	order  int
}

// Classify adds the weight of every rule that matches to its kind. The kind with the highest
// total wins when the total reaches 40 and leads the next kind by at least 10; otherwise the
// device stays unknown and the reasons name the candidates.
func (c *Classifier) Classify(description, vendor string, f Facts) Verdict {
	description, vendor = strings.ToLower(description), strings.ToLower(vendor)
	ssdp, nmap := strings.Join(f.SSDP, "\n"), strings.Join(f.Nmap, "\n")
	tls, dhcp := strings.Join(f.TLS, "\n"), strings.Join(f.DHCP, "\n")
	var hits []match
	order := 0
	for _, field := range ruleOrder {
		for _, r := range c.byField[field] {
			var hit bool
			var why string
			switch field {
			case "descr":
				hit, why = strings.Contains(description, r.match), "sysDescr: "+r.match
			case "vendor":
				hit, why = strings.Contains(vendor, r.match), "vendor: "+r.match
			case "http":
				hit, why = strings.Contains(strings.ToLower(f.HTTP), r.match), "HTTP banner: "+r.match
			case "mdns":
				for _, t := range f.MDNS {
					if strings.EqualFold(t, r.match) {
						hit = true
					}
				}
				why = "mDNS: " + r.match
			case "ssdp":
				hit, why = strings.Contains(strings.ToLower(ssdp), r.match), "SSDP: "+r.match
			case "nmap":
				hit, why = strings.Contains(strings.ToLower(nmap), r.match), "Nmap: "+r.match
			case "tls":
				hit, why = strings.Contains(strings.ToLower(tls), r.match), "TLS subject: "+r.match
			case "dhcp":
				hit, why = strings.Contains(strings.ToLower(dhcp), r.match), "DHCP: "+r.match
			case "services":
				switch r.match {
				case "bridge":
					hit = f.Services >= 0 && f.Services&sysServicesBridge != 0
				case "router":
					hit = f.Services >= 0 && f.Services&sysServicesRouter != 0
				}
				why = "SNMP services: " + r.match
			case "port":
				for _, p := range f.Ports {
					if p == r.port {
						hit = true
					}
				}
				why = "port " + strconv.Itoa(r.port)
			}
			if hit {
				hits = append(hits, match{kind: r.kind, weight: r.weight, why: shorten(why, whyLen), order: order})
			}
			order++
		}
	}
	totals := map[model.DeviceKind]int{}
	for _, h := range hits {
		totals[h.kind] += h.weight
	}
	type ranked struct {
		kind  model.DeviceKind
		total int
	}
	var ranking []ranked
	for k, t := range totals {
		ranking = append(ranking, ranked{k, t})
	}
	sort.Slice(ranking, func(i, j int) bool {
		if ranking[i].total != ranking[j].total {
			return ranking[i].total > ranking[j].total
		}
		return ranking[i].kind < ranking[j].kind
	})
	if len(ranking) == 0 {
		return Verdict{Kind: model.KindUnknown}
	}
	top := ranking[0]
	if top.total < minScore || (len(ranking) > 1 && top.total-ranking[1].total < minGap) {
		var why []string
		for _, r := range ranking {
			if len(why) == maxWhy {
				break
			}
			why = append(why, fmt.Sprintf("candidate %s (%d)", r.kind, min(r.total, maxScore)))
		}
		return Verdict{Kind: model.KindUnknown, Reasons: why}
	}
	var mine []match
	for _, h := range hits {
		if h.kind == top.kind {
			mine = append(mine, h)
		}
	}
	sort.SliceStable(mine, func(i, j int) bool {
		if mine[i].weight != mine[j].weight {
			return mine[i].weight > mine[j].weight
		}
		return mine[i].order < mine[j].order
	})
	var why []string
	for _, h := range mine {
		if len(why) == maxWhy {
			break
		}
		why = append(why, h.why)
	}
	return Verdict{Kind: top.kind, Score: min(top.total, maxScore), Reasons: why}
}

// shorten cuts text to n characters, on a character boundary.
func shorten(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// Untyped returns the devices that have no kind yet and that the rules cannot type from
// what is known now. It makes no network requests.
func (c *Classifier) Untyped(devs []model.DeviceInput, facts func(model.DeviceKey) Facts) []model.DeviceKey {
	copied := append([]model.DeviceInput(nil), devs...)
	c.Apply(copied, facts)
	typed := map[model.DeviceKey]bool{}
	var order []model.DeviceKey
	for _, d := range copied {
		if _, seen := typed[d.Key]; !seen {
			typed[d.Key] = false
			order = append(order, d.Key)
		}
		if d.Kind != "" && d.Kind != model.KindUnknown {
			typed[d.Key] = true
		}
	}
	var out []model.DeviceKey
	for _, k := range order {
		if !typed[k] {
			out = append(out, k)
		}
	}
	return out
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
		v := c.Classify(m.description, m.vendor, f)
		if v.Kind != model.KindUnknown {
			devs[m.first].Kind = v.Kind
		}
		devs[m.first].KindScore = v.Score
		devs[m.first].KindReasons = v.Reasons
	}
}
