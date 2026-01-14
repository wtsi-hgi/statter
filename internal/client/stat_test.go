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
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
)

func TestStatLoop(t *testing.T) {
	Convey("You can stat files", t, func() {
		a, b, err := os.Pipe()
		So(err, ShouldBeNil)

		c, d, err := os.Pipe()
		So(err, ShouldBeNil)

		errCh := make(chan error)

		go startRun(readWriter{Reader: a, WriteCloser: d}, errCh)

		local := readWriter{Reader: c, WriteCloser: b}

		tmp := t.TempDir()

		testPathA := filepath.Join(tmp, "aFile")
		testPathB := filepath.Join(tmp, "bFile")

		So(os.WriteFile(testPathA, []byte("some data"), 0600), ShouldBeNil)
		So(os.WriteFile(testPathB, []byte("some data"), 0600), ShouldBeNil)

		fiA, err := os.Lstat(testPathA)
		So(err, ShouldBeNil)

		fiB, err := os.Lstat(testPathB)
		So(err, ShouldBeNil)

		fi, err := Stat(local, testPathA)
		So(err, ShouldBeNil)

		So(fiA.Sys().(*syscall.Stat_t).Ino, ShouldEqual, fi.Sys().(*syscall.Stat_t).Ino) //nolint:errcheck,forcetypeassert

		fi, err = Stat(local, testPathB)
		So(err, ShouldBeNil)

		So(fiB.Sys().(*syscall.Stat_t).Ino, ShouldEqual, fi.Sys().(*syscall.Stat_t).Ino) //nolint:errcheck,forcetypeassert

		fi, err = Stat(local, "/not/a/path")
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldEqual, "lstat /not/a/path: no such file or directory")
		So(fi, ShouldBeNil)

		stat = func(string) (os.FileInfo, error) {
			time.Sleep(time.Second * 5)

			return nil, ErrTimeout
		}

		_, err = Stat(local, testPathA)
		So(err, ShouldEqual, io.EOF)
	})
}

func startRun(remote io.ReadWriteCloser, errCh chan error) {
	conn = remote
	err := Loop()

	remote.Close()

	errCh <- err
}
