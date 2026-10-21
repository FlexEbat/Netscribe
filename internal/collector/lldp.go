package collector

import (
	"context"
	"encoding/hex"
	"log/slog"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/FlexEbat/Netscribe/internal/model"
)

// LLDP-MIB (IEEE 802.1AB), numeric OIDs.
const (
	oidLLDPLocCaps  = "1.0.8802.1.1.2.1.3.6.0"
	oidLLDPLocPorts = "1.0.8802.1.1.2.1.3.7.1" // columns: 2 id subtype, 3 id, 4 description
	oidLLDPRemTable = "1.0.8802.1.1.2.1.4.1.1" // columns: 4..12, index timeMark.localPort.remIndex
	oidLLDPRemMan   = "1.0.8802.1.1.2.1.4.2.1" // column 3, index carries the management address
)

// Columns of lldpRemTable.
const (
	remChassisSubtype = 4
	remChassis        = 5
	remPortSubtype    = 6
	remPort           = 7
	remPortDesc       = 8
	remSysName        = 9
	remSysDesc        = 10
	remCapEnabled     = 12
)

// Capability bits of lldpSysCapEnabled, from the most significant bit of the first octet.
const (
	capBridge = 0x20
	capWLAN   = 0x10
	capRouter = 0x08
)

// LLDP reads the neighbor tables of devices that answered SNMP and turns them into links
// between known devices. A neighbor nobody knows becomes a new device.
//
// It runs after the SNMP collector, which tells it the community of every device and
// the MACs of their interfaces.
type LLDP struct {
	snmp *SNMP
	log  *slog.Logger

	mu     sync.Mutex
	trunks map[string]map[string]struct{} // device ip -> local ports that have an LLDP neighbor
}

// NewLLDP returns an LLDP collector that reuses the sessions details of snmp.
func NewLLDP(snmp *SNMP, log *slog.Logger) *LLDP {
	return &LLDP{snmp: snmp, log: log, trunks: map[string]map[string]struct{}{}}
}

func (c *LLDP) Name() string { return "lldp" }

// hasNeighbor tells whether port of the device has an LLDP neighbor. Such a port is a
// trunk: the MACs behind it belong to other switches' hosts, not to this switch.
func (c *LLDP) hasNeighbor(ip, port string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.trunks[ip][port]
	return ok
}

