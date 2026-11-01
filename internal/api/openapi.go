package api

import (
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/FlexEbat/Netscribe/internal/auth"
	"github.com/FlexEbat/Netscribe/internal/model"
)

var pathParam = regexp.MustCompile(`\{([a-z]+)\}`)

// getOpenAPI answers GET /api/openapi.json: the routes of the table with the words of
// routeDocs, as an OpenAPI 3.1 document. It lists only what the caller may use.
func (s *server) getOpenAPI(w http.ResponseWriter, r *http.Request) {
	p, _ := principal(r)
	writeJSON(w, http.StatusOK, s.openAPI(func(rt Route) bool {
		if rt.Permission != permPublic && rt.Permission != permSession && !p.Can(rt.Permission) {
			return false
		}
		return p.Scope == "" || s.tokenMayCall(p, rt, rt.Method)
	}))
}

// openAPI builds the document. keep decides which routes appear.
func (s *server) openAPI(keep func(Route) bool) map[string]any {
	paths := map[string]map[string]any{}
	table := s.routes()
	sort.SliceStable(table, func(i, j int) bool { return table[i].Path < table[j].Path })
	for _, rt := range table {
		if !keep(rt) {
			continue
		}
		doc := routeDocs[rt.Method+" "+rt.Path]
		op := map[string]any{
			"summary":      doc.Summary,
			"description":  describe(rt, doc, s.tokenMayCallAtAll(rt)),
			"x-permission": string(rt.Permission),
			"x-token":      s.tokenMayCallAtAll(rt),
			"responses":    map[string]any{"200": map[string]any{"description": answerText(doc)}},
		}
		var params []map[string]any
		for _, m := range pathParam.FindAllStringSubmatch(rt.Path, -1) {
			params = append(params, map[string]any{"name": m[1], "in": "path", "required": true, "schema": map[string]any{"type": "string"}})
		}
		for _, q := range doc.Query {
			name, text, _ := strings.Cut(q, ": ")
			params = append(params, map[string]any{"name": name, "in": "query", "description": text, "schema": map[string]any{"type": "string"}})
		}
		if len(params) > 0 {
			op["parameters"] = params
		}
		if doc.Body != "" {
			op["requestBody"] = map[string]any{"description": doc.Body, "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{}}}}
		}
		item := paths[rt.Path]
		if item == nil {
			item = map[string]any{}
			paths[rt.Path] = item
		}
		item[strings.ToLower(rt.Method)] = op
	}
	return map[string]any{
		"openapi": "3.1.0",
		"info":    map[string]any{"title": "Netscribe API", "version": s.version},
		"paths":   paths,
		"components": map[string]any{"securitySchemes": map[string]any{
			"token":   map[string]any{"type": "http", "scheme": "bearer", "description": "An access token from the Tokens page."},
			"session": map[string]any{"type": "apiKey", "in": "cookie", "name": "netscribe_session"},
		}},
	}
}

func answerText(d RouteDoc) string {
	if d.Answer != "" {
		return d.Answer
	}
	return "Success."
}

func describe(rt Route, d RouteDoc, tokenOK bool) string {
	var b strings.Builder
	switch rt.Permission {
	case permPublic:
		b.WriteString("Needs no sign in.")
	case permSession:
		b.WriteString("Needs a signed in session.")
	default:
		fmt.Fprintf(&b, "Needs the right %s.", rt.Permission)
	}
	if rt.Permission != permPublic {
		if tokenOK {
			b.WriteString(" An access token may call it.")
		} else {
			b.WriteString(" Not available to access tokens.")
		}
	}
	return b.String()
}

// tokenMayCallAtAll says whether any access token could call the route: it asks the
// strongest token there is, a write token of an administrator.
func (s *server) tokenMayCallAtAll(rt Route) bool {
	if rt.Permission == permPublic {
		return true
	}
	probe := auth.Principal{User: model.User{Role: model.RoleAdmin}, Scope: auth.ScopeWrite}
	if rt.Permission != permSession && !probe.Can(rt.Permission) {
		return false
	}
	return s.tokenMayCall(probe, rt, rt.Method)
}
