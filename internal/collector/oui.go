package collector

import (
	"bufio"
	_ "embed" // oui.csv is part of the binary
	"strings"
	"sync"

	"github.com/FlexEbat/Netscribe/internal/model"
)

//go:embed oui.csv
var ouiCSV string

var (
	ouiOnce  sync.Once
	ouiTable map[string]string // "aa:bb:cc" -> vendor
)

func loadOUI() {
	ouiTable = map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(ouiCSV))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		prefix, vendor, ok := strings.Cut(line, ",")
		if !ok {
			continue
		}
		prefix, vendor = strings.ToLower(strings.TrimSpace(prefix)), strings.TrimSpace(vendor)
		if len(prefix) == 8 && vendor != "" {
			ouiTable[prefix] = vendor
		}
	}
}

// Vendor returns the manufacturer of a MAC address, or "" when it is unknown. An address
// with the locally administered bit set (random addresses of phones, virtual
// interfaces) has no manufacturer, so it gets none.
func Vendor(mac string) string {
	ouiOnce.Do(loadOUI)
	mac = strings.ToLower(strings.TrimSpace(mac))
	if len(mac) < 8 {
		return ""
	}
	prefix := mac[:8]
	first := hexByte(prefix[:2])
	if first < 0 || first&0x02 != 0 {
		return ""
	}
	return ouiTable[prefix]
}

func hexByte(s string) int {
	n := 0
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9':
			n = n*16 + int(c-'0')
		case c >= 'a' && c <= 'f':
			n = n*16 + int(c-'a') + 10
		default:
			return -1
		}
	}
	return n
}

// fillVendors sets the vendor of every device that has a MAC and no vendor yet.
func fillVendors(devs []model.DeviceInput) {
	for i := range devs {
		if devs[i].Vendor == "" && devs[i].Key.MAC != "" {
			devs[i].Vendor = Vendor(devs[i].Key.MAC)
		}
	}
}
