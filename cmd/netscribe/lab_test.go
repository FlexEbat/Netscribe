//go:build lab

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/model"
	"github.com/FlexEbat/Netscribe/internal/store"
)

// TestLabScanFindsTheLabContainers scans the network of deploy/compose.lab.yaml. Start
// the lab first, the test does not.
func TestLabScanFindsTheLabContainers(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "lab.db")
	cfgPath := filepath.Join(dir, "netscribe.yaml")
	body := fmt.Sprintf("database: %s\nscan:\n  targets: [172.30.0.0/24]\nsnmp:\n  communities: [public]\n  timeout: 2s\ndocker:\n  enabled: false\n", db)
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	if code := run([]string{"scan", "--config", cfgPath}, streams{in: strings.NewReader(""), out: &out, err: &errOut}); code != 0 {
		t.Fatalf("scan exited with %d: %s", code, errOut.String())
	}

	st, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	devices, err := st.ListDevices(context.Background(), store.DeviceFilter{})
	if err != nil {
		t.Fatal(err)
	}
	byIP := map[string]model.Device{}
	for _, d := range devices {
		byIP[d.IP] = d
	}
	if len(devices) < 3 {
		t.Fatalf("found %d devices, want at least the 3 lab containers: %+v", len(devices), devices)
	}

	if d, ok := byIP["172.30.0.10"]; !ok {
		t.Error("snmpd (172.30.0.10) was not found")
	} else if !strings.Contains(d.Description, "Netscribe lab") {
		t.Errorf("snmpd description = %q, want the sysDescr from its SNMP agent", d.Description)
	}
	for ip, port := range map[string]int{"172.30.0.11": 22, "172.30.0.12": 80} {
		d, ok := byIP[ip]
		if !ok {
			t.Errorf("%s was not found", ip)
			continue
		}
		services, err := st.ListServices(context.Background(), d.ID)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, s := range services {
			found = found || s.Port == port
		}
		if !found {
			t.Errorf("%s: no service on port %d in %+v", ip, port, services)
		}
	}
}
