package collector

import (
	"testing"

	"github.com/FlexEbat/Netscribe/internal/config"
)

func TestSetConfigRetargetsDockerOnlyWhenTheTargetsChange(t *testing.T) {
	sc := newTestScanner(&fakeStore{}, &fakeCollector{}, &fakeResolver{})
	sc.docker = NewDocker(config.DockerConfig{}, quietLogger())

	cfg := config.Config{}
	cfg.Scan.Targets = []string{"192.168.1.0/24"}
	sc.SetConfig(cfg)
	if n := len(sc.docker.wake); n != 1 {
		t.Fatalf("wake-ups after new targets = %d, want 1", n)
	}
	<-sc.docker.wake

	sc.SetConfig(cfg)
	if n := len(sc.docker.wake); n != 0 {
		t.Errorf("wake-ups after the same config = %d, want 0", n)
	}

	cfg.AllowPublicTargets = true
	sc.SetConfig(cfg)
	if n := len(sc.docker.wake); n != 1 {
		t.Errorf("wake-ups after allowPublicTargets changed = %d, want 1", n)
	}
}