func (c *LLDP) Collect(ctx context.Context, in Input) (Result, error) {
	known := newKnown(in.Devices)
	timeout := in.Config.SNMP.Timeout.Std()

	var (
		wg  sync.WaitGroup
		mu  sync.Mutex
		res Result
		sem = make(chan struct{}, snmpConcurrency)
	)
loop:
	for _, d := range in.Devices {
		community, ok := c.snmp.communityFor(d.IP)
		if !ok {
			continue
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break loop
		}
		wg.Add(1)
		go func(d model.Device, community string) {
			defer wg.Done()
			defer func() { <-sem }()
			one, polled := c.collectOne(ctx, d, community, timeout, known)
			mu.Lock()
			defer mu.Unlock()
			res.Devices = append(res.Devices, one.Devices...)
			res.Links = append(res.Links, one.Links...)
			if polled {
				res.LinksPolled = append(res.LinksPolled, model.DeviceKey{MAC: d.MAC, IP: d.IP})
			}
		}(d, community)
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	sortResult(&res)
	sort.SliceStable(res.Links, func(i, j int) bool {
		a, b := res.Links[i], res.Links[j]
		if a.A.IP != b.A.IP {
			return a.A.IP < b.A.IP
		}
		return a.APort < b.APort
	})
	sort.SliceStable(res.LinksPolled, func(i, j int) bool { return res.LinksPolled[i].IP < res.LinksPolled[j].IP })
	return res, nil
}

// knownDevices finds devices that exist already, so a neighbor is matched to them
// instead of being created again.
type knownDevices struct {
	byIP   map[string]model.DeviceKey
	byMAC  map[string]model.DeviceKey
	byName map[string][]model.DeviceKey
}

func newKnown(devs []model.Device) knownDevices {
	k := knownDevices{
		byIP: map[string]model.DeviceKey{}, byMAC: map[string]model.DeviceKey{}, byName: map[string][]model.DeviceKey{},
	}
	for _, d := range devs {
		key := model.DeviceKey{MAC: d.MAC, IP: d.IP}
		if d.IP != "" {
			k.byIP[d.IP] = key
		}
		if d.MAC != "" {
			k.byMAC[d.MAC] = key
		}
		if n := strings.ToLower(d.Hostname); n != "" {
			k.byName[n] = append(k.byName[n], key)
		}
	}
	return k
}

type neighbor struct {
	localPort  string
	chassisMAC string
	chassisIP  string
	mgmtIP     string
	port       string
	sysName    string
	sysDesc    string
	caps       model.DeviceKind
}

func (c *LLDP) collectOne(ctx context.Context, d model.Device, community string, timeout time.Duration, known knownDevices) (Result, bool) {
	cl, err := c.snmp.dial(ctx, d.IP, community, timeout)
	if err != nil {
		c.log.Debug("lldp connect failed", "ip", d.IP, "err", err)
		return Result{}, false
	}
	defer func() { _ = cl.Close() }() // closing a UDP socket cannot lose data

	local := model.DeviceKey{MAC: d.MAC, IP: d.IP}
	var res Result

	if got, err := cl.Get(ctx, []string{oidLLDPLocCaps}); err == nil && len(got) == 1 {
		if b, ok := pduBytes(got[0].Value); ok {
			if kind := kindFromCaps(b); kind != "" {
				res.Devices = append(res.Devices, model.DeviceInput{Key: local, Kind: kind, Source: "snmp"})
			}
		}
	}

	locPorts, err := cl.Walk(ctx, oidLLDPLocPorts)
	if err != nil {
		c.log.Debug("lldp local ports failed", "ip", d.IP, "err", err)
		return res, false
	}
	rem, err := cl.Walk(ctx, oidLLDPRemTable)
	if err != nil {
		c.log.Debug("lldp neighbor table failed", "ip", d.IP, "err", err)
		return res, false
	}
	man, err := cl.Walk(ctx, oidLLDPRemMan)
	if err != nil {
		man = nil // management addresses are a convenience, links work without them
	}

	neighbors := parseNeighbors(locPorts, rem, man)
	ports := map[string]struct{}{}
	for _, n := range neighbors {
		ports[n.localPort] = struct{}{}
	}
	c.mu.Lock()
	c.trunks[d.IP] = ports
	c.mu.Unlock()

	for _, n := range neighbors {
		key, isNew := c.resolve(n, known)
		if key == (model.DeviceKey{}) || key == local {
			continue
		}
		if isNew {
			res.Devices = append(res.Devices, model.DeviceInput{
				Key: key, Hostname: cleanHostname(n.sysName), Description: oneLine(n.sysDesc),
				Kind: n.caps, Source: "snmp",
			})
		} else if n.caps != "" {
			// The device exists, LLDP only tells its kind. "arp" is the lowest source and
			// never replaces the source a device already has.
			res.Devices = append(res.Devices, model.DeviceInput{Key: key, Kind: n.caps, Source: "arp"})
		}
		res.Links = append(res.Links, model.LinkInput{
			A: local, B: key, APort: n.localPort, BPort: n.port, Source: "lldp",
		})
	}
	return res, true
}

// resolve finds the device a neighbor is: by management address, by a MAC that is known
// from the interface tables or ARP, or by a unique host name. Otherwise the neighbor is
// new and gets the key the neighbor announces. isNew reports that case.
func (c *LLDP) resolve(n neighbor, known knownDevices) (key model.DeviceKey, isNew bool) {
	for _, ip := range []string{n.mgmtIP, n.chassisIP} {
		if ip == "" {
			continue
		}
		if k, ok := known.byIP[ip]; ok {
			return k, false
		}
	}
	if n.chassisMAC != "" {
		if k, ok := c.snmp.ownerOf(n.chassisMAC); ok {
			return k, false
		}
		if k, ok := known.byMAC[n.chassisMAC]; ok {
			return k, false
		}
	}
	if names := known.byName[strings.ToLower(cleanHostname(n.sysName))]; len(names) == 1 && n.sysName != "" {
		return names[0], false
	}
	ip := firstNonEmpty(n.mgmtIP, n.chassisIP)
	if n.chassisMAC == "" && ip == "" {
		return model.DeviceKey{}, false // nothing to identify the neighbor by
	}
	return model.DeviceKey{MAC: n.chassisMAC, IP: ip}, true
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// parseNeighbors joins the three LLDP tables.
func parseNeighbors(locPorts, rem, man []snmpPDU) []neighbor {
	// Local port names by lldpLocPortNum.
	type locCols struct {
		subtype int
		id      []byte
		desc    []byte
	}
	loc := map[int]*locCols{}
	for _, p := range locPorts {
		suf, ok := suffix(p.OID, oidLLDPLocPorts)
		if !ok {
			continue
		}
		parts := strings.Split(suf, ".")
		if len(parts) != 2 {
			continue
		}
		col, err1 := strconv.Atoi(parts[0])
		num, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil {
			continue
		}
		r, ok := loc[num]
		if !ok {
			r = &locCols{}
			loc[num] = r
		}
		switch col {
		case 2:
			if n, ok := pduInt(p.Value); ok {
				r.subtype = int(n)
			}
		case 3:
			r.id, _ = pduBytes(p.Value)
		case 4:
			r.desc, _ = pduBytes(p.Value)
		}
	}

	type remCols struct {
		port, remIdx int
		cols         map[int]any
	}
	rows := map[string]*remCols{}
	var order []string
	for _, p := range rem {
		suf, ok := suffix(p.OID, oidLLDPRemTable)
		if !ok {
			continue
		}
		parts := strings.Split(suf, ".")
		if len(parts) != 4 { // column, timeMark, localPort, remIndex
			continue
		}
		nums := make([]int, 4)
		bad := false
		for i, s := range parts {
			n, err := strconv.Atoi(s)
			if err != nil {
				bad = true
				break
			}
			nums[i] = n
		}
		if bad {
			continue
		}
		id := parts[2] + "." + parts[3]
		r, ok := rows[id]
		if !ok {
			r = &remCols{port: nums[2], remIdx: nums[3], cols: map[int]any{}}
			rows[id] = r
			order = append(order, id)
		}
		r.cols[nums[0]] = p.Value
	}

	mgmt := map[string]string{} // "localPort.remIndex" -> IPv4 management address
	for _, p := range man {
		suf, ok := suffix(p.OID, oidLLDPRemMan+".3")
		if !ok {
			continue
		}
		parts := strings.Split(suf, ".")
		// timeMark, localPort, remIndex, addrSubtype, addrLen, address...
		if len(parts) < 6 || parts[3] != "1" || parts[4] != "4" || len(parts) != 9 {
			continue
		}
		addr, err := netip.ParseAddr(strings.Join(parts[5:9], "."))
		if err != nil {
			continue
		}
		mgmt[parts[1]+"."+parts[2]] = addr.String()
	}

	sort.Strings(order)
	var out []neighbor
	for _, id := range order {
		r := rows[id]
		n := neighbor{sysName: pduText(r.cols[remSysName]), sysDesc: pduText(r.cols[remSysDesc]), mgmtIP: mgmt[id]}

		if l, ok := loc[r.port]; ok {
			n.localPort = portName(l.subtype, l.id, l.desc)
		} else {
			n.localPort = strconv.Itoa(r.port)
		}
		sub, _ := pduInt(r.cols[remChassisSubtype])
		chassis, _ := pduBytes(r.cols[remChassis])
		n.chassisMAC, n.chassisIP = decodeChassis(int(sub), chassis)

		psub, _ := pduInt(r.cols[remPortSubtype])
		pid, _ := pduBytes(r.cols[remPort])
		pdesc, _ := pduBytes(r.cols[remPortDesc])
		n.port = portName(int(psub), pid, pdesc)

		if caps, ok := pduBytes(r.cols[remCapEnabled]); ok {
			n.caps = kindFromCaps(caps)
		}
		out = append(out, n)
	}
	return out
}

// decodeChassis returns the MAC or IPv4 address a chassis id carries. Subtype 4 is a MAC
// address, subtype 5 is a network address whose first octet is the address family.
func decodeChassis(subtype int, b []byte) (mac, ip string) {
	switch subtype {
	case 4:
		return formatMAC(b), ""
	case 5:
		if len(b) == 5 && b[0] == 1 {
			if a, ok := netip.AddrFromSlice(b[1:]); ok {
				return "", a.String()
			}
		}
	}
	return "", ""
}

// portName makes one label from an LLDP port id. Interface alias (1), interface name (5)
// and locally assigned (7) hold text, MAC (3) holds an address. Everything else falls
// back to the port description, then to hex. The same rule runs for both ends of a link,
// so both ends call a port the same way.
func portName(subtype int, id, desc []byte) string {
	switch subtype {
	case 1, 5, 7:
		if s := printable(id); s != "" {
			return s
		}
	case 3:
		if m := formatMAC(id); m != "" {
			return m
		}
	}
	if s := printable(desc); s != "" {
		return s
	}
	if len(id) > 0 {
		return hex.EncodeToString(id)
	}
	return ""
}

// printable returns b as text when it is valid UTF-8 without control characters.
func printable(b []byte) string {
	if len(b) == 0 || !utf8.Valid(b) {
		return ""
	}
	s := strings.TrimSpace(string(b))
	for _, r := range s {
		if unicode.IsControl(r) || model.IsBidiControl(r) {
			return ""
		}
	}
	return s
}

// kindFromCaps maps the enabled system capabilities to a device kind. An access point
// wins, then a bridge (a switch that also routes stays a switch), then a router. A
// device with none of these says nothing about its kind.
func kindFromCaps(caps []byte) model.DeviceKind {
	if len(caps) == 0 {
		return ""
	}
	switch c := caps[0]; {
	case c&capWLAN != 0:
		return model.KindAP
	case c&capBridge != 0:
		return model.KindSwitch
	case c&capRouter != 0:
		return model.KindRouter
	}
	return ""
}
