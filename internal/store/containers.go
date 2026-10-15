package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/FlexEbat/Netscribe/internal/collector"
	"github.com/FlexEbat/Netscribe/internal/model"
	dbgen "github.com/FlexEbat/Netscribe/internal/store/db"
)

// Limits on what one container may carry. Docker allows more, a page of thousands of
// port rows helps nobody.
const (
	maxContainerPorts    = 64
	maxContainerNetworks = 32
)

// ListContainers returns every container with its host device, ordered by device and
// name. The result is never nil.
func (s *Store) ListContainers(ctx context.Context) ([]model.Container, error) {
	rows, err := s.q.ListContainers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}
	out := make([]model.Container, 0, len(rows))
	for _, r := range rows {
		first, err := parseTime(r.FirstSeenAt)
		if err != nil {
			return nil, fmt.Errorf("container %s: first_seen_at: %w", r.ID, err)
		}
		last, err := parseTime(r.LastSeenAt)
		if err != nil {
			return nil, fmt.Errorf("container %s: last_seen_at: %w", r.ID, err)
		}
		c := model.Container{
			ID: r.ID, DeviceID: r.DeviceID, Name: r.Name, Image: r.Image, State: r.State,
			ComposeProject: r.ComposeProject, FirstSeenAt: first, LastSeenAt: last,
			Ports: []model.PortMapping{}, Networks: []model.ContainerNetwork{},
		}
		if err := json.Unmarshal([]byte(r.Ports), &c.Ports); err != nil {
			return nil, fmt.Errorf("container %s: ports: %w", r.ID, err)
		}
		if err := json.Unmarshal([]byte(r.Networks), &c.Networks); err != nil {
			return nil, fmt.Errorf("container %s: networks: %w", r.ID, err)
		}
		if c.Ports == nil {
			c.Ports = []model.PortMapping{}
		}
		if c.Networks == nil {
			c.Networks = []model.ContainerNetwork{}
		}
		out = append(out, c)
	}
	return out, nil
}

// SaveContainer stores one container of a host that is already known, as the event
// stream reports it. ErrNotFound means the host device does not exist.
func (s *Store) SaveContainer(ctx context.Context, in model.ContainerInput) error {
	q := s.q
	d, found, err := findDevice(ctx, q, strings.ToLower(strings.TrimSpace(in.Device.MAC)), strings.TrimSpace(in.Device.IP))
	if err != nil {
		return fmt.Errorf("save container: %w", err)
	}
	if !found {
		return ErrNotFound
	}
	if err := upsertContainer(ctx, q, d.ID, in, formatTime(s.now())); err != nil {
		return fmt.Errorf("save container: %w", err)
	}
	return nil
}

// DeleteContainer removes a container. A container that is not stored is not an error:
// a destroy event may arrive for one the scan never saw.
func (s *Store) DeleteContainer(ctx context.Context, id string) error {
	if _, err := s.q.DeleteContainer(ctx, id); err != nil {
		return fmt.Errorf("delete container: %w", err)
	}
	return nil
}

