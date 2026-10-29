package model

// RiskSeverity ranks how much an open service worries the owner.
type RiskSeverity string

const (
	RiskMedium RiskSeverity = "medium"
	RiskHigh   RiskSeverity = "high"
)

// Risk says why an open service deserves a look. It is computed from the port and the
// protocol, never stored.
type Risk struct {
	Severity RiskSeverity `json:"severity"`
	Message  string       `json:"message"`
}

type riskKey struct {
	proto string
	port  int
}

// The table is fixed on purpose: a rule that depends on the config would let a typo hide a
// dangerous service. SMB is medium because a scan sees the port and not the protocol version.
var riskRules = map[riskKey]Risk{
	{"tcp", 23}:   {RiskHigh, "Telnet sends passwords in clear text"},
	{"udp", 69}:   {RiskHigh, "TFTP has no authentication"},
	{"tcp", 5900}: {RiskHigh, "VNC is often open without a strong password"},
	{"tcp", 21}:   {RiskMedium, "FTP sends passwords in clear text"},
	{"tcp", 445}:  {RiskMedium, "SMB is open, check that old versions are off"},
	{"tcp", 3389}: {RiskMedium, "RDP is open, keep it off the open network"},
	{"udp", 161}:  {RiskMedium, "SNMP is open, check the community and the ACL"},
}

// RateService returns the risk of an open service, or nil when the port is not on the list.
func RateService(port int, proto string) *Risk {
	r, ok := riskRules[riskKey{proto, port}]
	if !ok {
		return nil
	}
	return &r
}
