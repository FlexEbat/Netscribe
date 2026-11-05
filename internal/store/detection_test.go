package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/FlexEbat/Netscribe/internal/collector"
	"github.com/FlexEbat/Netscribe/internal/model"
)

func typed(mac, ip string, kind model.DeviceKind, score int, why ...string) model.DeviceInput {
	d := dev(mac, ip)
	d.Kind, d.KindScore, d.KindReasons = kind, score, why
	return d
}

func TestTheKindComesWithItsScoreAndReasons(t *testing.T) {
	s := newTestStore(t)
	scanOnce(t, s, lan, typed("aa:aa:aa:aa:aa:01", "192.168.1.1", model.KindPrinter, 90, "port 9100", "HTTP banner: cups"))

	d := byMAC(t, s, "aa:aa:aa:aa:aa:01")
	if d.Kind != model.KindPrinter || d.KindScore != 90 || len(d.KindReasons) != 2 || d.KindReasons[0] != "port 9100" {
		t.Errorf("device = %+v, want a printer with score 90 and its two reasons", d)
	}
}

func TestAnUnknownKindStoresItsCandidatesAndNeverEatsAKnownKind(t *testing.T) {
	s := newTestStore(t)
	mac := "aa:aa:aa:aa:aa:02"
	scanOnce(t, s, lan, typed(mac, "192.168.1.2", "", 0, "candidate camera (45)", "candidate server (45)"))
	d := byMAC(t, s, mac)
	if d.Kind != model.KindUnknown || d.KindScore != 0 || len(d.KindReasons) != 2 {
		t.Fatalf("device = %+v, want unknown with the two candidates", d)
	}

	scanOnce(t, s, lan, typed(mac, "192.168.1.2", model.KindCamera, 95, "port 554", "vendor: hikvision"))
	d = byMAC(t, s, mac)
	if d.Kind != model.KindCamera || d.KindScore != 95 || d.KindReasons[0] != "port 554" {
		t.Fatalf("device = %+v, want the camera with its reasons", d)
	}

	scanOnce(t, s, lan, typed(mac, "192.168.1.2", "", 0, "candidate server (50)"))
	d = byMAC(t, s, mac)
	if d.Kind != model.KindCamera || d.KindScore != 95 || d.KindReasons[0] != "port 554" {
		t.Errorf("device = %+v, want a later unknown to leave the camera and its reasons alone", d)
	}
}

func TestReasonsAreCappedAndCleaned(t *testing.T) {
	s := newTestStore(t)
	var why []string
	for i := 0; i < 9; i++ {
		why = append(why, "reason\x07 "+strings.Repeat("x", 200))
	}
	scanOnce(t, s, lan, typed("aa:aa:aa:aa:aa:03", "192.168.1.3", model.KindServer, 500, why...))

	d := byMAC(t, s, "aa:aa:aa:aa:aa:03")
	if len(d.KindReasons) != 5 || d.KindScore != 100 {
		t.Fatalf("reasons = %d, score = %d, want 5 and the score capped at 100", len(d.KindReasons), d.KindScore)
	}
	for _, r := range d.KindReasons {
		if len([]rune(r)) > 80 || strings.ContainsRune(r, '\x07') {
			t.Errorf("reason %q is not cleaned and cut to 80 characters", r)
		}
	}
}

func TestAKindAPersonSetSurvivesEveryScan(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	mac := "aa:aa:aa:aa:aa:04"
	scanOnce(t, s, lan, typed(mac, "192.168.1.4", model.KindPrinter, 60, "port 9100"))
	d := byMAC(t, s, mac)
	kind := model.KindNAS
	if _, err := s.UpdateDevice(ctx, d.ID, model.DeviceEdit{Kind: &kind, Version: d.Version}, 0); err != nil {
		t.Fatal(err)
	}
	scanOnce(t, s, lan, typed(mac, "192.168.1.4", model.KindCamera, 99, "port 554"))
	scanOnce(t, s, lan, typed(mac, "192.168.1.4", "", 0, "candidate server (50)"))

	got := byMAC(t, s, mac)
	if got.Kind != model.KindNAS || !got.KindLocked || got.KindScore != 60 || got.KindReasons[0] != "port 9100" {
		t.Errorf("device = %+v, want the person's kind and the old reasons untouched", got)
	}
}

