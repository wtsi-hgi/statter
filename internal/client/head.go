/*******************************************************************************
 * Copyright (c) 2026 Genome Research Ltd.
 *
 * Author: Michael Woolnough <mw31@sanger.ac.uk>
 *
 * Permission is hereby granted, free of charge, to any person obtaining
 * a copy of this software and associated documentation files (the
 * "Software"), to deal in the Software without restriction, including
 * without limitation the rights to use, copy, modify, merge, publish,
 * distribute, sublicense, and/or sell copies of the Software, and to
 * permit persons to whom the Software is furnished to do so, subject to
 * the following conditions:
 *
 * The above copyright notice and this permission notice shall be included
 * in all copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
 * EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
 * MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT.
 * IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY
 * CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT,
 * TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE
 * SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
 ******************************************************************************/

package client

import (
	"encoding/binary"
	"errors"
	"io"
	"os"
	"syscall"
	"time"
)

const headBufSize = 5

// Stat takes the net.Conn from CreateStatter and sends a byte read query for
// the path given.
func Head(c io.ReadWriter, path string) (byte, error) {
	if err := writePath(c, path, true); err != nil {
		return 0, err
	}

	return getByte(path, c)
}

func getByte(path string, r io.Reader) (byte, error) {
	var buf [headBufSize]byte

	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return 0, err
	}

	if buf[0] == 1 {
		return buf[1], nil
	}

	return 0, &os.PathError{
		Op:   "read",
		Path: path,
		Err:  syscall.Errno(binary.LittleEndian.Uint32(buf[1:headBufSize])),
	}
}

func (s *statter) headPath(path string, timeout time.Duration) error {
	ch := make(chan struct{})

	go s.doHead(path, ch)

	select {
	case <-time.After(timeout):
		return ErrTimeout
	case <-ch:
		_, err := conn.Write(s[:headBufSize])
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *statter) doHead(path string, ch chan<- struct{}) {
	defer close(ch)

	s[0] = 0

	f, err := os.Open(path)
	if err != nil {
		binary.LittleEndian.AppendUint32(s[:1], errNo(err))

		return
	}

	_, err = f.Read(s[1:2])
	if err != nil {
		binary.LittleEndian.AppendUint32(s[:1], errNo(err))

		return
	}

	s[0] = 1
}

func errNo(err error) uint32 {
	var sysErr syscall.Errno

	errors.As(err, &sysErr)

	return uint32(sysErr)
}
