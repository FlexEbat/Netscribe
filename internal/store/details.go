package store

import (
	"context"
	"fmt"

	"github.com/FlexEbat/Netscribe/internal/model"
	dbgen "github.com/FlexEbat/Netscribe/internal/store/db"
)

// deviceDetails holds the tags and the fields of every device, read in two queries however
// many devices there are.
type deviceDetails struct {
	tags   map[int64][]string
	fields map[int64][]model.DeviceField
}

func (s *Store) loadAllDetails(ctx context.Context) (deviceDetails, error) {
	out := deviceDetails{tags: map[int64][]string{}, fields: map[int64][]model.DeviceField{}}
	tags, err := s.q.ListAllDeviceTags(ctx)
	if err != nil {
		return out, fmt.Errorf("list device tags: %w", err)
	}
	for _, t := range tags {
		out.tags[t.DeviceID] = append(out.tags[t.DeviceID], t.Tag)
	}
	fields, err := s.q.ListAllDeviceFields(ctx)
	if err != nil {
		return out, fmt.Errorf("list device fields: %w", err)
	}
	for _, f := range fields {
		out.fields[f.DeviceID] = append(out.fields[f.DeviceID], model.DeviceField{Name: f.Name, Value: f.Value})
	}
	return out, nil
}

// apply puts the tags and the fields of a device on it. Both lists are never nil.
func (dd deviceDetails) apply(d *model.Device) {
	d.Tags = dd.tags[d.ID]
	if d.Tags == nil {
		d.Tags = []string{}
	}
	d.Fields = dd.fields[d.ID]
	if d.Fields == nil {
		d.Fields = []model.DeviceField{}
	}
}

// withDetails adds the tags and the fields to one device.
func (s *Store) withDetails(ctx context.Context, d model.Device) (model.Device, error) {
	tags, err := s.q.ListDeviceTags(ctx, d.ID)
	if err != nil {
		return d, fmt.Errorf("list device tags: %w", err)
	}
	fields, err := s.q.ListDeviceFields(ctx, d.ID)
	if err != nil {
		return d, fmt.Errorf("list device fields: %w", err)
	}
	d.Tags = append([]string{}, tags...)
	d.Fields = make([]model.DeviceField, 0, len(fields))
	for _, f := range fields {
		d.Fields = append(d.Fields, model.DeviceField{Name: f.Name, Value: f.Value})
	}
	return d, nil
}

// replaceTags makes the tags of a device exactly the given list, which is already checked.
func replaceTags(ctx context.Context, q *dbgen.Queries, id int64, tags []string) error {
	if err := q.DeleteDeviceTags(ctx, id); err != nil {
		return fmt.Errorf("replace tags: %w", err)
	}
	for _, t := range tags {
		if err := q.InsertDeviceTag(ctx, dbgen.InsertDeviceTagParams{DeviceID: id, Tag: t}); err != nil {
			return fmt.Errorf("replace tags: %w", err)
		}
	}
	return nil
}

// replaceFields makes the fields of a device exactly the given list, which is already
// checked, in that order.
func replaceFields(ctx context.Context, q *dbgen.Queries, id int64, fields []model.DeviceField) error {
	if err := q.DeleteDeviceFields(ctx, id); err != nil {
		return fmt.Errorf("replace fields: %w", err)
	}
	for i, f := range fields {
		if err := q.InsertDeviceField(ctx, dbgen.InsertDeviceFieldParams{DeviceID: id, Name: f.Name, Value: f.Value, Seq: int64(i)}); err != nil {
			return fmt.Errorf("replace fields: %w", err)
		}
	}
	return nil
}

// hasTag says whether a device carries a tag (already in lower case).
func hasTag(d model.Device, tag string) bool {
	for _, t := range d.Tags {
		if t == tag {
			return true
		}
	}
	return false
}
