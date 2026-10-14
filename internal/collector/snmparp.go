package collector

import (
	"context"
	"log/slog"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/FlexEbat/Netscribe/internal/model"
)

// Numeric OIDs of the ARP and bridge tables.
const (
	oidIPNetToMediaPhys = "1.3.6.1.2.1.4.22.1.2"   // index: ifIndex.a.b.c.d
	oidIPNetToMediaType = "1.3.6.1.2.1.4.22.1.4"   // 2 means invalid
	oidBasePortIf       = "1.3.6.1.2.1.17.1.4.1.2" // dot1dBasePortIfIndex, index: bridge port
	oidFdbPort          = "1.3.6.1.2.1.17.4.3.1.2" // dot1dTpFdbPort, index: MAC as 6 numbers
	oidFdbStatus        = "1.3.6.1.2.1.17.4.3.1.3"
	oidQFdbPort         = "1.3.6.1.2.1.17.7.1.2.2.1.2" // dot1qTpFdbPort, index: vlan.MAC
	oidQFdbStatus       = "1.3.6.1.2.1.17.7.1.2.2.1.3"

	fdbLearned = 3 // dot1dTpFdbStatus: the switch learned this MAC from traffic

	// Limits against a device that lists far more than any real network (section 9.5).
	maxTableDevices = 4096
	maxTableLinks   = 8192

	sysServicesBridge = 2
	sysServicesRouter = 4
)

// SNMPARP reads the ARP and forwarding tables of the routers and switches that answered
// SNMP. A router's ARP table shows devices of other segments, a switch's forwarding table
// shows devices that did not answer the ARP warm-up but sent traffic lately.
//
// It runs after the SNMP and LLDP collectors: it needs their communities, interface
// names and the list of trunk ports.
type SNMPARP struct {
	snmp *SNMP
	lldp *LLDP
	log  *slog.Logger
}

// NewSNMPARP returns a collector that reads router and switch tables.
func NewSNMPARP(snmp *SNMP, lldp *LLDP, log *slog.Logger) *SNMPARP {
	return &SNMPARP{snmp: snmp, lldp: lldp, log: log}
}

func (c *SNMPARP) Name() string { return "snmparp" }

type fdbEntry struct {
	mac  string
	port string // interface name
}

type deviceTables struct {
	key       model.DeviceKey
	arp       map[string]string // mac -> ip
	fdb       []fdbEntry
	fdbPolled bool
}

func (c *SNMPARP) Collect(ctx context.Context, in Input) (Result, error) {
	if !in.Config.Discovery.RouterTables {
		c.log.Info("router tables are not read: discovery.routerTables is false")
		return Result{}, nil
	}
	timeout := in.Config.SNMP.Timeout.Std()

	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		tables []deviceTables
		sem    = make(chan struct{}, snmpConcurrency)
	)
loop:
	for _, d := range in.Devices {
		community, ok := c.snmp.communityFor(d.IP)
		if !ok {
			continue
		}
		if svc, _ := c.snmp.servicesOf(d.IP); svc&(sysServicesBridge|sysServicesRouter) == 0 {
			continue // neither a bridge nor a router
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
			t, ok := c.readTables(ctx, d, community, timeout)
			if !ok {
				return
			}
			mu.Lock()
			tables = append(tables, t)
			mu.Unlock()
		}(d, community)
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	sort.Slice(tables, func(i, j int) bool { return tables[i].key.IP < tables[j].key.IP })
	return c.build(tables, in.Devices), nil
}

func (c *SNMPARP) readTables(ctx context.Context, d model.Device, community string, timeout time.Duration) (deviceTables, bool) {
	cl, err := c.snmp.dial(ctx, d.IP, community, timeout)
	if err != nil {
		c.log.Debug("snmparp connect failed", "ip", d.IP, "err", err)
		return deviceTables{}, false
	}
	defer func() { _ = cl.Close() }() // closing a UDP socket cannot lose data

	t := deviceTables{key: model.DeviceKey{MAC: d.MAC, IP: d.IP}, arp: map[string]string{}}
	walkErr := func(root string) ([]snmpPDU, error) {
		rows, err := cl.Walk(ctx, root)
		if err != nil {
			c.log.Debug("snmparp walk failed", "ip", d.IP, "oid", root, "err", err)
			return nil, err
		}
		return rows, nil
	}
	walk := func(root string) []snmpPDU {
		rows, _ := walkErr(root)
		return rows
	}

	t.arp = parseARP(walk(oidIPNetToMediaPhys), walk(oidIPNetToMediaType))

	basePorts := map[int]int{} // bridge port -> ifIndex
	for _, p := range walk(oidBasePortIf) {
		if port, ok := indexOf(p.OID, oidBasePortIf); ok {
			if n, ok := pduInt(p.Value); ok {
				basePorts[port] = int(n)
			}
		}
	}
	portName := func(bridgePort int) string {
		ifIndex, ok := basePorts[bridgePort]
		if !ok {
			ifIndex = bridgePort // many switches use the same number for both
		}
		if name := c.snmp.ifName(d.IP, ifIndex); name != "" {
			return name
		}
		return "port" + strconv.Itoa(bridgePort)
	}

	fdb := map[string]int{} // mac -> bridge port
	for _, set := range []struct {
		port, status string
		macDigits    int // numbers before the MAC in the row index
	}{{oidFdbPort, oidFdbStatus, 0}, {oidQFdbPort, oidQFdbStatus, 1}} {
		ports, err := walkErr(set.port)
		if err == nil {
			t.fdbPolled = true // an empty table is an answer too
		}
		status := walk(set.status)
		for mac, port := range parseFDB(ports, status, set.port, set.status, set.macDigits) {
			fdb[mac] = port
		}
	}
	macs := make([]string, 0, len(fdb))
	for m := range fdb {
		macs = append(macs, m)
	}
	sort.Strings(macs)
	for _, m := range macs {
		t.fdb = append(t.fdb, fdbEntry{mac: m, port: portName(fdb[m])})
	}
	return t, true
}

