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

package client

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

// Stat takes the net.Conn from either the CreateConns or CreateStatter funcs
// and sends an stat query for the path given.
func Stat(c io.ReadWriter, path string) (fs.FileInfo, error) {
	if err := writePath(c, path); err != nil {
		return nil, err
	}

	return getStat(path, c)
}

func writePath(w io.Writer, path string) error {
	var buf [2]byte

	_, err := w.Write(binary.LittleEndian.AppendUint16(buf[:0], uint16(len(path))))
	if err != nil {
		return err
	}

	_, err = io.WriteString(w, path)

	return err
}

type fileInfo struct {
	name string
	data syscall.Stat_t
}

func (f *fileInfo) Name() string       { return f.name }
func (f *fileInfo) Size() int64        { return f.data.Size }
func (f *fileInfo) ModTime() time.Time { return time.Unix(f.data.Mtim.Unix()) }
func (f *fileInfo) IsDir() bool        { return f.Mode().IsDir() }
func (f *fileInfo) Sys() any           { return &f.data }

func (f *fileInfo) Mode() fs.FileMode {
	mode := fs.FileMode(f.data.Mode) & fs.ModePerm

	switch f.data.Mode & syscall.S_IFMT {
	case syscall.S_IFBLK:
		mode |= fs.ModeDevice
	case syscall.S_IFCHR:
		mode |= fs.ModeDevice | fs.ModeCharDevice
	case syscall.S_IFDIR:
		mode |= fs.ModeDir
	case syscall.S_IFIFO:
		mode |= fs.ModeNamedPipe
	case syscall.S_IFLNK:
		mode |= fs.ModeSymlink
	case syscall.S_IFSOCK:
		mode |= fs.ModeSocket
	}

	if f.data.Mode&syscall.S_ISGID != 0 {
		mode |= fs.ModeSetgid
	}

	if f.data.Mode&syscall.S_ISUID != 0 {
		mode |= fs.ModeSetuid
	}

	if f.data.Mode&syscall.S_ISVTX != 0 {
		mode |= fs.ModeSticky
	}

	return mode
}

func getStat(name string, r io.Reader) (fs.FileInfo, error) {
	var buf [44]byte

	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return nil, err
	}

	inode := binary.LittleEndian.Uint64(buf[:8])
	if inode == 0 {
		return nil, fs.ErrInvalid
	}

	return &fileInfo{
		name: filepath.Base(name),
		data: syscall.Stat_t{
			Ino:   inode,
			Mode:  binary.LittleEndian.Uint32(buf[8:12]),
			Nlink: readNlink(&buf),
			Uid:   binary.LittleEndian.Uint32(buf[20:24]),
			Gid:   binary.LittleEndian.Uint32(buf[24:28]),
			Size:  int64(binary.LittleEndian.Uint64(buf[28:36])),
			Mtim: syscall.Timespec{
				Sec: int64(binary.LittleEndian.Uint64(buf[36:44])),
			},
		},
	}, nil
}

// ReadWriter combines a Reader and a Writer.
type ReadWriter struct {
	io.Reader
	io.WriteCloser
}

// CreateStatter runs the statter at the given path and returns the net.Conn
// used to communicate with it.
func CreateStatter(exe string) (io.ReadWriteCloser, int, error) {
	cmd := exec.Command(exe)

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

type walker struct {
	*bufio.Reader
	cmd *exec.Cmd
}

func (w *walker) Close() error {
	return w.cmd.Process.Kill()
}

// CreateWalker starts a file walk for the given path, using the given statter
// executable.
func CreateWalker(exe, path string) (io.ReadCloser, error) {
	cmd := exec.Command(exe, path)

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

// PathCallback is a function that can recieve DirEnts.
type PathCallback func(entry *Dirent) error

// PathCallback is a function that can recieve errors encountered for a path
// during the walk.
type ErrCallback func(string, error) error

// ReaddirEnt will read a single directory entry from the given Reader and pass
// either a DirEnt to the PathCallback or the path and an error to the
// ErrCallback.
func ReadDirEnt(r io.ReadCloser, cb PathCallback, errCB ErrCallback) error {
	var buf [14]byte

	_, err := io.ReadFull(r, buf[:])
	if err != nil {
		return err
	}

	pathLen := binary.LittleEndian.Uint16(buf[:2])
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
	inode := binary.LittleEndian.Uint64(buf[2:10])
	other := binary.LittleEndian.Uint32(buf[10:14])

	if inode == 0 {
		return errCB(path, syscall.Errno(other))
	} else {
		return cb(&Dirent{
			Path:  path,
			Mode:  fs.FileMode(other),
			Inode: inode,
		})
	}
}

func readError(r io.Reader, errLen uint16) error {
	errBuf := make([]byte, errLen)

	if _, err := io.ReadFull(r, errBuf); err != nil {
		return err
	}

	return errors.New(unsafe.String(unsafe.SliceData(errBuf), errLen))
}
