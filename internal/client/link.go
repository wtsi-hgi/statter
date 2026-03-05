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
	"io/fs"
	"os"
	"syscall"
)

// Readlink takes the net.Conn from CreateStatter and readlink request for the
// path given.
func Readlink(c io.ReadWriter, path string) (string, error) {
	if err := writePath(c, path, modeReadlink); err != nil {
		return "", err
	}

	var buf [4]byte

	if err := readBuf(c, buf[:2]); err != nil {
		return "", err
	}

	l := binary.LittleEndian.Uint16(buf[:2])
	if l == 0 {
		if err := readBuf(c, buf[:]); err != nil {
			return "", err
		}

		return "", &os.PathError{
			Op:   "readlink",
			Path: path,
			Err:  syscall.Errno(binary.LittleEndian.Uint32(buf[:4])),
		}
	}

	link := make([]byte, l)

	if err := readBuf(c, link); err != nil {
		return "", err
	}

	return string(link), nil
}

func readBuf(c io.Reader, buf []byte) error {
	_, err := io.ReadFull(c, buf)
	if errors.Is(err, fs.ErrClosed) {
		return io.EOF
	}

	return err
}

type link struct {
	path string
	err  uint32
}

func readLink(path string, readlinkCh chan<- link) {
	l, err := os.Readlink(path)

	readlinkCh <- link{l, errNo(err)}
}

func (s *statter) writeLink(l link) error {
	var buf [6]byte

	if l.err == 0 {
		_, err := conn.Write(binary.LittleEndian.AppendUint16(buf[:0], uint16(len(l.path)))) //nolint:gosec
		if err != nil {
			return err
		}

		_, err = io.WriteString(conn, l.path)

		return err
	}

	binary.LittleEndian.AppendUint32(buf[:2], l.err)

	_, err := conn.Write(buf[:])

	return err
}
