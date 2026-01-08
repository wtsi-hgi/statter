/*******************************************************************************
 * Copyright (c) 2023 Genome Research Ltd.
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
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	. "github.com/wtsi-hgi/statter/convey"
	"github.com/wtsi-hgi/statter/internal/helpers"
)

const permNoWrite = 0500

func TestWalk(t *testing.T) {
	Convey("Given a directory to walk and an output directory", t, func() {
		walkDir, outDir, expectedPaths := prepareTestDirs(t)

		var (
			mu         sync.Mutex
			walkErrors []error
		)

		cb := func(_ string, err error) {
			mu.Lock()
			defer mu.Unlock()

			walkErrors = append(walkErrors, err)
		}

		Convey("You can output the paths to a file", func() {
			ok := testOutputToFiles(true, false, walkDir, outDir, cb, expectedPaths, &StatData{
				Open:  65,
				Close: 65,
			})
			So(ok, ShouldBeTrue)
			So(len(walkErrors), ShouldEqual, 0)
		})

		Convey("You can output the paths to multiple files", func() {
			n := 4
			files, err := NewFiles(outDir, n)
			So(err, ShouldBeNil)

			w := New(files.WritePaths(), true, false)
			err = w.Walk(walkDir, cb)
			So(err, ShouldBeNil)

			err = files.Close()
			So(err, ShouldBeNil)

			splitExpected := make([][]string, n)
			splitI := 0

			for _, path := range expectedPaths {
				splitExpected[splitI] = append(splitExpected[splitI], path)
				splitI++

				if splitI == n {
					splitI = 0
				}
			}

			for i, expectedPaths := range splitExpected {
				outPath := filepath.Join(outDir, fmt.Sprintf("walk.%d", i+1))
				content, errr := os.ReadFile(outPath)

				if i < n {
					So(errr, ShouldBeNil)

					So(files.Paths[i], ShouldEqual, outPath)

					ok := checkPaths(string(content), expectedPaths)
					So(ok, ShouldBeTrue)
				} else {
					So(errr, ShouldNotBeNil)
				}
			}

			So(len(walkErrors), ShouldEqual, 0)

			err = files.files[0].Close()
			So(err, ShouldNotBeNil)

			err = files.Close()
			So(err, ShouldNotBeNil)
		})

		Convey("You can ignore symlinks", func() {
			expectedPaths = slices.Delete(expectedPaths, 3, 4)
			ok := testOutputToFiles(true, true, walkDir, outDir, cb, expectedPaths, nil)
			So(ok, ShouldBeTrue)
			So(len(walkErrors), ShouldEqual, 0)
		})

		Convey("Write errors during a walk are reported and the walk terminated", func() {
			files, err := NewFiles(outDir, 1)
			So(err, ShouldBeNil)

			w := New(files.WritePaths(), true, false)
			err = files.files[0].Close()
			So(err, ShouldBeNil)

			err = w.Walk(walkDir, cb)
			if err == nil {
				err = files.Close()
			}

			So(err, ShouldNotBeNil)

			var writeError *WriteError

			So(errors.As(err, &writeError), ShouldBeTrue)

			werr := err.(*WriteError) //nolint:errcheck,errorlint,forcetypeassert
			So(werr.Unwrap(), ShouldEqual, werr.Err)
		})

		Convey("Read errors during a walk are reported and the path skipped", func() {
			files, err := NewFiles(outDir, 1)
			So(err, ShouldBeNil)

			w := New(files.WritePaths(), true, false)
			err = w.Walk("/root", cb)
			So(err, ShouldBeNil)

			mu.Lock()
			l := len(walkErrors)
			mu.Unlock()

			So(l, ShouldEqual, 1)

			var writeError *WriteError

			mu.Lock()
			err = walkErrors[0]
			mu.Unlock()

			So(errors.As(err, &writeError), ShouldBeFalse)

			outPath := filepath.Join(outDir, "walk.1")
			_, err = os.ReadFile(outPath)
			So(err, ShouldBeNil)
		})

		Convey("You can get the inode of files in your callback", func() {
			tdir := t.TempDir()
			file := filepath.Join(tdir, "file")
			f, err := os.Create(file)
			So(err, ShouldBeNil)

			err = f.Close()
			So(err, ShouldBeNil)

			info, err := os.Stat(file)
			So(err, ShouldBeNil)

			u, ok := info.Sys().(*syscall.Stat_t)
			So(ok, ShouldBeTrue)
			So(u.Ino, ShouldNotEqual, 0)

			var gotInode uint64

			pcb := func(entry *Dirent) error {
				gotInode = entry.Inode

				return nil
			}

			w := New(pcb, false, false)
			err = w.Walk(tdir, cb)
			So(err, ShouldBeNil)
			So(len(walkErrors), ShouldEqual, 0)
			So(gotInode, ShouldEqual, u.Ino)
		})

		Convey("You can print just the files", func() {
			expected := make([]string, 0, len(expectedPaths))

			for _, path := range expectedPaths {
				if !strings.HasSuffix(path, "/\"") {
					expected = append(expected, path)
				}
			}

			ok := testOutputToFiles(false, false, walkDir, outDir, cb, expected, nil)
			So(ok, ShouldBeTrue)
			So(len(walkErrors), ShouldEqual, 0)
		})
	})

	Convey("You can't create output files in a bad directory", t, func() {
		_, err := NewFiles("/foo", 1)
		So(err, ShouldNotBeNil)

		tmpDir := t.TempDir()
		outDir := filepath.Join(tmpDir, "out")
		err = os.Mkdir(outDir, permNoWrite)
		So(err, ShouldBeNil)

		_, err = NewFiles(outDir, 1)
		So(err, ShouldNotBeNil)
	})

	Convey("With absolute paths longer than 4096", t, func() {
		tdir := t.TempDir()
		fname := strings.Repeat("a", 255)

		So(os.Chdir(tdir), ShouldBeNil)

		for range 18 {
			So(os.Mkdir(fname, 0700), ShouldBeNil)
			So(os.Chdir(fname), ShouldBeNil)
		}

		Convey("the walk throws an error, but does not crash", func() {
			pwd, err := os.Getwd()
			So(err, ShouldBeNil)
			So(len(pwd), ShouldBeGreaterThan, maxPathLength)

			outDir := t.TempDir()

			files, err := NewFiles(outDir, 1)
			So(err, ShouldBeNil)

			w := New(files.WritePaths(), true, false)

			var errors []error

			err = w.Walk(tdir, func(_ string, err error) {
				errors = append(errors, err)
			})
			So(err, ShouldBeNil)
			So(len(errors), ShouldEqual, 1)
			So(errors[0].Error(), ShouldEqual, "file name too long")

			So(files.Close(), ShouldBeNil)

			f, err := os.Open(filepath.Join(outDir, "walk.1"))
			So(err, ShouldBeNil)

			var buf strings.Builder

			_, err = io.Copy(&buf, f)
			So(err, ShouldBeNil)

			base := tdir + "/"

			var expectation string

			for i := 0; i < 17; i++ {
				expectation += strconv.Quote(base) + "\n"
				base = filepath.Join(base, fname) + "/"
			}

			So(buf.String(), ShouldEqual, expectation)
		})

		Reset(func() {
			So(os.Chdir(".."), ShouldBeNil)
			So(os.Remove(fname), ShouldBeNil)
			So(os.Chdir(".."), ShouldBeNil)
			So(os.Remove(fname), ShouldBeNil)
		})
	})

	Convey("The race checker doesn't throw an allocation stradling error when "+
		"the Dirent could theoretically cross an allocation boundary", t, func() {
		tdir := t.TempDir()
		fname := strings.Repeat("a", 200)

		expectation := make([]string, 1, 19)

		expectation[0] = strconv.Quote(tdir + "/")

		for i := 0; i < 18; i++ {
			p := filepath.Join(tdir, fname+strconv.Itoa(i))
			expectation = append(expectation, strconv.Quote(p+"/"))

			So(os.Mkdir(p, 0700), ShouldBeNil)
		}

		sort.Strings(expectation)

		outDir := t.TempDir()

		files, err := NewFiles(outDir, 1)
		So(err, ShouldBeNil)

		w := New(files.WritePaths(), true, false)

		err = w.Walk(tdir, func(_ string, err error) {})
		So(err, ShouldBeNil)

		So(files.Close(), ShouldBeNil)

		f, err := os.Open(filepath.Join(outDir, "walk.1"))
		So(err, ShouldBeNil)

		var buf strings.Builder

		_, err = io.Copy(&buf, f)
		So(err, ShouldBeNil)

		expectedOutput := strings.Join(expectation, "\n") + "\n"

		So(buf.String(), ShouldEqual, expectedOutput)
	})
}

// prepareTestDirs creates a temporary directory filled with files to walk, and
// an empty directory you can output to. Also returns all the paths created in a
// map. One of the files will be a symlink to another of the files.
func prepareTestDirs(t *testing.T) (string, string, []string) {
	t.Helper()
	tmpDir := t.TempDir()

	walkDir := filepath.Join(tmpDir, "walk")
	outDir := filepath.Join(tmpDir, "out")

	if err := os.Mkdir(walkDir, os.ModePerm); err != nil {
		t.Fatalf("mkdir failed: %s", err)
	}

	if err := os.Mkdir(outDir, os.ModePerm); err != nil {
		t.Fatalf("mkdir failed: %s", err)
	}

	paths := []string{walkDir + "/"}
	paths = helpers.FillDirWithFiles(t, walkDir, 4, paths)

	replaceFileWithSymlink(t, paths)

	pathsEncoded := make([]string, len(paths))
	for i, v := range paths {
		pathsEncoded[i] = strconv.Quote(v)
	}

	return walkDir, outDir, pathsEncoded
}

func replaceFileWithSymlink(t *testing.T, paths []string) {
	t.Helper()

	dest := ""

	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat failed: %s", err)
		}

		if info.Mode().IsRegular() {
			if dest == "" {
				dest = path

				continue
			}

			removeAndSymlink(t, path, dest)

			break
		}
	}
}

func removeAndSymlink(t *testing.T, path, dest string) {
	t.Helper()

	err := os.Remove(path)
	if err != nil {
		t.Fatalf("remove failed: %s", err)
	}

	err = os.Symlink(dest, path)
	if err != nil {
		t.Fatalf("symlink failed: %s", err)
	}
}

func testOutputToFiles(includDirs, ignoreSymlinks bool, walkDir, outDir string, cb ErrorCallback,
	expectedPaths []string, expectedStats *StatData) bool {
	files, err := NewFiles(outDir, 1)
	So(err, ShouldBeNil)

	w := New(files.WritePaths(), includDirs, ignoreSymlinks)

	var s StatData

	w.EnableStats(time.Second, func(t time.Time, sd StatData) {
		s.Open += sd.Open
		s.Close += sd.Close
	})

	err = w.Walk(walkDir, cb)
	So(err, ShouldBeNil)

	err = files.Close()
	So(err, ShouldBeNil)

	outPath := filepath.Join(outDir, "walk.1")
	So(files.Paths[0], ShouldEqual, outPath)
	content, err := os.ReadFile(outPath)
	So(err, ShouldBeNil)

	if expectedStats != nil {
		So(s, ShouldResemble, *expectedStats)
	}

	return checkPaths(string(content), expectedPaths)
}

// checkPaths parses the string content of a Walk() output file and returns true
// only if the content and order is the same.
func checkPaths(content string, paths []string) bool {
	scanner := bufio.NewScanner(strings.NewReader(content))

	i := 0

	for scanner.Scan() {
		path := scanner.Text()

		if paths[i] != path {
			return false
		}

		i++
	}

	return i == len(paths)
}
