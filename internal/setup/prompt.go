package setup

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

var errInput = errors.New("input closed before the question was answered")

// errQuit is the answer of a person who typed q, quit or exit at a question. It stops
// whatever is running and unwinds to the top of the program, which leaves quietly.
var errQuit = errors.New("you chose to quit")

// errDeclined is the answer no to the confirmation of an action the run cannot go on without.
var errDeclined = errors.New("you declined an action")

// isExit tells whether the person wants out: the terminal closed or q was typed.
func isExit(err error) bool { return errors.Is(err, errInput) || errors.Is(err, errQuit) }

// isQuitWord is true for the words that leave the program from any question.
func isQuitWord(s string) bool {
	switch strings.ToLower(s) {
	case "q", "quit", "exit":
		return true
	}
	return false
}

// isBackWord is true for the words that return to the menu above.
func isBackWord(s string) bool {
	switch strings.ToLower(s) {
	case "b", "back":
		return true
	}
	return false
}

// prompter asks the questions of the setup. With yes set it answers every question with
// its default and prints the answer, so a log shows what was chosen.
type prompter struct {
	in  *bufio.Reader
	ui  ui
	yes bool
}

func (p *prompter) line() (string, error) {
	s, err := p.in.ReadString('\n')
	if err != nil && (!errors.Is(err, io.EOF) || s == "") {
		return "", errInput
	}
	s = strings.TrimSpace(s)
	if isQuitWord(s) {
		return "", errQuit
	}
	return s, nil
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func (p *prompter) confirm(question string, def bool) (bool, error) {
	hint := "[y/N/q]"
	if def {
		hint = "[Y/n/q]"
	}
	if p.yes {
		p.ui.printf("  %s %s %s %s\n", p.ui.paint("36", "?"), question, hint, p.ui.dim("→ "+yesNo(def)))
		return def, nil
	}
	for {
		p.ui.printf("  %s %s %s ", p.ui.paint("36", "?"), question, hint)
		s, err := p.line()
		if err != nil {
			return false, err
		}
		switch strings.ToLower(s) {
		case "":
			return def, nil
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}
		p.ui.printf("    answer y or n, or q to quit\n")
	}
}

// ask reads a value. An empty answer takes the default. valid may be nil.
func (p *prompter) ask(question, def string, valid func(string) error) (string, error) {
	if p.yes {
		if valid != nil {
			if err := valid(def); err != nil {
				return "", fmt.Errorf("%s: the default %q is not usable: %w", question, def, err)
			}
		}
		p.ui.printf("  %s %s %s\n", p.ui.paint("36", "?"), question, p.ui.dim("→ "+def))
		return def, nil
	}
	for {
		suffix := " " + p.ui.dim("(q quits)")
		if def != "" {
			suffix = " " + p.ui.dim("["+def+"], q quits")
		}
		p.ui.printf("  %s %s%s ", p.ui.paint("36", "?"), question, suffix)
		s, err := p.line()
		if err != nil {
			return "", err
		}
		if s == "" {
			s = def
		}
		if valid != nil {
			if err := valid(s); err != nil {
				p.ui.printf("    %v\n", err)
				continue
			}
		}
		return s, nil
	}
}

// choose lets the person pick any of the options. The answer is a list of numbers
// separated by commas, "all" or "none". An empty answer keeps the defaults.
func (p *prompter) choose(question string, options []string, def []bool) ([]bool, error) {
	p.ui.printf("  %s %s\n", p.ui.paint("36", "?"), question)
	for i, o := range options {
		box := "[ ]"
		if def[i] {
			box = "[x]"
		}
		p.ui.printf("      %s %d  %s\n", box, i+1, o)
	}
	if p.yes {
		return def, nil
	}
	for {
		p.ui.printf("    numbers, all, none, Enter for the marked ones, q to quit: ")
		s, err := p.line()
		if err != nil {
			return nil, err
		}
		got, err := parseChoice(s, def)
		if err != nil {
			p.ui.printf("    %v\n", err)
			continue
		}
		return got, nil
	}
}

func parseChoice(s string, def []bool) ([]bool, error) {
	switch strings.ToLower(s) {
	case "":
		return def, nil
	case "all":
		out := make([]bool, len(def))
		for i := range out {
			out[i] = true
		}
		return out, nil
	case "none":
		return make([]bool, len(def)), nil
	}
	out := make([]bool, len(def))
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' }) {
		n, err := strconv.Atoi(f)
		if err != nil || n < 1 || n > len(def) {
			return nil, fmt.Errorf("%q is not a number from 1 to %d", f, len(def))
		}
		out[n-1] = true
	}
	return out, nil
}

// pick lets the person choose one of the options by its number. An empty answer takes the
// option at index def.
func (p *prompter) pick(question string, options []string, def int) (int, error) {
	p.ui.printf("  %s %s\n", p.ui.paint("36", "?"), question)
	for i, o := range options {
		mark := " "
		if i == def {
			mark = "›"
		}
		p.ui.printf("      %s %d  %s\n", p.ui.paint("36", mark), i+1, o)
	}
	if p.yes {
		return def, nil
	}
	back := len(options) - 1
	canBack := strings.HasPrefix(options[back], "Back")
	hint := "q quits"
	if canBack {
		hint = "b goes back, q quits"
	}
	for {
		p.ui.printf("    number [%d], %s: ", def+1, hint)
		s, err := p.line()
		if err != nil {
			return 0, err
		}
		if s == "" {
			return def, nil
		}
		if canBack && isBackWord(s) {
			return back, nil
		}
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > len(options) {
			p.ui.printf("    choose a number from 1 to %d\n", len(options))
			continue
		}
		return n - 1, nil
	}
}
