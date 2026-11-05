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

type Device struct {
	ID  int64  `json:"id"`
	MAC string `json:"mac"`
	IP  string `json:"ip"`
	// IP6 is the one IPv6 address of the device, in canonical form, empty when it has none.
	IP6      string     `json:"ip6"`
	Hostname string     `json:"hostname"`
	Vendor   string     `json:"vendor"`
	Kind     DeviceKind `json:"kind"`
	// KindScore (0 to 100) and KindReasons (at most 5 short phrases) say why the classifier
	// chose the kind. For an unknown kind the reasons name the candidates.
	KindScore   int      `json:"kindScore"`
	KindReasons []string `json:"kindReasons"`
	Description string   `json:"description"`
	// OSGuess is the system nmap guessed, for example "Linux 5.3 - 5.4". It is empty when
	// nmap never ran for the device or had no guess.
	OSGuess    string `json:"osGuess"`
	Source     string `json:"source"`
	Online     bool   `json:"online"`
	Manual     bool   `json:"manual"`
	Label      string `json:"label"`
	Notes      string `json:"notes"`
	KindLocked bool   `json:"kindLocked"`
	// Trusted is true when a person marked the device as known. A scan creates devices
	// untrusted.
	Trusted bool `json:"trusted"`
	// GroupID is the group a person chose, nil when none was chosen. Group is the effective
	// group: the chosen one, else the first group whose ranges contain the address.
	GroupID     *int64    `json:"groupId"`
	Group       *GroupRef `json:"group"`
	GroupAuto   bool      `json:"groupAuto"`
	Version     int64     `json:"version"`
	FirstSeenAt time.Time `json:"firstSeenAt"`
	LastSeenAt  time.Time `json:"lastSeenAt"`
	X           *float64  `json:"x"`
	Y           *float64  `json:"y"`
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
	ID       int64 `json:"id"`
	DeviceID int64 `json:"deviceId"`
	// IP is the address the service answered on when that is not the IPv4 address of the
	// device, that is its IPv6 address. Empty means the primary address.
	IP          string    `json:"ip"`
	Port        int       `json:"port"`
	Proto       string    `json:"proto"`
	Name        string    `json:"name"`
	Banner      string    `json:"banner"`
	FirstSeenAt time.Time `json:"firstSeenAt"`
	LastSeenAt  time.Time `json:"lastSeenAt"`
	// Risk is computed from the port and the protocol, nil when the service is not on the list.
	Risk *Risk `json:"risk"`
	// TLS is the certificate the service presented, nil when none was read.
	TLS *TLSCert `json:"tls"`
}

// TLSCert is what a TLS service showed in its handshake. It is read, never validated: an
// expired or self signed certificate is still data about the device.
type TLSCert struct {
	Subject     string    `json:"subject"` // the common name of the subject
	SANs        []string  `json:"sans"`    // the first alternative names
	Issuer      string    `json:"issuer"`  // the common name of the issuer
	NotBefore   time.Time `json:"notBefore"`
	NotAfter    time.Time `json:"notAfter"`
	Fingerprint string    `json:"fingerprint"` // SHA-256 of the DER bytes, lower case hex
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
	// PermSystemManage covers the settings page, the update and the restart of the service.
	// Only an administrator holds it, and no token ever does.
	PermSystemManage Permission = "system:manage"
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
	MAC, IP, IP6, Hostname, Label, Notes string
	Kind                                 DeviceKind
}

type DeviceEdit struct { // a nil field stays unchanged
	Label   *string
	Notes   *string
	Kind    *DeviceKind // a non-empty value sets kind_locked = 1
	Trusted *bool
	GroupID *int64 // 0 removes the group, nil keeps it
	Version int64  // version the editor saw, required
}

type ManualLink struct {
	ADeviceID, BDeviceID int64
	APort, BPort         string
}

// Address is the address a device is shown by: its IPv4 address, else its IPv6 address.
func (d Device) Address() string {
	if d.IP != "" {
		return d.IP
	}
	return d.IP6
}

// Addresses lists every address of the device for a table cell, IPv4 first.
func (d Device) Addresses() string {
	if d.IP != "" && d.IP6 != "" {
		return d.IP + ", " + d.IP6
	}
	return d.Address()
}

type DeviceKey struct {
	MAC string
	IP  string
}

type DeviceInput struct {
	Key DeviceKey
	// IP6 is the IPv6 address the device was found on. A device is still identified by its
	// MAC: the address only fills the second column.
	IP6         string
	Hostname    string
	Vendor      string
	Description string
	OSGuess     string // the system nmap guessed, kept when a later input has none
	Kind        DeviceKind
	Source      string
	// KindScore and KindReasons say why the classifier chose Kind, see section 5.2.
	KindScore   int
	KindReasons []string
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
	// IP is the address the service answered on when that is not the IPv4 address of the
	// device, that is its IPv6 address. Empty means the primary address.
	IP    string
	Port  int
	Proto string
	Name  string
	// Banner is what the service said about itself, for example the HTTP Server header.
	// It comes from the network, so the store cleans and cuts it.
	Banner string
	// TLS is the certificate read in the handshake, nil for a service that has none or whose
	// handshake failed; the stored certificate stays then.
	TLS *TLSCert
}

type ContainerInput struct {
	Device         DeviceKey
	ID, Name       string
	Image, State   string
	ComposeProject string
	Ports          []PortMapping
	Networks       []ContainerNetwork
}

// Group names a part of the network: a site, a VLAN, a range of addresses.
type Group struct {
	ID          int64    `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	VLAN        *int     `json:"vlan"`
	Ranges      []string `json:"ranges"`
	DeviceCount int      `json:"deviceCount"` // devices whose effective group this is
	Version     int64    `json:"version"`
}

// GroupRef is the part of a group that a device carries.
type GroupRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	VLAN *int   `json:"vlan"`
}

// ManualGroup is the input of a new group, and the checked form of an edit.
type ManualGroup struct {
	Name, Description string
	VLAN              *int
	Ranges            []string
}

// GroupEdit changes a group; a nil field stays as it is. A non-nil VLAN pointing at 0
// removes the VLAN number.
type GroupEdit struct {
	Name, Description *string
	VLAN              *int
	Ranges            *[]string
	Version           int64
}
