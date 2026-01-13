package walk

import (
	"encoding/binary"
	"io"
	"os"
	"sync"
	"syscall"

	"github.com/wtsi-hgi/statter/internal/client"
	"github.com/wtsi-hgi/walk"
)

var conn io.Writer = os.Stdout //nolint:gochecknoglobals

type walkWriter struct {
	mu  sync.Mutex
	buf [4096 + 8 + 4 + 2]byte
}

func Do(path string) {
	var w walkWriter

	if err := walk.New(w.pathCallback, true, false).Walk(path, w.errCallback); err != nil {
		w.writeError(err)
	}
}

func (w *walkWriter) pathCallback(entry *walk.Dirent) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	buf := entry.AppendTo(w.buf[:14])
	binary.LittleEndian.AppendUint16(w.buf[:client.LenStart], uint16(len(buf))-14) //nolint:gosec
	binary.LittleEndian.AppendUint64(w.buf[:client.InodeStart], entry.Inode)
	binary.LittleEndian.AppendUint32(w.buf[:client.TypeStart], uint32(entry.Type()))

	_, err := conn.Write(buf)

	return err
}

func (w *walkWriter) errCallback(path string, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	binary.LittleEndian.AppendUint16(w.buf[:client.LenStart], uint16(len(path))) //nolint:gosec
	binary.LittleEndian.AppendUint64(w.buf[:client.InodeStart], 0)
	binary.LittleEndian.AppendUint32(w.buf[:client.TypeStart], uint32(err.(syscall.Errno))) //nolint:errcheck,forcetypeassert,errorlint

	conn.Write(append(w.buf[:14], path...)) //nolint:errcheck
}

func (w *walkWriter) writeError(err error) {
	w.buf[0] = 0
	w.buf[1] = 0
	errMsg := err.Error()

	binary.LittleEndian.AppendUint16(w.buf[:client.InodeStart], uint16(len(errMsg))) //nolint:gosec
	conn.Write(append(w.buf[:14], errMsg...))                                        //nolint:errcheck
}
