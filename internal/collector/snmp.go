package collector

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/FlexEbat/Netscribe/internal/model"
)

// Numeric OIDs (the spec forbids loading MIB files).
const (
	oidSysDescr    = "1.3.6.1.2.1.1.1.0"
	oidSysName     = "1.3.6.1.2.1.1.5.0"
	oidSysServices = "1.3.6.1.2.1.1.7.0"

	oidIfDescr      = "1.3.6.1.2.1.2.2.1.2"
	oidIfPhysAddr   = "1.3.6.1.2.1.2.2.1.6"
	oidIfOperStatus = "1.3.6.1.2.1.2.2.1.8"
	oidIfName       = "1.3.6.1.2.1.31.1.1.1.1"
	oidIPAdEntIf    = "1.3.6.1.2.1.4.20.1.2" // ipAdEntIfIndex, the row index is the address

	snmpConcurrency = 32 // section 9.6 of the spec
	maxInterfaces   = 512
)

// SNMP reads sysName, sysDescr, interfaces and addresses over SNMP v2c.
//
// A device that does not answer stays as it is and the scan goes on. The collector
// remembers which community worked for which address and which MACs belong to which
// device, the LLDP collector needs both.
type SNMP struct {
	dial snmpDialer
	log  *slog.Logger

	mu        sync.Mutex
	community map[string]string          // ip -> the community that answered
	macOwner  map[string]model.DeviceKey // interface MAC -> device
	services  map[string]int             // ip -> sysServices
	ifNames   map[string]map[int]string  // ip -> ifIndex -> interface name
}

// NewSNMP returns an SNMP collector that talks to devices over UDP.
func NewSNMP(log *slog.Logger) *SNMP {
	return newSNMP(dialGoSNMP, log)
}

func newSNMP(dial snmpDialer, log *slog.Logger) *SNMP {
	return &SNMP{dial: dial, log: log, community: map[string]string{}, macOwner: map[string]model.DeviceKey{},
		services: map[string]int{}, ifNames: map[string]map[int]string{}}
}

func (c *SNMP) Name() string { return "snmp" }

func (c *SNMP) communityFor(ip string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.community[ip]
	return s, ok
}

// servicesOf returns sysServices of a device that answered SNMP. Bit 2 (value 2) is
// "bridge", bit 3 (value 4) is "router".
func (c *SNMP) servicesOf(ip string) (int, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n, ok := c.services[ip]
	return n, ok
}

// ifName returns the name of an interface of a device that answered SNMP.
func (c *SNMP) ifName(ip string, index int) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ifNames[ip][index]
}

func (c *SNMP) ownerOf(mac string) (model.DeviceKey, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	k, ok := c.macOwner[mac]
	return k, ok
}

