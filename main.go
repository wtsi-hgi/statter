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
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/wtsi-hgi/statter/internal/client"
	"github.com/wtsi-hgi/statter/walk"
)

const lengthSize = 2

var conn io.ReadWriter = client.ReadWriter{Reader: os.Stdin, WriteCloser: os.Stdout}

var ErrTimeout = errors.New("timeout")

var stat = os.Lstat

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)

		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) == 2 {
		doWalk(os.Args[1])

		return nil
	}

	return statLoop()
}

type statter [4096]byte

func (s *statter) ReadPath() (string, error) {
	n, err := conn.Read(s[:lengthSize])
	if err != nil {
		return "", err
	} else if n != lengthSize {
		return "", io.ErrShortBuffer
	}

	pathLen := binary.LittleEndian.Uint16(s[:lengthSize])

	n, err = conn.Read(s[:pathLen])
	if err != nil {
		return "", err
	} else if n != int(pathLen) {
		return "", io.ErrShortBuffer
	}

	return string(s[:pathLen]), nil
}

func (s *statter) WriteStat(stat *syscall.Stat_t) error {
	binary.LittleEndian.AppendUint64(s[:0], stat.Ino)
	binary.LittleEndian.AppendUint32(s[:8], stat.Mode)
	binary.LittleEndian.AppendUint64(s[:12], uint64(stat.Nlink))
	binary.LittleEndian.AppendUint32(s[:20], stat.Uid)
	binary.LittleEndian.AppendUint32(s[:24], stat.Gid)
	binary.LittleEndian.AppendUint64(s[:28], uint64(stat.Size))
	binary.LittleEndian.AppendUint64(s[:36], uint64(stat.Mtim.Sec))

	_, err := conn.Write(s[:44])

	return err
}

func statLoop() error {
	timeout := time.Second

	flag.DurationVar(&timeout, "timeout", timeout, "timeout to wait for stat to finish")
	flag.Parse()

	var s statter

	ch := make(chan *syscall.Stat_t)

	for {
		path, err := s.ReadPath()
		if err != nil {
			return err
		}

		go doStat(path, ch)

		select {
		case <-time.After(timeout):
			return ErrTimeout
		case stat := <-ch:
			err := s.WriteStat(stat)
			if err != nil {
				return err
			}
		}
	}
}

var statErr = new(syscall.Stat_t)

func doStat(path string, ch chan *syscall.Stat_t) {
	fi, err := stat(path)
	if err != nil {
		var sysErr syscall.Errno

		errors.As(err, &sysErr)

		statErr.Mode = uint32(sysErr)

		ch <- statErr
	} else {
		ch <- fi.Sys().(*syscall.Stat_t)
	}
}

type walkWriter struct {
	mu  sync.Mutex
	buf [4096 + 8 + 4 + 2]byte
}

func doWalk(path string) {
	var w walkWriter

	if err := walk.New(w.pathCallback, true, false).Walk(path, w.errCallback); err != nil {
		w.writeError(err)
	}
}

func (w *walkWriter) pathCallback(entry *walk.Dirent) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	buf := entry.AppendTo(w.buf[:14])
	binary.LittleEndian.AppendUint16(w.buf[:0], uint16(len(buf))-14)
	binary.LittleEndian.AppendUint64(w.buf[:2], entry.Inode)
	binary.LittleEndian.AppendUint32(w.buf[:10], uint32(entry.Type()))

	_, err := conn.Write(buf)

	return err
}

func (w *walkWriter) errCallback(path string, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	binary.LittleEndian.AppendUint16(w.buf[:0], uint16(len(path)))
	binary.LittleEndian.AppendUint64(w.buf[:2], 0)
	binary.LittleEndian.AppendUint32(w.buf[:10], uint32(err.(syscall.Errno)))

	conn.Write(append(w.buf[:14], path...))
}

func (w *walkWriter) writeError(err error) {
	w.buf[0] = 0
	w.buf[1] = 0
	errMsg := err.Error()

	binary.LittleEndian.AppendUint16(w.buf[:2], uint16(len(errMsg)))
	conn.Write(append(w.buf[:14], errMsg...))
}
