// Command seed fills a database with a demo network, so the interface can be tried
// and developed without scanning a real one. It is a development tool and is not
// part of the released binary.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/FlexEbat/Netscribe/internal/auth"
	"github.com/FlexEbat/Netscribe/internal/collector"
	"github.com/FlexEbat/Netscribe/internal/config"
	"github.com/FlexEbat/Netscribe/internal/model"
	"github.com/FlexEbat/Netscribe/internal/store"
)

const devPasswordEnv = "NETSCRIBE_DEV_PASSWORD" //nolint:gosec // the name of an environment variable, not a credential

func main() {
	if err := run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
}

func run(args []string, getenv func(string) string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("seed", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "netscribe.yaml", "path to the config file")
	allowLoose := fs.Bool("allow-loose-permissions", false, "accept a config file readable by others")
	devUser := fs.Bool("dev-user", false, "create the admin account \"dev\" with the password from "+devPasswordEnv)
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*configPath, *allowLoose)
	if err != nil {
		return err
	}
	st, err := store.Open(cfg.Database)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	ctx := context.Background()
	n, err := seedNetwork(ctx, st)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "demo network stored: %d devices\n", n)

	if *devUser {
		svc, err := auth.NewService(st, cfg.Auth, nil)
		if err != nil {
			return err
		}
		if err := createDevUser(ctx, svc, getenv(devPasswordEnv)); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(stdout, `account "dev" is ready (role admin)`)
	}
	return nil
}

// demoDevices is a router, two switches, four servers and twenty hosts.
func demoDevices() []model.DeviceInput {
	mk := func(n int, ip, name string, kind model.DeviceKind) model.DeviceInput {
		return model.DeviceInput{
			Key:      model.DeviceKey{MAC: fmt.Sprintf("02:00:00:00:00:%02x", n), IP: ip},
			Hostname: name,
			Kind:     kind,
			Source:   "arp",
		}
	}
	devs := []model.DeviceInput{
		mk(1, "192.168.1.1", "gateway", model.KindRouter),
		mk(2, "192.168.1.2", "switch-core", model.KindSwitch),
		mk(3, "192.168.1.3", "switch-office", model.KindSwitch),
	}
	for i := 0; i < 4; i++ {
		devs = append(devs, mk(10+i, fmt.Sprintf("192.168.1.%d", 10+i), fmt.Sprintf("server-%d", i+1), model.KindServer))
	}
	for i := 0; i < 20; i++ {
		devs = append(devs, mk(100+i, fmt.Sprintf("192.168.1.%d", 100+i), fmt.Sprintf("host-%02d", i+1), model.KindHost))
	}
	return devs
}

func demoKey(n int, ip string) model.DeviceKey {
	return model.DeviceKey{MAC: fmt.Sprintf("02:00:00:00:00:%02x", n), IP: ip}
}

// demoLinks cables the demo network the way LLDP would report it: the router to the core
// switch, the core switch to the office switch and the servers, the office switch to ten
// hosts.
func demoLinks() []model.LinkInput {
	gateway, core, office := demoKey(1, "192.168.1.1"), demoKey(2, "192.168.1.2"), demoKey(3, "192.168.1.3")
	link := func(a model.DeviceKey, aPort string, b model.DeviceKey, bPort string) model.LinkInput {
		return model.LinkInput{A: a, B: b, APort: aPort, BPort: bPort, Source: "lldp"}
	}
	links := []model.LinkInput{link(gateway, "eth1", core, "Gi1"), link(core, "Gi2", office, "Gi1")}
	for i := 0; i < 4; i++ {
		server := demoKey(10+i, fmt.Sprintf("192.168.1.%d", 10+i))
		links = append(links, link(core, fmt.Sprintf("Gi%d", 3+i), server, "eth0"))
	}
	for i := 0; i < 10; i++ {
		host := demoKey(100+i, fmt.Sprintf("192.168.1.%d", 100+i))
		links = append(links, link(office, fmt.Sprintf("Gi%d", 2+i), host, "eth0"))
	}
	return links
}

// demoInterfaces gives the router and the switches the interfaces the cables use.
func demoInterfaces() []model.InterfaceInput {
	gateway, core, office := demoKey(1, "192.168.1.1"), demoKey(2, "192.168.1.2"), demoKey(3, "192.168.1.3")
	out := []model.InterfaceInput{
		{Device: gateway, Name: "eth0", MAC: "02:00:00:00:01:00", IP: "203.0.113.2", Up: true},
		{Device: gateway, Name: "eth1", MAC: "02:00:00:00:01:01", IP: "192.168.1.1", Up: true},
		{Device: core, Name: "mgmt0", MAC: "02:00:00:00:02:00", IP: "192.168.1.2", Up: true},
		{Device: office, Name: "mgmt0", MAC: "02:00:00:00:03:00", IP: "192.168.1.3", Up: true},
	}
	for i := 1; i <= 6; i++ {
		out = append(out, model.InterfaceInput{Device: core, Name: fmt.Sprintf("Gi%d", i), Up: i <= 5})
	}
	for i := 1; i <= 11; i++ {
		out = append(out, model.InterfaceInput{Device: office, Name: fmt.Sprintf("Gi%d", i), Up: i <= 10})
	}
	return out
}

// seedNetwork stores the demo devices, their interfaces and links through the scanner's own
// path, as one finished scan. Running it twice changes nothing: devices are matched by MAC address.
func seedNetwork(ctx context.Context, st store.Repo) (int, error) {
	devs := demoDevices()
	scan, err := st.StartScan(ctx, "192.168.1.0/24")
	if err != nil {
		return 0, err
	}
	res := collector.Result{Devices: devs, Interfaces: demoInterfaces(), Links: demoLinks()}
	if err := st.ApplyResult(ctx, scan.ID, res); err != nil {
		_ = st.FinishScan(ctx, scan.ID, "failed", err.Error())
		return 0, err
	}
	if err := st.FinishScan(ctx, scan.ID, "done", ""); err != nil {
		return 0, err
	}
	return len(devs), nil
}

func createDevUser(ctx context.Context, svc *auth.Service, password string) error {
	if password == "" {
		return fmt.Errorf("set %s to the password of the dev account", devPasswordEnv)
	}
	_, err := svc.CreateUser(ctx, auth.NewUser{Username: "dev", Role: model.RoleAdmin, Password: password})
	if errors.Is(err, model.ErrConflict) {
		return nil // already there: running seed again is fine
	}
	return err
}