// ReplaceContainers makes the containers of a host equal to list. The host device is
// created when it is not stored yet. Containers of the host that are not in list are
// removed.
func (s *Store) ReplaceContainers(ctx context.Context, host model.DeviceInput, list []model.ContainerInput) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("replace containers: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // after Commit this is a no-op
	q := s.q.WithTx(tx)

	ts := formatTime(s.now())
	id, err := mergeDevice(ctx, q, host, ts)
	if err != nil {
		return fmt.Errorf("replace containers: %w", err)
	}
	for _, in := range list {
		if err := upsertContainer(ctx, q, id, in, ts); err != nil {
			return fmt.Errorf("replace containers: %w", err)
		}
	}
	if _, err := q.DeleteDeviceContainersBefore(ctx, dbgen.DeleteDeviceContainersBeforeParams{DeviceID: id, LastSeenAt: ts}); err != nil {
		return fmt.Errorf("replace containers: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("replace containers: %w", err)
	}
	return nil
}

// applyContainers stores the containers a scan found. A host whose daemon was read
// completely loses the containers that the daemon no longer lists, a host that was not
// read keeps what it had.
func applyContainers(ctx context.Context, q *dbgen.Queries, r collector.Result, ts string) error {
	resolve := func(k model.DeviceKey) (int64, bool, error) {
		d, found, err := findDevice(ctx, q, strings.ToLower(strings.TrimSpace(k.MAC)), strings.TrimSpace(k.IP))
		return d.ID, found, err
	}
	for _, in := range r.Containers {
		id, found, err := resolve(in.Device)
		if err != nil {
			return err
		}
		if !found {
			continue
		}
		if err := upsertContainer(ctx, q, id, in, ts); err != nil {
			return err
		}
	}
	for _, k := range r.ContainersPolled {
		id, found, err := resolve(k)
		if err != nil {
			return err
		}
		if !found {
			continue
		}
		if _, err := q.DeleteDeviceContainersBefore(ctx, dbgen.DeleteDeviceContainersBeforeParams{DeviceID: id, LastSeenAt: ts}); err != nil {
			return fmt.Errorf("delete gone containers of device %d: %w", id, err)
		}
	}
	return nil
}

// upsertContainer inserts a container or refreshes the stored one. Text from the Docker
// daemon is cleaned, because image names and labels are set by whoever builds them.
func upsertContainer(ctx context.Context, q *dbgen.Queries, deviceID int64, in model.ContainerInput, ts string) error {
	id := strings.TrimSpace(in.ID)
	if id == "" {
		return errors.New("container has no id")
	}
	ports := in.Ports
	if len(ports) > maxContainerPorts {
		ports = ports[:maxContainerPorts]
	}
	nets := in.Networks
	if len(nets) > maxContainerNetworks {
		nets = nets[:maxContainerNetworks]
	}
	portsJSON, err := json.Marshal(nonNilPorts(ports))
	if err != nil {
		return fmt.Errorf("container %s: ports: %w", id, err)
	}
	netsJSON, err := json.Marshal(nonNilNetworks(nets))
	if err != nil {
		return fmt.Errorf("container %s: networks: %w", id, err)
	}
	name, image := cleanText(in.Name), cleanText(in.Image)
	state, project := cleanText(strings.ToLower(in.State)), cleanText(in.ComposeProject)

	_, err = q.GetContainer(ctx, id)
	switch {
	case err == nil:
		return q.UpdateContainer(ctx, dbgen.UpdateContainerParams{
			DeviceID: deviceID, Name: name, Image: image, State: state, ComposeProject: project,
			Ports: string(portsJSON), Networks: string(netsJSON), LastSeenAt: ts, ID: id,
		})
	case errors.Is(err, sql.ErrNoRows):
		return q.InsertContainer(ctx, dbgen.InsertContainerParams{
			ID: id, DeviceID: deviceID, Name: name, Image: image, State: state, ComposeProject: project,
			Ports: string(portsJSON), Networks: string(netsJSON), FirstSeenAt: ts, LastSeenAt: ts,
		})
	}
	return fmt.Errorf("find container %s: %w", id, err)
}

func nonNilPorts(p []model.PortMapping) []model.PortMapping {
	if p == nil {
		return []model.PortMapping{}
	}
	out := make([]model.PortMapping, len(p))
	for i, m := range p {
		m.HostIP, m.Proto = cleanText(m.HostIP), cleanText(strings.ToLower(m.Proto))
		out[i] = m
	}
	return out
}

func nonNilNetworks(n []model.ContainerNetwork) []model.ContainerNetwork {
	if n == nil {
		return []model.ContainerNetwork{}
	}
	out := make([]model.ContainerNetwork, len(n))
	for i, m := range n {
		m.Name, m.IP = cleanText(m.Name), cleanText(m.IP)
		out[i] = m
	}
	return out
}
