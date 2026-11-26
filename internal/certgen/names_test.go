package certgen

import (
	"slices"
	"testing"
)

func TestDefaultNames(t *testing.T) {
	for _, listen := range []string{"127.0.0.1:8080", "0.0.0.0:8443", "", "192.168.1.5:8080", "panel.lan:8080", "[::]:8080"} {
		names := DefaultNames(listen)
		if len(names) > MaxNames {
			t.Errorf("%q: %d names", listen, len(names))
		}
		if _, err := NormalizeNames(names); err != nil {
			t.Errorf("%q: %v", listen, err)
		}
		for _, want := range []string{"localhost", "127.0.0.1", "::1"} {
			if !slices.Contains(names, want) {
				t.Errorf("%q: no %s in %v", listen, want, names)
			}
		}
	}
	if names := DefaultNames("192.168.1.5:8080"); !slices.Contains(names, "192.168.1.5") {
		t.Errorf("the listen address is missing: %v", names)
	}
	if names := DefaultNames("panel.lan:8080"); !slices.Contains(names, "panel.lan") {
		t.Errorf("the listen host is missing: %v", names)
	}
}

func TestDefaultNamesAreUsable(t *testing.T) {
	if _, _, err := Generate(Options{Names: DefaultNames("0.0.0.0:8080")}); err != nil {
		t.Errorf("the defaults cannot make a certificate: %v", err)
	}
}
