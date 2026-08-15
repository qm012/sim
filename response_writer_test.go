// Copyright (c) 2026 The Sim Authors
// Use of this source code is governed by a MIT license
// that can be found in the LICENSE file.

package sim

import (
	"bufio"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// fakeWriter is an http.ResponseWriter that implements every optional
// interface and records what happened to it.
type fakeWriter struct {
	h        http.Header
	code     int
	body     []byte
	writeErr error

	flushed  bool
	hijacked bool
	pushed   bool
}

func newFakeWriter() *fakeWriter {
	return &fakeWriter{h: make(http.Header)}
}

func (f *fakeWriter) Header() http.Header { return f.h }

func (f *fakeWriter) Write(p []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	f.body = append(f.body, p...)
	return len(p), nil
}

func (f *fakeWriter) WriteHeader(code int) { f.code = code }

func (f *fakeWriter) Flush() { f.flushed = true }

func (f *fakeWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	f.hijacked = true
	return nil, nil, nil
}

func (f *fakeWriter) Push(_ string, _ *http.PushOptions) error {
	f.pushed = true
	return nil
}

func (f *fakeWriter) ReadFrom(r io.Reader) (int64, error) {
	if f.code == 0 {
		f.WriteHeader(http.StatusOK)
	}
	return io.Copy(struct{ io.Writer }{f}, r)
}

// bareWriter implements only http.ResponseWriter, no optional interfaces.
type bareWriter struct {
	h    http.Header
	code int
	body []byte
}

func (b *bareWriter) Header() http.Header {
	if b.h == nil {
		b.h = make(http.Header)
	}
	return b.h
}

func (b *bareWriter) Write(p []byte) (int, error) {
	b.body = append(b.body, p...)
	return len(p), nil
}

func (b *bareWriter) WriteHeader(code int) { b.code = code }

type statusTest struct {
	name       string
	write      func(rw *responseWriter, f *fakeWriter)
	wantStatus int
}

func statusTests() []statusTest {
	return []statusTest{
		{
			name: "explicit status code",
			write: func(rw *responseWriter, _ *fakeWriter) {
				rw.WriteHeader(http.StatusNotFound)
				_, _ = rw.Write([]byte("nf"))
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "implicit 200 on body write",
			write:      func(rw *responseWriter, _ *fakeWriter) { _, _ = rw.Write([]byte("hello")) },
			wantStatus: http.StatusOK,
		},
		{
			name: "first WriteHeader wins",
			write: func(rw *responseWriter, _ *fakeWriter) {
				rw.WriteHeader(http.StatusInternalServerError)
				rw.WriteHeader(http.StatusOK)
			},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:       "no response written",
			write:      func(_ *responseWriter, _ *fakeWriter) {},
			wantStatus: 0,
		},
		{
			name: "WriteHeader after Write is ignored",
			write: func(rw *responseWriter, _ *fakeWriter) {
				_, _ = rw.Write([]byte("body"))
				rw.WriteHeader(http.StatusInternalServerError)
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "1xx then explicit final status",
			write: func(rw *responseWriter, _ *fakeWriter) {
				rw.WriteHeader(http.StatusEarlyHints)
				rw.WriteHeader(http.StatusInternalServerError)
			},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:       "101 switching protocols is final",
			write:      func(rw *responseWriter, _ *fakeWriter) { rw.WriteHeader(http.StatusSwitchingProtocols) },
			wantStatus: http.StatusSwitchingProtocols,
		},
	}
}

func TestResponseWriterStatus(t *testing.T) {
	for _, tt := range statusTests() {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeWriter()
			rw := &responseWriter{w: f}
			tt.write(rw, f)
			if rw.statusCode != tt.wantStatus {
				t.Errorf("statusCode = %d, want %d", rw.statusCode, tt.wantStatus)
			}
			if f.code != tt.wantStatus {
				t.Errorf("underlying status = %d, want %d", f.code, tt.wantStatus)
			}
		})
	}
}

func TestResponseWriterBytesWritten(t *testing.T) {
	f := newFakeWriter()
	rw := &responseWriter{w: f}

	n1, err1 := rw.Write([]byte("hello"))
	n2, err2 := rw.Write([]byte(" world"))
	if n1 != len("hello") || err1 != nil || n2 != len(" world") || err2 != nil {
		t.Errorf("Write = (%d,%v),(%d,%v)", n1, err1, n2, err2)
	}
	if rw.bytesWritten != len("hello world") {
		t.Errorf("bytesWritten = %d, want %d", rw.bytesWritten, len("hello world"))
	}
	if got := string(f.body); got != "hello world" {
		t.Errorf("body = %q, want %q", got, "hello world")
	}

	f.writeErr = io.ErrUnexpectedEOF
	n3, err3 := rw.Write([]byte("!"))
	if n3 != 0 || !errors.Is(err3, io.ErrUnexpectedEOF) {
		t.Errorf("Write with underlying error = (%d,%v), want (0, boom)", n3, err3)
	}
	if rw.bytesWritten != len("hello world") {
		t.Errorf("bytesWritten after error = %d, want %d", rw.bytesWritten, len("hello world"))
	}
}

type readFromTest struct {
	name       string
	setup      func() *responseWriter
	src        io.Reader
	payload    string
	wantStatus int
	readBody   func(rw *responseWriter) string
}

func readFromTests() []readFromTest {
	return []readFromTest{
		{
			name:       "fast path",
			setup:      func() *responseWriter { return &responseWriter{w: newFakeWriter()} },
			src:        io.LimitReader(strings.NewReader("hello world"), int64(len("hello world"))),
			payload:    "hello world",
			wantStatus: http.StatusOK,
			readBody: func(rw *responseWriter) string {
				fw, _ := rw.w.(*fakeWriter)
				return string(fw.body)
			},
		},
		{
			name:       "fallback path",
			setup:      func() *responseWriter { return &responseWriter{w: httptest.NewRecorder()} },
			src:        io.LimitReader(strings.NewReader("hello world"), int64(len("hello world"))),
			payload:    "hello world",
			wantStatus: http.StatusOK,
			readBody: func(rw *responseWriter) string {
				rec, _ := rw.w.(*httptest.ResponseRecorder)
				return rec.Body.String()
			},
		},
		{
			name:       "WriterTo source",
			setup:      func() *responseWriter { return &responseWriter{w: newFakeWriter()} },
			src:        strings.NewReader("hello"),
			payload:    "hello",
			wantStatus: http.StatusOK,
			readBody: func(rw *responseWriter) string {
				fw, _ := rw.w.(*fakeWriter)
				return string(fw.body)
			},
		},
		{
			name: "after explicit status",
			setup: func() *responseWriter {
				rw := &responseWriter{w: newFakeWriter()}
				rw.WriteHeader(http.StatusNotFound)
				return rw
			},
			src:        io.LimitReader(strings.NewReader("hi"), int64(len("hi"))),
			payload:    "hi",
			wantStatus: http.StatusNotFound,
			readBody: func(rw *responseWriter) string {
				fw, _ := rw.w.(*fakeWriter)
				return string(fw.body)
			},
		},
	}
}

func TestResponseWriterReadFrom(t *testing.T) {
	for _, tt := range readFromTests() {
		t.Run(tt.name, func(t *testing.T) {
			rw := tt.setup()
			n, err := io.Copy(rw, tt.src)
			want := int64(len(tt.payload))
			if n != want || err != nil {
				t.Errorf("io.Copy = (%d,%v), want (%d,nil)", n, err, want)
			}
			if rw.statusCode != tt.wantStatus {
				t.Errorf("statusCode = %d, want %d", rw.statusCode, tt.wantStatus)
			}
			if rw.bytesWritten != len(tt.payload) {
				t.Errorf("bytesWritten = %d, want %d", rw.bytesWritten, len(tt.payload))
			}
			if got := tt.readBody(rw); got != tt.payload {
				t.Errorf("body = %q, want %q", got, tt.payload)
			}
		})
	}
}

func TestResponseWriterInvalidStatusCode(t *testing.T) {
	for _, code := range []int{0, -1, 99, 1000} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			f := newFakeWriter()
			rw := &responseWriter{w: f}
			rw.WriteHeader(code)
			if rw.wroteHeader || rw.statusCode != 0 || f.code != 0 {
				t.Errorf("WriteHeader(%d): wroteHeader=%v statusCode=%d underlying=%d, want ignored",
					code, rw.wroteHeader, rw.statusCode, f.code)
			}
		})
	}
}

