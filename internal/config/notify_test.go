package config

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func webhook(name string) ChannelConfig {
	return ChannelConfig{Name: name, Type: ChannelWebhook, URL: "https://example.org/hook"}
}

func TestNotifyBlockIsValidatedAsAWhole(t *testing.T) {
	email := ChannelConfig{Name: "mail", Type: ChannelEmail, Host: "smtp.example.org", Port: 587,
		From: "netscribe@example.org", To: []string{"admin@example.org"}}
	tests := []struct {
		name string
		edit func(*NotifyConfig)
		want string // part of the error, empty means valid
	}{
		{"defaults", func(*NotifyConfig) {}, ""},
		{"one of each type", func(n *NotifyConfig) {
			n.Channels = []ChannelConfig{webhook("ops"), {Name: "phone", Type: ChannelTelegram, BotToken: "t", ChatID: "1"}, email}
		}, ""},
		{"unknown event", func(n *NotifyConfig) { n.Events = []string{"device_exploded"} }, "notify.events[0]: unknown event"},
		{"event listed twice", func(n *NotifyConfig) { n.Events = []string{EventScanFailed, EventScanFailed} }, "notify.events[1]: listed twice"},
		{"duplicate name", func(n *NotifyConfig) { n.Channels = []ChannelConfig{webhook("ops"), webhook("ops")} }, "notify.channels[1]: name is used twice"},
		{"name with a space", func(n *NotifyConfig) { n.Channels = []ChannelConfig{webhook("my hook")} }, "name must be"},
		{"name of 33 characters", func(n *NotifyConfig) { n.Channels = []ChannelConfig{webhook(strings.Repeat("a", 33))} }, "name must be"},
		{"unknown type", func(n *NotifyConfig) { n.Channels = []ChannelConfig{{Name: "x", Type: "sms"}} }, "type must be"},
		{"webhook without a url", func(n *NotifyConfig) { n.Channels = []ChannelConfig{{Name: "x", Type: ChannelWebhook}} }, "url must be"},
		{"webhook with a file url", func(n *NotifyConfig) {
			n.Channels = []ChannelConfig{{Name: "x", Type: ChannelWebhook, URL: "file:///etc/passwd"}}
		}, "url must be"},
		{"telegram without a chat", func(n *NotifyConfig) {
			n.Channels = []ChannelConfig{{Name: "x", Type: ChannelTelegram, BotToken: "t"}}
		}, "botToken and chatId"},
		{"email without a host", func(n *NotifyConfig) { c := email; c.Host = ""; n.Channels = []ChannelConfig{c} }, "host and a port"},
		{"email port 70000", func(n *NotifyConfig) { c := email; c.Port = 70000; n.Channels = []ChannelConfig{c} }, "host and a port"},
		{"email user without a password", func(n *NotifyConfig) { c := email; c.User = "u"; n.Channels = []ChannelConfig{c} }, "go together"},
		{"email from is not an address", func(n *NotifyConfig) { c := email; c.From = "nobody"; n.Channels = []ChannelConfig{c} }, "from is not an address"},
		{"email without recipients", func(n *NotifyConfig) { c := email; c.To = nil; n.Channels = []ChannelConfig{c} }, "to needs"},
		{"email recipient is not an address", func(n *NotifyConfig) { c := email; c.To = []string{"x"}; n.Channels = []ChannelConfig{c} }, "not an address"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := valid()
			tc.edit(&c.Notify)
			err := c.Validate()
			switch {
			case tc.want == "" && err != nil:
				t.Fatalf("Validate() = %v, want nil", err)
			case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
				t.Fatalf("Validate() = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestNotifySecretsNeverShowUp(t *testing.T) {
	c := valid()
	c.Notify.Channels = []ChannelConfig{
		{Name: "ops", Type: ChannelWebhook, URL: "https://example.org/hook-secret-path"},
		{Name: "phone", Type: ChannelTelegram, BotToken: "bot-secret", ChatID: "chat-secret"},
		{Name: "mail", Type: ChannelEmail, Host: "h", Port: 25, User: "u", Password: "pw-secret", From: "a@b.c", To: []string{"d@e.f"}},
	}
	raw, err := json.Marshal(c.Notify)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{string(raw), fmt.Sprintf("%#v|%+v|%v", c.Notify, c.Notify, c.Notify)} {
		for _, secret := range []string{"hook-secret-path", "bot-secret", "chat-secret", "pw-secret"} {
			if strings.Contains(s, secret) {
				t.Errorf("%q leaked into %s", secret, s)
			}
		}
	}
}

func TestDefaultPortsLetTheRiskRulesFire(t *testing.T) {
	have := map[int]bool{}
	for _, p := range defaults().Scan.Ports {
		have[p] = true
	}
	for _, p := range []int{21, 23, 445, 3389, 5900} {
		if !have[p] {
			t.Errorf("default scan.ports lacks %d", p)
		}
	}
}
