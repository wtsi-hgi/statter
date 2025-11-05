/*******************************************************************************
 * Copyright (c) 2025 Genome Research Ltd.
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

package internal

import (
	"encoding/binary"
	"io"
	"os/exec"
)

// Stat takes the net.Conn from either the CreateConns or CreateStatter funcs
// and sends an inode query for the path given.
func Stat(c io.ReadWriter, path string) (uint64, error) {
	if err := writePath(c, path); err != nil {
		return 0, err
	}

	return getInode(c)
}

func writePath(w io.Writer, path string) error {
	var buf [4]byte

	_, err := w.Write(binary.LittleEndian.AppendUint16(buf[:0], uint16(len(path))))
	if err != nil {
		return err
	}

	_, err = io.WriteString(w, path)

	return err
}

func getInode(r io.Reader) (uint64, error) {
	var buf [8]byte

	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return 0, err
	}

	return binary.LittleEndian.Uint64(buf[:]), nil
}

// ReadWriter combines a Reader and a Writer.
type ReadWriter struct {
	io.Reader
	io.WriteCloser
}

// CreateStatter runs the statter at the given path and returns the net.Conn
// used to communicate with it.
func CreateStatter(path string) (io.ReadWriteCloser, int, error) {
	cmd := exec.Command(path)

	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, 0, err
	}

	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, 0, err
	}

	if err := cmd.Start(); err != nil {
		return nil, 0, err
	}

	return ReadWriter{Reader: out, WriteCloser: in}, cmd.Process.Pid, nil
}
