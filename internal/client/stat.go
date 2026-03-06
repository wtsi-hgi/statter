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

	ErrTimeout     = errors.New("timeout")
	ErrInvalidMode = errors.New("invalid mode")

	stat = os.Lstat //nolint:gochecknoglobals
)

// Stat takes the net.Conn from CreateStatter and sends a stat query for the
// path given.
func Stat(c io.ReadWriter, path string) (fs.FileInfo, error) {
	if err := writePath(c, path, modeStat); err != nil {
		return nil, err
	}

	return getStat(path, c)
}

func writePath(w io.Writer, path string, m mode) error {
	var buf [3]byte

	buf[0] = byte(m)

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

	if err := readBuf(r, buf[:]); err != nil {
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

type mode uint8

const (
	modeStat mode = iota
	modeHead
	modeReadlink

	invalidMode
)

// readPath reads a length-prefixed path from stdin.
func (s *statter) readPath() (string, mode, error) {
	if err := readBuf(conn, s[:modeLengthSize]); err != nil {
		return "", 0, err
	}

	pathLen := binary.LittleEndian.Uint16(s[1:modeLengthSize])
	m := mode(s[0])

	if m >= invalidMode {
		return "", 0, ErrInvalidMode
	}

	if err := readBuf(conn, s[:pathLen]); err != nil {
		return "", 0, err
	}

	return string(s[:pathLen]), m, nil
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

	headPath := make(chan string)
	statPath := make(chan string)
	readlinkPath := make(chan string)
	headCh := make(chan struct{})
	statCh := make(chan *syscall.Stat_t)
	readlinkCh := make(chan link)

	go s.do(statPath, headPath, readlinkPath, statCh, headCh, readlinkCh)

	return s.doLoop(timeout, statPath, headPath, readlinkPath, statCh, headCh, readlinkCh)
}

func (s *statter) doLoop( //nolint:gocyclo,cyclop,funlen
	timeout time.Duration,
	statPath, headPath, readlinkPath chan<- string,
	statCh <-chan *syscall.Stat_t,
	headCh <-chan struct{},
	readlinkCh <-chan link,
) error {
	for {
		path, mode, err := s.readPath()
		if err != nil {
			return err
		}

		switch mode { //nolint:exhaustive
		case modeHead:
			headPath <- path
		case modeStat:
			statPath <- path
		case modeReadlink:
			readlinkPath <- path
		}

		select {
		case <-time.After(timeout):
			return ErrTimeout
		case <-headCh:
			_, err = conn.Write(s[:headBufSize])
		case stat := <-statCh:
			err = s.writeStat(stat)
		case l := <-readlinkCh:
			err = s.writeLink(l)
		}

		if err != nil {
			return err
		}
	}
}

func (s *statter) do(statPath, headPath, readlinkPath <-chan string,
	statCh chan<- *syscall.Stat_t, headCh chan<- struct{}, readlinkCh chan<- link,
) {
	for {
		select {
		case path := <-statPath:
			doStat(path, statCh)
		case path := <-headPath:
			s.doHead(path, headCh)
		case path := <-readlinkPath:
			readLink(path, readlinkCh)
		}
	}
}

var statErr = new(syscall.Stat_t) //nolint:gochecknoglobals

func doStat(path string, ch chan<- *syscall.Stat_t) {
	fi, err := stat(path)
	if err != nil {
		statErr.Mode = errNo(err)

		ch <- statErr
	} else {
		ch <- fi.Sys().(*syscall.Stat_t) //nolint:errcheck,forcetypeassert
	}
}