// testNow is where the clock of newTestStore stands during the tests.
var testNow = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

func cert(subject, fingerprint string, notAfter time.Time) *model.TLSCert {
	return &model.TLSCert{
		Subject: subject, SANs: []string{subject}, Issuer: subject,
		NotBefore: notAfter.Add(-365 * 24 * time.Hour), NotAfter: notAfter, Fingerprint: fingerprint,
	}
}

func fp(c string) string { return strings.Repeat(c, 64) }

func scanServices(t *testing.T, s *Store, key model.DeviceKey, svc ...model.ServiceInput) {
	t.Helper()
	ctx := context.Background()
	sc, err := s.StartScan(ctx, lan)
	if err != nil {
		t.Fatal(err)
	}
	res := collector.Result{Devices: []model.DeviceInput{{Key: key}}, Services: svc, ServicesPolled: []model.DeviceKey{key}}
	if err := s.ApplyResult(ctx, sc.ID, res); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishScan(ctx, sc.ID, "done", ""); err != nil {
		t.Fatal(err)
	}
}

func tlsChangesOf(t *testing.T, s *Store, kind string) []model.Change {
	t.Helper()
	all, err := s.ListChanges(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	var out []model.Change
	for _, c := range all {
		if c.Kind == kind {
			out = append(out, c)
		}
	}
	return out
}

func TestACertificateIsStoredWithTheServiceAndKeptWhenAHandshakeFails(t *testing.T) {
	s := newTestStore(t)
	key := model.DeviceKey{MAC: "aa:aa:aa:aa:aa:05", IP: "192.168.1.5"}
	end := testNow.Add(300 * 24 * time.Hour).Truncate(time.Second)
	scanServices(t, s, key, model.ServiceInput{Device: key, Port: 443, Proto: "tcp", Name: "https", TLS: cert("nas.lan", fp("a"), end)})
	scanServices(t, s, key, model.ServiceInput{Device: key, Port: 443, Proto: "tcp", Name: "https"}) // the handshake failed this time

	got, err := s.ListServices(context.Background(), byMAC(t, s, key.MAC).ID)
	if err != nil || len(got) != 1 {
		t.Fatalf("services = %+v, %v", got, err)
	}
	c := got[0].TLS
	if c == nil || c.Subject != "nas.lan" || c.Fingerprint != fp("a") || !c.NotAfter.Equal(end) || len(c.SANs) != 1 {
		t.Errorf("certificate = %+v, want the stored one kept", c)
	}
}

func TestACertificateIsNormalizedAndCappedByTheStore(t *testing.T) {
	s := newTestStore(t)
	key := model.DeviceKey{MAC: "aa:aa:aa:aa:aa:06", IP: "192.168.1.6"}
	var sans []string
	for i := 0; i < 100; i++ {
		sans = append(sans, "h"+strings.Repeat("n", 300))
	}
	c := cert("evil\x07"+strings.Repeat("a", 10<<10), "ABCDEF"+strings.Repeat("0", 58), testNow.Add(time.Hour))
	c.SANs = sans
	scanServices(t, s, key, model.ServiceInput{Device: key, Port: 443, Proto: "tcp", Name: "https", TLS: c})

	got, _ := s.ListServices(context.Background(), byMAC(t, s, key.MAC).ID)
	if len(got) != 1 || got[0].TLS == nil {
		t.Fatalf("services = %+v", got)
	}
	tc := got[0].TLS
	if len([]rune(tc.Subject)) > 200 || strings.ContainsRune(tc.Subject, '\x07') || len(tc.SANs) != 5 || len([]rune(tc.SANs[0])) > 200 {
		t.Errorf("certificate = %d character subject, %d names: want it normalized and capped", len([]rune(tc.Subject)), len(tc.SANs))
	}
	if tc.Fingerprint != strings.ToLower(tc.Fingerprint) {
		t.Errorf("fingerprint %q is not lower case", tc.Fingerprint)
	}
}

func TestACertificateWithoutAValidFingerprintIsNotStored(t *testing.T) {
	s := newTestStore(t)
	key := model.DeviceKey{MAC: "aa:aa:aa:aa:aa:07", IP: "192.168.1.7"}
	c := cert("x", "not-hex", testNow.Add(time.Hour))
	scanServices(t, s, key, model.ServiceInput{Device: key, Port: 443, Proto: "tcp", Name: "https", TLS: c})
	got, _ := s.ListServices(context.Background(), byMAC(t, s, key.MAC).ID)
	if len(got) != 1 || got[0].TLS != nil {
		t.Errorf("services = %+v, want the service without a certificate", got)
	}
}

func TestAChangedFingerprintWritesExactlyOneEntryAndTheSameDataAgainNone(t *testing.T) {
	s := newTestStore(t)
	key := model.DeviceKey{MAC: "aa:aa:aa:aa:aa:08", IP: "192.168.1.8"}
	end := testNow.Add(300 * 24 * time.Hour)
	svc := func(c *model.TLSCert) model.ServiceInput {
		return model.ServiceInput{Device: key, Port: 443, Proto: "tcp", Name: "https", TLS: c}
	}
	scanServices(t, s, key, svc(cert("old.lan", fp("a"), end)))
	if got := tlsChangesOf(t, s, ChangeTLSChanged); len(got) != 0 {
		t.Fatalf("the first certificate wrote %d tls_changed entries, want none", len(got))
	}
	scanServices(t, s, key, svc(cert("new.lan", fp("b"), end)))
	got := tlsChangesOf(t, s, ChangeTLSChanged)
	if len(got) != 1 || !strings.Contains(got[0].Summary, "old.lan -> new.lan") {
		t.Fatalf("entries = %+v, want exactly one naming old.lan and new.lan", got)
	}
	scanServices(t, s, key, svc(cert("new.lan", fp("b"), end)))
	scanServices(t, s, key, svc(cert("new.lan", fp("b"), end)))
	if got := tlsChangesOf(t, s, ChangeTLSChanged); len(got) != 1 {
		t.Errorf("the same data again wrote %d entries in all, want still 1", len(got))
	}
}

func TestACertificateEndingSoonIsReportedOnceUntilItChanges(t *testing.T) {
	s := newTestStore(t)
	key := model.DeviceKey{MAC: "aa:aa:aa:aa:aa:09", IP: "192.168.1.9"}
	svc := func(c *model.TLSCert) model.ServiceInput {
		return model.ServiceInput{Device: key, Port: 8443, Proto: "tcp", Name: "https-alt", TLS: c}
	}
	soon := testNow.Add(5 * 24 * time.Hour)
	scanServices(t, s, key, svc(cert("a.lan", fp("c"), soon)))
	scanServices(t, s, key, svc(cert("a.lan", fp("c"), soon)))
	if got := tlsChangesOf(t, s, ChangeTLSExpiring); len(got) != 1 {
		t.Fatalf("entries = %+v, want one for a certificate ending in 5 days seen twice", got)
	}
	scanServices(t, s, key, svc(cert("a.lan", fp("d"), testNow.Add(6*24*time.Hour))))
	if got := tlsChangesOf(t, s, ChangeTLSExpiring); len(got) != 2 {
		t.Errorf("entries = %d, want a second one for the new certificate that also ends soon", len(got))
	}
}

func TestACertificateFarFromItsEndWritesNoExpiryEntry(t *testing.T) {
	s := newTestStore(t)
	key := model.DeviceKey{MAC: "aa:aa:aa:aa:aa:0a", IP: "192.168.1.10"}
	scanServices(t, s, key, model.ServiceInput{Device: key, Port: 443, Proto: "tcp", Name: "https", TLS: cert("b.lan", fp("e"), testNow.Add(15*24*time.Hour))})
	if got := tlsChangesOf(t, s, ChangeTLSExpiring); len(got) != 0 {
		t.Errorf("entries = %+v, want none for a certificate ending in 15 days", got)
	}
}
