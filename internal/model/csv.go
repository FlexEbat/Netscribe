package model

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"
)

// MaxImportRows is the most devices one CSV file may create.
const MaxImportRows = 1000

// ImportRow is one line of an import file. Every cell is text and stays text: nothing in
// this package or after it evaluates a cell, so "=cmd()" is stored as it was typed.
type ImportRow struct {
	Line                                         int
	Label, IP, MAC, Hostname, Kind, Group, Notes string
}

// The errors of ParseDeviceCSV.
var (
	ErrCSVNoHeader = errors.New("the first line must name the columns")
	ErrCSVTooMany  = fmt.Errorf("a file may have at most %d rows", MaxImportRows)
)

var importColumns = map[string]bool{"label": true, "ip": true, "mac": true, "hostname": true, "kind": true, "group": true, "notes": true}

// ParseDeviceCSV reads an import file. The first line names the columns, in any order, from
// label, ip, mac, hostname, kind, group and notes. Blank lines are skipped. A byte order
// mark and CRLF line endings are accepted. Nothing is checked about the cells except that
// every row has as many as the header.
func ParseDeviceCSV(r io.Reader) ([]ImportRow, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading the file: %w", err)
	}
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	cr := csv.NewReader(bytes.NewReader(raw))
	cr.ReuseRecord = false

	header, err := cr.Read()
	if errors.Is(err, io.EOF) {
		return nil, ErrCSVNoHeader
	}
	if err != nil {
		return nil, csvProblem(err)
	}
	cols := make([]string, len(header))
	seen := map[string]bool{}
	for i, h := range header {
		name := strings.ToLower(strings.TrimSpace(h))
		switch {
		case !importColumns[name]:
			return nil, fmt.Errorf("line 1: %q is not a column, use label, ip, mac, hostname, kind, group or notes", strings.TrimSpace(h))
		case seen[name]:
			return nil, fmt.Errorf("line 1: the column %q is listed twice", name)
		}
		seen[name] = true
		cols[i] = name
	}

	var rows []ImportRow
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			return rows, nil
		}
		if err != nil {
			return nil, csvProblem(err)
		}
		if len(rows) == MaxImportRows {
			return nil, ErrCSVTooMany
		}
		line, _ := cr.FieldPos(0)
		row := ImportRow{Line: line}
		for i, cell := range rec {
			cell = strings.TrimSpace(cell)
			switch cols[i] {
			case "label":
				row.Label = cell
			case "ip":
				row.IP = cell
			case "mac":
				row.MAC = cell
			case "hostname":
				row.Hostname = cell
			case "kind":
				row.Kind = cell
			case "group":
				row.Group = cell
			case "notes":
				row.Notes = cell
			}
		}
		rows = append(rows, row)
	}
}

func csvProblem(err error) error {
	var pe *csv.ParseError
	if errors.As(err, &pe) {
		if errors.Is(pe.Err, csv.ErrFieldCount) {
			return fmt.Errorf("line %d: the row has a different number of cells than the header", pe.Line)
		}
		return fmt.Errorf("line %d: the file is not valid CSV (a quote is not closed or misplaced)", pe.Line)
	}
	return errors.New("the file is not valid CSV")
}
