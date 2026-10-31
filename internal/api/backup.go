package api

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/FlexEbat/Netscribe/internal/auth"
)

// maxBackupBody is the largest backup the restore route reads: the database limit of the
// archive plus room for the config, the manifest and the key.
const maxBackupBody = 1<<30 + 2<<20

// backupWindow is how long one backup or restore request may take to send or receive. The
// server's own timeouts are for small requests.
const backupWindow = 20 * time.Minute

// confirmHeader carries the password of the caller, base64 encoded because a header holds
// no characters beyond Latin-1 and a password may.
const confirmHeader = "X-Confirm-Password"

// BackupRefusal is an archive or a state of the install the backup service refuses, with a
// sentence for the person. Any other error from the service is a server error.
type BackupRefusal string

func (e BackupRefusal) Error() string { return string(e) }

// BackupSummary says what a staged backup holds.
type BackupSummary struct {
	Version   string    `json:"version"`
	CreatedAt time.Time `json:"createdAt"`
	Schema    int       `json:"schema"`
	Devices   int       `json:"devices"`
	Users     int       `json:"users"`
}

// BackupService makes and checks backups. *backupService in cmd/netscribe implements it.
type BackupService interface {
	// Create makes the archive of this install and returns it with its size. Closing the
	// reader removes whatever the archive needed on disk.
	Create(ctx context.Context) (io.ReadCloser, int64, error)
	// Stage reads an archive, checks it and leaves it ready for the helper. A refusal is a
	// BackupRefusal.
	Stage(ctx context.Context, r io.Reader) (BackupSummary, error)
}

// confirmPassword checks the password of the caller from the confirm header. It answers the
// request itself and returns false when the password is missing or wrong.
func (s *server) confirmPassword(w http.ResponseWriter, r *http.Request, action string) bool {
	p, _ := principal(r)
	raw, err := base64.StdEncoding.DecodeString(r.Header.Get(confirmHeader))
	if err != nil || len(raw) == 0 || len(raw) > maxPasswordBytes {
		writeError(w, http.StatusBadRequest, "the password is missing: send it in the "+confirmHeader+" header, base64 encoded")
		return false
	}
	switch err := s.auth.ConfirmPassword(r.Context(), p, string(raw)); {
	case err == nil:
		return true
	case errors.Is(err, auth.ErrWrongPassword):
		s.record(r, "access_denied", "denied", action+": the password was wrong")
		writeError(w, http.StatusForbidden, "the password is not right")
	default:
		s.internalError(w, "confirm password", err)
	}
	return false
}

// extendDeadlines gives a long transfer the time it needs. The errors are ignored: a server
// that cannot move its deadlines keeps the short ones and the transfer fails by itself.
func extendDeadlines(w http.ResponseWriter, now time.Time) {
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(now.Add(backupWindow))
	_ = rc.SetWriteDeadline(now.Add(backupWindow))
}

// getBackup answers GET /api/backup with the archive of this install: the database, the
// config with its secrets and a manifest. It asks for the password again, because a stolen
// session must not be enough to take every secret.
func (s *server) getBackup(w http.ResponseWriter, r *http.Request) {
	if !s.confirmPassword(w, r, "backup") {
		return
	}
	if !s.backupMu.TryLock() {
		writeError(w, http.StatusConflict, "another backup or restore is running")
		return
	}
	defer s.backupMu.Unlock()
	rc, size, err := s.backup.Create(r.Context())
	if err != nil {
		s.record(r, "backup_create", "error", "")
		s.internalError(w, "create backup", err)
		return
	}
	defer func() { _ = rc.Close() }() // read only, and closing removes the temporary file
	extendDeadlines(w, s.now())
	name := "netscribe-" + s.now().UTC().Format("2006-01-02") + ".tar.gz"
	h := w.Header()
	h.Set("Content-Type", "application/gzip")
	h.Set("Content-Disposition", `attachment; filename="`+name+`"`)
	h.Set("Content-Length", strconv.FormatInt(size, 10))
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, rc); err != nil {
		s.record(r, "backup_create", "error", "the download stopped")
		s.log.Warn("send backup", "err", err)
		return
	}
	s.record(r, "backup_create", "ok", name)
}

// restoreBackup answers POST /api/backup/restore. The body is the archive. It checks the
// archive and stages it, then asks the helper to put it in place: the service restarts and
// everybody signs in again. 202 means the helper took the request, not that it finished.
func (s *server) restoreBackup(w http.ResponseWriter, r *http.Request) {
	if s.helper == nil || !s.helper.State().Available {
		writeError(w, http.StatusNotImplemented, "this install cannot restore itself: run sudo netscribe restore FILE")
		return
	}
	if !s.confirmPassword(w, r, "restore") {
		return
	}
	if !s.backupMu.TryLock() {
		writeError(w, http.StatusConflict, "another backup or restore is running")
		return
	}
	defer s.backupMu.Unlock()
	extendDeadlines(w, s.now())
	body := http.MaxBytesReader(w, r.Body, maxBackupBody)
	sum, err := s.backup.Stage(r.Context(), body)
	var refusal BackupRefusal
	var tooBig *http.MaxBytesError
	switch {
	case errors.As(err, &tooBig):
		s.record(r, "backup_restore", "denied", "the backup is too large")
		writeError(w, http.StatusRequestEntityTooLarge, "the backup is too large")
		return
	case errors.As(err, &refusal):
		s.record(r, "backup_restore", "denied", string(refusal))
		writeError(w, http.StatusBadRequest, string(refusal))
		return
	case err != nil:
		s.record(r, "backup_restore", "error", "")
		s.internalError(w, "stage backup", err)
		return
	}
	switch err := s.helper.Request("restore"); {
	case errors.Is(err, ErrHelperBusy):
		writeError(w, http.StatusConflict, err.Error())
	case err != nil:
		s.record(r, "backup_restore", "error", "the helper did not take the request")
		s.internalError(w, "request restore", err)
	default:
		s.record(r, "backup_restore", "ok", fmt.Sprintf("backup of %s with %d devices", sum.CreatedAt.UTC().Format(time.RFC3339), sum.Devices))
		writeJSON(w, http.StatusAccepted, sum)
	}
}
