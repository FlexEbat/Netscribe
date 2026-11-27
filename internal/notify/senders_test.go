package notify

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FlexEbat/Netscribe/internal/config"
)

func TestWebhookPostsJSONAndRetriesOnA500(t *testing.T) {
	var hits atomic.Int32
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	if err := (webhook{url: srv.URL + "/secret-path", client: newClient(time.Second)}).Send(context.Background(), "s", "b"); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 1 || gotBody["subject"] != "s" || gotBody["text"] != "b" {
		t.Errorf("hits %d, body %v", hits.Load(), gotBody)
	}

	var bad atomic.Int32
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		bad.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failing.Close()
	n := New(fakeSource{}, nil, nil)
	n.retryDelay = time.Millisecond
	err := n.deliver(context.Background(), cfgChannel(failing.URL+"/secret-path"), "s", "b")
	if err == nil || bad.Load() != 2 {
		t.Errorf("err %v after %d calls, want an error after exactly 2", err, bad.Load())
	}
	if err != nil && (strings.Contains(err.Error(), "secret-path") || strings.Contains(err.Error(), failing.URL)) {
		t.Errorf("the error holds the address: %v", err)
	}
}

func TestWebhookDoesNotFollowARedirect(t *testing.T) {
	var elsewhere atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { elsewhere.Add(1) }))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusFound)
	}))
	defer srv.Close()
	err := (webhook{url: srv.URL, client: newClient(time.Second)}).Send(context.Background(), "s", "b")
	if err == nil || elsewhere.Load() != 0 {
		t.Errorf("err %v, the redirect target was hit %d times, want an error and 0", err, elsewhere.Load())
	}
}

func TestWebhookTimesOutWithoutNamingTheAddress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { time.Sleep(300 * time.Millisecond) }))
	defer srv.Close()
	start := time.Now()
	err := (webhook{url: srv.URL + "/secret-path", client: newClient(50 * time.Millisecond)}).Send(context.Background(), "s", "b")
	if err == nil || err.Error() != "timeout" || time.Since(start) > 250*time.Millisecond {
		t.Errorf("err %v after %v, want a plain timeout quickly", err, time.Since(start))
	}
}

func TestTelegramUsesPlainTextAndHidesTheTokenInErrors(t *testing.T) {
	var path string
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&got)
	}))
	tg := telegram{base: srv.URL, token: "123:SECRET", chat: "42", client: newClient(time.Second)}
	if err := tg.Send(context.Background(), "s", strings.Repeat("x", 5000)); err != nil {
		t.Fatal(err)
	}
	if path != "/bot123:SECRET/sendMessage" || got["chat_id"] != "42" || len([]rune(got["text"])) != telegramRunes {
		t.Errorf("path %q, body chat %q, text of %d runes", path, got["chat_id"], len([]rune(got["text"])))
	}
	if _, has := got["parse_mode"]; has {
		t.Error("a parse mode was sent")
	}
	srv.Close()
	if err := tg.Send(context.Background(), "s", "b"); err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Errorf("error after the server is gone: %v", err)
	}
}

// fakeSMTP greets, answers EHLO without STARTTLS and records every command after that.
func fakeSMTP(t *testing.T) (port int, commands func() []string) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	var seen atomic.Value
	seen.Store([]string{})
	go func() {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		_, _ = io.WriteString(conn, "220 test ESMTP\r\n")
		r := bufio.NewReader(conn)
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimSpace(line)
			seen.Store(append(seen.Load().([]string), line))
			switch {
			case strings.HasPrefix(line, "EHLO"):
				_, _ = io.WriteString(conn, "250 test\r\n") // no STARTTLS line
			case strings.HasPrefix(line, "QUIT"):
				_, _ = io.WriteString(conn, "221 bye\r\n")
				return
			default:
				_, _ = io.WriteString(conn, "250 ok\r\n")
			}
		}
	}()
	return l.Addr().(*net.TCPAddr).Port, func() []string { return seen.Load().([]string) }
}