func TestResponseWriterInformationalStatus(t *testing.T) {
	f := newFakeWriter()
	rw := &responseWriter{w: f}
	rw.WriteHeader(http.StatusEarlyHints)
	if f.code != http.StatusEarlyHints {
		t.Errorf("underlying status = %d, want %d", f.code, http.StatusEarlyHints)
	}
	if rw.wroteHeader {
		t.Error("1xx should not finalize the header")
	}
	if rw.statusCode != 0 {
		t.Errorf("statusCode = %d, want 0 before the final status", rw.statusCode)
	}
	_, _ = rw.Write([]byte("body"))
	if rw.statusCode != http.StatusOK {
		t.Errorf("statusCode = %d, want 200 after 1xx", rw.statusCode)
	}
}

func TestResponseWriterHeader(t *testing.T) {
	f := newFakeWriter()
	rw := &responseWriter{w: f}
	rw.Header().Set("X-Test", "1")
	if got := f.h.Get("X-Test"); got != "1" {
		t.Errorf("header = %q, want %q", got, "1")
	}
}

func TestResponseWriterFlush(t *testing.T) {
	tests := []struct {
		name       string
		preWrite   func(rw *responseWriter)
		wantStatus int
	}{
		{
			name:       "implicit 200",
			preWrite:   func(_ *responseWriter) {},
			wantStatus: http.StatusOK,
		},
		{
			name: "after explicit status",
			preWrite: func(rw *responseWriter) {
				rw.WriteHeader(http.StatusNotFound)
			},
			wantStatus: http.StatusNotFound,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeWriter()
			rw := &responseWriter{w: f}
			tt.preWrite(rw)
			http.Flusher(rw).Flush()
			if !f.flushed {
				t.Error("Flush was not forwarded to the underlying writer")
			}
			if !rw.wroteHeader {
				t.Error("wroteHeader not set after Flush")
			}
			if rw.statusCode != tt.wantStatus {
				t.Errorf("statusCode = %d, want %d", rw.statusCode, tt.wantStatus)
			}
			if f.code != tt.wantStatus {
				t.Errorf("underlying status = %d, want %d", f.code, tt.wantStatus)
			}
		})
	}
}

