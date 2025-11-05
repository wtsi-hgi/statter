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
	"syscall"
	"time"

	"github.com/wtsi-hgi/statter/internal"
)

const lengthSize = 2

var conn io.ReadWriter = internal.ReadWriter{Reader: os.Stdin, WriteCloser: os.Stdout}

var ErrTimeout = errors.New("timeout")

var stat = os.Lstat

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)

		os.Exit(1)
	}
}

func run() error {
	timeout := time.Second

	flag.DurationVar(&timeout, "timeout", timeout, "timeout to wait for stat to finish")
	flag.Parse()

	var buf [4096]byte

	ch := make(chan uint64)

	for {
		n, err := conn.Read(buf[:lengthSize])
		if err != nil {
			return err
		} else if n != lengthSize {
			return io.ErrShortBuffer
		}

		pathLen := binary.LittleEndian.Uint16(buf[:lengthSize])

		n, err = conn.Read(buf[:pathLen])
		if err != nil {
			return err
		} else if n != int(pathLen) {
			return io.ErrShortBuffer
		}

		go doStat(string(buf[:pathLen]), ch)

		select {
		case <-time.After(timeout):
			return ErrTimeout
		case inode := <-ch:
			_, err := conn.Write(binary.LittleEndian.AppendUint64(buf[:0], inode))
			if err != nil {
				return err
			}
		}
	}
}

func doStat(path string, ch chan uint64) {
	fi, err := stat(path)
	if err != nil {
		ch <- 0
	} else {
		ch <- fi.Sys().(*syscall.Stat_t).Ino
	}
}
