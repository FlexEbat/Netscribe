package collector

import (
	"testing"

	"github.com/gosnmp/gosnmp"
)

func TestRowCapStopsTheWalkAtTheLimit(t *testing.T) {
	r := &rowCap{limit: 3}
	for i := 0; i < 3; i++ {
		if err := r.add(gosnmp.SnmpPDU{Name: ".1.3.6.1"}); err != nil {
			t.Fatalf("row %d refused: %v", i, err)
		}
	}
	if r.capped {
		t.Fatal("capped before the limit was passed")
	}
	if err := r.add(gosnmp.SnmpPDU{Name: ".1.3.6.2"}); err == nil {
		t.Fatal("the row past the limit was accepted")
	}
	if !r.capped || len(r.pdus) != 3 {
		t.Errorf("capped = %v with %d rows, want true with 3", r.capped, len(r.pdus))
	}
}
