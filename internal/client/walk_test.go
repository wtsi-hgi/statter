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
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/wtsi-hgi/statter/internal/testhelper"
)

func TestWalk(t *testing.T) {
	Convey("You can read and write walk data", t, func() {
		pr, pw := io.Pipe()
		conn = &readWriter{WriteCloser: pw}

		var (
			dirents []*Dirent
			errs    []string
		)

		tmp := t.TempDir()
		paths := append([]string{tmp + "/"}, testhelper.FillDirWithFiles(t, tmp, 2, nil)...)

		So(os.Chmod(filepath.Join(tmp, "2"), 0), ShouldBeNil)
		Reset(func() { os.Chmod(filepath.Join(tmp, "2"), 0700) }) //nolint:errcheck

		go func() {
			Walk(tmp)
			pw.Close()
		}()

		cb := func(de *Dirent) error {
			dirents = append(dirents, de)

			return nil
		}

		errCB := func(path string, err error) error {
			errs = append(errs, fmt.Sprintf("%s: %s", path, err))

			return nil
		}

		for {
			err := ReadDirEnt(pr, cb, errCB)
			if errors.Is(err, io.EOF) {
				break
			}

			So(err, ShouldBeNil)
		}

		So(dirents, ShouldResemble, []*Dirent{
			makeDirEnt(t, paths[0]),
			makeDirEnt(t, paths[1]),
			makeDirEnt(t, paths[2]),
			makeDirEnt(t, paths[3]),
			makeDirEnt(t, paths[4]),
			makeDirEnt(t, paths[5]),
			makeDirEnt(t, paths[6]),
		})
		So(errs, ShouldEqual, []string{tmp + "/2/: permission denied"})
	})
}

func makeDirEnt(t *testing.T, path string) *Dirent {
	t.Helper()

	fi, err := os.Stat(path)
	So(err, ShouldBeNil)

	return &Dirent{
		Path:  path,
		Mode:  fi.Mode() &^ fs.ModePerm,
		Inode: fi.Sys().(*syscall.Stat_t).Ino, //nolint:errcheck,forcetypeassert
	}
}
