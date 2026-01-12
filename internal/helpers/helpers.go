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

package helpers

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
)

// FillDirWithFiles fills the given directory with files, size dirs wide and
// deep.
func FillDirWithFiles(t *testing.T, dir string, size int, paths []string) []string {
	t.Helper()

	for i := range size {
		base := strconv.Itoa(i + 1)
		path := filepath.Join(dir, base)

		filePath := path + ".file"
		if len(paths) == 1 {
			filePath += "\ntest"
		}

		paths = append(paths, path+"/", filePath)

		if err := os.WriteFile(filePath, []byte(base), 0700); err != nil {
			t.Fatalf("file creation failed: %s", err)
		}

		if err := os.Mkdir(path, os.ModePerm); err != nil {
			t.Fatalf("mkdir failed: %s", err)
		}

		if size > 1 {
			paths = FillDirWithFiles(t, path, size-1, paths)
		}
	}

	sort.Strings(paths)

	return paths
}
