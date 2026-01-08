/*******************************************************************************
 * Copyright (c) 2022, 2023, 2024 Genome Research Ltd.
 *
 * Author: Sendu Bala <sb10@sanger.ac.uk>
 *         Michael Woolnough <mw31@sanger.ac.uk>
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

// package walk is used to quickly walk a filesystem to just see what paths
// there are on it.

package walk

import (
	"bytes"
	"context"
	"errors"
	"os"
	"slices"
	"syscall"
	"time"
	"unsafe"
)

const walkers = 16

const (
	dot    = ".\x00"
	dotdot = "..\x00"
)

// PathCallback is a callback used by Walker.Walk() that receives a directory
// entry containing the path, inode and file type each time it's called. It
// should only return an error if you can no longer cope with receiving more
// paths, and wish to terminate the Walk.
type PathCallback func(entry *Dirent) error

// Walker can be used to quickly walk a filesystem to just see what paths there
// are on it.
type Walker struct {
	pathCB         PathCallback
	sendDirs       bool
	ignoreSymlinks bool
	stats
}

// New creates a new Walker that can Walk() a filesystem and send all the
// encountered paths to the given PathCallback.
//
// Set includeDirs to true to have your PathCallback receive directory paths in
// addition to file paths.
//
// Set ignoreSymlinks to true to have symlinks not be sent do your PathCallback.
func New(cb PathCallback, includDirs, ignoreSymlinks bool) *Walker {
	return &Walker{
		pathCB:         cb,
		sendDirs:       includDirs,
		ignoreSymlinks: ignoreSymlinks,
	}
}

func (w *Walker) EnableStats(interval time.Duration, output StatsOutput) {
	w.stats.interval = interval
	w.stats.output = output
}

// ErrorCallback is a callback function you supply Walker.Walk(), and it
// will be provided problematic paths encountered during the walk.
type ErrorCallback func(path string, err error)

// Walk will discover all the paths nested under the given dir, and send them to
// our PathCallback.
//
// The given error callback will be called every time there's an error handling
// a file during the walk. Errors writing to an output file will result in the
// walk terminating early and this method returning the error; other kinds of
// errors will mean the path isn't output, but the walk will continue and this
// method won't return an error.
func (w *Walker) Walk(dir string, errCB ErrorCallback) error {
	r, err := NewDirent(dir)
	if err != nil {
		return err
	}

	requestCh := make(chan *Dirent)
	sortedRequestCh := make(chan *Dirent)
	ctx, stop := context.WithCancel(context.Background())

	if w.stats.output != nil {
		defer w.stats.LogStats(ctx)()
	}

	for range walkers {
		go w.handleDirReads(ctx, sortedRequestCh, requestCh, errCB, w.ignoreSymlinks)
	}

	go sortDirents(ctx, requestCh, sortedRequestCh)

	sortedRequestCh <- r

	defer stop()

	return w.sendDirentsToPathCallback(r)
}

func (w *Walker) sendDirentsToPathCallback(r *Dirent) error {
	for ; r != nullDirEnt; r = r.done() {
		if r.name != nil && (w.sendDirs || !r.IsDir()) {
			if err := w.pathCB(r); err != nil {
				return err
			}
		}
	}

	return nil
}

type heap []*Dirent

func (h *heap) Insert(req *Dirent) {
	pos, _ := slices.BinarySearchFunc(*h, req, (*Dirent).compare)
	*h = slices.Insert(*h, pos, req)
}

func (h heap) Top() *Dirent {
	return h[len(h)-1]
}

func (h *heap) Pop() {
	*h = (*h)[:len(*h)-1]
}

func (h *heap) Push(req *Dirent) {
	*h = append(*h, req)
}

func sortDirents(ctx context.Context, requestCh <-chan *Dirent, //nolint:gocyclo
	sortedRequestCh chan<- *Dirent,
) {
	var h heap

	for {
		if len(h) == 0 {
			select {
			case <-ctx.Done():
				return
			case req := <-requestCh:
				h.Push(req)
			}
		}

		select {
		case <-ctx.Done():
			return
		case req := <-requestCh:
			h.Insert(req)
		case sortedRequestCh <- h.Top():
			h.Pop()
		}
	}
}

func (w *Walker) handleDirReads(ctx context.Context, sortedRequests, requestCh chan *Dirent,
	errCB ErrorCallback, ignoreSymlinks bool,
) {
	buffer := make([]byte, os.Getpagesize())

	var pathBuffer [maxPathLength + maxFilenameLength + 1]byte

Loop:
	for {
		select {
		case <-ctx.Done():
			break Loop
		case request := <-sortedRequests:
			l := len(request.AppendTo(pathBuffer[:0]))
			pathBuffer[l] = 0

			children, err := scan(buffer, &pathBuffer[0], ignoreSymlinks, &w.stats)
			if err != nil {
				errCB(string(pathBuffer[:l]), err)
			}

			go scanChildDirs(ctx, requestCh, request, children)
		}
	}
}

func scanChildDirs(ctx context.Context, requestCh chan *Dirent, request, children *Dirent) {
	marker := getDirent(0)
	marker.next = request.next
	marker.parent = request

	sortChildren(children).flatten(request, request, request.depth+1).next = marker

	for r := request.next; r != marker; {
		next := r.next

		if r.IsDir() {
			select {
			case <-ctx.Done():
				return
			case requestCh <- r:
			}
		}

		r = next
	}

	request.markReady()
}

func sortChildren(children *Dirent) *Dirent {
	root := nullDirEnt

	for children != nullDirEnt {
		this := children
		children = children.next
		this.next = nullDirEnt
		root = root.insert(this)
	}

	return root
}

type dirent struct {
	Ino    uint64
	Off    int64
	Reclen uint16
	Type   uint8
	Name   uint8
}

type scanner struct {
	buffer, read []byte
	fh           int
	stats        *stats
	*dirent
	err error
}

func (s *scanner) Next() bool {
	for len(s.read) == 0 {
		n, err := syscall.ReadDirent(s.fh, s.buffer)

		s.stats.AddRead(n)

		if err != nil {
			if errors.Is(err, syscall.EINTR) {
				continue
			}

			s.err = err

			return false
		}

		if n <= 0 {
			return false
		}

		s.read = s.buffer[:n]
	}

	s.dirent = (*dirent)(unsafe.Pointer(&s.read[0]))
	s.read = s.read[s.Reclen:]

	if s.dirent.Type == syscall.DT_UNKNOWN {
		return s.getType()
	}

	return true
}

func (s *scanner) getType() bool {
	const symlinkNoFollow = 0x100

	var stat syscall.Stat_t

	if _, _, err := syscall.Syscall6(statCall, uintptr(s.fh),
		uintptr(unsafe.Pointer(&s.dirent.Name)), uintptr(unsafe.Pointer(&stat)),
		symlinkNoFollow, 0, 0); err != 0 {
		s.err = err

		s.stats.AddStat()

		return false
	}

	s.dirent.Type = modeToType(stat.Mode)

	return true
}

func modeToType(mode uint32) uint8 {
	switch mode & syscall.S_IFMT {
	case syscall.S_IFBLK:
		return syscall.DT_BLK
	case syscall.S_IFCHR:
		return syscall.DT_CHR
	case syscall.S_IFDIR:
		return syscall.DT_DIR
	case syscall.S_IFIFO:
		return syscall.DT_FIFO
	case syscall.S_IFLNK:
		return syscall.DT_LNK
	case syscall.S_IFSOCK:
		return syscall.DT_SOCK
	}

	return syscall.DT_REG
}

func (s *scanner) Get() ([]byte, uint8, uint64) {
	return s.getName(), s.Type, s.Ino
}

func (s *scanner) getName() []byte {
	name := unsafe.Slice(&s.Name, uintptr(s.Reclen)-unsafe.Offsetof(s.dirent.Name))

	l := bytes.IndexByte(name, 0)
	if l <= 0 || string(name[:2]) == dot || string(name[:3]) == dotdot {
		return nil
	}

	if s.Type == syscall.DT_DIR {
		name[l] = '/'
		l++
	}

	return name[:l]
}

func scan(buffer []byte, path *byte, ignoreSymlinks bool, stats *stats) (*Dirent, error) {
	children := nullDirEnt

	fh, err := open(path, stats)
	if err != nil {
		return children, err
	}

	defer closeFH(fh, stats)

	s := scanner{buffer: buffer, fh: fh, stats: stats}

	for s.Next() {
		name, mode, inode := s.Get()
		if inode == 0 || len(name) == 0 || ignoreSymlinks && mode == syscall.DT_LNK {
			continue
		}

		de := getDirent(len(name))
		de.typ = mode
		de.Inode = inode
		de.next = children
		children = de

		copy(de.bytes(), name)
	}

	return children, s.err
}

func open(path *byte, stats *stats) (int, error) {
	defer stats.AddOpen()

	const atFDCWD = -0x64

	dfd := atFDCWD

	ifh, _, err := syscall.Syscall6(
		syscall.SYS_OPENAT,
		uintptr(dfd),
		uintptr(unsafe.Pointer(path)),
		uintptr(syscall.O_RDONLY),
		0, 0, 0)
	if err != 0 {
		return 0, err
	}

	return int(ifh), nil
}

func closeFH(fh int, stats *stats) {
	defer stats.AddClose()

	syscall.Close(fh)
}
