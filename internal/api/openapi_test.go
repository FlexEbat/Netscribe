package api

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/events"
	"github.com/FlexEbat/Netscribe/internal/store"
	"github.com/FlexEbat/Netscribe/internal/totp"
	"github.com/FlexEbat/Netscribe/internal/update"
)

// fullServer has every optional part switched on, with embedded nil interfaces that are
// never called: only the route table is read from it.
func fullServer() *server {
	return &server{
		version:    "v-test",
		updates:    &update.Checker{},
		helper:     struct{ SystemHelper }{},
		health:     struct{ HealthRunner }{},
		sessions:   struct{ SessionStore }{},
		mfa:        struct{ MFAStore }{},
		sealer:     &totp.Sealer{},
		backup:     struct{ BackupService }{},
		settings:   struct{ SettingsEditor }{},
		notifier:   struct{ Notifications }{},
		auditLog:   struct{ AuditReader }{},
		containers: struct{ ContainerReader }{},
		repo:       struct{ store.Repo }{},
		scanner:    struct{ Scanner }{},
		nmap:       struct{ NmapStatus }{},
		identifier: struct{ DeviceIdentifier }{},
		hub:        &events.Hub{},
		metricsOn:  func() bool { return true },
	}
}

func TestEveryRouteHasADocAndEveryDocHasARoute(t *testing.T) {
	seen := map[string]bool{}
	for _, rt := range fullServer().routes() {
		key := rt.Method + " " + rt.Path
		seen[key] = true
		if d, ok := routeDocs[key]; !ok || d.Summary == "" {
			t.Errorf("the route %s has no documentation", key)
		}
	}
	for key := range routeDocs {
		if !seen[key] {
			t.Errorf("the documentation of %s has no route", key)
		}
	}
}

func TestTheDocumentIsOpenAPIShaped(t *testing.T) {
	doc := fullServer().openAPI(func(Route) bool { return true })
	if doc["openapi"] != "3.1.0" {
		t.Fatalf("openapi = %v", doc["openapi"])
	}
	info := doc["info"].(map[string]any)
	if info["title"] == "" || info["version"] != "v-test" {
		t.Errorf("info = %v", info)
	}
	curly := regexp.MustCompile(`\{([a-z]+)\}`)
	for path, item := range doc["paths"].(map[string]map[string]any) {
		for method, raw := range item {
			op := raw.(map[string]any)
			if op["summary"] == "" || op["responses"] == nil {
				t.Errorf("%s %s lacks a summary or responses", method, path)
			}
			declared := map[string]bool{}
			if ps, ok := op["parameters"].([]map[string]any); ok {
				for _, p := range ps {
					if p["in"] == "path" {
						declared[p["name"].(string)] = true
						if p["required"] != true {
							t.Errorf("%s %s: a path parameter that is not required", method, path)
						}
					}
				}
			}
			for _, m := range curly.FindAllStringSubmatch(path, -1) {
				if !declared[m[1]] {
					t.Errorf("%s %s: the parameter %s is not declared", method, path, m[1])
				}
			}
		}
	}
}

func TestTokensAreMarkedWhereTheyMayNotGo(t *testing.T) {
	doc := fullServer().openAPI(func(Route) bool { return true })
	paths := doc["paths"].(map[string]map[string]any)
	get := func(path, method string) map[string]any { return paths[path][method].(map[string]any) }
	for _, c := range []struct {
		path, method string
		token        bool
	}{
		{"/api/devices", "get", true},
		{"/api/scans", "post", true},
		{"/metrics", "get", true},
		{"/api/auth/me", "get", true},
		{"/api/auth/login", "post", true},
		{"/api/backup", "get", false},
		{"/api/settings", "put", false},
		{"/api/tokens", "post", false},
		{"/api/audit", "get", false},
		{"/api/auth/password", "put", false},
	} {
		if got := get(c.path, c.method)["x-token"]; got != c.token {
			t.Errorf("%s %s: x-token = %v, want %v", c.method, c.path, got, c.token)
		}
	}
}

func TestTheReferenceListsOnlyWhatTheCallerMayUse(t *testing.T) {
	e, admin, viewer := systemEnv(t)
	body := func(s session) map[string]map[string]any {
		rec := asWith(e, s, http.MethodGet, "/api/openapi.json", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		var doc struct {
			Paths map[string]map[string]any `json:"paths"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
			t.Fatal(err)
		}
		return doc.Paths
	}
	if _, ok := body(viewer)["/api/users"]; ok {
		t.Error("a viewer sees the routes for users")
	}
	if _, ok := body(viewer)["/api/devices"]; !ok {
		t.Error("a viewer does not see the devices route")
	}
	if _, ok := body(admin)["/api/users"]; !ok {
		t.Error("an administrator does not see the routes for users")
	}
	if code := e.do(http.MethodGet, "/api/openapi.json", nil).Code; code != http.StatusUnauthorized {
		t.Errorf("no session: status = %d, want 401", code)
	}
}

func TestTheReferenceForATokenSkipsWhatATokenCannotCall(t *testing.T) {
	e, admin, _ := systemEnv(t)
	tok := issueToken(t, e, admin, map[string]any{"name": "doc", "scope": "read"})
	rec := e.do(http.MethodGet, "/api/openapi.json", nil, bearer(tok.Value))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var doc struct {
		Paths map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/users", "/api/backup", "/api/auth/password", "/api/settings"} {
		if _, ok := doc.Paths[path]; ok {
			t.Errorf("a token is shown %s", path)
		}
	}
	if post := doc.Paths["/api/devices"]["post"]; post != nil {
		t.Error("a read token is shown a route that changes things")
	}
}

func TestNoExampleHoldsARealToken(t *testing.T) {
	var all strings.Builder
	for _, d := range routeDocs {
		all.WriteString(d.Summary + d.Body + d.Answer + strings.Join(d.Query, " "))
	}
	if regexp.MustCompile(`ndt_[A-Za-z0-9]{10,}`).MatchString(all.String()) {
		t.Error("a documentation text holds a token")
	}
}
