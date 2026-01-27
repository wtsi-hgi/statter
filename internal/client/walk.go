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
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"github.com/wtsi-hgi/walk"
)

const (
	lenStart       = 0
	walkInodeStart = 2
	typeStart      = 10
	pathStart      = 14
)

type walker struct {
	*bufio.Reader
	cmd *exec.Cmd
}

func (w *walker) Close() error {
	go w.cmd.Wait() //nolint:errcheck

	return w.cmd.Process.Kill()
}

// CreateWalker starts a file walk for the given path, using the given statter
// executable.
func CreateWalker(exe, path string) (io.ReadCloser, error) {
	cmd := exec.Command(exe, path) //nolint:noctx

	cmd.Stderr = os.Stderr

	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return &walker{bufio.NewReader(out), cmd}, nil
}

// Dirent contains information for a single path entry discovered during the
// walk.
type Dirent struct {
	Path  string
	Mode  fs.FileMode
	Inode uint64
}

// PathCallback is a function that can receive DirEnts.
type PathCallback func(entry *Dirent) error

// PathCallback is a function that can receive errors encountered for a path
// during the walk.
type ErrCallback func(string, error) error

// ReaddirEnt will read a single directory entry from the given Reader and pass
// either a DirEnt to the PathCallback or the path and an error to the
// ErrCallback.
func ReadDirEnt(r io.ReadCloser, cb PathCallback, errCB ErrCallback) error {
	var buf [14]byte

	_, err := io.ReadFull(r, buf[:])
	if err != nil {
		if errors.Is(err, fs.ErrClosed) {
			return io.EOF
		}

		return err
	}

	pathLen := binary.LittleEndian.Uint16(buf[:walkInodeStart])
	if pathLen == 0 {
		return readError(r, binary.LittleEndian.Uint16(buf[2:]))
	}

	return readDirEnt(r, pathLen, &buf, cb, errCB)
}

func readDirEnt(r io.Reader, pl uint16, buf *[14]byte, cb PathCallback, errCB ErrCallback) error {
	pathBuf := make([]byte, pl)

	_, err := io.ReadFull(r, pathBuf)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return io.ErrUnexpectedEOF
		}

		return err
	}

	path := unsafe.String(unsafe.SliceData(pathBuf), pl)
	inode := binary.LittleEndian.Uint64(buf[walkInodeStart:typeStart])
	other := binary.LittleEndian.Uint32(buf[typeStart:])

	if inode == 0 {
		return errCB(path, syscall.Errno(other))
	}

	return cb(&Dirent{
		Path:  path,
		Mode:  fs.FileMode(other),
		Inode: inode,
	})
}

func readError(r io.Reader, errLen uint16) error {
	errBuf := make([]byte, errLen)

	if _, err := io.ReadFull(r, errBuf); err != nil {
		return err
	}

	return errors.New(unsafe.String(unsafe.SliceData(errBuf), errLen)) //nolint:err113
}

// walkWriter provides the functions required for a directory walk.
type walkWriter struct {
	mu  sync.Mutex
	buf [4096 + pathStart]byte
}

func Walk(path string) {
	var w walkWriter

	if err := walk.New(w.PathCallback, true, false).Walk(path, w.ErrCallback); err != nil {
		w.WriteError(err)
	}
}

// PathCallback is called for each Dirent discovered, writing the path length,
// inode, and entry type to stdout, in little endian format,
// followed by the path.
func (w *walkWriter) PathCallback(entry *walk.Dirent) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	buf := entry.AppendTo(w.buf[:pathStart])
	binary.LittleEndian.AppendUint16(w.buf[:lenStart], uint16(len(buf))-pathStart) //nolint:gosec
	binary.LittleEndian.AppendUint64(w.buf[:walkInodeStart], entry.Inode)
	binary.LittleEndian.AppendUint32(w.buf[:typeStart], uint32(entry.Type()))

	_, err := conn.Write(buf)

	return err
}

// ErrCallback is called for each non-fatal error, writing the path length, a
// zero inode, and the error number to stdout, in little endian format, followed
// by the path.
func (w *walkWriter) ErrCallback(path string, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	binary.LittleEndian.AppendUint16(w.buf[:lenStart], uint16(len(path))) //nolint:gosec
	binary.LittleEndian.AppendUint64(w.buf[:walkInodeStart], 0)
	binary.LittleEndian.AppendUint32(w.buf[:typeStart], uint32(err.(syscall.Errno))) //nolint:errcheck,forcetypeassert,errorlint,lll

	conn.Write(append(w.buf[:pathStart], path...)) //nolint:errcheck
}

// WriteError writes fatal errors to stdout.
func (w *walkWriter) WriteError(err error) {
	w.buf[0] = 0
	w.buf[1] = 0
	errMsg := err.Error()

	binary.LittleEndian.AppendUint16(w.buf[:walkInodeStart], uint16(len(errMsg))) //nolint:gosec
	conn.Write(append(w.buf[:14], errMsg...))                                     //nolint:errcheck
}
