/*******************************************************************************
 * Copyright (c) 2022, 2023, 2024 Genome Research Ltd.
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
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"unsafe"
)

const userOnlyPerm = 0700

// non-ascii bytes could become \xXX (4x the length at worst), the two
// speech-marks are +2 and a newline is +1.
const maxQuotedPathLength = (maxPathLength+maxFilenameLength)*4 + 2 + 1

const bufferSize = 1 << 20

// WriteError is an error received when trying to write strings to disk.
type WriteError struct {
	Err error
}

func (e *WriteError) Error() string { return e.Err.Error() }

func (e *WriteError) Unwrap() error { return e.Err }

type bufferedFile struct {
	*bufio.Writer
	aw *asyncWriter
}

func (b *bufferedFile) Close() error {
	if err := b.Flush(); err != nil {
		return err
	}

	return b.aw.Close()
}

type asyncWriter struct {
	mu          sync.Mutex
	buffer      [bufferSize]byte
	len         int
	err         error
	writeLogger func(int)
	io.WriteCloser
}

func (a *asyncWriter) Write(p []byte) (int, error) {
	a.mu.Lock()

	if a.err != nil {
		defer a.mu.Unlock()

		return 0, a.err
	}

	a.len = copy(a.buffer[:], p)

	go func() {
		defer a.mu.Unlock()

		var b int

		b, a.err = a.WriteCloser.Write(a.buffer[:a.len])

		if a.writeLogger != nil {
			a.writeLogger(b)
		}
	}()

	return len(p), nil
}

func (a *asyncWriter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.err != nil {
		return a.err
	}

	return a.WriteCloser.Close()
}

// Files represents a collection of output files that can be written to in a
// round-robin.
type Files struct {
	files    []bufferedFile
	Paths    []string
	filesI   int
	filesMax int
	mus      []sync.Mutex
}

// NewFiles returns a Files that has a WritePaths method that will return a
// PathsCallback function suitable for passing to New().
//
// This creates n output files in outDir, and writes the walk paths to those
// files 1 per line in a round-robin.
//
// The output file paths can be found in the Paths property.
//
// Be sure to Close() after you've finished walking.
func NewFiles(outDir string, n int) (*Files, error) {
	if err := os.MkdirAll(outDir, userOnlyPerm); err != nil {
		return nil, err
	}

	files := make([]bufferedFile, n)
	outPaths := make([]string, n)

	for i := range files {
		path := filepath.Join(outDir, fmt.Sprintf("walk.%d", i+1))

		file, err := os.Create(path)
		if err != nil {
			return nil, err
		}

		f := &asyncWriter{WriteCloser: file}

		files[i].Writer = bufio.NewWriterSize(f, bufferSize)
		files[i].aw = f

		outPaths[i] = path
	}

	return &Files{
		files:    files,
		Paths:    outPaths,
		filesMax: len(files),
		mus:      make([]sync.Mutex, len(files)),
	}, nil
}

// Set logger takes a callback fn that will be given the number of bytes written
// after each write.
func (f *Files) SetLogger(fn func(int)) {
	for _, file := range f.files {
		file.aw.writeLogger = fn
	}
}

// WritePaths returns a PathCallback function suitable for passing to New().
//
// Paths are written quoted 1 per line to our output files in a round-robin.
//
// It will terminate the walk if writes to our output files fail.
func (f *Files) WritePaths() PathCallback {
	var (
		quoted  [maxQuotedPathLength]byte
		tmpPath [maxPathLength + maxFilenameLength]byte
	)

	return func(entry *Dirent) error {
		return f.writePath(append(
			strconv.AppendQuote(
				quoted[:0], unsafe.String(&tmpPath[0], len(entry.AppendTo(tmpPath[:0]))),
			), '\n'))
	}
}

// writePath is a thread-safe way of writing the given path to our next output
// file. Returns a WriteError on failure to write to an output file.
func (f *Files) writePath(path []byte) error {
	i := f.filesI
	f.filesI++

	if f.filesI == f.filesMax {
		f.filesI = 0
	}

	_, err := f.files[i].Write(path)
	if err != nil {
		err = &WriteError{Err: err}
	}

	return err
}

// Close should be called after Walk()ing to close all the output files.
func (f *Files) Close() error {
	for _, file := range f.files {
		if err := file.Close(); err != nil {
			return &WriteError{err}
		}
	}

	return nil
}
