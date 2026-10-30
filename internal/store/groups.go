package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"strings"

	"github.com/FlexEbat/Netscribe/internal/model"
	dbgen "github.com/FlexEbat/Netscribe/internal/store/db"
)

// ErrNoSuchGroup is a group id that does not exist, in a request that is about something
// else, a device edit for example.
var ErrNoSuchGroup = errors.New("the group does not exist")

// groupInfo is a group as the effective-group rule needs it.
type groupInfo struct {
	ref      model.GroupRef
	prefixes []netip.Prefix
}

func (s *Store) loadGroups(ctx context.Context) ([]groupInfo, []dbgen.DeviceGroup, error) {
	rows, err := s.q.ListGroups(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("list groups: %w", err)
	}
	out := make([]groupInfo, 0, len(rows))
	for _, r := range rows {
		ranges, err := decodeRanges(r.Ranges)
		if err != nil {
			return nil, nil, fmt.Errorf("group %d: %w", r.ID, err)
		}
		g := groupInfo{ref: model.GroupRef{ID: r.ID, Name: r.Name, VLAN: vlanPtr(r.Vlan)}}
		for _, c := range ranges {
			if p, err := netip.ParsePrefix(c); err == nil {
				g.prefixes = append(g.prefixes, p)
			}
		}
		out = append(out, g)
	}
	return out, rows, nil
}

func decodeRanges(raw string) ([]string, error) {
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("ranges: %w", err)
	}
	if out == nil {
		out = []string{}
	}
	return out, nil
}

func vlanPtr(v sql.NullInt64) *int {
	if !v.Valid {
		return nil
	}
	n := int(v.Int64)
	return &n
}

// resolveGroup sets Group and GroupAuto of a device. A group that a person chose wins; else
// the group with the lowest id whose ranges contain the address; else none. groups is in id
// order.
func resolveGroup(d *model.Device, groups []groupInfo) {
	d.Group, d.GroupAuto = nil, false
	if d.GroupID != nil {
		for _, g := range groups {
			if g.ref.ID == *d.GroupID {
				ref := g.ref
				d.Group = &ref
				return
			}
		}
		return
	}
	addr, err := netip.ParseAddr(d.IP)
	if err != nil {
		return
	}
	for _, g := range groups {
		for _, p := range g.prefixes {
			if p.Contains(addr) {
				ref := g.ref
				d.Group, d.GroupAuto = &ref, true
				return
			}
		}
	}
}

// withGroup resolves the group of one device.
func (s *Store) withGroup(ctx context.Context, d model.Device) (model.Device, error) {
	groups, _, err := s.loadGroups(ctx)
	if err != nil {
		return model.Device{}, err
	}
	resolveGroup(&d, groups)
	return d, nil
}

func toGroup(r dbgen.DeviceGroup) (model.Group, error) {
	ranges, err := decodeRanges(r.Ranges)
	if err != nil {
		return model.Group{}, fmt.Errorf("group %d: %w", r.ID, err)
	}
	return model.Group{ID: r.ID, Name: r.Name, Description: r.Description, VLAN: vlanPtr(r.Vlan), Ranges: ranges, Version: r.Version}, nil
}

func vlanArg(v *int) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*v), Valid: true}
}

func encodeRanges(r []string) (string, error) {
	if r == nil {
		r = []string{}
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return "", fmt.Errorf("ranges: %w", err)
	}
	return string(raw), nil
}

// ListGroups returns every group, ordered by name, with the number of devices that have it
// as their effective group.
func (s *Store) ListGroups(ctx context.Context) ([]model.Group, error) {
	infos, rows, err := s.loadGroups(ctx)
	if err != nil {
		return nil, err
	}
	devices, err := s.ListDevices(ctx, DeviceFilter{})
	if err != nil {
		return nil, err
	}
	count := map[int64]int{}
	for _, d := range devices {
		if d.Group != nil {
			count[d.Group.ID]++
		}
	}
	_ = infos
	out := make([]model.Group, 0, len(rows))
	for _, r := range rows {
		g, err := toGroup(r)
		if err != nil {
			return nil, err
		}
		g.DeviceCount = count[g.ID]
		out = append(out, g)
	}
	sortGroups(out)
	return out, nil
}

func sortGroups(g []model.Group) {
	for i := 1; i < len(g); i++ {
		for j := i; j > 0 && strings.ToLower(g[j].Name) < strings.ToLower(g[j-1].Name); j-- {
			g[j], g[j-1] = g[j-1], g[j]
		}
	}
}

// GetGroup returns one group without its device count, or ErrNotFound.
func (s *Store) GetGroup(ctx context.Context, id int64) (model.Group, error) {
	r, err := s.q.GetGroup(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Group{}, ErrNotFound
	}
	if err != nil {
		return model.Group{}, fmt.Errorf("get group: %w", err)
	}
	return toGroup(r)
}

// CreateGroup stores a checked group. A name that is taken is ErrConflict.
func (s *Store) CreateGroup(ctx context.Context, in model.ManualGroup) (model.Group, error) {
	ranges, err := encodeRanges(in.Ranges)
	if err != nil {
		return model.Group{}, err
	}
	r, err := s.q.InsertGroup(ctx, dbgen.InsertGroupParams{
		Name: in.Name, Description: in.Description, Vlan: vlanArg(in.VLAN), Ranges: ranges, CreatedAt: formatTime(s.now()),
	})
	if isUniqueViolation(err) {
		return model.Group{}, ErrConflict
	}
	if err != nil {
		return model.Group{}, fmt.Errorf("create group: %w", err)
	}
	return toGroup(r)
}

// UpdateGroup applies a checked edit if the editor saw the current version. It returns
// ErrNotFound, ErrVersionConflict, or ErrConflict for a name that is taken.
func (s *Store) UpdateGroup(ctx context.Context, id int64, e model.GroupEdit) (model.Group, error) {
	cur, err := s.GetGroup(ctx, id)
	if err != nil {
		return model.Group{}, err
	}
	name, desc, vlan, ranges := cur.Name, cur.Description, cur.VLAN, cur.Ranges
	if e.Name != nil {
		name = *e.Name
	}
	if e.Description != nil {
		desc = *e.Description
	}
	if e.VLAN != nil {
		if *e.VLAN == 0 {
			vlan = nil
		} else {
			vlan = e.VLAN
		}
	}
	if e.Ranges != nil {
		ranges = *e.Ranges
	}
	enc, err := encodeRanges(ranges)
	if err != nil {
		return model.Group{}, err
	}
	n, err := s.q.UpdateGroup(ctx, dbgen.UpdateGroupParams{
		Name: name, Description: desc, Vlan: vlanArg(vlan), Ranges: enc, ID: id, Version: e.Version,
	})
	if isUniqueViolation(err) {
		return model.Group{}, ErrConflict
	}
	if err != nil {
		return model.Group{}, fmt.Errorf("update group: %w", err)
	}
	if n == 0 {
		return model.Group{}, ErrVersionConflict
	}
	return s.GetGroup(ctx, id)
}

// DeleteGroup removes a group. Its devices lose it and are not deleted.
func (s *Store) DeleteGroup(ctx context.Context, id int64) error {
	n, err := s.q.DeleteGroup(ctx, id)
	if err != nil {
		return fmt.Errorf("delete group: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
