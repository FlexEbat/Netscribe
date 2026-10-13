package collector

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/gosnmp/gosnmp"
)

// maxWalkRows caps how many rows one table walk may return. A device cannot make the
// service hold an unbounded table in memory (section 9.5 of the spec).
const maxWalkRows = 20000

// snmpPDU is one variable of an SNMP answer. Value is one of: nil (no such object),
// []byte (octet string), int64, uint64 or string (object identifier, IP address).
type snmpPDU struct {
	OID   string // numeric, without a leading dot
	Value any
}

// snmpClient is the read-only part of SNMP the collectors use. There is no method that
// writes: SNMP SET cannot happen (section 9.6 of the spec).
type snmpClient interface {
	Get(ctx context.Context, oids []string) ([]snmpPDU, error)
	Walk(ctx context.Context, root string) ([]snmpPDU, error)
	Close() error
}

// snmpDialer opens a session to one device. Tests replace it with a fake.
type snmpDialer func(ctx context.Context, ip, community string, timeout time.Duration) (snmpClient, error)

type gosnmpClient struct {
	g *gosnmp.GoSNMP
}

// dialGoSNMP opens an SNMP v2c session over UDP. A UDP "connection" sends nothing, so
// a dead device only shows up as a timeout on the first request.
func dialGoSNMP(ctx context.Context, ip, community string, timeout time.Duration) (snmpClient, error) {
	g := &gosnmp.GoSNMP{
		Target:         ip,
		Port:           161,
		Community:      community,
		Version:        gosnmp.Version2c,
		Timeout:        timeout,
		Retries:        1,
		Context:        ctx,
		MaxRepetitions: 20,
	}
	if err := g.Connect(); err != nil {
		return nil, fmt.Errorf("snmp connect %s: %w", ip, err)
	}
	return &gosnmpClient{g: g}, nil
}

func (c *gosnmpClient) Get(ctx context.Context, oids []string) ([]snmpPDU, error) {
	c.g.Context = ctx
	pkt, err := c.g.Get(oids)
	if err != nil {
		return nil, redact(err)
	}
	return convertPDUs(pkt.Variables), nil
}

func (c *gosnmpClient) Walk(ctx context.Context, root string) ([]snmpPDU, error) {
	c.g.Context = ctx
	rows, err := c.g.BulkWalkAll(root)
	if err != nil {
		return nil, redact(err)
	}
	if len(rows) > maxWalkRows {
		rows = rows[:maxWalkRows]
	}
	return convertPDUs(rows), nil
}

func (c *gosnmpClient) Close() error {
	if c.g.Conn == nil {
		return nil
	}
	return c.g.Conn.Close()
}

// redact keeps the community out of error text. gosnmp does not put it into errors,
// this is a second line of defense for wrapped network errors.
func redact(err error) error {
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return errSNMPTimeout
	}
	return err
}

var errSNMPTimeout = errors.New("snmp request timed out")

func convertPDUs(in []gosnmp.SnmpPDU) []snmpPDU {
	out := make([]snmpPDU, 0, len(in))
	for _, p := range in {
		out = append(out, snmpPDU{OID: strings.TrimPrefix(p.Name, "."), Value: convertValue(p)})
	}
	return out
}

func convertValue(p gosnmp.SnmpPDU) any {
	switch p.Type {
	case gosnmp.NoSuchObject, gosnmp.NoSuchInstance, gosnmp.EndOfMibView, gosnmp.Null:
		return nil
	case gosnmp.OctetString, gosnmp.Opaque:
		if b, ok := p.Value.([]byte); ok {
			return b
		}
		return nil
	case gosnmp.ObjectIdentifier:
		if s, ok := p.Value.(string); ok {
			return strings.TrimPrefix(s, ".")
		}
		return nil
	case gosnmp.IPAddress:
		if s, ok := p.Value.(string); ok {
			return s
		}
		return nil
	}
	switch v := p.Value.(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case uint:
		return uint64(v)
	case uint32:
		return uint64(v)
	case uint64:
		return v
	}
	return nil
}

// pduInt returns a numeric value as int64.
func pduInt(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case uint64:
		if n > 1<<62 {
			return 0, false
		}
		return int64(n), true
	}
	return 0, false
}

// pduBytes returns an octet string.
func pduBytes(v any) ([]byte, bool) {
	b, ok := v.([]byte)
	return b, ok
}

// pduText returns an octet string as one clean line of text.
func pduText(v any) string {
	b, ok := pduBytes(v)
	if !ok {
		return ""
	}
	return oneLine(string(b))
}

// oneLine replaces line breaks and tabs with spaces and trims the ends. sysDescr of many
// devices spans several lines.
func oneLine(s string) string {
	s = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ", "\t", " ").Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

// suffix returns the part of oid after the table column prefix, or false when oid is
// not under prefix. Both are numeric OIDs without a leading dot.
func suffix(oid, prefix string) (string, bool) {
	if !strings.HasPrefix(oid, prefix+".") {
		return "", false
	}
	return oid[len(prefix)+1:], true
}
