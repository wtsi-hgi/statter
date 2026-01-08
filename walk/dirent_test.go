/*******************************************************************************
 * Copyright (c) 2022 Genome Research Ltd.
 *
 * Author: Sendu Bala <sb10@sanger.ac.uk>
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
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"

	. "github.com/wtsi-hgi/statter/convey"
)

func TestDirent(t *testing.T) {
	Convey("Dirents of different types return correct Is*() results", t, func() {
		tdir := t.TempDir()
		reg := filepath.Join(tdir, "reg")
		f, err := os.Create(reg)
		So(err, ShouldBeNil)

		err = f.Close()
		So(err, ShouldBeNil)

		sym := filepath.Join(tdir, "sym")
		err = os.Symlink(reg, sym)
		So(err, ShouldBeNil)

		info, err := os.Lstat(tdir)
		So(err, ShouldBeNil)

		d := &Dirent{typ: fsModeToType(info.Mode())}
		So(d.IsDir(), ShouldBeTrue)
		So(d.IsRegular(), ShouldBeFalse)
		So(d.IsSymlink(), ShouldBeFalse)

		info, err = os.Lstat(reg)
		So(err, ShouldBeNil)

		d = &Dirent{typ: fsModeToType(info.Mode())}
		So(d.IsDir(), ShouldBeFalse)
		So(d.IsRegular(), ShouldBeTrue)
		So(d.IsSymlink(), ShouldBeFalse)

		info, err = os.Lstat(sym)
		So(err, ShouldBeNil)

		d = &Dirent{typ: fsModeToType(info.Mode())}
		So(d.IsDir(), ShouldBeFalse)
		So(d.IsRegular(), ShouldBeFalse)
		So(d.IsSymlink(), ShouldBeTrue)
	})

	Convey("You can sort a heap of Dirents", t, func() {
		root := newDirent("/some/path/", nil)
		dirA := newDirent("dirA/", root)
		dirB := newDirent("dirB/", root)
		dirC := newDirent("dirC/", root)
		dirD := newDirent("dirD/", dirC)
		dirE := newDirent("dirE/", dirC)

		list := []*Dirent{dirA, dirB, dirD, dirE}
		result := []*Dirent{dirE, dirD, dirB, dirA}

		for i := 0; i < 100; i++ {
			shuffle(list)

			sort.Slice(list, func(i, j int) bool {
				return list[i].compare(list[j]) == -1
			})

			So(list, ShouldResemble, result)
		}
	})

	Convey("You can create a Dirent from a path", t, func() {
		str100 := strings.Repeat("a", 100)
		str255 := strings.Repeat("b", 255)

		for _, test := range [...]struct {
			Path   string
			Output *Dirent
		}{
			{
				"/a/",
				&Dirent{
					parent: nil,
					next:   nullDirEnt,
					name:   &append(make([]byte, 0, 32), "a/"...)[0],
					len:    1,
					typ:    syscall.DT_DIR,
					Inode:  1,
				},
			},
			{
				"/a/b/",
				&Dirent{
					parent: nil,
					next:   nullDirEnt,
					name:   &append(make([]byte, 0, 32), "a/b/"...)[0],
					len:    3,
					typ:    syscall.DT_DIR,
					Inode:  1,
				},
			},
			{
				"/" + str100 + "/b/",
				&Dirent{
					parent: nil,
					next:   nullDirEnt,
					name:   &append(make([]byte, 0, 128), str100+"/b/"...)[0],
					len:    102,
					typ:    syscall.DT_DIR,
					Inode:  1,
				},
			},
			{
				"/" + str100 + "/" + str100 + "/",
				&Dirent{
					parent: nil,
					next:   nullDirEnt,
					name:   &append(make([]byte, 0, 256), str100+"/"+str100+"/"...)[0],
					len:    201,
					typ:    syscall.DT_DIR,
					Inode:  1,
				},
			},
			{
				"/" + str100 + "/" + str100 + "/c/",
				&Dirent{
					parent: nil,
					next:   nullDirEnt,
					name:   &append(make([]byte, 0, 256), str100+"/"+str100+"/c/"...)[0],
					len:    203,
					typ:    syscall.DT_DIR,
					Inode:  1,
				},
			},
			{
				"/" + str100 + "/" + str100 + "/" + str100 + "/",
				&Dirent{
					parent: &Dirent{
						parent: nil,
						next:   nullDirEnt,
						name:   &append(make([]byte, 0, 256), str100+"/"+str100+"/"...)[0],
						len:    201,
						typ:    syscall.DT_DIR,
						Inode:  0,
					},
					next:  nullDirEnt,
					name:  &append(make([]byte, 0, 128), str100+"/"...)[0],
					len:   100,
					typ:   syscall.DT_DIR,
					Inode: 1,
				},
			},
			{
				"/" + str100 + "/" + str255 + "/" + str100 + "/",
				&Dirent{
					parent: &Dirent{
						parent: &Dirent{
							parent: nil,
							next:   nullDirEnt,
							name:   &append(make([]byte, 0, 256), str100+"/"...)[0],
							len:    100,
							typ:    syscall.DT_DIR,
							Inode:  0,
						},
						next:  nullDirEnt,
						name:  &append(make([]byte, 0, 256), str255+"/"...)[0],
						len:   255,
						typ:   syscall.DT_DIR,
						Inode: 0,
					},
					next:  nullDirEnt,
					name:  &append(make([]byte, 0, 128), str100+"/"...)[0],
					len:   100,
					typ:   syscall.DT_DIR,
					Inode: 1,
				},
			},
		} {
			d, err := pathToDirEnt(test.Path[1:], 1)
			So(err, ShouldBeNil)
			So(d, ShouldResemble, test.Output)
			So(string(d.AppendTo(nil)), ShouldEqual, test.Path)
		}
	})
}

func newDirent(path string, parent *Dirent) *Dirent {
	var depth int16

	if parent != nil {
		depth = parent.depth + 1
	}

	pathBytes := []byte(path)

	return &Dirent{
		parent: parent,
		name:   &pathBytes[0],
		len:    uint8(len(path)), //nolint:gosec
		depth:  depth,
	}
}

func shuffle[T any](list []T) {
	for i := range list {
		j := rand.Intn(i + 1) //nolint:gosec
		list[i], list[j] = list[j], list[i]
	}
}
