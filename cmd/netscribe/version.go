package main

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// version and commit are set at release time with -ldflags "-X main.version=...". A build
// from a checkout reads them from the build information of the toolchain instead.
var (
	version = "dev"
	commit  = ""
)

// buildVersion is the version and the commit of this build.
func buildVersion() (v, c string) {
	v, c = version, commit
	if info, ok := debug.ReadBuildInfo(); ok {
		if v == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
			v = info.Main.Version
		}
		if c == "" {
			for _, s := range info.Settings {
				if s.Key == "vcs.revision" {
					c = s.Value
				}
			}
		}
	}
	return v, c
}

// versionLine describes this build for a bug report.
func versionLine() string {
	v, c := buildVersion()
	if len(c) > 7 {
		c = c[:7]
	}
	if c == "" {
		c = "unknown"
	}
	return fmt.Sprintf("netscribe %s (%s) %s %s/%s", v, c, runtime.Version(), runtime.GOOS, runtime.GOARCH)
}
