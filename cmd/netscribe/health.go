package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/FlexEbat/Netscribe/internal/config"
	"github.com/FlexEbat/Netscribe/internal/health"
	"github.com/FlexEbat/Netscribe/internal/model"
	"github.com/FlexEbat/Netscribe/internal/setup"
)

// overviewSource is the part of the store the health checks read.
type overviewSource interface {
	Overview(ctx context.Context) (model.Overview, error)
}

// healthRunner runs the checks of the Health page against the running install.
type healthRunner struct {
	config     func() config.Config
	configPath string
	overview   overviewSource
	helper     helperClient
}

func newHealthRunner(live func() config.Config, configPath string, overview overviewSource, helper helperClient) *healthRunner {
	return &healthRunner{config: live, configPath: configPath, overview: overview, helper: helper}
}

func (h *healthRunner) Run(ctx context.Context) []health.Check {
	in := health.Input{Config: h.config(), ConfigPath: h.configPath, UpdateHelper: h.helper.State().Available}
	// An overview that cannot be read leaves the scan check to say that no scan has run;
	// the database trouble itself shows in the log and in every other page.
	if o, err := h.overview.Overview(ctx); err == nil {
		in.Overview = o
	}
	free := setup.NewEnv(nil, io.Discard, false, false).FreeBytes
	return health.Run(ctx, health.Probe{
		Now:       time.Now,
		Stat:      os.Stat,
		ReadFile:  os.ReadFile,
		LookPath:  exec.LookPath,
		FreeBytes: free,
	}, in)
}