// parseARP returns mac -> ip from ipNetToMediaTable, without invalid rows.
func parseARP(phys, types []snmpPDU) map[string]string {
	invalid := map[string]bool{}
	for _, p := range types {
		if suf, ok := suffix(p.OID, oidIPNetToMediaType); ok {
			if n, _ := pduInt(p.Value); n == 2 {
				invalid[suf] = true
			}
		}
	}
	out := map[string]string{}
	for _, p := range phys {
		suf, ok := suffix(p.OID, oidIPNetToMediaPhys)
		if !ok || invalid[suf] {
			continue
		}
		i := strings.IndexByte(suf, '.')
		if i < 0 {
			continue
		}
		addr, err := netip.ParseAddr(suf[i+1:])
		if err != nil || !usableIP(addr) {
			continue
		}
		b, _ := pduBytes(p.Value)
		if mac := formatMAC(b); usableMAC(mac) {
			out[mac] = addr.String()
		}
	}
	return out
}

// parseFDB returns mac -> bridge port for the learned rows of a forwarding table. The
// row index is the MAC as six numbers, preceded by macDigits other numbers (the VLAN).
func parseFDB(ports, status []snmpPDU, portRoot, statusRoot string, skip int) map[string]int {
	notLearned := map[string]bool{}
	for _, p := range status {
		if suf, ok := suffix(p.OID, statusRoot); ok {
			if n, _ := pduInt(p.Value); n != fdbLearned {
				notLearned[suf] = true
			}
		}
	}
	out := map[string]int{}
	for _, p := range ports {
		suf, ok := suffix(p.OID, portRoot)
		if !ok || notLearned[suf] {
			continue
		}
		parts := strings.Split(suf, ".")
		if len(parts) != skip+6 {
			continue
		}
		b := make([]byte, 6)
		bad := false
		for i := 0; i < 6; i++ {
			n, err := strconv.Atoi(parts[skip+i])
			if err != nil || n < 0 || n > 255 {
				bad = true
				break
			}
			b[i] = byte(n)
		}
		port, ok := pduInt(p.Value)
		if bad || !ok || port <= 0 {
			continue
		}
		if mac := formatMAC(b); usableMAC(mac) {
			out[mac] = int(port)
		}
	}
	return out
}

// usableMAC rejects the empty address, broadcast and multicast addresses.
func usableMAC(mac string) bool {
	if mac == "" || mac == "ff:ff:ff:ff:ff:ff" {
		return false
	}
	return hexByte(mac[:2])&1 == 0
}

// usableIP rejects addresses that are not a host: unspecified, loopback, multicast,
// link-local and the limited broadcast.
func usableIP(a netip.Addr) bool {
	return a.Is4() && !a.IsUnspecified() && !a.IsLoopback() && !a.IsMulticast() &&
		!a.IsLinkLocalUnicast() && a != netip.AddrFrom4([4]byte{255, 255, 255, 255})
}

// build turns the tables into devices and links.
func (c *SNMPARP) build(tables []deviceTables, known []model.Device) Result {
	byMAC := map[string]model.DeviceKey{}
	for _, d := range known {
		if d.MAC != "" {
			byMAC[d.MAC] = model.DeviceKey{MAC: d.MAC, IP: d.IP}
		}
	}
	arpIP := map[string]string{}
	for _, t := range tables {
		for mac, ip := range t.arp {
			if _, dup := arpIP[mac]; !dup {
				arpIP[mac] = ip
			}
		}
	}

	var res Result
	added := map[string]struct{}{}
	keyOf := func(mac, ip string) (model.DeviceKey, bool) {
		if k, ok := byMAC[mac]; ok {
			return k, true
		}
		if ip == "" {
			ip = arpIP[mac]
		}
		key := model.DeviceKey{MAC: mac, IP: ip}
		if _, ok := added[mac]; !ok {
			if len(res.Devices) >= maxTableDevices {
				return model.DeviceKey{}, false
			}
			added[mac] = struct{}{}
			res.Devices = append(res.Devices, model.DeviceInput{Key: key, Source: "snmp"})
		}
		return key, true
	}

	for _, t := range tables {
		macs := make([]string, 0, len(t.arp))
		for m := range t.arp {
			macs = append(macs, m)
		}
		sort.Strings(macs)
		for _, m := range macs {
			if _, own := c.snmp.ownerOf(m); own {
				continue
			}
			keyOf(m, t.arp[m])
		}
	}
	truncated := false
	for _, t := range tables {
		if t.fdbPolled {
			res.FDBPolled = append(res.FDBPolled, t.key)
		}
		for _, e := range t.fdb {
			if _, own := c.snmp.ownerOf(e.mac); own {
				continue // an interface of a switch, not a host behind it
			}
			if c.lldp.hasNeighbor(t.key.IP, e.port) {
				continue // a trunk: the MACs behind it are seen on their own switch
			}
			if len(res.Links) >= maxTableLinks {
				truncated = true
				break
			}
			host, ok := keyOf(e.mac, "")
			if !ok {
				truncated = true
				continue
			}
			res.Links = append(res.Links, model.LinkInput{A: t.key, B: host, APort: e.port, Source: "fdb"})
		}
	}
	if truncated || len(res.Devices) >= maxTableDevices {
		c.log.Warn("router and switch tables were cut short", "devices", len(res.Devices), "links", len(res.Links))
	}
	return res
}
