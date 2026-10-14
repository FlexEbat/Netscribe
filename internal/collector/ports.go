package collector

// wellKnownPorts names the TCP ports people expect to meet on a network. It is a small
// built-in table of IANA service names (a few have the name everyone uses instead of the
// registry's long one). A port that is not here has no name, and the port number says it.
var wellKnownPorts = map[int]string{
	21: "ftp", 22: "ssh", 23: "telnet", 25: "smtp", 53: "domain", 80: "http",
	110: "pop3", 111: "sunrpc", 135: "epmap", 139: "netbios-ssn", 143: "imap",
	389: "ldap", 443: "https", 445: "microsoft-ds", 465: "submissions", 515: "printer",
	548: "afpovertcp", 554: "rtsp", 587: "submission", 631: "ipp", 636: "ldaps",
	873: "rsync", 993: "imaps", 995: "pop3s", 1433: "ms-sql-s", 1723: "pptp",
	1883: "mqtt", 2049: "nfs", 3306: "mysql", 3389: "ms-wbt-server", 5060: "sip",
	5432: "postgresql", 5900: "rfb", 6379: "redis", 8080: "http-alt", 8443: "https-alt",
	9100: "pdl-datastream", 27017: "mongodb",
}

// portName returns the service name of a TCP port, or "".
func portNameOf(port int) string { return wellKnownPorts[port] }
