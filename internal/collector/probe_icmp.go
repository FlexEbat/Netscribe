package collector

import "encoding/binary"

const (
	icmpEchoRequest = 8
	icmpEchoReply   = 0
)

// echoRequest builds an ICMP echo request with the given sequence number. The kernel
// replaces the identifier on a ping socket.
func echoRequest(seq uint16) []byte {
	msg := make([]byte, 8, 17)
	msg[0] = icmpEchoRequest
	binary.BigEndian.PutUint16(msg[6:], seq)
	msg = append(msg, "netscribe"...)
	binary.BigEndian.PutUint16(msg[2:], icmpChecksum(msg))
	return msg
}

// icmpChecksum is the Internet checksum of RFC 1071.
func icmpChecksum(b []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(b); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(b[i:]))
	}
	if len(b)%2 == 1 {
		sum += uint32(b[len(b)-1]) << 8
	}
	for sum>>16 != 0 {
		sum = sum&0xffff + sum>>16
	}
	return ^uint16(sum) // sum fits in 16 bits after folding
}
