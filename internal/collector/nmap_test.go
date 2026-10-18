package collector

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/FlexEbat/Netscribe/internal/config"
	"github.com/FlexEbat/Netscribe/internal/model"
)

const nmapSample = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE nmaprun>
<nmaprun scanner="nmap">
<host><status state="up"/><address addr="192.168.1.9" addrtype="ipv4"/>
<os>
<osmatch name="MikroTik RouterOS 7.2 - 7.5" accuracy="97" line="1">
<osclass type="router" vendor="MikroTik" osfamily="RouterOS" osgen="7.X" accuracy="97"/>
<osclass type="general purpose" vendor="Linux" osfamily="Linux" accuracy="90"/>
</osmatch>
<osmatch name="Linux 5.4" accuracy="88" line="2">
<osclass type="general purpose" vendor="Linux" osfamily="Linux" accuracy="88"/>
</osmatch>
</os>
</host>
</nmaprun>`

// fakeNmap records every run and answers from a function.
type fakeNmap struct {
	mu    sync.Mutex
	paths []string
	calls [][]string
	reply func(args []string) ([]byte, error)
}

func (f *fakeNmap) run(_ context.Context, path string, args []string) ([]byte, error) {
	f.mu.Lock()
	f.paths = append(f.paths, path)
	f.calls = append(f.calls, args)
	f.mu.Unlock()
	if f.reply == nil {
		return []byte(nmapSample), nil
	}
	return f.reply(args)
}

func (f *fakeNmap) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func newFakeNmapCollector(f *fakeNmap, found string) *Nmap {
	c := NewNmap(quietLogger())
	c.run = f.run
	c.lookPath = func(string) (string, error) {
		if found == "" {
			return "", errors.New("not found")
		}
		return found, nil
	}
	return c
}

func nmapInput(on bool, path string, ips ...string) Input {
	cfg := testConfig()
	cfg.Discovery = config.DiscoveryConfig{Nmap: on, NmapPath: path}
	return Input{Devices: liveDevices(ips...), Config: cfg}
}

func TestParseNmapTakesTheBestGuessOnly(t *testing.T) {
	got, err := parseNmap([]byte(nmapSample))
	if err != nil {
		t.Fatal(err)
	}
	want := "os=mikrotik routeros 7.2 - 7.5,type=general purpose,type=router"
	if strings.Join(got.facts, ",") != want {
		t.Errorf("facts = %v, want %q", got.facts, want)
	}
	if got.os != "MikroTik RouterOS 7.2 - 7.5" {
		t.Errorf("os = %q, want the name as nmap wrote it", got.os)
	}
}

func TestParseNmapIgnoresGuessesBelowTheAccuracyLimit(t *testing.T) {
	low := strings.ReplaceAll(nmapSample, `accuracy="97"`, `accuracy="60"`)
	low = strings.ReplaceAll(low, `accuracy="90"`, `accuracy="60"`)
	low = strings.ReplaceAll(low, `accuracy="88"`, `accuracy="60"`)
	got, err := parseNmap([]byte(low))
	if err != nil || len(got.facts) != 0 || got.os != "" {
		t.Errorf("guess = %+v, err = %v, want nothing from weak guesses", got, err)
	}
}

func TestParseNmapRefusesBrokenAndHostileText(t *testing.T) {
	if _, err := parseNmap([]byte("<nmaprun><host>")); err == nil {
		t.Error("a cut XML document was accepted")
	}
	if _, err := parseNmap([]byte("not xml at all")); err == nil {
		t.Error("text that is not XML was accepted")
	}
	hostile := `<nmaprun><host><os><osmatch name="Evil&#9;OS ` + strings.Repeat("x", 300) + `" accuracy="99">` +
		`<osclass type="router&#1;" accuracy="99"/></osmatch></os></host></nmaprun>`
	got, err := parseNmap([]byte(hostile))
	if err != nil {
		// A control character that XML cannot hold is refused as a whole, which is fine.
		return
	}
	for _, f := range append(got.facts, got.os) {
		if len([]rune(f)) > 110 || strings.ContainsAny(f, "\t\x01") {
			t.Errorf("fact %q was not cleaned or cut", f)
		}
	}
}

func TestNmapRunsOnlyWithFixedArgumentsAndAParsedAddress(t *testing.T) {
	f := &fakeNmap{}
	c := newFakeNmapCollector(f, "/usr/bin/nmap")
	in := nmapInput(true, "", "192.168.1.9", "fe80::1", "not-an-ip", "1.2.3.4; reboot")

	if _, err := c.Collect(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if f.count() != 1 {
		t.Fatalf("nmap ran %d times, want once: only the IPv4 address is asked", f.count())
	}
	args := f.calls[0]
	if args[len(args)-1] != "192.168.1.9" {
		t.Errorf("last argument = %q, want the address", args[len(args)-1])
	}
	for _, want := range []string{"-O", "-n", "-Pn", "-oX"} {
		found := false
		for _, a := range args {
			found = found || a == want
		}
		if !found {
			t.Errorf("arguments %v lack %s", args, want)
		}
	}
	if f.paths[0] != "/usr/bin/nmap" {
		t.Errorf("path = %q, want the one found in PATH", f.paths[0])
	}
	if got := c.guess("192.168.1.9"); len(got) == 0 {
		t.Error("the guess is not kept for the classifier")
	}
}

func TestNmapUsesThePathOfTheConfig(t *testing.T) {
	f := &fakeNmap{}
	c := newFakeNmapCollector(f, "") // PATH has no nmap
	if _, err := c.Collect(context.Background(), nmapInput(true, "/opt/nmap/bin/nmap", "192.168.1.9")); err != nil {
		t.Fatal(err)
	}
	if f.count() != 1 || f.paths[0] != "/opt/nmap/bin/nmap" {
		t.Errorf("runs = %d, paths = %v, want one run of the configured path", f.count(), f.paths)
	}
}

func TestNmapDoesNothingWhenOffOrMissing(t *testing.T) {
	f := &fakeNmap{}
	if _, err := newFakeNmapCollector(f, "/usr/bin/nmap").Collect(context.Background(), nmapInput(false, "", "192.168.1.9")); err != nil || f.count() != 0 {
		t.Errorf("err = %v, runs = %d, want nothing run while the switch is off", err, f.count())
	}
	res, err := newFakeNmapCollector(f, "").Collect(context.Background(), nmapInput(true, "", "192.168.1.9"))
	if err != nil || f.count() != 0 || len(res.Devices) != 0 {
		t.Errorf("err = %v, runs = %d, want a missing nmap to be skipped without an error", err, f.count())
	}
}

func TestNmapStopsAfterRepeatedFailuresButSurvivesOne(t *testing.T) {
	failing := &fakeNmap{reply: func([]string) ([]byte, error) { return nil, errors.New("requires root privileges") }}
	ips := make([]string, 20)
	for i := range ips {
		ips[i] = "10.0.0." + strconv.Itoa(i+1)
	}
	c := newFakeNmapCollector(failing, "/usr/bin/nmap")
	if _, err := c.Collect(context.Background(), nmapInput(true, "", ips...)); err != nil {
		t.Fatal(err)
	}
	if n := failing.count(); n > nmapGiveUpAfter+nmapConcurrency {
		t.Errorf("nmap ran %d times, want the stage to stop after %d failures", n, nmapGiveUpAfter)
	}

	// One failure among successes does not stop the stage.
	var once sync.Once
	mixed := &fakeNmap{reply: func([]string) ([]byte, error) {
		failed := false
		once.Do(func() { failed = true })
		if failed {
			return nil, errors.New("one bad run")
		}
		return []byte(nmapSample), nil
	}}
	c = newFakeNmapCollector(mixed, "/usr/bin/nmap")
	if _, err := c.Collect(context.Background(), nmapInput(true, "", ips[:6]...)); err != nil {
		t.Fatal(err)
	}
	if mixed.count() != 6 {
		t.Errorf("nmap ran %d times, want all 6 after a single failure", mixed.count())
	}
}

func TestNmapStopsWhenTheScanIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	f := &fakeNmap{reply: func([]string) ([]byte, error) {
		cancel()
		time.Sleep(10 * time.Millisecond)
		return nil, ctx.Err()
	}}
	c := newFakeNmapCollector(f, "/usr/bin/nmap")
	if _, err := c.Collect(ctx, nmapInput(true, "", "192.168.1.9", "192.168.1.10", "192.168.1.11")); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestCappedKeepsOnlyTheFirstBytes(t *testing.T) {
	c := &capped{max: 5}
	n, err := c.Write([]byte("abcdefgh"))
	if err != nil || n != 8 {
		t.Errorf("Write = %d, %v, want the whole write reported as taken", n, err)
	}
	_, _ = c.Write([]byte("more"))
	if c.buf.String() != "abcde" {
		t.Errorf("kept %q, want abcde", c.buf.String())
	}
}

const nmapRules = "nmap,type=router,router\nnmap,type=printer,printer\nmdns,_ipp._tcp,printer\nport,22,server\n"

func TestClassifierNmapRulesAndOrder(t *testing.T) {
	c := mustClassifier(t, nmapRules)
	if got := c.Kind("", "", Facts{Services: -1, Nmap: []string{"os=routeros 7", "type=router"}}); got != model.KindRouter {
		t.Errorf("kind = %q, want router from the nmap type", got)
	}
	if got := c.Kind("", "", Facts{Services: -1, Nmap: []string{"type=general purpose"}}); got != model.KindUnknown {
		t.Errorf("kind = %q, a general purpose guess must not type a device", got)
	}
	// mDNS is tried before nmap, a port after it.
	if got := c.Kind("", "", Facts{Services: -1, MDNS: []string{"_ipp._tcp"}, Nmap: []string{"type=router"}}); got != model.KindPrinter {
		t.Errorf("kind = %q, want mdns to beat nmap", got)
	}
	if got := c.Kind("", "", Facts{Services: -1, Ports: []int{22}, Nmap: []string{"type=router"}}); got != model.KindRouter {
		t.Errorf("kind = %q, want nmap to beat a port", got)
	}
}

func TestDefaultRulesMapTheNmapTypes(t *testing.T) {
	c, err := DefaultClassifier()
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]model.DeviceKind{
		"type=router": model.KindRouter, "type=wap": model.KindAP, "type=printer": model.KindPrinter,
		"type=webcam": model.KindCamera, "type=storage-misc": model.KindNAS, "type=general purpose": model.KindUnknown,
	}
	for fact, want := range cases {
		if got := c.Kind("", "", Facts{Services: -1, Nmap: []string{fact}}); got != want {
			t.Errorf("%s -> %q, want %q", fact, got, want)
		}
	}
}

func TestUntypedListsOnlyWhatNothingCouldType(t *testing.T) {
	c := mustClassifier(t, "mdns,_ipp._tcp,printer\n")
	a, b, d := model.DeviceKey{IP: "192.168.1.1"}, model.DeviceKey{IP: "192.168.1.2"}, model.DeviceKey{IP: "192.168.1.3"}
	devs := []model.DeviceInput{
		{Key: a, Source: "lldp", Kind: model.KindSwitch}, // typed by LLDP
		{Key: b, Source: "arp"},                          // nothing says what it is
		{Key: d, Source: "arp"},                          // mDNS will type it
	}
	facts := func(k model.DeviceKey) Facts {
		if k == d {
			return Facts{Services: -1, MDNS: []string{"_ipp._tcp"}}
		}
		return Facts{Services: -1}
	}
	got := c.Untyped(devs, facts)
	if len(got) != 1 || got[0] != b {
		t.Errorf("untyped = %v, want only %v", got, b)
	}
	if devs[2].Kind != "" {
		t.Error("Untyped changed the devices it was given")
	}
}

func TestTheScannerAsksNmapOnlyAboutUntypedDevices(t *testing.T) {
	f := &fakeNmap{}
	s := &Scanner{
		log:        quietLogger(),
		classifier: mustClassifier(t, "mdns,_ipp._tcp,printer\n"),
		nmap:       newFakeNmapCollector(f, "/usr/bin/nmap"),
		scanCfg:    nmapInput(true, "").Config,
	}
	s.facts = func(k model.DeviceKey) Facts {
		if k.IP == "192.168.1.3" {
			return Facts{Services: -1, MDNS: []string{"_ipp._tcp"}}
		}
		return Facts{Services: -1}
	}
	devices := liveDevices("192.168.1.1", "192.168.1.2", "192.168.1.3")
	inputs := []model.DeviceInput{
		{Key: model.DeviceKey{MAC: devices[0].MAC, IP: devices[0].IP}, Source: "lldp", Kind: model.KindSwitch},
		{Key: model.DeviceKey{MAC: devices[1].MAC, IP: devices[1].IP}, Source: "arp"},
		{Key: model.DeviceKey{MAC: devices[2].MAC, IP: devices[2].IP}, Source: "arp"},
	}

	out := s.askNmap(context.Background(), nil, devices, inputs)
	if f.count() != 1 || f.calls[0][len(f.calls[0])-1] != "192.168.1.2" {
		t.Errorf("nmap runs = %v, want one run for 192.168.1.2 only", f.calls)
	}
	if len(out) != 1 || out[0].Key.IP != "192.168.1.2" || out[0].OSGuess == "" || out[0].Source != "nmap" {
		t.Errorf("inputs = %+v, want the OS guess of the one device that was asked", out)
	}

	s.scanCfg.Discovery.Nmap = false
	if out := s.askNmap(context.Background(), nil, devices, inputs); len(out) != 0 || f.count() != 1 {
		t.Errorf("nmap ran with the switch off")
	}
}
