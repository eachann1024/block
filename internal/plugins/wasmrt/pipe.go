package wasmrt

import (
	"errors"
	"io"
	"os"
	"sync"
	"time"
)

const (
	initialPipeCapacity = 4 * 1024
	defaultPipeCapacity = 256 * 1024
)

type memPipe struct {
	mu            sync.Mutex
	cond          *sync.Cond
	buf           []byte
	head          int
	size          int
	maxCap        int
	readClosed    bool
	readErr       error
	writeClosed   bool
	writeErr      error
	writeDeadline time.Time
}

// memPipeReader is the read end of an in-memory bounded pipe.
type memPipeReader struct {
	pipe *memPipe
}

// memPipeWriter is the write end of an in-memory bounded pipe.
type memPipeWriter struct {
	pipe *memPipe
}

func newMemPipe(capacity int) (*memPipeReader, *memPipeWriter) {
	if capacity <= 0 {
		capacity = defaultPipeCapacity
	}
	p := &memPipe{
		maxCap: capacity,
	}
	p.cond = sync.NewCond(&p.mu)
	return &memPipeReader{pipe: p}, &memPipeWriter{pipe: p}
}

func (p *memPipe) growLocked(need int) {
	curCap := len(p.buf)
	if curCap >= p.maxCap {
		return
	}
	nextCap := max(curCap, min(p.maxCap, initialPipeCapacity))
	for nextCap < need && nextCap < p.maxCap {
		if nextCap > p.maxCap/2 {
			nextCap = p.maxCap
			break
		}
		nextCap *= 2
	}
	if nextCap > p.maxCap {
		nextCap = p.maxCap
	}
	if nextCap <= curCap {
		return
	}
	nextBuf := make([]byte, nextCap)
	if p.size > 0 {
		first := min(p.size, curCap-p.head)
		copy(nextBuf, p.buf[p.head:p.head+first])
		if first < p.size {
			copy(nextBuf[first:p.size], p.buf[:p.size-first])
		}
	}
	p.buf = nextBuf
	p.head = 0
}

func (r *memPipeReader) Read(dst []byte) (int, error) {
	if len(dst) == 0 {
		return 0, nil
	}
	p := r.pipe
	p.mu.Lock()
	defer p.mu.Unlock()

	for p.size == 0 {
		if p.readClosed {
			if p.readErr != nil {
				return 0, p.readErr
			}
			return 0, io.ErrClosedPipe
		}
		if p.writeClosed {
			if p.writeErr != nil {
				return 0, p.writeErr
			}
			return 0, io.EOF
		}
		p.cond.Wait()
	}

	curCap := len(p.buf)
	n := min(len(dst), p.size)
	first := min(n, curCap-p.head)
	copy(dst[:first], p.buf[p.head:p.head+first])
	if first < n {
		copy(dst[first:n], p.buf[:n-first])
	}
	p.head = (p.head + n) % curCap
	p.size -= n
	if p.size == 0 && (p.writeClosed || p.readClosed) {
		p.buf = nil
		p.head = 0
	}
	p.cond.Broadcast()
	return n, nil
}

func (r *memPipeReader) Close() error {
	return r.CloseWithError(io.ErrClosedPipe)
}

func (r *memPipeReader) CloseWithError(err error) error {
	if err == nil {
		err = io.ErrClosedPipe
	}
	p := r.pipe
	p.mu.Lock()
	if !p.readClosed {
		p.readClosed = true
		p.readErr = err
		p.buf = nil
		p.size = 0
		p.head = 0
		p.cond.Broadcast()
	}
	p.mu.Unlock()
	return nil
}

func (w *memPipeWriter) SetWriteDeadline(t time.Time) error {
	p := w.pipe
	p.mu.Lock()
	p.writeDeadline = t
	p.cond.Broadcast()
	p.mu.Unlock()
	return nil
}

func (w *memPipeWriter) Write(src []byte) (int, error) {
	// musl/wasi-libc flush() can issue 0-byte writev calls; return immediately without blocking.
	if len(src) == 0 {
		return 0, nil
	}
	p := w.pipe
	p.mu.Lock()
	defer p.mu.Unlock()

	written := 0
	for written < len(src) {
		if p.readClosed {
			if p.readErr != nil {
				return written, p.readErr
			}
			return written, io.ErrClosedPipe
		}
		if p.writeClosed {
			return written, io.ErrClosedPipe
		}
		if !p.writeDeadline.IsZero() && !time.Now().Before(p.writeDeadline) {
			return written, os.ErrDeadlineExceeded
		}

		remaining := len(src) - written
		if p.size+remaining > len(p.buf) && len(p.buf) < p.maxCap {
			p.growLocked(p.size + remaining)
		}
		curCap := len(p.buf)
		avail := curCap - p.size
		if avail > 0 {
			n := min(avail, remaining)
			tail := (p.head + p.size) % curCap
			first := min(n, curCap-tail)
			copy(p.buf[tail:tail+first], src[written:written+first])
			if first < n {
				copy(p.buf[:n-first], src[written+first:written+n])
			}
			p.size += n
			written += n
			p.cond.Broadcast()
			continue
		}

		var timer *time.Timer
		if !p.writeDeadline.IsZero() {
			waitFor := time.Until(p.writeDeadline)
			if waitFor <= 0 {
				return written, os.ErrDeadlineExceeded
			}
			timer = time.AfterFunc(waitFor, func() {
				p.mu.Lock()
				p.cond.Broadcast()
				p.mu.Unlock()
			})
		}
		p.cond.Wait()
		if timer != nil {
			timer.Stop()
		}
	}
	return written, nil
}

func (w *memPipeWriter) Close() error {
	return w.CloseWithError(io.EOF)
}

func (w *memPipeWriter) CloseWithError(err error) error {
	if err == nil {
		err = io.EOF
	}
	p := w.pipe
	p.mu.Lock()
	if !p.writeClosed {
		p.writeClosed = true
		p.writeErr = err
		if p.size == 0 || p.readClosed {
			p.buf = nil
			p.head = 0
		}
		p.cond.Broadcast()
	}
	p.mu.Unlock()
	return nil
}

func closePipePair(r *memPipeReader, w *memPipeWriter, err error) {
	if err == nil {
		err = errors.New("pipe closed")
	}
	_ = r.CloseWithError(err)
	_ = w.CloseWithError(err)
}
