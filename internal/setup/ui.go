package setup

import (
	"fmt"
	"io"
	"strings"
)

// ui prints the progress of the setup. It uses plain text and ANSI colors only, so it
// works in any terminal and in a log.
type ui struct {
	w     io.Writer
	color bool
}

func (u ui) printf(format string, a ...any) { _, _ = fmt.Fprintf(u.w, format, a...) }

func (u ui) paint(code, s string) string {
	if !u.color {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func (u ui) dim(s string) string  { return u.paint("2", s) }
func (u ui) bold(s string) string { return u.paint("1", s) }

func (u ui) banner(title, version string) {
	u.printf("\n%s\n%s\n", u.paint("1;36", title), u.dim(version))
}

func (u ui) step(n, total int, title string) {
	head := fmt.Sprintf("%d/%d  %s", n, total, title)
	u.printf("\n%s %s\n", u.paint("1;34", head), u.dim(strings.Repeat("─", max(2, 56-len([]rune(head))))))
}

func (u ui) section(title string) {
	u.printf("\n%s\n", u.bold(title))
}

var marks = map[Level]struct{ symbol, color string }{
	LevelOK:   {"✓", "32"},
	LevelInfo: {"·", "36"},
	LevelWarn: {"!", "33"},
	LevelFail: {"✗", "31"},
}

// finding prints one result. The fix is shown for warnings and failures only.
func (u ui) finding(f Finding) {
	m := marks[f.Level]
	u.printf("  %s %s\n", u.paint(m.color, m.symbol), f.Title)
	if f.Detail != "" {
		u.printf("      %s\n", u.dim(f.Detail))
	}
	if f.Fix != "" && f.Level >= LevelWarn {
		for _, line := range strings.Split(f.Fix, "\n") {
			u.printf("      %s %s\n", u.paint("36", "→"), line)
		}
	}
}

func (u ui) findings(list []Finding) {
	for _, f := range list {
		u.finding(f)
	}
}

func (u ui) note(format string, a ...any) { u.printf("  %s\n", u.dim(fmt.Sprintf(format, a...))) }

func (u ui) planned(desc string) { u.printf("  %s %s\n", u.paint("35", "would"), desc) }
func (u ui) doing(desc string)   { u.printf("  %s %s\n", u.paint("36", "…"), desc) }
func (u ui) done(desc string)    { u.printf("  %s %s\n", u.paint("32", "✓"), desc) }
func (u ui) failed(desc string, err error) {
	u.printf("  %s %s: %v\n", u.paint("31", "✗"), desc, err)
}
