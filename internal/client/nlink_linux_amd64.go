package client

import "encoding/binary"

func readNlink(buf *[44]byte) uint64 {
	return binary.LittleEndian.Uint64(buf[12:20])
}
