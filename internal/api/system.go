package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/FlexEbat/Netscribe/internal/setup"
	"github.com/FlexEbat/Netscribe/internal/update"
)

// maxSettingsBody is the largest body of PUT /api/settings.
const maxSettingsBody = 16 << 10

// recheckEvery is the shortest time between two checks asked for from the panel.
const recheckEvery = 30 * time.Second

// ErrHelperBusy is returned by SystemHelper.Request while an earlier request waits or runs.
var ErrHelperBusy = errors.New("the helper is busy with an earlier request")

// HelperState is what the panel knows about the root helper that updates and restarts the
// service. Without it (Docker, a install that did not set it up) Available is false and the
// panel shows the command to run instead of a button.
type HelperState struct {
	Available bool       `json:"available"`
	State     string     `json:"state"` // idle, waiting, running, done, failed
	Action    string     `json:"action"`
	Message   string     `json:"message"`
	At        *time.Time `json:"at"`
}

// SystemHelper hands a request to the root helper. *helperClient in cmd/netscribe implements it.
type SystemHelper interface {
	State() HelperState
	// Request asks for "update" or "restart" and returns at once.
	Request(verb string) error
}

// SettingsEditor reads and changes the config file. *settingsEditor in cmd/netscribe
// implements it on top of internal/setup.
type SettingsEditor interface {
	Settings() ([]setup.PanelGroup, error)
	Apply(changes map[string]json.RawMessage) (setup.PanelResult, error)
}

// versionAnswer is the body of GET /api/version.
type versionAnswer struct {
	Version string        `json:"version"`
	Update  update.Status `json:"update"`
	Helper  HelperState   `json:"helper"`
}

func (s *server) versionAnswer() versionAnswer {
	return versionAnswer{Version: s.version, Update: s.updateStatus(), Helper: s.helperState()}
}

func (s *server) updatesOn() bool {
	return s.updates != nil && (s.updatesEnabled == nil || s.updatesEnabled())
}

// updateStatus is what is known about the newest release. Without a checker nothing is.
func (s *server) updateStatus() update.Status {
	if s.updates == nil {
		return update.Status{Current: s.version, ReleasesURL: update.ReleasesURL, ChangelogURL: update.ReleasesURL}
	}
	return s.updates.Status(s.version, s.updatesOn())
}

func (s *server) helperState() HelperState {
	if s.helper == nil {
		return HelperState{State: "idle"}
	}
	return s.helper.State()
}

// getVersion answers GET /api/version with the running version, what is known about the
// newest release and the state of the helper. It never asks the network: the checker does
// that in the background.
func (s *server) getVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.versionAnswer())
}

// recheck answers POST /api/update/check: it asks GitHub now, at most once in 30 seconds.
func (s *server) recheck(w http.ResponseWriter, r *http.Request) {
	if !s.updatesOn() {
		writeError(w, http.StatusConflict, "the update check is switched off in the settings")
		return
	}
	s.recheckMu.Lock()
	wait := recheckEvery - s.now().Sub(s.lastRecheck)
	if wait > 0 {
		s.recheckMu.Unlock()
		writeJSON(w, http.StatusOK, s.versionAnswer()) // a fresh answer is already there
		return
	}
	s.lastRecheck = s.now()
	s.recheckMu.Unlock()
	if err := s.updates.Refresh(r.Context()); err != nil {
		s.log.Warn("update check", "err", err)
	}
	writeJSON(w, http.StatusOK, s.versionAnswer())
}

// startUpdate answers POST /api/update: 202 when the helper took the request, 409 when
// there is nothing to update to or the helper is busy, 501 when no helper is installed.
func (s *server) startUpdate(w http.ResponseWriter, r *http.Request) {
	st := s.helper.State()
	if !st.Available {
		writeError(w, http.StatusNotImplemented, "this install cannot update itself: run sudo netscribe and choose Update")
		return
	}
	cur := s.updateStatus()
	if !cur.Available || cur.Latest == nil {
		writeError(w, http.StatusConflict, "there is no newer release")
		return
	}
	switch err := s.helper.Request("update"); {
	case errors.Is(err, ErrHelperBusy):
		writeError(w, http.StatusConflict, err.Error())
	case err != nil:
		s.record(r, "update_request", "error", "update to "+cur.Latest.Tag)
		s.internalError(w, "request update", err)
	default:
		s.record(r, "update_request", "ok", "update to "+cur.Latest.Tag)
		writeJSON(w, http.StatusAccepted, s.versionAnswer())
	}
}

// restartService answers POST /api/system/restart.
func (s *server) restartService(w http.ResponseWriter, r *http.Request) {
	if !s.helper.State().Available {
		writeError(w, http.StatusNotImplemented, "this install cannot restart itself: run sudo systemctl restart netscribe")
		return
	}
	switch err := s.helper.Request("restart"); {
	case errors.Is(err, ErrHelperBusy):
		writeError(w, http.StatusConflict, err.Error())
	case err != nil:
		s.record(r, "restart_request", "error", "")
		s.internalError(w, "request restart", err)
	default:
		s.record(r, "restart_request", "ok", "")
		writeJSON(w, http.StatusAccepted, s.versionAnswer())
	}
}

// settingsAnswer is the body of GET /api/settings.
type settingsAnswer struct {
	Groups []setup.PanelGroup `json:"groups"`
}

// getSettings answers GET /api/settings with every setting, its value, its default and
// whether a change applies at once or needs a restart. Secrets are never sent.
func (s *server) getSettings(w http.ResponseWriter, _ *http.Request) {
	groups, err := s.settings.Settings()
	if err != nil {
		s.internalError(w, "read settings", err)
		return
	}
	writeJSON(w, http.StatusOK, settingsAnswer{Groups: groups})
}

// putSettings answers PUT /api/settings with {"changes": {"scan.interval": "30m", ...}}.
// The whole set is checked as one config and saved as one file, or nothing is saved (400).
func (s *server) putSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Changes map[string]json.RawMessage `json:"changes"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxSettingsBody))
	if err := dec.Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "the request body is not valid")
		return
	}
	if len(body.Changes) == 0 {
		writeError(w, http.StatusBadRequest, "there is nothing to change")
		return
	}
	s.settingsMu.Lock()
	res, err := s.settings.Apply(body.Changes)
	s.settingsMu.Unlock()
	var bad setup.PanelInputError
	switch {
	case errors.As(err, &bad):
		s.record(r, "settings_change", "denied", changeKeys(body.Changes))
		writeError(w, http.StatusBadRequest, bad.Error())
		return
	case err != nil:
		s.record(r, "settings_change", "error", changeKeys(body.Changes))
		s.internalError(w, "save settings", err)
		return
	}
	s.record(r, "settings_change", "ok", strings.Join(res.Saved, ", "))
	writeJSON(w, http.StatusOK, res)
}

// changeKeys lists the keys of a change, never the values: a value can be a secret.
func changeKeys(changes map[string]json.RawMessage) string {
	keys := make([]string, 0, len(changes))
	for k := range changes {
		if len(k) > 64 {
			k = k[:64]
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}
