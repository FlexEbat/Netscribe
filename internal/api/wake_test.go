package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/FlexEbat/Netscribe/internal/collector"
	"github.com/FlexEbat/Netscribe/internal/model"
	"github.com/FlexEbat/Netscribe/internal/store"
)

type wakeCall struct{ ip, mac string }

type fakeWaker struct {
	err   error
	calls []wakeCall
}

func (f *fakeWaker) Wake(_ context.Context, ip, mac string) error {
	f.calls = append(f.calls, wakeCall{ip, mac})
	return f.err
}

// wakeEnv seeds one device and returns the environment, an operator session and the device id.
func wakeEnv(t *testing.T, w *fakeWaker) (*env, session, string) {
	t.Helper()
	e := newEnv(t, func(o *envOptions) { o.waker = w })
	e.addUser("olga", model.RoleOperator)
	s := e.mustLogin("olga")
	seedScan(t, e, device("aa:bb:cc:00:00:01", "192.168.1.20", "nas", model.KindHost))
	devs := listDevices(t, e, s, "")
	if len(devs) != 1 {
		t.Fatalf("seeded %d devices, want 1", len(devs))
	}
	return e, s, strconv.FormatInt(devs[0].ID, 10)
}

func wakeAudit(t *testing.T, e *env) []model.AuditEntry {
	t.Helper()
	entries, err := e.store.ListAudit(context.Background(), store.AuditFilter{Action: "device_wake", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func TestWakeSendsTheStoredAddressAndMACAndWritesAnAuditEntry(t *testing.T) {
	w := &fakeWaker{}
	e, s, id := wakeEnv(t, w)

	// A MAC in the request is not read: the packet is built from the stored device only.
	rec := e.as(s, http.MethodPost, "/api/devices/"+id+"/wake", map[string]string{"mac": "de:ad:be:ef:00:01", "ip": "10.9.9.9"})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	if len(w.calls) != 1 || w.calls[0] != (wakeCall{"192.168.1.20", "aa:bb:cc:00:00:01"}) {
		t.Errorf("calls = %+v, want the stored address and MAC", w.calls)
	}
	if entries := wakeAudit(t, e); len(entries) != 1 || !strings.Contains(entries[0].Detail, id) {
		t.Errorf("audit = %+v, want one device_wake entry naming device %s", entries, id)
	}
}

func TestWakeNeedsTheRightToRunScans(t *testing.T) {
	w := &fakeWaker{}
	e, _, id := wakeEnv(t, w)
	path := "/api/devices/" + id + "/wake"
	e.addUser("vera", model.RoleViewer)
	e.addUser("ada", model.RoleAdmin)

	if rec := e.do(http.MethodPost, path, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("no session: status = %d, want 401", rec.Code)
	}
	if rec := e.as(e.mustLogin("vera"), http.MethodPost, path, nil); rec.Code != http.StatusForbidden {
		t.Errorf("viewer: status = %d, want 403", rec.Code)
	}
	admin := e.mustLogin("ada")
	read := issueToken(t, e, admin, map[string]any{"name": "ro", "scope": "read"})
	if rec := withToken(e, read.Value, http.MethodPost, path, nil); rec.Code != http.StatusForbidden {
		t.Errorf("read token: status = %d, want 403", rec.Code)
	}
	if len(w.calls) != 0 {
		t.Fatalf("%d packets were requested by callers who may not wake, want none", len(w.calls))
	}
	write := issueToken(t, e, admin, map[string]any{"name": "rw", "scope": "write"})
	if rec := withToken(e, write.Value, http.MethodPost, path, nil); rec.Code != http.StatusAccepted {
		t.Errorf("write token: status = %d, want 202, body %s", rec.Code, rec.Body)
	}
}

func TestWakeRefusalsAnswer4xxAndSendNothingMore(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want int
	}{
		"no MAC":           {collector.ErrNoMAC, http.StatusUnprocessableEntity},
		"no local subnet":  {collector.ErrNoLocalSubnet, http.StatusUnprocessableEntity},
		"public broadcast": {collector.ErrPublicBroadcast, http.StatusUnprocessableEntity},
		"a broken socket":  {errors.New("disk on fire"), http.StatusInternalServerError},
	} {
		e, s, id := wakeEnv(t, &fakeWaker{err: tc.err})
		rec := e.as(s, http.MethodPost, "/api/devices/"+id+"/wake", nil)
		if rec.Code != tc.want {
			t.Errorf("%s: status = %d, want %d", name, rec.Code, tc.want)
		}
		if tc.want == http.StatusUnprocessableEntity && !strings.Contains(rec.Body.String(), tc.err.Error()) {
			t.Errorf("%s: body %s does not give the reason", name, rec.Body)
		}
		if tc.want == http.StatusInternalServerError && strings.Contains(rec.Body.String(), "disk on fire") {
			t.Errorf("%s: the internal error text reached the client: %s", name, rec.Body)
		}
		if entries := wakeAudit(t, e); len(entries) != 0 {
			t.Errorf("%s: %d audit entries for a wake that did not happen", name, len(entries))
		}
	}
}

func TestWakeOfAnUnknownDeviceAnswers404WithoutAPacket(t *testing.T) {
	w := &fakeWaker{}
	e, s, _ := wakeEnv(t, w)

	for _, id := range []string{"9999", "0", "abc"} {
		if rec := e.as(s, http.MethodPost, "/api/devices/"+id+"/wake", nil); rec.Code != http.StatusNotFound {
			t.Errorf("device %q: status = %d, want 404", id, rec.Code)
		}
	}
	if len(w.calls) != 0 {
		t.Errorf("%d packets for devices that do not exist, want none", len(w.calls))
	}
}

func TestASecondWakeWithinTenSecondsAnswers429AndSendsNothing(t *testing.T) {
	w := &fakeWaker{}
	e, s, id := wakeEnv(t, w)
	path := "/api/devices/" + id + "/wake"

	if rec := e.as(s, http.MethodPost, path, nil); rec.Code != http.StatusAccepted {
		t.Fatalf("first wake: status = %d, want 202", rec.Code)
	}
	e.clk.advance(9 * time.Second)
	if rec := e.as(s, http.MethodPost, path, nil); rec.Code != http.StatusTooManyRequests {
		t.Errorf("second wake after 9 s: status = %d, want 429", rec.Code)
	}
	if len(w.calls) != 1 {
		t.Errorf("%d packets after the refused second request, want 1", len(w.calls))
	}
	e.clk.advance(time.Second)
	if rec := e.as(s, http.MethodPost, path, nil); rec.Code != http.StatusAccepted {
		t.Errorf("wake after 10 s: status = %d, want 202", rec.Code)
	}
}

func TestARefusedWakeDoesNotUseUpTheTenSeconds(t *testing.T) {
	w := &fakeWaker{err: collector.ErrNoLocalSubnet}
	e, s, id := wakeEnv(t, w)
	path := "/api/devices/" + id + "/wake"

	if rec := e.as(s, http.MethodPost, path, nil); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("refused wake: status = %d, want 422", rec.Code)
	}
	w.err = nil
	if rec := e.as(s, http.MethodPost, path, nil); rec.Code != http.StatusAccepted {
		t.Errorf("wake right after a refusal: status = %d, want 202", rec.Code)
	}
}
