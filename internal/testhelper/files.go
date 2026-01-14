package testhelper

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

		if err := os.WriteFile(filePath, []byte(base), 0600); err != nil {
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
