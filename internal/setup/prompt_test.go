package setup

import (
	"bufio"
	"bytes"
	"errors"
	"strings"
	"testing"
)

func newPrompter(input string, yes bool) (*prompter, *bytes.Buffer) {
	out := &bytes.Buffer{}
	return &prompter{in: bufio.NewReader(strings.NewReader(input)), ui: ui{w: out}, yes: yes}, out
}

func TestConfirm(t *testing.T) {
	for _, tc := range []struct {
		in   string
		def  bool
		want bool
	}{
		{"\n", true, true}, {"\n", false, false}, {"y\n", false, true}, {"YES\n", false, true},
		{"n\n", true, false}, {"maybe\ny\n", false, true},
	} {
		p, _ := newPrompter(tc.in, false)
		got, err := p.confirm("ok?", tc.def)
		if err != nil || got != tc.want {
			t.Errorf("confirm(%q, %v) = %v, %v; want %v", tc.in, tc.def, got, err, tc.want)
		}
	}
}

func TestConfirmWithYesTakesTheDefaultAndPrintsIt(t *testing.T) {
	p, out := newPrompter("", true)
	got, err := p.confirm("Start?", true)
	if err != nil || !got {
		t.Fatalf("got %v, %v", got, err)
	}
	if !strings.Contains(out.String(), "Start?") || !strings.Contains(out.String(), "yes") {
		t.Errorf("the log does not show the answer: %q", out.String())
	}
}

func TestClosedInputIsAnError(t *testing.T) {
	p, _ := newPrompter("", false)
	if _, err := p.confirm("ok?", true); !errors.Is(err, errInput) {
		t.Errorf("confirm: %v", err)
	}
	if _, err := p.ask("name", "x", nil); !errors.Is(err, errInput) {
		t.Errorf("ask: %v", err)
	}
	if _, err := p.choose("pick", []string{"a"}, []bool{true}); !errors.Is(err, errInput) {
		t.Errorf("choose: %v", err)
	}
}

func TestAskRepeatsUntilTheAnswerIsValid(t *testing.T) {
	p, out := newPrompter("bad\n\nfine\n", false)
	got, err := p.ask("name", "", func(v string) error {
		if v != "fine" {
			return errors.New("not fine")
		}
		return nil
	})
	if err != nil || got != "fine" {
		t.Fatalf("got %q, %v", got, err)
	}
	if strings.Count(out.String(), "not fine") != 2 {
		t.Errorf("the refusal was not shown twice: %q", out.String())
	}
}

func TestAskWithYesRefusesAnUnusableDefault(t *testing.T) {
	p, _ := newPrompter("", true)
	_, err := p.ask("port", "nope", func(string) error { return errors.New("bad") })
	if err == nil {
		t.Fatal("an unusable default must stop a run that cannot ask again")
	}
}

func TestParseChoice(t *testing.T) {
	def := []bool{true, false, true}
	for _, tc := range []struct {
		in   string
		want []bool
		bad  bool
	}{
		{"", def, false}, {"all", []bool{true, true, true}, false}, {"none", []bool{false, false, false}, false},
		{"2", []bool{false, true, false}, false}, {"1, 3", []bool{true, false, true}, false},
		{"4", nil, true}, {"0", nil, true}, {"x", nil, true},
	} {
		got, err := parseChoice(tc.in, def)
		if (err != nil) != tc.bad {
			t.Errorf("parseChoice(%q) error %v", tc.in, err)
			continue
		}
		if !tc.bad && strings.Trim(strings.Join(boolStrings(got), ""), " ") != strings.Join(boolStrings(tc.want), "") {
			t.Errorf("parseChoice(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func boolStrings(b []bool) []string {
	out := make([]string, len(b))
	for i, v := range b {
		out[i] = map[bool]string{true: "x", false: "-"}[v]
	}
	return out
}

func TestChooseKeepsTheMarkedOptionsOnEnter(t *testing.T) {
	p, out := newPrompter("\n", false)
	got, err := p.choose("Which?", []string{"a", "b"}, []bool{true, false})
	if err != nil || !got[0] || got[1] {
		t.Fatalf("got %v, %v", got, err)
	}
	if !strings.Contains(out.String(), "[x] 1  a") || !strings.Contains(out.String(), "[ ] 2  b") {
		t.Errorf("the list is not shown with its marks: %q", out.String())
	}
}