func TestEmailWithoutStartTLSSendsNothing(t *testing.T) {
	port, commands := fakeSMTP(t)
	e := email{host: "127.0.0.1", port: port, user: "u", password: "SECRET-PW", from: "n@example.org", to: []string{"a@example.org"},
		dial: (&net.Dialer{Timeout: time.Second}).DialContext}
	err := e.Send(context.Background(), "s", "b")
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("err = %v, want a refusal because of STARTTLS", err)
	}
	time.Sleep(50 * time.Millisecond)
	for _, c := range commands() {
		if strings.HasPrefix(c, "MAIL") || strings.HasPrefix(c, "AUTH") || strings.HasPrefix(c, "RCPT") || strings.HasPrefix(c, "DATA") {
			t.Errorf("the client sent %q to a server without STARTTLS", c)
		}
	}
}

func TestMessageHasSafeHeaders(t *testing.T) {
	from, err := mail.ParseAddress("Net Scribe <n@example.org>")
	if err != nil {
		t.Fatal(err)
	}
	to, err := mail.ParseAddress("a@example.org")
	if err != nil {
		t.Fatal(err)
	}
	m := message(from, []*mail.Address{to}, "Subject\r\nBcc: evil@example.org", "line1\nline2")
	head := strings.SplitN(m, "\r\n\r\n", 2)[0]
	if strings.Contains(head, "\r\nBcc:") {
		t.Errorf("the subject injected a header:\n%s", head)
	}
	if !strings.Contains(m, "line1\r\nline2\r\n") {
		t.Errorf("body not CRLF: %q", m)
	}
}

func cfgChannel(url string) config.ChannelConfig {
	return config.ChannelConfig{Name: "x", Type: config.ChannelWebhook, URL: config.Secret(url)}
}

// captured is what an in-process server saw.
type captured struct {
	path, auth, gotifyKey, contentType string
	body                               map[string]any
	query                              string
}

func capture(t *testing.T) (*httptest.Server, *captured) {
	t.Helper()
	c := &captured{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.path, c.query = r.URL.Path, r.URL.RawQuery
		c.auth, c.gotifyKey, c.contentType = r.Header.Get("Authorization"), r.Header.Get("X-Gotify-Key"), r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&c.body)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, c
}

func TestNtfyPublishesJSONWithTheTopicInTheBody(t *testing.T) {
	srv, got := capture(t)
	n := ntfy{url: srv.URL, topic: "lan-alerts", token: "tk_secret", client: newClient(time.Second)}
	if err := n.Send(context.Background(), "Subject", "line one\nline two"); err != nil {
		t.Fatal(err)
	}
	if got.path != "/" || got.query != "" {
		t.Errorf("the topic or a token is in the URL: %q?%q", got.path, got.query)
	}
	if got.body["topic"] != "lan-alerts" || got.body["title"] != "Subject" || got.body["message"] != "line one\nline two" {
		t.Errorf("body = %v", got.body)
	}
	if got.auth != "Bearer tk_secret" || got.contentType != "application/json" {
		t.Errorf("auth %q, content type %q", got.auth, got.contentType)
	}
}

func TestNtfyWithoutATokenSendsNoAuthorization(t *testing.T) {
	srv, got := capture(t)
	if err := (ntfy{url: srv.URL, topic: "t", client: newClient(time.Second)}).Send(context.Background(), "s", "b"); err != nil {
		t.Fatal(err)
	}
	if got.auth != "" {
		t.Errorf("Authorization = %q", got.auth)
	}
}

func TestNtfyCutsALongMessageWithoutBreakingACharacter(t *testing.T) {
	srv, got := capture(t)
	long := strings.Repeat("ж", 3000) // 6000 bytes
	if err := (ntfy{url: srv.URL, topic: "t", client: newClient(time.Second)}).Send(context.Background(), "s", long); err != nil {
		t.Fatal(err)
	}
	msg, _ := got.body["message"].(string)
	if len(msg) > ntfyBytes || len(msg) < ntfyBytes-1 || strings.ContainsRune(msg, '\uFFFD') || len([]rune(msg))*2 != len(msg) {
		t.Errorf("message of %d bytes and %d characters", len(msg), len([]rune(msg)))
	}
}

