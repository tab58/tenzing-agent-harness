package nexus

import (
	"bytes"
	"context"
	"io"
	"os"
	"time"
)

// headFingerprintLen is how many leading bytes of the tailed file are
// remembered to detect in-place content replacement (logrotate
// copytruncate), which changes neither the inode nor — when the new
// content outgrows the old read offset between polls — the size ordering.
const headFingerprintLen = 256

// runFileTail tails path, emitting each complete line to ingest. It polls
// every pollInterval (stdlib only, no fsnotify). On first open of a
// pre-existing file it seeks to the end; a file that appears later is read
// from the start. Rotation (inode change), truncation (size shrink), or an
// in-place head rewrite (copytruncate) reopens from the start. Blocks
// until ctx is cancelled.
func runFileTail(ctx context.Context, path string, pollInterval time.Duration, ingest func(string), setStatus func(string)) {
	setStatus("running")
	defer setStatus("stopped")

	var (
		f       *os.File
		offset  int64
		pending []byte
		head    []byte
		// seekEnd is true only for the very first open, so a tail started
		// against an existing file skips its historical content.
		seekEnd = true
	)
	if _, err := os.Stat(path); err != nil {
		seekEnd = false // file doesn't exist yet; read new file from start
	}

	closeFile := func() {
		if f != nil {
			f.Close()
			f = nil
		}
		offset = 0
		pending = nil
		head = nil
	}
	defer closeFile()

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		if f == nil {
			if nf, err := os.Open(path); err == nil {
				f = nf
				if seekEnd {
					offset, _ = f.Seek(0, io.SeekEnd)
				}
				seekEnd = false
				head = readHead(f)
			}
		}

		if f != nil {
			// rotation / truncation / in-place rewrite check
			pathInfo, statErr := os.Stat(path)
			fileInfo, fstatErr := f.Stat()
			if statErr != nil || fstatErr != nil || !os.SameFile(pathInfo, fileInfo) || pathInfo.Size() < offset || !headMatches(f, &head) {
				closeFile()
				continue // reopen immediately from start
			}

			buf := make([]byte, 64*1024)
			for {
				n, err := f.Read(buf)
				if n > 0 {
					offset += int64(n)
					pending = append(pending, buf[:n]...)
					for {
						idx := bytes.IndexByte(pending, '\n')
						if idx < 0 {
							break
						}
						line := string(pending[:idx])
						pending = pending[idx+1:]
						if line != "" {
							ingest(line)
						}
					}
				}
				if err != nil {
					break // io.EOF or real error; either way wait for next poll
				}
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// readHead returns up to headFingerprintLen leading bytes of the file
// without disturbing its read position.
func readHead(f *os.File) []byte {
	buf := make([]byte, headFingerprintLen)
	n, _ := f.ReadAt(buf, 0)
	return buf[:n:n]
}

// headMatches reports whether the file still begins with the recorded
// fingerprint, extending the fingerprint toward the cap as the file grows.
// A mismatch or a shrunken head means the content was replaced in place
// even though the inode and size checks pass. ponytail: a rewrite whose
// first 256 bytes are byte-identical to the old file still slips through;
// timestamped log lines never are.
func headMatches(f *os.File, head *[]byte) bool {
	buf := make([]byte, headFingerprintLen)
	n, _ := f.ReadAt(buf, 0)
	cmp := min(n, len(*head))
	if n < len(*head) || !bytes.Equal(buf[:cmp], (*head)[:cmp]) {
		return false
	}
	if n > len(*head) {
		*head = buf[:n:n]
	}
	return true
}
