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
		{"ntfy", func(n *NotifyConfig) {
			n.Channels = []ChannelConfig{{Name: "p", Type: ChannelNtfy, URL: "https://ntfy.sh", Topic: "my_topic-1"}}
		}, ""},
		{"ntfy with a token", func(n *NotifyConfig) {
			n.Channels = []ChannelConfig{{Name: "p", Type: ChannelNtfy, URL: "http://ntfy.lan:8080", Topic: "t", Token: "tk_abc"}}
		}, ""},
		{"ntfy without a topic", func(n *NotifyConfig) {
			n.Channels = []ChannelConfig{{Name: "p", Type: ChannelNtfy, URL: "https://ntfy.sh"}}
		}, "topic must be"},
		{"ntfy topic with a slash", func(n *NotifyConfig) {
			n.Channels = []ChannelConfig{{Name: "p", Type: ChannelNtfy, URL: "https://ntfy.sh", Topic: "a/b"}}
		}, "topic must be"},
		{"ntfy with a file url", func(n *NotifyConfig) {
			n.Channels = []ChannelConfig{{Name: "p", Type: ChannelNtfy, URL: "file:///etc/passwd", Topic: "t"}}
		}, "url must be"},
		{"gotify", func(n *NotifyConfig) {
			n.Channels = []ChannelConfig{{Name: "g", Type: ChannelGotify, URL: "https://gotify.lan", Token: "AbC"}}
		}, ""},
		{"gotify without a token", func(n *NotifyConfig) {
			n.Channels = []ChannelConfig{{Name: "g", Type: ChannelGotify, URL: "https://gotify.lan"}}
		}, "token is required"},
		{"gotify with a gopher url", func(n *NotifyConfig) {
			n.Channels = []ChannelConfig{{Name: "g", Type: ChannelGotify, URL: "gopher://gotify.lan", Token: "t"}}
		}, "url must be"},
		{"webhook formats", func(n *NotifyConfig) {
			for _, f := range []string{"", FormatJSON, FormatSlack, FormatDiscord, FormatTeams} {
				c := webhook("h" + f)
				c.Format = f
				n.Channels = append(n.Channels, c)
			}
		}, ""},
		{"webhook with an unknown format", func(n *NotifyConfig) {
			c := webhook("h")
			c.Format = "mattermost"
			n.Channels = []ChannelConfig{c}
		}, "format must be"},
		{"unknown type now lists the new ones", func(n *NotifyConfig) { n.Channels = []ChannelConfig{{Name: "x", Type: "sms"}} }, "ntfy or gotify"},
		{"every new event", func(n *NotifyConfig) {
			n.Events = []string{EventDeviceOnline, EventTLSExpiring, EventAddressChanged, EventLinkDown, EventDeviceFlapping}
		}, ""},
		{"flap count 1", func(n *NotifyConfig) { n.FlapCount = 1 }, "notify.flapCount"},
		{"flap count 21", func(n *NotifyConfig) { n.FlapCount = 21 }, "notify.flapCount"},
		{"flap count 2", func(n *NotifyConfig) { n.FlapCount = 2 }, ""},
		{"flap count 20", func(n *NotifyConfig) { n.FlapCount = 20 }, ""},
		{"quiet hours across midnight", func(n *NotifyConfig) { n.QuietHours = "22:00-07:00" }, ""},
		{"quiet hours with the same times", func(n *NotifyConfig) { n.QuietHours = "22:00-22:00" }, "notify.quietHours"},
		{"quiet hours that are not times", func(n *NotifyConfig) { n.QuietHours = "night" }, "notify.quietHours"},
		{"a maintenance window", func(n *NotifyConfig) {
			n.Maintenance = []MaintenanceWindow{{From: "2026-12-01T22:00", To: "2026-12-02T02:00", Group: "Lab"}}
		}, ""},
		{"a maintenance window of the whole network", func(n *NotifyConfig) {
			n.Maintenance = []MaintenanceWindow{{From: "2026-12-01T22:00", To: "2026-12-02T02:00"}}
		}, ""},
		{"maintenance that ends before it starts", func(n *NotifyConfig) {
			n.Maintenance = []MaintenanceWindow{{From: "2026-12-02T02:00", To: "2026-12-01T22:00"}}
		}, "notify.maintenance[0]: to must be after from"},
		{"maintenance with a bad time", func(n *NotifyConfig) {
			n.Maintenance = []MaintenanceWindow{{From: "tomorrow", To: "2026-12-02T02:00"}}
		}, "from must look like"},
		{"maintenance for 32 days", func(n *NotifyConfig) {
			n.Maintenance = []MaintenanceWindow{{From: "2026-12-01T00:00", To: "2027-01-02T00:01"}}
		}, "31 days"},
		{"maintenance for 31 days", func(n *NotifyConfig) {
			n.Maintenance = []MaintenanceWindow{{From: "2026-12-01T00:00", To: "2027-01-01T00:00"}}
		}, ""},
		{"maintenance with a control character in the group", func(n *NotifyConfig) {
			n.Maintenance = []MaintenanceWindow{{From: "2026-12-01T22:00", To: "2026-12-02T02:00", Group: "a\x07b"}}
		}, "group must be"},
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
		{Name: "push", Type: ChannelNtfy, URL: "https://ntfy-secret.example.org", Topic: "topic-secret", Token: "ntfy-token-secret"},
		{Name: "gotify", Type: ChannelGotify, URL: "https://gotify-secret.example.org", Token: "gotify-token-secret"},
	}
	raw, err := json.Marshal(c.Notify)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{string(raw), fmt.Sprintf("%#v|%+v|%v", c.Notify, c.Notify, c.Notify)} {
		for _, secret := range []string{"hook-secret-path", "bot-secret", "chat-secret", "pw-secret", "ntfy-secret", "topic-secret", "ntfy-token-secret", "gotify-secret", "gotify-token-secret"} {
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
