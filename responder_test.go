// Copyright (c) 2026 The Sim Authors
// Use of this source code is governed by a MIT license
// that can be found in the LICENSE file.

package sim_test

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/qm012/sim"
)

func TestJSON(t *testing.T) {
	tests := []struct {
		name     string
		data     any
		opts     []sim.JSONEncoderOption
		wantBody string
		wantErr  bool
	}{
		{
			name:     "escapes HTML by default",
			data:     map[string]string{"html": "<b>"},
			wantBody: "{\"html\":\"\\u003cb\\u003e\"}\n",
		},
		{
			name:     "EscapeForHTML(false) disables escaping",
			data:     map[string]string{"html": "<b>"},
			opts:     []sim.JSONEncoderOption{sim.EscapeForHTML(false)},
			wantBody: "{\"html\":\"<b>\"}\n",
		},
		{
			name:     "Indented(true) indents output",
			data:     map[string]string{"name": "sim"},
			opts:     []sim.JSONEncoderOption{sim.Indented(true)},
			wantBody: "{\n    \"name\": \"sim\"\n}\n",
		},
		{
			name:    "channel fails to encode",
			data:    make(chan int),
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			err := sim.JSON(rec, http.StatusCreated, tt.data, tt.opts...)
			if tt.wantErr {
				if err == nil {
					t.Fatal("JSON() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("JSON() error = %v", err)
			}
			if got := rec.Code; got != http.StatusCreated {
				t.Errorf("status = %d, want %d", got, http.StatusCreated)
			}
			if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
				t.Errorf("Content-Type = %q", got)
			}
			if got := rec.Body.String(); got != tt.wantBody {
				t.Errorf("body = %q, want %q", got, tt.wantBody)
			}
		})
	}
}

type xmlUser struct {
	XMLName xml.Name `xml:"user"`
	Name    string   `xml:"name"`
}

type errWriter struct {
	header http.Header
}

var errWriteFailed = errors.New("write failed")

func (w *errWriter) Header() http.Header { return w.header }

func (w *errWriter) WriteHeader(int) {}

func (w *errWriter) Write([]byte) (int, error) { return 0, errWriteFailed }

func TestXML(t *testing.T) {
	tests := []struct {
		name      string
		data      any
		failWrite bool
		wantBody  string
		wantErr   bool
	}{
		{
			name:     "ok",
			data:     xmlUser{Name: "sim"},
			wantBody: xml.Header + "<user><name>sim</name></user>",
		},
		{
			name:    "channel fails to marshal",
			data:    make(chan int),
			wantErr: true,
		},
		{
			name:      "write error surfaces",
			data:      xmlUser{Name: "sim"},
			failWrite: true,
			wantErr:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			var w http.ResponseWriter = rec
			if tt.failWrite {
				w = &errWriter{header: make(http.Header)}
			}
			err := sim.XML(w, http.StatusOK, tt.data)
			if tt.wantErr {
				if err == nil {
					t.Fatal("XML() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("XML() error = %v", err)
			}
			if got := rec.Header().Get("Content-Type"); got != "application/xml; charset=utf-8" {
				t.Errorf("Content-Type = %q", got)
			}
			if got := rec.Body.String(); got != tt.wantBody {
				t.Errorf("body = %q, want %q", got, tt.wantBody)
			}
		})
	}
}

func TestText(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		s          string
	}{
		{"ok", http.StatusOK, "hello"},
		{"not found", http.StatusNotFound, "missing"},
		{"empty", http.StatusOK, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			if err := sim.Text(rec, tt.statusCode, tt.s); err != nil {
				t.Fatalf("Text() error = %v", err)
			}
			if got := rec.Code; got != tt.statusCode {
				t.Errorf("status = %d, want %d", got, tt.statusCode)
			}
			if got := rec.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
				t.Errorf("Content-Type = %q", got)
			}
			if got := rec.Body.String(); got != tt.s {
				t.Errorf("body = %q, want %q", got, tt.s)
			}
		})
	}
}

