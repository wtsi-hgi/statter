package client

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/wtsi-hgi/statter/internal/testhelper"
)

func TestWalk(t *testing.T) {
	Convey("You can read and write walk data", t, func() {
		pr, pw := io.Pipe()
		conn = &readWriter{WriteCloser: pw}

		var (
			dirents []*Dirent
			errs    []string
		)

		tmp := t.TempDir()
		paths := append([]string{tmp + "/"}, testhelper.FillDirWithFiles(t, tmp, 2, nil)...)

		So(os.Chmod(filepath.Join(tmp, "2"), 0), ShouldBeNil)
		Reset(func() { os.Chmod(filepath.Join(tmp, "2"), 0700) }) //nolint:errcheck

		go func() {
			Walk(tmp)
			pw.Close()
		}()

		cb := func(de *Dirent) error {
			dirents = append(dirents, de)

			return nil
		}

		errCB := func(path string, err error) error {
			errs = append(errs, fmt.Sprintf("%s: %s", path, err))

			return nil
		}

		for {
			err := ReadDirEnt(pr, cb, errCB)
			if errors.Is(err, io.EOF) {
				break
			}

			So(err, ShouldBeNil)
		}

		So(dirents, ShouldResemble, []*Dirent{
			makeDirEnt(t, paths[0]),
			makeDirEnt(t, paths[1]),
			makeDirEnt(t, paths[2]),
			makeDirEnt(t, paths[3]),
			makeDirEnt(t, paths[4]),
			makeDirEnt(t, paths[5]),
			makeDirEnt(t, paths[6]),
		})
		So(errs, ShouldEqual, []string{tmp + "/2/: permission denied"})
	})
}

func makeDirEnt(t *testing.T, path string) *Dirent {
	t.Helper()

	fi, err := os.Stat(path)
	So(err, ShouldBeNil)

	return &Dirent{
		Path:  path,
		Mode:  fi.Mode() &^ fs.ModePerm,
		Inode: fi.Sys().(*syscall.Stat_t).Ino, //nolint:errcheck,forcetypeassert
	}
}
