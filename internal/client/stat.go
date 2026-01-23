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
	"encoding/binary"
	"errors"
	"flag"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

const (
	inodeStart  = 0
	modeStart   = 8
	nlinkStart  = 12
	uidStart    = 20
	gidStart    = 24
	sizeStart   = 28
	mtimeStart  = 36
	statBufSize = 44
)

const modeLengthSize = 3

var (
	conn io.ReadWriter = readWriter{Reader: os.Stdin, WriteCloser: os.Stdout} //nolint:gochecknoglobals

	ErrTimeout = errors.New("timeout")

	stat = os.Lstat //nolint:gochecknoglobals
)

// Stat takes the net.Conn from CreateStatter and sends a stat query for the
// path given.
func Stat(c io.ReadWriter, path string) (fs.FileInfo, error) {
	if err := writePath(c, path, false); err != nil {
		return nil, err
	}

	return getStat(path, c)
}

func writePath(w io.Writer, path string, head bool) error {
	var buf [3]byte

	if head {
		buf[0] = 1
	}

	_, err := w.Write(binary.LittleEndian.AppendUint16(buf[:1], uint16(len(path)))) //nolint:gosec
	if err != nil {
		if errors.Is(err, fs.ErrClosed) {
			return io.EOF
		}

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

func (f *fileInfo) Mode() fs.FileMode { //nolint:gocyclo,funlen
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

func getStat(name string, r io.Reader) (fs.FileInfo, error) { //nolint:funlen
	var buf [statBufSize]byte

	if _, err := io.ReadFull(r, buf[:]); err != nil {
		if errors.Is(err, fs.ErrClosed) {
			return nil, io.EOF
		}

		return nil, err
	}

	inode := binary.LittleEndian.Uint64(buf[inodeStart:modeStart])
	if inode == 0 {
		return nil, &os.PathError{
			Op:   "lstat",
			Path: name,
			Err:  syscall.Errno(binary.LittleEndian.Uint32(buf[modeStart:nlinkStart])),
		}
	}

	return &fileInfo{
		name: filepath.Base(name),
		data: syscall.Stat_t{
			Ino:   inode,
			Mode:  binary.LittleEndian.Uint32(buf[modeStart:nlinkStart]),
			Nlink: readNlink(&buf),
			Uid:   binary.LittleEndian.Uint32(buf[uidStart:gidStart]),
			Gid:   binary.LittleEndian.Uint32(buf[gidStart:sizeStart]),
			Size:  int64(binary.LittleEndian.Uint64(buf[sizeStart:mtimeStart])), //nolint:gosec
			Mtim: syscall.Timespec{
				Sec: int64(binary.LittleEndian.Uint64(buf[mtimeStart:statBufSize])), //nolint:gosec
			},
		},
	}, nil
}

// ReadWriter combines a Reader and a Writer.
type readWriter struct {
	io.Reader
	io.WriteCloser
}

// CreateStatter runs the statter at the given path and returns the net.Conn
// used to communicate with it.
func CreateStatter(exe string) (io.ReadWriteCloser, int, error) {
	cmd := exec.Command(exe) //nolint:noctx

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

	go cmd.Wait() //nolint:errcheck

	return readWriter{Reader: out, WriteCloser: in}, cmd.Process.Pid, nil
}

type statter [4096]byte

// readPath reads a length-prefixed path from stdin.
func (s *statter) readPath() (string, bool, error) {
	n, err := conn.Read(s[:modeLengthSize])
	if err != nil {
		return "", false, err
	} else if n != modeLengthSize {
		return "", false, io.ErrShortBuffer
	}

	head := s[0] == 1
	pathLen := binary.LittleEndian.Uint16(s[1:modeLengthSize])

	n, err = conn.Read(s[:pathLen])
	if err != nil {
		return "", false, err
	} else if n != int(pathLen) {
		return "", false, io.ErrShortBuffer
	}

	return string(s[:pathLen]), head, nil
}

// writeStat writes the inode, mode, nlink, uid, gid, size, and mtime to stdout
// in a little endian binary format.
func (s *statter) writeStat(stat *syscall.Stat_t) error {
	binary.LittleEndian.AppendUint64(s[:0], stat.Ino)
	binary.LittleEndian.AppendUint32(s[:8], stat.Mode)
	binary.LittleEndian.AppendUint64(s[:12], uint64(stat.Nlink)) //nolint:unconvert,nolintlint
	binary.LittleEndian.AppendUint32(s[:20], stat.Uid)
	binary.LittleEndian.AppendUint32(s[:24], stat.Gid)
	binary.LittleEndian.AppendUint64(s[:28], uint64(stat.Size))     //nolint:gosec
	binary.LittleEndian.AppendUint64(s[:36], uint64(stat.Mtim.Sec)) //nolint:gosec

	_, err := conn.Write(s[:44])

	return err
}

// Loop infinitely reads a path from stdin, performs a stat with a timeout, and
// writes the result to stdout.
func Loop() error {
	timeout := time.Second

	flag.DurationVar(&timeout, "timeout", timeout, "timeout to wait for stat to finish")
	flag.Parse()

	var s statter

	for {
		path, isHead, err := s.readPath()
		if err != nil {
			return err
		}

		if isHead {
			err = s.headPath(path, timeout)
		} else {
			err = s.statPath(path, timeout)
		}

		if err != nil {
			return err
		}
	}
}

func (s *statter) statPath(path string, timeout time.Duration) error {
	ch := make(chan *syscall.Stat_t)

	go doStat(path, ch)

	select {
	case <-time.After(timeout):
		return ErrTimeout
	case stat := <-ch:
		err := s.writeStat(stat)
		if err != nil {
			return err
		}
	}

	return nil
}

var statErr = new(syscall.Stat_t) //nolint:gochecknoglobals

func doStat(path string, ch chan *syscall.Stat_t) {
	fi, err := stat(path)
	if err != nil {
		statErr.Mode = errNo(err)

		ch <- statErr
	} else {
		ch <- fi.Sys().(*syscall.Stat_t) //nolint:errcheck,forcetypeassert
	}
}
