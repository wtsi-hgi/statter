/*******************************************************************************
 * Copyright (c) 2023, 2024 Genome Research Ltd.
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

package walk

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

const maxEntryNameLength = 255

func newDirentPool(size int) *sync.Pool {
	return &sync.Pool{
		New: func() any {
			return &Dirent{
				name:   &make([]byte, size)[0],
				parent: nullDirEnt,
				next:   nullDirEnt,
			}
		},
	}
}

var (
	direntPool0 = &sync.Pool{ //nolint:gochecknoglobals
		New: func() any {
			return &Dirent{
				parent: nullDirEnt,
				next:   nullDirEnt,
			}
		},
	}
	direntPool32  = newDirentPool(32)  //nolint:gochecknoglobals,mnd
	direntPool64  = newDirentPool(64)  //nolint:gochecknoglobals,mnd
	direntPool128 = newDirentPool(128) //nolint:gochecknoglobals,mnd
	dirEntPool256 = newDirentPool(256) //nolint:gochecknoglobals,mnd

	nullDirEnt = new(Dirent) //nolint:gochecknoglobals
)

func init() { //nolint:gochecknoinits
	nullDirEnt.parent = nullDirEnt
	nullDirEnt.next = nullDirEnt
}

func getDirentPool(size int) *sync.Pool {
	switch {
	case size == 0:
		return direntPool0
	case size <= 32: //nolint:mnd
		return direntPool32
	case size <= 64: //nolint:mnd
		return direntPool64
	case size <= 128: //nolint:mnd
		return direntPool128
	}

	return dirEntPool256
}

func getDirent(size int) *Dirent {
	de := getDirentPool(size).Get().(*Dirent) //nolint:forcetypeassert,errcheck

	if de.name != nil {
		de.len = uint8(size - 1) //nolint:gosec
	}

	return de
}

func putDirent(d *Dirent) {
	d.parent = nullDirEnt
	d.next = nullDirEnt
	d.depth = 0
	length := int(d.len)

	if d.name != nil {
		length++
	}

	getDirentPool(length).Put(d)
}

// Dirent represents a file system directory entry (a file or a directory),
// providing information about the entry's path, type and inode.
type Dirent struct {
	parent *Dirent // left
	next   *Dirent // right
	name   *byte
	len    uint8
	typ    uint8
	depth  int16
	ready  atomic.Uint32

	// Inode is the file system inode number for this entry.
	Inode uint64
}

func NewDirent(path string) (*Dirent, error) {
	if !strings.HasPrefix(path, "/") {
		return nil, fs.ErrInvalid
	}

	mode, inode, err := statNode(path)
	if err != nil {
		return nil, err
	}

	if !mode.IsDir() {
		return nil, fs.ErrInvalid
	}

	return pathToDirEnt(filepath.Clean(path)[1:]+"/", inode)
}

func pathToDirEnt(path string, inode uint64) (*Dirent, error) {
	var de *Dirent

	for len(path) > 0 {
		name := path

		if len(path) > maxEntryNameLength { //nolint:nestif
			split := strings.LastIndexByte(path[:maxEntryNameLength+1], '/')

			if split <= 0 {
				return nil, fs.ErrInvalid
			}

			name = path[:split+1]
			path = path[split+1:]
		} else {
			path = ""
		}

		d := getDirent(len(name))
		d.parent = de
		d.typ = syscall.DT_DIR
		de = d

		copy(d.bytes(), name)
	}

	de.Inode = inode

	return de, nil
}

func fsModeToType(mode fs.FileMode) uint8 {
	switch mode.Type() {
	case fs.ModeDir:
		return syscall.DT_DIR
	case fs.ModeSymlink:
		return syscall.DT_LNK
	case fs.ModeDevice | fs.ModeCharDevice, fs.ModeCharDevice:
		return syscall.DT_CHR
	case fs.ModeDevice:
		return syscall.DT_BLK
	case fs.ModeNamedPipe:
		return syscall.DT_FIFO
	case fs.ModeSocket:
		return syscall.DT_SOCK
	default:
		return syscall.DT_REG
	}
}

func (d *Dirent) markReady() {
	d.ready.Store(1)
}

func (d *Dirent) isReady() bool {
	return d.ready.CompareAndSwap(1, 0)
}

func statNode(path string) (fs.FileMode, uint64, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, 0, err
	}

	if !fi.IsDir() {
		return 0, 0, fs.ErrInvalid
	}

	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, fs.ErrInvalid
	}

	return fi.Mode(), st.Ino, nil
}

// Type returns the type bits of the file mode of this entry.
func (d *Dirent) Type() fs.FileMode {
	switch d.typ {
	case syscall.DT_DIR:
		return fs.ModeDir
	case syscall.DT_LNK:
		return fs.ModeSymlink
	case syscall.DT_CHR:
		return fs.ModeDevice | fs.ModeCharDevice
	case syscall.DT_BLK:
		return fs.ModeDevice
	case syscall.DT_FIFO:
		return fs.ModeNamedPipe
	case syscall.DT_SOCK:
		return fs.ModeSocket
	}

	return 0
}

func (d *Dirent) bytes() []byte {
	return unsafe.Slice(d.name, int(d.len)+1)
}

// IsDir returns true if we are a directory.
func (d *Dirent) IsDir() bool {
	return d.typ == syscall.DT_DIR
}

// IsRegular returns true if we are a regular file.
func (d *Dirent) IsRegular() bool {
	return d.typ == syscall.DT_REG
}

// IsSymlink returns true if we are a symlink.
func (d *Dirent) IsSymlink() bool {
	return d.typ == syscall.DT_LNK
}

func (d *Dirent) AppendTo(p []byte) []byte {
	if d.parent == nil {
		p = append(p, '/')
	} else {
		p = d.parent.AppendTo(p)
	}

	p = append(p, d.bytes()...)

	return p
}

// Bytes returns the FilePath as a literal byte-slice.
func (d *Dirent) Bytes() []byte {
	return d.AppendTo(nil)
}

func (d *Dirent) compare(e *Dirent) int {
	for d.depth > e.depth {
		d = d.parent
	}

	for e.depth > d.depth {
		e = e.parent
	}

	for d.parent != e.parent {
		d = d.parent
		e = e.parent
	}

	return bytes.Compare(e.bytes(), d.bytes())
}

func (d *Dirent) done() *Dirent {
	if d.IsDir() {
		for !d.isReady() {
			time.Sleep(time.Millisecond)
		}
	}

	next := d.next

	if d.name == nil {
		putDirent(d.parent)
	}

	if !d.IsDir() {
		putDirent(d)
	} else {
		d.next = nullDirEnt
	}

	return next
}

func (d *Dirent) insert(e *Dirent) *Dirent { //nolint:gocyclo
	if d == nullDirEnt {
		return e
	}

	switch bytes.Compare(d.bytes(), e.bytes()) {
	case 1:
		d.parent = d.parent.insert(e)
	case -1:
		d.next = d.next.insert(e)
	}

	d.setDepth()

	switch d.parent.depth - d.next.depth {
	case -2:
		if d.next.parent.depth > d.next.next.depth {
			d.next = d.next.rotateRight()
		}

		return d.rotateLeft()
	case 2: //nolint:mnd
		if d.parent.next.depth > d.parent.parent.depth {
			d.parent = d.parent.rotateLeft()
		}

		return d.rotateRight()
	}

	return d
}

func (d *Dirent) setDepth() {
	if d == nullDirEnt {
		return
	}

	if d.parent.depth > d.next.depth {
		d.depth = d.parent.depth + 1
	} else {
		d.depth = d.next.depth + 1
	}
}

func (d *Dirent) rotateLeft() *Dirent {
	n := d.next
	d.next = n.parent
	n.parent = d

	d.setDepth()
	n.setDepth()

	return n
}

func (d *Dirent) rotateRight() *Dirent {
	n := d.parent
	d.parent = n.next
	n.next = d

	d.setDepth()
	n.setDepth()

	return n
}

func (d *Dirent) flatten(parent, prev *Dirent, depth int16) *Dirent {
	if d == nullDirEnt {
		return prev
	}

	d.parent.flatten(parent, prev, depth).next = d
	d.parent = parent
	d.depth = depth

	return d.next.flatten(parent, d, depth)
}
