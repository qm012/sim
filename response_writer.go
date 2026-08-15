// Copyright (c) 2026 The Sim Authors
// Use of this source code is governed by a MIT license
// that can be found in the LICENSE file.

package sim

import (
	"bufio"
	"io"
	"net"
	"net/http"
)

type responseWriter struct {
	w http.ResponseWriter

	wroteHeader  bool
	statusCode   int
	bytesWritten int
}

var (
	_ http.Pusher         = (*responseWriter)(nil)
	_ http.Flusher        = (*responseWriter)(nil)
	_ http.Hijacker       = (*responseWriter)(nil)
	_ io.ReaderFrom       = (*responseWriter)(nil)
	_ http.ResponseWriter = (*responseWriter)(nil)
)

// Push implements the [http.Pusher] interface.
func (rw *responseWriter) Push(target string, opts *http.PushOptions) error {
	if pusher, ok := rw.w.(http.Pusher); ok {
		return pusher.Push(target, opts)
	}
	return http.ErrNotSupported
}

func (rw *responseWriter) Header() http.Header {
	return rw.w.Header()
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	rw.writeHeader(http.StatusOK)

	n, err := rw.w.Write(b)
	rw.bytesWritten += n
	return n, err
}

func (rw *responseWriter) writeHeader(code int) {
	if rw.wroteHeader {
		return
	}

	// Informational 1xx responses (except 101) are sent immediately
	// without finalizing the header, mirroring net/http.
	if code >= 100 && code <= 199 && code != http.StatusSwitchingProtocols {
		rw.w.WriteHeader(code)
		return
	}

	// Codes outside 100-999 are invalid; ignore them (net/http panics).
	if code < 100 || code > 999 {
		return
	}
	rw.wroteHeader = true
	rw.statusCode = code
	rw.w.WriteHeader(code)
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.writeHeader(code)
}

func (rw *responseWriter) Unwrap() http.ResponseWriter { return rw.w }

// ReadFrom implements the io.ReaderFrom interface
func (rw *responseWriter) ReadFrom(r io.Reader) (int64, error) {
	if rf, ok := rw.w.(io.ReaderFrom); ok {
		rw.writeHeader(http.StatusOK)
		n, err := rf.ReadFrom(r)
		rw.bytesWritten += int(n)
		return n, err
	}
	return io.Copy(writerOnly{rw}, r)
}

// Hijack implements the http.Hijacker interface.
func (rw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := rw.w.(http.Hijacker); ok {
		conn, buf, err := h.Hijack()
		// After a successful hijack no status line may be emitted.
		if err == nil {
			rw.wroteHeader = true
		}
		return conn, buf, err
	}
	return nil, nil, http.ErrNotSupported
}

// Flush implements the http.Flusher interface.
func (rw *responseWriter) Flush() {
	rw.writeHeader(http.StatusOK)
	if f, ok := rw.w.(http.Flusher); ok {
		f.Flush()
	}
}

// writerOnly exposes only Write so that io.Copy cannot route back into
// responseWriter.ReadFrom and recurse.
type writerOnly struct{ io.Writer }
