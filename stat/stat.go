package stat

import (
	"encoding/binary"
	"errors"
	"flag"
	"io"
	"os"
	"syscall"
	"time"

	"github.com/wtsi-hgi/statter/internal/client"
)

const lengthSize = 2

var (
	conn io.ReadWriter = client.ReadWriter{Reader: os.Stdin, WriteCloser: os.Stdout} //nolint:gochecknoglobals

	ErrTimeout = errors.New("timeout")

	stat = os.Lstat //nolint:gochecknoglobals
)

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
	binary.LittleEndian.AppendUint64(s[:12], uint64(stat.Nlink)) //nolint:unconvert,nolintlint
	binary.LittleEndian.AppendUint32(s[:20], stat.Uid)
	binary.LittleEndian.AppendUint32(s[:24], stat.Gid)
	binary.LittleEndian.AppendUint64(s[:28], uint64(stat.Size))     //nolint:gosec
	binary.LittleEndian.AppendUint64(s[:36], uint64(stat.Mtim.Sec)) //nolint:gosec

	_, err := conn.Write(s[:44])

	return err
}

func Loop() error {
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

var statErr = new(syscall.Stat_t) //nolint:gochecknoglobals

func doStat(path string, ch chan *syscall.Stat_t) {
	fi, err := stat(path)
	if err != nil {
		var sysErr syscall.Errno

		errors.As(err, &sysErr)

		statErr.Mode = uint32(sysErr)

		ch <- statErr
	} else {
		ch <- fi.Sys().(*syscall.Stat_t) //nolint:errcheck,forcetypeassert
	}
}
