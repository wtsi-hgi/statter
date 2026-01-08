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
	"errors"
	"io"

	"github.com/wtsi-hgi/statter/internal/client"
)

type Statter func(string) (uint64, error)

// CreateStatter runs the statter at the given path and returns the a function
// which can be given paths to get an inode.
func CreateStatter(path string) (Statter, error) {
	local, _, err := client.CreateStatter(path)
	if err != nil {
		return nil, err
	}

	return func(path string) (uint64, error) {
		return client.Stat(local, path)
	}, nil
}

type Dirent = client.Dirent
type PathCallback = client.PathCallback
type ErrCallback = client.ErrCallback

// WalkPath runs the statter at the given exe path and performs a walk for the
// given path.
//
// For each path entry, the PathCallback will be called with the directory entry
// details.
//
// For each non-fatal error, such as permission issues, the ErrCallback will be
// called with the failing path and the error.
func WalkPath(exe, path string, cb PathCallback, errCB ErrCallback) error {
	r, err := client.CreateWalker(exe, path)
	if err != nil {
		return err
	}

	for {
		err := client.ReadDirEnt(r, cb, errCB)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}

			r.Close()

			return err
		}
	}
}