func TestBytes(t *testing.T) {
	b := []byte{0x89, 'P', 'N', 'G'}
	rec := httptest.NewRecorder()
	if err := sim.Bytes(rec, http.StatusOK, "image/png", b); err != nil {
		t.Fatalf("Bytes() error = %v", err)
	}
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Errorf("Content-Type = %q, want %q", got, "image/png")
	}
	if got := rec.Body.Bytes(); !bytes.Equal(got, b) {
		t.Errorf("body = %v, want %v", got, b)
	}
}

type errReader struct{}

var errReadFailed = errors.New("read failed")

func (errReader) Read([]byte) (int, error) {
	return 0, errReadFailed
}

func TestStream(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        io.Reader
		preHeaders  map[string]string
		wantBody    string
		wantErr     bool
	}{
		{
			name:        "ok",
			contentType: "text/csv",
			body:        strings.NewReader("a,b,c"),
			wantBody:    "a,b,c",
		},
		{
			name:        "preserves extra headers",
			contentType: "image/png",
			body:        strings.NewReader("png"),
			preHeaders:  map[string]string{"Content-Disposition": `attachment; filename="gopher.png"`},
			wantBody:    "png",
		},
		{
			name:        "reader error surfaces",
			contentType: "image/png",
			body:        errReader{},
			wantErr:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			for k, v := range tt.preHeaders {
				rec.Header().Set(k, v)
			}
			err := sim.Stream(rec, http.StatusOK, tt.contentType, tt.body)
			if tt.wantErr {
				if err == nil {
					t.Fatal("Stream() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Stream() error = %v", err)
			}
			if got := rec.Header().Get("Content-Type"); got != tt.contentType {
				t.Errorf("Content-Type = %q, want %q", got, tt.contentType)
			}
			for k, want := range tt.preHeaders {
				if got := rec.Header().Get(k); got != want {
					t.Errorf("%s = %q, want %q", k, got, want)
				}
			}
			if got := rec.Body.String(); got != tt.wantBody {
				t.Errorf("body = %q, want %q", got, tt.wantBody)
			}
		})
	}
}

//nolint:funlen // table-driven test
func TestAttachment(t *testing.T) {
	tests := []struct {
		name            string
		filename        string
		body            io.Reader
		wantDisposition string
		wantContentType string
		wantBody        string
		wantErr         bool
	}{
		{
			name:            "ascii filename infers type",
			filename:        "report.txt",
			body:            strings.NewReader("a,b,c"),
			wantDisposition: `attachment; filename=report.txt`,
			wantContentType: "text/plain; charset=utf-8",
			wantBody:        "a,b,c",
		},
		{
			name:            "unicode filename uses RFC 5987 encoding",
			filename:        "报告.txt",
			body:            strings.NewReader("a,b,c"),
			wantDisposition: `attachment; filename*=utf-8''%E6%8A%A5%E5%91%8A.txt`,
			wantContentType: "text/plain; charset=utf-8",
			wantBody:        "a,b,c",
		},
		{
			name:            "quote in filename is escaped",
			filename:        `say "hi".txt`,
			body:            strings.NewReader("hi"),
			wantDisposition: `attachment; filename="say \"hi\".txt"`,
			wantContentType: "text/plain; charset=utf-8",
			wantBody:        "hi",
		},
		{
			name:            "unknown extension falls back to octet-stream",
			filename:        "blob",
			body:            strings.NewReader("raw"),
			wantDisposition: `attachment; filename=blob`,
			wantContentType: "application/octet-stream",
			wantBody:        "raw",
		},
		{
			name:     "reader error surfaces",
			filename: "blob",
			body:     errReader{},
			wantErr:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			err := sim.Attachment(rec, tt.filename, tt.body)
			if tt.wantErr {
				if err == nil {
					t.Fatal("Attachment() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Attachment() error = %v", err)
			}
			if got := rec.Code; got != http.StatusOK {
				t.Errorf("status = %d, want %d", got, http.StatusOK)
			}
			if got := rec.Header().Get("Content-Disposition"); got != tt.wantDisposition {
				t.Errorf("Content-Disposition = %q, want %q", got, tt.wantDisposition)
			}
			if got := rec.Header().Get("Content-Type"); got != tt.wantContentType {
				t.Errorf("Content-Type = %q, want %q", got, tt.wantContentType)
			}
			if got := rec.Body.String(); got != tt.wantBody {
				t.Errorf("body = %q, want %q", got, tt.wantBody)
			}
		})
	}
}
