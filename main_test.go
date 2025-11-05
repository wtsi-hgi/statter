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
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/wtsi-hgi/statter/internal"
)

func TestRun(t *testing.T) {
	a, b, err := os.Pipe()
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	c, d, err := os.Pipe()
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	errCh := make(chan error)

	go startRun(internal.ReadWriter{Reader: a, WriteCloser: d}, errCh)

	local := internal.ReadWriter{Reader: c, WriteCloser: b}

	tmp := t.TempDir()

	testPathA := filepath.Join(tmp, "aFile")
	testPathB := filepath.Join(tmp, "bFile")

	if err := os.WriteFile(testPathA, []byte("some data"), 0600); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if err := os.WriteFile(testPathB, []byte("some data"), 0600); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	fiA, err := os.Lstat(testPathA)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	fiB, err := os.Lstat(testPathB)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	inode, err := internal.Stat(local, testPathA)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if i := fiA.Sys().(*syscall.Stat_t).Ino; inode != i {
		t.Errorf("incorrect inodeA, expected %d, got %d", i, inode)
	}

	inode, err = internal.Stat(local, testPathB)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if i := fiB.Sys().(*syscall.Stat_t).Ino; inode != i {
		t.Errorf("incorrect inodeB, expected %d, got %d", i, inode)
	}

	inode, err = internal.Stat(local, "/not/a/path")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	} else if inode != 0 {
		t.Fatalf("expecting inode 0, got %d", inode)
	}

	stat = func(string) (os.FileInfo, error) {
		time.Sleep(time.Second * 5)

		return nil, ErrTimeout
	}

	if _, err = internal.Stat(local, testPathA); !errors.Is(err, io.EOF) {
		t.Fatalf("expecting io.EOF, got %v", err)
	}
}

func startRun(remote io.ReadWriteCloser, errCh chan error) {
	conn = remote

	err := run()

	remote.Close()

	errCh <- err
}

func TestMain(t *testing.T) {
	tmp := t.TempDir()
	statter := filepath.Join(tmp, "statter")

	if err := exec.Command("go", "build", "-o", statter).Run(); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	conn, pid, err := internal.CreateStatter(statter)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	_, err = internal.Stat(conn, statter)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	p, err := os.FindProcess(pid)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if err = p.Kill(); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if _, err = internal.Stat(conn, statter); !errors.Is(err, io.EOF) {
		t.Fatalf("expecting EOF, got %s", err)
	}
}
