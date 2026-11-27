package model

import (
	"strings"
	"time"
)

// RiskSeverity ranks how much an open service worries the owner.
type RiskSeverity string

const (
	RiskMedium RiskSeverity = "medium"
	RiskHigh   RiskSeverity = "high"
)

// Risk says why an open service deserves a look. It is computed from the service row,
// never stored. Code is a stable id the panel uses to find the text in its own language,
// Message and Why are the English fallback.
type Risk struct {
	Severity RiskSeverity `json:"severity"`
	Message  string       `json:"message"`
	Code     string       `json:"code"`
	Why      string       `json:"why"` // one sentence: what an attacker gains
}

type riskKey struct {
	proto string
	port  int
}

// portRule is a risk that follows from the port and the protocol of a service.
type portRule struct {
	risk  Risk
	ports []riskKey
}

func tcp(ports ...int) []riskKey { return keys("tcp", ports) }
func udp(ports ...int) []riskKey { return keys("udp", ports) }

func keys(proto string, ports []int) []riskKey {
	out := make([]riskKey, len(ports))
	for i, p := range ports {
		out[i] = riskKey{proto, p}
	}
	return out
}

// The table is fixed on purpose: a rule that depends on the config would let a typo hide a
// dangerous service. SMB is medium because a scan sees the port and not the protocol version.
// Netscribe reads only: no rule tries a user name or a password, so a device with default
// credentials is out of scope.
var portRules = []portRule{
	{Risk{RiskHigh, "Telnet sends passwords in clear text", "telnet", "Anyone on the path reads the password and the session."}, tcp(23)},
	{Risk{RiskHigh, "TFTP has no authentication", "tftp", "Anyone who reaches it can read or replace files such as configs."}, udp(69)},
	{Risk{RiskHigh, "VNC is often open without a strong password", "vnc", "A weak or missing password gives a full remote screen."}, tcp(5900)},
	{Risk{RiskHigh, "Docker API is open without TLS", "docker_api", "Anyone who reaches it can start containers as root on the host."}, tcp(2375)},
	{Risk{RiskHigh, "Redis is open to the network", "redis", "It often has no password, so anyone reads and writes its data."}, tcp(6379)},
	{Risk{RiskHigh, "MongoDB is open to the network", "mongodb", "Without authentication anyone reads, changes or deletes the data."}, tcp(27017)},
	{Risk{RiskHigh, "Elasticsearch is open to the network", "elasticsearch", "Its API often has no login, so anyone reads or deletes the indexes."}, tcp(9200)},
	{Risk{RiskHigh, "Memcached is open to the network", "memcached", "Anyone reads its cache, and its UDP port can amplify an attack."}, append(tcp(11211), udp(11211)...)},
	{Risk{RiskMedium, "FTP sends passwords in clear text", "ftp", "Anyone on the path reads the password and the files."}, tcp(21)},
	{Risk{RiskMedium, "SMB is open, check that old versions are off", "smb", "Old SMB versions allow relay and worm attacks."}, tcp(445)},
	{Risk{RiskMedium, "RDP is open, keep it off the open network", "rdp", "It is a common target of password guessing and of remote code flaws."}, tcp(3389)},
	{Risk{RiskMedium, "SNMP is open, check the community and the ACL", "snmp", "A guessed community lists the device and sometimes lets a writer change it."}, udp(161)},
	{Risk{RiskMedium, "MySQL is open to the network", "mysql", "A weak password or a flaw exposes the whole database."}, tcp(3306)},
	{Risk{RiskMedium, "PostgreSQL is open to the network", "postgresql", "A weak password or a flaw exposes the whole database."}, tcp(5432)},
	{Risk{RiskMedium, "Microsoft SQL Server is open to the network", "mssql", "A weak password or a flaw exposes the database and often the host."}, tcp(1433)},
	{Risk{RiskMedium, "CouchDB is open to the network", "couchdb", "Its HTTP API can expose or change data when the admin account is weak."}, tcp(5984)},
	{Risk{RiskMedium, "NFS is open, check the export list", "nfs", "A loose export lets any host on the network mount the files."}, append(tcp(2049), udp(2049)...)},
	{Risk{RiskMedium, "rpcbind is open", "rpcbind", "It lists the RPC services of the host and helps an attacker pick a target."}, append(tcp(111), udp(111)...)},
	{Risk{RiskMedium, "rsync is open, check the modules", "rsync", "An open module lets anyone read, and sometimes write, its files."}, tcp(873)},
	{Risk{RiskMedium, "MQTT is open without TLS", "mqtt", "Messages and passwords cross the network in clear text."}, tcp(1883)},
	{Risk{RiskMedium, "The Windows RPC endpoint mapper is open", "msrpc", "It lists the RPC services of the host and is a path for remote attacks."}, tcp(135)},
}

// The rules that read what the scan already stored about a service.
var (
	riskCertExpired = Risk{RiskMedium, "The TLS certificate has expired", "tls_expired", "Clients cannot trust the service, and people learn to click through warnings."}
	riskSSH1        = Risk{RiskMedium, "The SSH server speaks protocol 1", "ssh_protocol_1", "Protocol 1 has known flaws that allow session hijacking."}
)

// RateService returns the most severe risk of a service, or nil when no rule matches. At
// most one risk is returned per service. When two rules share a severity, the one that
// comes first in the table wins.
func RateService(sv Service) *Risk {
	return rateServiceAt(sv, time.Now())
}

func rateServiceAt(sv Service, now time.Time) *Risk {
	var best *Risk
	consider := func(r Risk) {
		if best == nil || (r.Severity == RiskHigh && best.Severity != RiskHigh) {
			r := r
			best = &r
		}
	}
	for _, rule := range portRules {
		for _, k := range rule.ports {
			if k.port == sv.Port && k.proto == sv.Proto {
				consider(rule.risk)
			}
		}
	}
	if sv.TLS != nil && !sv.TLS.NotAfter.IsZero() && sv.TLS.NotAfter.Before(now) {
		consider(riskCertExpired)
	}
	if sv.Proto == "tcp" && strings.HasPrefix(sv.Banner, "SSH-1.") {
		consider(riskSSH1)
	}
	return best
}
