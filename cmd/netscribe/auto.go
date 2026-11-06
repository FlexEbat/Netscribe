package main

import (
	"context"
	"path/filepath"
	"time"

	"github.com/FlexEbat/Netscribe/internal/collector"
	"github.com/FlexEbat/Netscribe/internal/config"
	"github.com/FlexEbat/Netscribe/internal/model"
	"github.com/FlexEbat/Netscribe/internal/store"
	"github.com/FlexEbat/Netscribe/internal/update"
)

// newAutoUpdates puts the automatic updates together from the pieces of the running service.
// The config is read again at every look, so a change on the settings page counts at once.
func newAutoUpdates(db *store.Store, cfg func() config.Config, checker *update.Checker, scanner *collector.Scanner, helper helperClient, database string) *update.Auto {
	version := releaseVersion()
	return &update.Auto{
		Dir:     filepath.Dir(database),
		Version: version,
		Mode:    func() string { return cfg().Updates.AutoMode() },
		Window:  func() string { return cfg().Updates.UpdateWindow() },
		Status:  func() update.Status { return checker.Status(version, cfg().Updates.Check) },
		Busy:    scanner.Busy,
		Helper:  helper,
		Result: func() update.HelperOutcome {
			st := helper.State()
			out := update.HelperOutcome{Action: st.Action, State: st.State, Message: st.Message}
			if st.At != nil {
				out.At = *st.At
			}
			return out
		},
		Audit: func(action, result, detail string) {
			e := model.AuditEntry{Username: "system", Action: action, Result: result, Detail: detail}
			_ = db.AddAudit(context.Background(), e) // a lost audit line must not stop an update
		},
		Now: time.Now,
	}
}
