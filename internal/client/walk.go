package client

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"syscall"
	"unsafe"
)

const (
	LenStart   = 0
	InodeStart = 2
	TypeStart  = 10
)

type walker struct {
	*bufio.Reader
	cmd *exec.Cmd
}

func (w *walker) Close() error {
	return w.cmd.Process.Kill()
}

// CreateWalker starts a file walk for the given path, using the given statter
// executable.
func CreateWalker(exe, path string) (io.ReadCloser, error) {
	cmd := exec.Command(exe, path) //nolint:noctx

	cmd.Stderr = os.Stderr

	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return &walker{bufio.NewReader(out), cmd}, nil
}

// Dirent contains information for a single path entry discovered during the
// walk.
type Dirent struct {
	Path  string
	Mode  fs.FileMode
	Inode uint64
}

// PathCallback is a function that can receive DirEnts.
type PathCallback func(entry *Dirent) error

// PathCallback is a function that can receive errors encountered for a path
// during the walk.
type ErrCallback func(string, error) error

// ReaddirEnt will read a single directory entry from the given Reader and pass
// either a DirEnt to the PathCallback or the path and an error to the
// ErrCallback.
func ReadDirEnt(r io.ReadCloser, cb PathCallback, errCB ErrCallback) error {
	var buf [14]byte

	_, err := io.ReadFull(r, buf[:])
	if err != nil {
		return err
	}

	pathLen := binary.LittleEndian.Uint16(buf[:InodeStart])
	if pathLen == 0 {
		return readError(r, binary.LittleEndian.Uint16(buf[2:]))
	}

	return readDirEnt(r, pathLen, &buf, cb, errCB)
}

func readDirEnt(r io.Reader, pl uint16, buf *[14]byte, cb PathCallback, errCB ErrCallback) error {
	pathBuf := make([]byte, pl)

	_, err := io.ReadFull(r, pathBuf)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return io.ErrUnexpectedEOF
		}

		return err
	}

	path := unsafe.String(unsafe.SliceData(pathBuf), pl)
	inode := binary.LittleEndian.Uint64(buf[InodeStart:TypeStart])
	other := binary.LittleEndian.Uint32(buf[TypeStart:])

	if inode == 0 {
		return errCB(path, syscall.Errno(other))
	}

	return cb(&Dirent{
		Path:  path,
		Mode:  fs.FileMode(other),
		Inode: inode,
	})
}

func readError(r io.Reader, errLen uint16) error {
	errBuf := make([]byte, errLen)

	if _, err := io.ReadFull(r, errBuf); err != nil {
		return err
	}

	return errors.New(unsafe.String(unsafe.SliceData(errBuf), errLen)) //nolint:err113
}
