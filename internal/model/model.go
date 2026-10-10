// Package model holds the domain types shared by store, collector, render and api.
package model

import "time"

type DeviceKind string

const (
	KindRouter   DeviceKind = "router"
	KindSwitch   DeviceKind = "switch"
	KindAP       DeviceKind = "ap"
	KindFirewall DeviceKind = "firewall"
	KindServer   DeviceKind = "server"
	KindNAS      DeviceKind = "nas"
	KindPrinter  DeviceKind = "printer"
	KindCamera   DeviceKind = "camera"
	KindIoT      DeviceKind = "iot"
	KindHost     DeviceKind = "host"
	KindUnknown  DeviceKind = "unknown"
)

// Valid reports whether k is one of the known kinds.
func (k DeviceKind) Valid() bool {
	switch k {
	case KindRouter, KindSwitch, KindAP, KindFirewall, KindServer, KindNAS,
		KindPrinter, KindCamera, KindIoT, KindHost, KindUnknown:
		return true
	}
	return false
}

type Device struct {
	ID          int64      `json:"id"`
	MAC         string     `json:"mac"`
	IP          string     `json:"ip"`
	Hostname    string     `json:"hostname"`
	Vendor      string     `json:"vendor"`
	Kind        DeviceKind `json:"kind"`
	Description string     `json:"description"`
	Source      string     `json:"source"`
	Online      bool       `json:"online"`
	Manual      bool       `json:"manual"`
	Label       string     `json:"label"`
	Notes       string     `json:"notes"`
	KindLocked  bool       `json:"kindLocked"`
	Version     int64      `json:"version"`
	FirstSeenAt time.Time  `json:"firstSeenAt"`
	LastSeenAt  time.Time  `json:"lastSeenAt"`
	X           *float64   `json:"x"`
	Y           *float64   `json:"y"`
}

type Interface struct {
	ID       int64  `json:"id"`
	DeviceID int64  `json:"deviceId"`
	Name     string `json:"name"`
	MAC      string `json:"mac"`
	IP       string `json:"ip"`
	Up       bool   `json:"up"`
}

type Link struct {
	ID         int64     `json:"id"`
	ADeviceID  int64     `json:"aDeviceId"` // always smaller than BDeviceID
	APort      string    `json:"aPort"`
	BDeviceID  int64     `json:"bDeviceId"`
	BPort      string    `json:"bPort"`
	Source     string    `json:"source"`
	LastSeenAt time.Time `json:"lastSeenAt"`
}

type Service struct {
	ID          int64     `json:"id"`
	DeviceID    int64     `json:"deviceId"`
	Port        int       `json:"port"`
	Proto       string    `json:"proto"`
	Name        string    `json:"name"`
	FirstSeenAt time.Time `json:"firstSeenAt"`
	LastSeenAt  time.Time `json:"lastSeenAt"`
}

type PortMapping struct {
	HostIP        string `json:"hostIp"`
	HostPort      int    `json:"hostPort"`
	ContainerPort int    `json:"containerPort"`
	Proto         string `json:"proto"`
}

type ContainerNetwork struct {
	Name string `json:"name"`
	IP   string `json:"ip"`
}

type Container struct {
	ID             string             `json:"id"`
	DeviceID       int64              `json:"deviceId"`
	Name           string             `json:"name"`
	Image          string             `json:"image"`
	State          string             `json:"state"`
	ComposeProject string             `json:"composeProject"`
	Ports          []PortMapping      `json:"ports"`
	Networks       []ContainerNetwork `json:"networks"`
	FirstSeenAt    time.Time          `json:"firstSeenAt"`
	LastSeenAt     time.Time          `json:"lastSeenAt"`
}

type Scan struct {
	ID             int64      `json:"id"`
	Target         string     `json:"target"`
	Status         string     `json:"status"` // running, done, failed
	StartedAt      time.Time  `json:"startedAt"`
	FinishedAt     *time.Time `json:"finishedAt"`
	Error          string     `json:"error"`
	DeviceCount    int        `json:"deviceCount"`
	LinkCount      int        `json:"linkCount"`
	ContainerCount int        `json:"containerCount"`
}

type Change struct {
	ID       int64     `json:"id"`
	ScanID   int64     `json:"scanId"`
	Kind     string    `json:"kind"`
	EntityID string    `json:"entityId"`
	Summary  string    `json:"summary"`
	At       time.Time `json:"at"`
}

type Topology struct {
	Devices    []Device    `json:"devices"`
	Links      []Link      `json:"links"`
	Containers []Container `json:"containers"`
}

type Role string

const (
	RoleViewer   Role = "viewer"
	RoleOperator Role = "operator"
	RoleAdmin    Role = "admin"
)

type Permission string

const (
	PermTopologyRead Permission = "topology:read"
	PermExportRun    Permission = "export:run"
	PermTokensManage Permission = "tokens:manage"
	PermScansRun     Permission = "scans:run"
	PermDevicesWrite Permission = "devices:write"
	PermAuditRead    Permission = "audit:read"
	PermUsersManage  Permission = "users:manage"
)

type User struct {
	ID                 int64      `json:"id"`
	Username           string     `json:"username"`
	DisplayName        string     `json:"displayName"`
	Role               Role       `json:"role"`
	Disabled           bool       `json:"disabled"`
	MustChangePassword bool       `json:"mustChangePassword"`
	CreatedAt          time.Time  `json:"createdAt"`
	LastLoginAt        *time.Time `json:"lastLoginAt"`
}

// UserRecord stays inside store and auth and never reaches the API.
type UserRecord struct {
	User
	PasswordHash string     `json:"-"`
	FailedLogins int        `json:"-"`
	LockedUntil  *time.Time `json:"-"`
}

type APIToken struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Scope      string     `json:"scope"` // read, write
	CreatedAt  time.Time  `json:"createdAt"`
	ExpiresAt  time.Time  `json:"expiresAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
	Revoked    bool       `json:"revoked"`
}

type AuditEntry struct {
	ID       int64     `json:"id"`
	At       time.Time `json:"at"`
	UserID   *int64    `json:"userId"`
	Username string    `json:"username"`
	Action   string    `json:"action"`
	Entity   string    `json:"entity"`
	EntityID string    `json:"entityId"`
	Result   string    `json:"result"` // ok, denied, error
	IP       string    `json:"ip"`
	Detail   string    `json:"detail"`
}

type ManualDevice struct {
	MAC, IP, Hostname, Label, Notes string
	Kind                            DeviceKind
}

type DeviceEdit struct { // a nil field stays unchanged
	Label   *string
	Notes   *string
	Kind    *DeviceKind // a non-empty value sets kind_locked = 1
	Version int64       // version the editor saw, required
}

type ManualLink struct {
	ADeviceID, BDeviceID int64
	APort, BPort         string
}

type DeviceKey struct {
	MAC string
	IP  string
}

type DeviceInput struct {
	Key         DeviceKey
	Hostname    string
	Vendor      string
	Description string
	Kind        DeviceKind
	Source      string
}

type InterfaceInput struct {
	Device DeviceKey
	Name   string
	MAC    string
	IP     string
	Up     bool
}

type LinkInput struct {
	A, B         DeviceKey
	APort, BPort string
	Source       string
}

type ServiceInput struct {
	Device DeviceKey
	Port   int
	Proto  string
	Name   string
}

type ContainerInput struct {
	Device         DeviceKey
	ID, Name       string
	Image, State   string
	ComposeProject string
	Ports          []PortMapping
	Networks       []ContainerNetwork
}