func TestResponseWriterFlushUnsupported(t *testing.T) {
	b := &bareWriter{}
	rw := &responseWriter{w: b}
	rw.Flush()
	if b.code != http.StatusOK {
		t.Errorf("underlying status = %d, want 200 even without Flusher support", b.code)
	}
	if !rw.wroteHeader {
		t.Error("wroteHeader not set after Flush")
	}
}

func TestResponseWriterHijack(t *testing.T) {
	f := newFakeWriter()
	rw := &responseWriter{w: f}
	if _, _, err := rw.Hijack(); err != nil {
		t.Fatalf("Hijack: %v", err)
	}
	if !f.hijacked {
		t.Error("Hijack was not forwarded to the underlying writer")
	}
	if !rw.wroteHeader {
		t.Error("wroteHeader not set after Hijack")
	}
	_, _ = rw.Write([]byte("x"))
	if f.code != 0 {
		t.Errorf("underlying status = %d, want no status line after hijack", f.code)
	}
	http.Flusher(rw).Flush()
	if f.code != 0 {
		t.Errorf("underlying status = %d, want Flush to not write a header after hijack", f.code)
	}
}

func TestResponseWriterHijackUnsupported(t *testing.T) {
	rw := &responseWriter{w: &bareWriter{}}
	if _, _, err := rw.Hijack(); !errors.Is(err, http.ErrNotSupported) {
		t.Errorf("Hijack err = %v, want http.ErrNotSupported", err)
	}
	if rw.wroteHeader {
		t.Error("failed Hijack must not mark the header as written")
	}
}

func TestResponseWriterPush(t *testing.T) {
	f := newFakeWriter()
	rw := &responseWriter{w: f}
	ps := http.Pusher(rw)
	if err := ps.Push("/asset", nil); err != nil {
		t.Errorf("Push: %v", err)
	}
	if !f.pushed {
		t.Error("Push was not forwarded to the underlying writer")
	}
}

func TestResponseWriterPushUnsupported(t *testing.T) {
	rw := &responseWriter{w: &bareWriter{}}
	if err := rw.Push("/asset", nil); !errors.Is(err, http.ErrNotSupported) {
		t.Errorf("Push err = %v, want http.ErrNotSupported", err)
	}
}

func TestResponseWriterUnwrap(t *testing.T) {
	t.Run("exposes underlying writer", func(t *testing.T) {
		f := newFakeWriter()
		rw := &responseWriter{w: f}
		if rw.Unwrap() != f {
			t.Error("Unwrap does not return the underlying writer")
		}
	})
	t.Run("controller reaches underlying writer", func(t *testing.T) {
		f := newFakeWriter()
		rw := &responseWriter{w: f}
		if err := http.NewResponseController(rw).Flush(); err != nil {
			t.Errorf("ResponseController.Flush: %v", err)
		}
		if !f.flushed {
			t.Error("ResponseController flush did not reach the underlying writer")
		}
	})
	t.Run("flush succeeds when underlying lacks Flusher", func(t *testing.T) {
		rw := &responseWriter{w: &bareWriter{}}
		if err := http.NewResponseController(rw).Flush(); err != nil {
			t.Errorf("Flush err = %v, want nil (silent no-op)", err)
		}
	})
}