// Collect asks every live device that has an IP address. Community values are used
// but never logged, returned or stored.
func (c *SNMP) Collect(ctx context.Context, in Input) (Result, error) {
	communities := make([]string, 0, len(in.Config.SNMP.Communities))
	for _, s := range in.Config.SNMP.Communities {
		if v := string(s); v != "" {
			communities = append(communities, v)
		}
	}
	timeout := in.Config.SNMP.Timeout.Std()
	if len(communities) == 0 {
		return Result{}, nil
	}

	var (
		wg  sync.WaitGroup
		mu  sync.Mutex
		res Result
		sem = make(chan struct{}, snmpConcurrency)
	)
loop:
	for _, d := range in.Devices {
		if d.IP == "" {
			continue
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break loop
		}
		wg.Add(1)
		go func(d model.Device) {
			defer wg.Done()
			defer func() { <-sem }()
			one, ok := c.collectOne(ctx, d, communities, timeout)
			if !ok {
				return
			}
			mu.Lock()
			res.Devices = append(res.Devices, one.Devices...)
			res.Interfaces = append(res.Interfaces, one.Interfaces...)
			mu.Unlock()
		}(d)
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	sortResult(&res)
	return res, nil
}

// sortResult makes the output independent of goroutine timing.
func sortResult(r *Result) {
	sort.SliceStable(r.Devices, func(i, j int) bool { return r.Devices[i].Key.IP < r.Devices[j].Key.IP })
	sort.SliceStable(r.Interfaces, func(i, j int) bool {
		a, b := r.Interfaces[i], r.Interfaces[j]
		if a.Device.IP != b.Device.IP {
			return a.Device.IP < b.Device.IP
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.IP < b.IP
	})
}

// collectOne tries the communities in order. The first one that answers wins.
func (c *SNMP) collectOne(ctx context.Context, d model.Device, communities []string, timeout time.Duration) (Result, bool) {
	key := model.DeviceKey{MAC: d.MAC, IP: d.IP}
	for _, community := range communities {
		if ctx.Err() != nil {
			return Result{}, false
		}
		cl, err := c.dial(ctx, d.IP, community, timeout)
		if err != nil {
			c.log.Debug("snmp connect failed", "ip", d.IP, "err", err)
			continue
		}
		res, st, ok := c.readDevice(ctx, cl, key)
		_ = cl.Close() // a failed close of a UDP socket changes nothing
		if !ok {
			continue
		}
		c.mu.Lock()
		c.community[d.IP] = community
		c.services[d.IP] = st.services
		c.ifNames[d.IP] = st.names
		for _, i := range res.Interfaces {
			if i.MAC != "" {
				c.macOwner[i.MAC] = key
			}
		}
		c.mu.Unlock()
		return res, true
	}
	c.log.Debug("snmp: no answer", "ip", d.IP)
	return Result{}, false
}

// readDevice reads the system group, then the interface tables. A failed table keeps
// what was read before it. ok is false when the device did not answer the first request.
func (c *SNMP) readDevice(ctx context.Context, cl snmpClient, key model.DeviceKey) (Result, devState, bool) {
	sys, err := cl.Get(ctx, []string{oidSysName, oidSysDescr, oidSysServices})
	if err != nil {
		return Result{}, devState{}, false
	}
	var name, descr string
	st := devState{services: -1}
	for _, p := range sys {
		switch p.OID {
		case oidSysName:
			name = cleanHostname(pduText(p.Value))
		case oidSysDescr:
			descr = pduText(p.Value)
		case oidSysServices:
			if n, ok := pduInt(p.Value); ok {
				st.services = int(n)
			}
		}
	}
	if name == "" && descr == "" {
		return Result{}, devState{}, false // answered, but with nothing a person can use
	}
	res := Result{Devices: []model.DeviceInput{{
		Key: key, Hostname: name, Description: descr, Source: "snmp",
	}}}

	rows := map[string][]snmpPDU{}
	for _, root := range []string{oidIfDescr, oidIfName, oidIfPhysAddr, oidIfOperStatus, oidIPAdEntIf} {
		got, err := cl.Walk(ctx, root)
		if err != nil {
			c.log.Debug("snmp walk failed", "ip", key.IP, "oid", root, "err", err)
			continue
		}
		rows[root] = got
	}
	res.Interfaces = buildInterfaces(key, rows)
	st.names = interfaceNames(rows)
	return res, st, true
}

// devState is what later collectors need to know about a device that answered.
type devState struct {
	services int // sysServices, -1 when the device did not say
	names    map[int]string
}

type ifRow struct {
	index int
	name  string
	descr string
	mac   string
	up    bool
	ips   []string
}

// readIfRows joins the interface tables by ifIndex.
func readIfRows(rows map[string][]snmpPDU) map[int]*ifRow {
	byIndex := map[int]*ifRow{}
	get := func(i int) *ifRow {
		r, ok := byIndex[i]
		if !ok {
			r = &ifRow{index: i}
			byIndex[i] = r
		}
		return r
	}
	for _, p := range rows[oidIfDescr] {
		if i, ok := indexOf(p.OID, oidIfDescr); ok {
			get(i).descr = pduText(p.Value)
		}
	}
	for _, p := range rows[oidIfName] {
		if i, ok := indexOf(p.OID, oidIfName); ok {
			get(i).name = pduText(p.Value)
		}
	}
	for _, p := range rows[oidIfPhysAddr] {
		if i, ok := indexOf(p.OID, oidIfPhysAddr); ok {
			if b, ok := pduBytes(p.Value); ok {
				get(i).mac = formatMAC(b)
			}
		}
	}
	for _, p := range rows[oidIfOperStatus] {
		if i, ok := indexOf(p.OID, oidIfOperStatus); ok {
			if n, ok := pduInt(p.Value); ok {
				get(i).up = n == 1
			}
		}
	}
	for _, p := range rows[oidIPAdEntIf] {
		suf, ok := suffix(p.OID, oidIPAdEntIf)
		if !ok {
			continue
		}
		addr, err := netip.ParseAddr(suf)
		if err != nil || !addr.Is4() {
			continue
		}
		if n, ok := pduInt(p.Value); ok {
			r := get(int(n))
			r.ips = append(r.ips, addr.String())
		}
	}
	return byIndex
}

// label is the name of an interface: ifName, then ifDescr, then ifN.
func (r *ifRow) label() string {
	if r.name != "" {
		return r.name
	}
	if r.descr != "" {
		return r.descr
	}
	return "if" + strconv.Itoa(r.index)
}

// interfaceNames maps ifIndex to the name buildInterfaces uses.
func interfaceNames(rows map[string][]snmpPDU) map[int]string {
	out := map[int]string{}
	for i, r := range readIfRows(rows) {
		out[i] = r.label()
	}
	return out
}

// buildInterfaces turns the interface tables into entries. An interface with several
// addresses gives one entry per address, one without addresses gives a single entry.
func buildInterfaces(dev model.DeviceKey, rows map[string][]snmpPDU) []model.InterfaceInput {
	byIndex := readIfRows(rows)
	indexes := make([]int, 0, len(byIndex))
	for i := range byIndex {
		indexes = append(indexes, i)
	}
	sort.Ints(indexes)

	var out []model.InterfaceInput
	for _, i := range indexes {
		r := byIndex[i]
		name := r.label()
		if len(out) >= maxInterfaces {
			break
		}
		sort.Strings(r.ips)
		if len(r.ips) == 0 {
			out = append(out, model.InterfaceInput{Device: dev, Name: name, MAC: r.mac, Up: r.up})
			continue
		}
		for _, ip := range r.ips {
			out = append(out, model.InterfaceInput{Device: dev, Name: name, MAC: r.mac, IP: ip, Up: r.up})
		}
	}
	return out
}

// indexOf parses the single-number row index of a table column.
func indexOf(oid, column string) (int, bool) {
	suf, ok := suffix(oid, column)
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(suf)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// formatMAC returns aa:bb:cc:dd:ee:ff for a 6-byte address and "" for anything else,
// including the all-zero address of interfaces that have none.
func formatMAC(b []byte) string {
	if len(b) != 6 {
		return ""
	}
	allZero := true
	parts := make([]string, 6)
	for i, v := range b {
		if v != 0 {
			allZero = false
		}
		parts[i] = fmt.Sprintf("%02x", v)
	}
	if allZero {
		return ""
	}
	return strings.Join(parts, ":")
}