func TestGotifyPostsToMessageWithTheKeyInAHeader(t *testing.T) {
	srv, got := capture(t)
	g := gotify{url: srv.URL + "/", token: "gotify-key", client: newClient(time.Second)}
	if err := g.Send(context.Background(), "Subject", "Body"); err != nil {
		t.Fatal(err)
	}
	if got.path != "/message" || got.query != "" {
		t.Errorf("request to %q?%q, want /message with no query", got.path, got.query)
	}
	if got.gotifyKey != "gotify-key" {
		t.Errorf("X-Gotify-Key = %q", got.gotifyKey)
	}
	if got.body["title"] != "Subject" || got.body["message"] != "Body" || got.body["priority"] != float64(5) {
		t.Errorf("body = %v", got.body)
	}
}

func TestWebhookFormatsSendOneTextFieldInTheShapeOfTheService(t *testing.T) {
	for _, tc := range []struct {
		format, field string
		keys          []string
	}{
		{"slack", "text", []string{"text"}},
		{"teams", "text", []string{"text"}},
		{"discord", "content", []string{"content"}},
	} {
		t.Run(tc.format, func(t *testing.T) {
			srv, got := capture(t)
			w := webhook{url: srv.URL + "/hook", format: tc.format, client: newClient(time.Second)}
			if err := w.Send(context.Background(), "Subject", "Body"); err != nil {
				t.Fatal(err)
			}
			if got.path != "/hook" || got.contentType != "application/json" {
				t.Errorf("path %q, content type %q", got.path, got.contentType)
			}
			if len(got.body) != len(tc.keys) || got.body[tc.field] != "Subject\nBody" {
				t.Errorf("body = %v, want only %s = %q", got.body, tc.field, "Subject\nBody")
			}
		})
	}
}

func TestWebhookWithoutAFormatKeepsTheOldBody(t *testing.T) {
	for _, format := range []string{"", "json"} {
		srv, got := capture(t)
		if err := (webhook{url: srv.URL, format: format, client: newClient(time.Second)}).Send(context.Background(), "s", "b"); err != nil {
			t.Fatal(err)
		}
		if len(got.body) != 2 || got.body["subject"] != "s" || got.body["text"] != "b" {
			t.Errorf("format %q: body = %v", format, got.body)
		}
	}
}

func TestDiscordTextIsCutBelowItsLimit(t *testing.T) {
	srv, got := capture(t)
	w := webhook{url: srv.URL, format: "discord", client: newClient(time.Second)}
	if err := w.Send(context.Background(), "s", strings.Repeat("x", 5000)); err != nil {
		t.Fatal(err)
	}
	if c, _ := got.body["content"].(string); len([]rune(c)) != discordRunes {
		t.Errorf("content has %d characters, want %d", len([]rune(c)), discordRunes)
	}
}

func TestNewChannelsFailWithoutAnAddressInTheError(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) }))
	defer failing.Close()
	n := New(fakeSource{}, nil, nil)
	n.retryDelay = time.Millisecond
	for _, ch := range []config.ChannelConfig{
		{Name: "p", Type: config.ChannelNtfy, URL: config.Secret(failing.URL), Topic: "topic-secret", Token: "token-secret"},
		{Name: "g", Type: config.ChannelGotify, URL: config.Secret(failing.URL + "/gotify-secret-path"), Token: "token-secret"},
		{Name: "s", Type: config.ChannelWebhook, Format: "slack", URL: config.Secret(failing.URL + "/slack-secret-path")},
	} {
		err := n.deliver(context.Background(), ch, "s", "b")
		if err == nil {
			t.Fatalf("%s: want an error", ch.Name)
		}
		for _, secret := range []string{"topic-secret", "token-secret", "secret-path", failing.URL} {
			if strings.Contains(err.Error(), secret) {
				t.Errorf("%s: the error holds %q: %v", ch.Name, secret, err)
			}
		}
	}
}

func TestNewSenderPicksTheSenderOfEachType(t *testing.T) {
	for typ, want := range map[string]string{
		config.ChannelWebhook: "notify.webhook", config.ChannelNtfy: "notify.ntfy", config.ChannelGotify: "notify.gotify",
		config.ChannelTelegram: "notify.telegram", config.ChannelEmail: "notify.email",
	} {
		if got := fmt.Sprintf("%T", newSender(config.ChannelConfig{Type: typ})); got != want {
			t.Errorf("%s: %s, want %s", typ, got, want)
		}
	}
}
