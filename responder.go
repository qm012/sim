// Copyright (c) 2026 The Sim Authors
// Use of this source code is governed by a MIT license
// that can be found in the LICENSE file.

package sim

import (
	"encoding/json"
	"encoding/xml"
	"io"
	"mime"
	"net/http"
	"path/filepath"
)

// JSONEncoderOption configures JSON.
type JSONEncoderOption func(*jsonEncoderOptions)

type jsonEncoderOptions struct {
	escapeHTML bool
	indent     bool
}

// EscapeForHTML controls HTML character escaping in JSON output.
// Escaping is enabled by default; EscapeForHTML(false) disables it.
func EscapeForHTML(v bool) JSONEncoderOption {
	return func(o *jsonEncoderOptions) { o.escapeHTML = v }
}

// Indented controls indentation in JSON output.
// Indentation is disabled by default; Indented(true) enables it.
func Indented(v bool) JSONEncoderOption {
	return func(o *jsonEncoderOptions) { o.indent = v }
}

// JSON writes data as JSON with the given status code.
func JSON(w http.ResponseWriter, statusCode int, data any, opts ...JSONEncoderOption) error {
	o := jsonEncoderOptions{escapeHTML: true}
	for _, opt := range opts {
		opt(&o)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	enc := json.NewEncoder(w)
	if !o.escapeHTML {
		enc.SetEscapeHTML(false)
	}
	if o.indent {
		enc.SetIndent("", "    ")
	}
	return enc.Encode(data)
}

// XML writes data as XML with the given status code.
func XML(w http.ResponseWriter, statusCode int, data any) error {
	b, err := xml.Marshal(data)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(statusCode)
	if _, err = w.Write([]byte(xml.Header)); err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}

// Text writes s as plain text with the given status code.
func Text(w http.ResponseWriter, statusCode int, s string) error {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(statusCode)
	_, err := io.WriteString(w, s)
	return err
}

// Bytes writes raw bytes with the given status code and content type.
func Bytes(w http.ResponseWriter, statusCode int, contentType string, b []byte) error {
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(statusCode)
	_, err := w.Write(b)
	return err
}

// Stream writes r with the given status code and content type. The body is
// copied in chunks, keeping memory use constant, so Stream suits large or
// not-yet-complete bodies: file downloads, proxied upstream responses and
// generated streams. With an *os.File, the copy uses sendfile when possible.
// Prefer http.ServeFile / http.ServeFileFS for disk files that need Range
// and caching support, and Attachment for downloads that prompt the client
// to save the body under a filename. Extra headers such as Content-Length
// can be set on w before calling Stream.
func Stream(w http.ResponseWriter, statusCode int, contentType string, r io.Reader) error {
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(statusCode)
	_, err := io.Copy(w, r)
	return err
}

// Attachment writes r with status 200 OK as a download attachment,
// prompting browsers to save it as filename. Content-Disposition is set from
// filename, and Content-Type is inferred from filename's extension, falling
// back to application/octet-stream. Prefer http.ServeFile / http.ServeFileFS
// for disk files that need Range and caching support.
func Attachment(w http.ResponseWriter, filename string, r io.Reader) error {
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	contentType := mime.TypeByExtension(filepath.Ext(filename))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return Stream(w, http.StatusOK, contentType, r)
}
