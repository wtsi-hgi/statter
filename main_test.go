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

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/wtsi-hgi/statter/client"
	internalclient "github.com/wtsi-hgi/statter/internal/client"
	"github.com/wtsi-hgi/statter/internal/helpers"
)

func TestStatRun(t *testing.T) {
	Convey("You can stat files", t, func() {
		a, b, err := os.Pipe()
		So(err, ShouldBeNil)

		c, d, err := os.Pipe()
		So(err, ShouldBeNil)

		errCh := make(chan error)

		go startRun(internalclient.ReadWriter{Reader: a, WriteCloser: d}, errCh)

		local := internalclient.ReadWriter{Reader: c, WriteCloser: b}

		tmp := t.TempDir()

		testPathA := filepath.Join(tmp, "aFile")
		testPathB := filepath.Join(tmp, "bFile")

		So(os.WriteFile(testPathA, []byte("some data"), 0600), ShouldBeNil)
		So(os.WriteFile(testPathB, []byte("some data"), 0600), ShouldBeNil)

		fiA, err := os.Lstat(testPathA)
		So(err, ShouldBeNil)

		fiB, err := os.Lstat(testPathB)
		So(err, ShouldBeNil)

		fi, err := internalclient.Stat(local, testPathA)
		So(err, ShouldBeNil)

		So(fiA.Sys().(*syscall.Stat_t).Ino, ShouldEqual, fi.Sys().(*syscall.Stat_t).Ino)

		fi, err = internalclient.Stat(local, testPathB)
		So(err, ShouldBeNil)

		So(fiB.Sys().(*syscall.Stat_t).Ino, ShouldEqual, fi.Sys().(*syscall.Stat_t).Ino)

		fi, err = internalclient.Stat(local, "/not/a/path")
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldEqual, "lstat /not/a/path: no such file or directory")
		So(fi, ShouldBeNil)

		stat = func(string) (os.FileInfo, error) {
			time.Sleep(time.Second * 5)

			return nil, ErrTimeout
		}

		_, err = internalclient.Stat(local, testPathA)
		So(err, ShouldEqual, io.EOF)
	})
}

func startRun(remote io.ReadWriteCloser, errCh chan error) {
	conn = remote
	err := run()

	remote.Close() //nolint:errcheck

	errCh <- err
}

var statterExe string

func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	statterExe = filepath.Join(tmp, "statter")

	var code int

	if err := exec.Command("go", "build", "-o", statterExe).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "unexpected error: %s", err)

		code = 1
	} else {
		code = m.Run()
	}

	os.RemoveAll(tmp) //nolint:errcheck
	os.Exit(code)
}

func TestStat(t *testing.T) {
	Convey("You can use the stat client to stat files", t, func() {
		t.TempDir()

		conn, pid, err := internalclient.CreateStatter(statterExe)
		So(err, ShouldBeNil)

		fi, err := internalclient.Stat(conn, statterExe)
		So(err, ShouldBeNil)

		stat, err := os.Lstat(statterExe)
		So(err, ShouldBeNil)

		So(fi.Name(), ShouldEqual, stat.Name())
		So(fi.Size(), ShouldEqual, stat.Size())
		So(fi.ModTime(), ShouldEqual, stat.ModTime().Truncate(time.Second))
		So(fi.Mode(), ShouldEqual, stat.Mode())
		So(fi.IsDir(), ShouldEqual, stat.IsDir())

		parent := filepath.Dir(statterExe)

		fi, err = internalclient.Stat(conn, parent)
		So(err, ShouldBeNil)

		stat, err = os.Lstat(parent)
		So(err, ShouldBeNil)

		So(fi.Name(), ShouldEqual, stat.Name())
		So(fi.Size(), ShouldEqual, stat.Size())
		So(fi.ModTime(), ShouldEqual, stat.ModTime().Truncate(time.Second))
		So(fi.Mode(), ShouldEqual, stat.Mode())
		So(fi.IsDir(), ShouldEqual, stat.IsDir())

		p, err := os.FindProcess(pid)
		So(err, ShouldBeNil)

		So(p.Kill(), ShouldBeNil)

		_, err = internalclient.Stat(conn, statterExe)
		So(err, ShouldEqual, io.EOF)
	})
}

func TestWalker(t *testing.T) {
	Convey("With a test directory to walk", t, func() {
		tmp := t.TempDir()
		paths := helpers.FillDirWithFiles(t, tmp, 3, nil)

		foundPaths := make([]string, 0, len(paths))
		gotErrors := []string{}

		So(client.WalkPath(statterExe, tmp, func(entry *client.Dirent) error {
			foundPaths = append(foundPaths, entry.Path)

			return nil
		}, func(path string, err error) error {
			gotErrors = append(gotErrors, fmt.Sprintf("%s: %s", path, err))

			return nil
		}), ShouldBeNil)
		So(len(gotErrors), ShouldEqual, 0)
		So(len(foundPaths), ShouldEqual, len(paths)+1)
		So(foundPaths[0], ShouldEqual, tmp+"/")
		So(foundPaths[1:], ShouldResemble, paths)

		So(os.Chmod(filepath.Join(tmp, "1"), 0), ShouldBeNil)

		Reset(func() { os.Chmod(filepath.Join(tmp, "1"), 0700) }) //nolint:errcheck

		So(client.WalkPath(statterExe, tmp, func(entry *client.Dirent) error {
			return nil
		}, func(path string, err error) error {
			gotErrors = append(gotErrors, fmt.Sprintf("%s: %s", path, err))

			return nil
		}), ShouldBeNil)
		So(len(gotErrors), ShouldEqual, 1)
		So(gotErrors[0], ShouldEqual, tmp+"/1/: permission denied")

		err := client.WalkPath(statterExe, tmp, func(entry *client.Dirent) error {
			return errors.New("bad!")
		}, func(path string, err error) error {
			return nil
		})
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldEqual, "bad!")

		err = client.WalkPath(statterExe, "", nil, nil)
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldEqual, "invalid argument")
	})
}
