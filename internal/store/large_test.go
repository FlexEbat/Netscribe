package store

import (
	"context"
	"database/sql"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/collector"
	"github.com/FlexEbat/Netscribe/internal/model"
	dbgen "github.com/FlexEbat/Netscribe/internal/store/db"
)

// countingDB counts every statement the generated queries send. It sits where sqlc takes its
// connection, so a read method that goes through s.q is counted whole.
type countingDB struct {
	dbgen.DBTX
	n atomic.Int64
}

func (c *countingDB) ExecContext(ctx context.Context, q string, a ...any) (sql.Result, error) {
	c.n.Add(1)
	return c.DBTX.ExecContext(ctx, q, a...)
}

func (c *countingDB) PrepareContext(ctx context.Context, q string) (*sql.Stmt, error) {
	c.n.Add(1)
	return c.DBTX.PrepareContext(ctx, q)
}

func (c *countingDB) QueryContext(ctx context.Context, q string, a ...any) (*sql.Rows, error) {
	c.n.Add(1)
	return c.DBTX.QueryContext(ctx, q, a...)
}

func (c *countingDB) QueryRowContext(ctx context.Context, q string, a ...any) *sql.Row {
	c.n.Add(1)
	return c.DBTX.QueryRowContext(ctx, q, a...)
}

// networkOf stores n devices in a tree, four services on each, as one finished scan.
func networkOf(t *testing.T, n int) (*Store, *countingDB) {
	t.Helper()
	s := newTestStore(t)
	ctx := context.Background()
	key := func(i int) model.DeviceKey {
		return model.DeviceKey{MAC: fmt.Sprintf("02:00:00:00:%02x:%02x", i>>8&0xff, i&0xff), IP: fmt.Sprintf("10.0.%d.%d", i/250, i%250+1)}
	}
	var res collector.Result
	for i := 1; i <= n; i++ {
		res.Devices = append(res.Devices, model.DeviceInput{Key: key(i), Hostname: fmt.Sprintf("dev-%d", i), Kind: model.KindHost, Source: "arp"})
		for _, p := range []int{22, 80, 443, 3306} {
			res.Services = append(res.Services, model.ServiceInput{Device: key(i), Port: p, Proto: "tcp", Name: fmt.Sprint(p)})
		}
		if i > 1 {
			res.Links = append(res.Links, model.LinkInput{A: key((i-2)/4 + 1), B: key(i), APort: fmt.Sprintf("p%d", i), BPort: "up", Source: "lldp"})
		}
	}
	sc, err := s.StartScan(ctx, "10.0.0.0/16")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyResult(ctx, sc.ID, res); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishScan(ctx, sc.ID, "done", ""); err != nil {
		t.Fatal(err)
	}
	c := &countingDB{DBTX: s.db}
	s.q = dbgen.New(c)
	return s, c
}

// Rule 4 of section 17: a list and the data around it come in a fixed number of queries, so
// the count at 500 devices is the count at 50.
func TestReadQueriesDoNotGrowWithTheNumberOfDevices(t *testing.T) {
	ctx := context.Background()
	reads := map[string]func(*Store) error{
		"ListDevices": func(s *Store) error { _, err := s.ListDevices(ctx, DeviceFilter{}); return err },
		"ListLinks":   func(s *Store) error { _, err := s.ListLinks(ctx); return err },
		"ListServices": func(s *Store) error { _, err := s.ListServices(ctx, 0); return err },
		"Topology":    func(s *Store) error { _, err := s.Topology(ctx); return err },
		"Overview":    func(s *Store) error { _, err := s.Overview(ctx); return err },
	}
	counts := func(n int) map[string]int64 {
		s, c := networkOf(t, n)
		out := map[string]int64{}
		for name, read := range reads {
			before := c.n.Load()
			if err := read(s); err != nil {
				t.Fatalf("%s at %d devices: %v", name, n, err)
			}
			out[name] = c.n.Load() - before
		}
		return out
	}
	small, large := counts(50), counts(500)
	for name, n := range small {
		if n == 0 {
			t.Errorf("%s: no query was counted at 50 devices, so the check proves nothing", name)
		}
		if large[name] != n {
			t.Errorf("%s: %d queries at 50 devices, %d at 500; the count must not grow", name, n, large[name])
		}
	}
}
