package main

import (
	"context"
	"strings"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/auth"
	"github.com/FlexEbat/Netscribe/internal/config"
	"github.com/FlexEbat/Netscribe/internal/model"
	"github.com/FlexEbat/Netscribe/internal/store"
)

func TestSeedNetworkStoresTheDemoNetworkOnce(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	ctx := context.Background()

	for i := 0; i < 2; i++ { // the second run must change nothing
		if n, err := seedNetwork(ctx, st); err != nil || n != 27 {
			t.Fatalf("seedNetwork() = %d, %v, want 27 devices", n, err)
		}
	}
	devs, err := st.ListDevices(ctx, store.DeviceFilter{})
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[model.DeviceKind]int{}
	for _, d := range devs {
		kinds[d.Kind]++
	}
	want := map[model.DeviceKind]int{model.KindRouter: 1, model.KindSwitch: 2, model.KindServer: 4, model.KindHost: 20}
	if len(devs) != 27 {
		t.Errorf("devices = %d, want 27", len(devs))
	}
	for k, n := range want {
		if kinds[k] != n {
			t.Errorf("%s devices = %d, want %d", k, kinds[k], n)
		}
	}
}

func TestCreateDevUser(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	svc, err := auth.NewService(st, config.AuthConfig{
		SessionIdleTimeout: config.Duration(3600e9), SessionMaxAge: config.Duration(7200e9), MinPasswordLength: 12,
		MaxFailedLogins: 5, LockoutDuration: config.Duration(900e9),
		Argon2: config.Argon2Config{MemoryKiB: 19456, Iterations: 2, Parallelism: 1},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if err := createDevUser(ctx, svc, ""); err == nil || !strings.Contains(err.Error(), devPasswordEnv) {
		t.Errorf("empty password: error = %v, want a hint about %s", err, devPasswordEnv)
	}
	for i := 0; i < 2; i++ {
		if err := createDevUser(ctx, svc, "correct horse battery"); err != nil {
			t.Fatalf("run %d: %v", i+1, err)
		}
	}
	users, err := svc.ListUsers(ctx)
	if err != nil || len(users) != 1 || users[0].Username != "dev" || users[0].Role != model.RoleAdmin {
		t.Errorf("users = %+v, %v", users, err)
	}
}
