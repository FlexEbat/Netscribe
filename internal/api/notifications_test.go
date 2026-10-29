package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/model"
	"github.com/FlexEbat/Netscribe/internal/notify"
)

type fakeNotifier struct {
	err    error
	tested []string
}

func (f *fakeNotifier) Channels() []notify.ChannelInfo {
	return []notify.ChannelInfo{{Name: "ops", Type: "webhook", Events: []string{"scan_failed"}}}
}

func (f *fakeNotifier) Test(_ context.Context, name string) error {
	f.tested = append(f.tested, name)
	return f.err
}

func withNotifier(f *fakeNotifier) func(*envOptions) {
	return func(o *envOptions) { o.notifier = f }
}

func TestAdminListsTheChannelsWithoutSecrets(t *testing.T) {
	e := newEnv(t, withNotifier(&fakeNotifier{}))
	rec := e.as(e.mustLogin("admin"), http.MethodGet, "/api/notifications", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET: %d %s", rec.Code, rec.Body)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"channels":[{"name":"ops","type":"webhook","events":["scan_failed"]}]}` {
		t.Errorf("body = %s", got)
	}
}

func TestNotificationRoutesAreForAdminsOnly(t *testing.T) {
	f := &fakeNotifier{}
	e := newEnv(t, withNotifier(f))
	admin := e.mustLogin("admin")
	e.addUser("olga", model.RoleOperator)
	e.addUser("vera", model.RoleViewer)

	routes := []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/api/notifications", nil},
		{http.MethodPost, "/api/notifications/test", map[string]string{"channel": "ops"}},
	}
	for _, r := range routes {
		if rec := e.do(r.method, r.path, r.body); rec.Code != http.StatusUnauthorized {
			t.Errorf("no session %s %s = %d, want 401", r.method, r.path, rec.Code)
		}
		for _, who := range []string{"olga", "vera"} {
			if rec := e.as(e.mustLogin(who), r.method, r.path, r.body); rec.Code != http.StatusForbidden {
				t.Errorf("%s %s %s = %d, want 403", who, r.method, r.path, rec.Code)
			}
		}
		// A token never carries system:manage, whoever issued it.
		tok := issueToken(t, e, admin, map[string]any{"name": "ci", "permissions": []string{"topology:read"}})
		if rec := withToken(e, tok.Value, r.method, r.path, r.body); rec.Code != http.StatusForbidden {
			t.Errorf("token %s %s = %d, want 403", r.method, r.path, rec.Code)
		}
	}
	if len(f.tested) != 0 {
		t.Errorf("the notifier was asked to send %v for people who may not", f.tested)
	}
}

func TestTestMessageAnswers(t *testing.T) {
	cases := []struct {
		name string
		err  error
		body any
		want int
	}{
		{"sent", nil, map[string]string{"channel": "ops"}, http.StatusOK},
		{"unknown channel", notify.ErrUnknownChannel, map[string]string{"channel": "nope"}, http.StatusNotFound},
		{"too soon", notify.ErrTooSoon, map[string]string{"channel": "ops"}, http.StatusTooManyRequests},
		{"delivery fails", errors.New("timeout"), map[string]string{"channel": "ops"}, http.StatusBadGateway},
		{"no channel", nil, map[string]string{}, http.StatusBadRequest},
		{"unknown field", nil, map[string]any{"channel": "ops", "url": "http://x"}, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, withNotifier(&fakeNotifier{err: tc.err}))
			rec := e.as(e.mustLogin("admin"), http.MethodPost, "/api/notifications/test", tc.body)
			if rec.Code != tc.want {
				t.Fatalf("%d %s, want %d", rec.Code, rec.Body, tc.want)
			}
			if tc.want == http.StatusBadGateway && !strings.Contains(rec.Body.String(), "timeout") {
				t.Errorf("body %s lost the reason", rec.Body)
			}
		})
	}
}

func TestTestMessageIsAudited(t *testing.T) {
	e := newEnv(t, withNotifier(&fakeNotifier{}))
	e.as(e.mustLogin("admin"), http.MethodPost, "/api/notifications/test", map[string]string{"channel": "ops"})
	if got := auditActions(t, e, "notify_test"); len(got) != 1 || got[0].Result != "ok" || got[0].Detail != "ops" {
		t.Errorf("audit = %+v", got)
	}

	e = newEnv(t, withNotifier(&fakeNotifier{err: errors.New("timeout")}))
	e.as(e.mustLogin("admin"), http.MethodPost, "/api/notifications/test", map[string]string{"channel": "ops"})
	if got := auditActions(t, e, "notify_test"); len(got) != 1 || got[0].Result != "error" {
		t.Errorf("audit after a failure = %+v", got)
	}
}

func TestNotificationRoutesAreAbsentWithoutANotifier(t *testing.T) {
	e := newEnv(t)
	if rec := e.as(e.mustLogin("admin"), http.MethodGet, "/api/notifications", nil); rec.Code == http.StatusOK {
		t.Errorf("GET without a notifier = %d", rec.Code)
	}
}
