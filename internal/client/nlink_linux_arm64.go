package client

import "encoding/binary"

func readNlink(buf *[44]byte) uint32 {
	return binary.LittleEndian.Uint32(buf[12:16])
}
