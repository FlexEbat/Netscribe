package model

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func parse(t *testing.T, in string) []ImportRow {
	t.Helper()
	rows, err := ParseDeviceCSV(strings.NewReader(in))
	if err != nil {
		t.Fatalf("ParseDeviceCSV: %v", err)
	}
	return rows
}

func TestCSVReadsColumnsInAnyOrderAndKeepsTheLineNumbers(t *testing.T) {
	rows := parse(t, "IP , Label,notes\n192.168.1.5,cam1,front door\n\n192.168.1.6,cam2,\n")
	if len(rows) != 2 {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0] != (ImportRow{Line: 2, Label: "cam1", IP: "192.168.1.5", Notes: "front door"}) {
		t.Errorf("row 0 = %+v", rows[0])
	}
	if rows[1].Line != 4 || rows[1].Label != "cam2" || rows[1].Notes != "" {
		t.Errorf("row 1 = %+v (the blank line still counts as a line)", rows[1])
	}
}

func TestCSVHandlesQuotesCommasLineBreaksBOMAndCRLF(t *testing.T) {
	in := "\xef\xbb\xbflabel,notes\r\n\"Switch, floor 2\",\"says \"\"hi\"\"\r\nand more\"\r\nplain,x\r\n"
	rows := parse(t, in)
	if len(rows) != 2 || rows[0].Label != "Switch, floor 2" || rows[0].Notes != "says \"hi\"\nand more" || rows[1].Label != "plain" {
		t.Errorf("rows = %+v", rows)
	}
}

func TestCSVNeverEvaluatesACell(t *testing.T) {
	rows := parse(t, "label,notes\n=cmd|' /C calc'!A0,@SUM(1+1)\n+1,-2\n")
	if rows[0].Label != "=cmd|' /C calc'!A0" || rows[0].Notes != "@SUM(1+1)" || rows[1].Label != "+1" || rows[1].Notes != "-2" {
		t.Errorf("rows = %+v, cells must stay as typed", rows)
	}
}

func TestCSVRefusesAFileWithoutAHeaderOrWithABadOne(t *testing.T) {
	if _, err := ParseDeviceCSV(strings.NewReader("")); !errors.Is(err, ErrCSVNoHeader) {
		t.Errorf("empty file: %v", err)
	}
	if _, err := ParseDeviceCSV(strings.NewReader("\n\n")); !errors.Is(err, ErrCSVNoHeader) {
		t.Errorf("blank lines only: %v", err)
	}
	for in, want := range map[string]string{
		"label,colour\nx,y\n": `"colour" is not a column`,
		"label,Label\nx,y\n":  "listed twice",
		"label,ip\nx\n":       "line 2: the row has a different number",
		"label\n\"open\n":     "not valid CSV",
	} {
		if _, err := ParseDeviceCSV(strings.NewReader(in)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want %q", in, err, want)
		}
	}
}

func TestCSVLimitsTheNumberOfRows(t *testing.T) {
	build := func(n int) string {
		var b strings.Builder
		b.WriteString("label\n")
		for i := 0; i < n; i++ {
			fmt.Fprintf(&b, "d%d\n", i)
		}
		return b.String()
	}
	if rows := parse(t, build(MaxImportRows)); len(rows) != MaxImportRows {
		t.Errorf("rows = %d", len(rows))
	}
	if _, err := ParseDeviceCSV(strings.NewReader(build(MaxImportRows + 1))); !errors.Is(err, ErrCSVTooMany) {
		t.Errorf("1001 rows: %v", err)
	}
}
