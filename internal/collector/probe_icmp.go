package collector

const (
	icmpEchoRequest = 8
	icmpEchoReply   = 0
)

// echoRequest builds an ICMP echo request with the given sequence number. The kernel
// replaces the identifier on a ping socket.
func echoRequest(seq uint16) []byte {
	msg := []byte{icmpEchoRequest, 0, 0, 0, 0, 0, byte(seq >> 8), byte(seq), 'n', 'e', 't', 's', 'c', 'r', 'i', 'b', 'e'}
	sum := icmpChecksum(msg)
	msg[2], msg[3] = byte(sum>>8), byte(sum)
	return msg
}

// icmpChecksum is the Internet checksum of RFC 1071.
func icmpChecksum(b []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(b); i += 2 {
		sum += uint32(b[i])<<8 | uint32(b[i+1])
	}
	if len(b)%2 == 1 {
		sum += uint32(b[len(b)-1]) << 8
	}
	for sum>>16 != 0 {
		sum = sum&0xffff + sum>>16
	}
	return ^uint16(sum)
}
