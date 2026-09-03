// Copyright (c) 2026 The Sim Authors
// Use of this source code is governed by a MIT license
// that can be found in the LICENSE file.

package sim_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/qm012/sim"
)

func queryRequest(t *testing.T, rawQuery string) *http.Request {
	t.Helper()
	return httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/?"+rawQuery, nil)
}

func formRequest(t *testing.T, body string) *http.Request {
	t.Helper()
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

type EmbedPage struct {
	Page int `query:"embeddedPage"`
}

// queryReq covers every BindQuery scenario in one struct: untagged,
// scalar, pointer, slice, []byte, TextUnmarshaler, default= and "-"
// fields, plus an embedded pointer struct and an embedded self-decoding
// type.
type queryReq struct {
	Name    string        // untagged: binds by field name
	OK      bool          `query:"ok"`
	Count   uint          `query:"count"`
	Rate    float64       `query:"rate"`
	Page    *int          `query:"page,default=1"`
	IDs     []int         `query:"id"`
	PtrIDs  []*int        `query:"pid"`
	Start   time.Time     `query:"start"`
	Addr    netip.Addr    `query:"addr"`
	Host    net.IP        `query:"host"` // self-decoding slice of bytes
	Timeout time.Duration `query:"timeout"`
	Tags    []time.Time   `query:"tags"`
	Data    []byte        `query:"data"`
	Secret  string        `query:"-"`
	*EmbedPage
	time.Time // embedded self-decoding type: binds by field name
}

func TestBindQuery(t *testing.T) {
	start := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	tag1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tag2 := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	unmarshalURL := "start=2026-05-01T10:00:00Z&addr=192.168.1.1&timeout=1h30m"
	tagsURL := "tags=2026-01-01T00:00:00Z&tags=2026-02-01T00:00:00Z"
	tests := []struct {
		name string
		url  string
		want queryReq
	}{
		{"untagged field", "Name=alice", queryReq{Name: "alice", Page: new(1)}},
		{
			"scalar kinds", "ok=true&count=7&rate=1.5&page=3",
			queryReq{OK: true, Count: 7, Rate: 1.5, Page: new(3)},
		},
		{"int slice", "id=1&id=2&id=3", queryReq{Page: new(1), IDs: []int{1, 2, 3}}},
		{"pointer slice", "pid=1&pid=2", queryReq{Page: new(1), PtrIDs: []*int{new(1), new(2)}}},
		{
			"text unmarshalers", unmarshalURL,
			queryReq{
				Page:    new(1),
				Start:   start,
				Addr:    netip.MustParseAddr("192.168.1.1"),
				Timeout: 90 * time.Minute,
			},
		},
		// A byte slice that decodes itself parses the whole value
		// instead of taking its raw bytes.
		{"self-decoding byte slice", "host=192.168.1.1", queryReq{Page: new(1), Host: net.ParseIP("192.168.1.1")}},
		{"embedded self-decoding type", "Time=2026-05-01T10:00:00Z", queryReq{Page: new(1), Time: start}},
		{"unmarshaler slice", tagsURL, queryReq{Page: new(1), Tags: []time.Time{tag1, tag2}}},
		{"byte slice", "data=hello", queryReq{Page: new(1), Data: []byte("hello")}},
		{"defaults on missing keys", "", queryReq{Page: new(1)}},
		{"defaults on empty values", "page=", queryReq{Page: new(1)}},
		{"defaults on repeated empty values", "page=&page=", queryReq{Page: new(1)}},
		// Scalars take the last value, which here beats the default.
		{"last value wins over empty", "page=&page=3", queryReq{Page: new(3)}},
		{"embedded pointer allocated", "embeddedPage=3", queryReq{Page: new(1), EmbedPage: &EmbedPage{Page: 3}}},
		{"embedded pointer stays nil", "ok=true", queryReq{OK: true, Page: new(1)}},
		// A "-" tag skips the field entirely.
		{"dash tag skipped", "Secret=x&Name=alice", queryReq{Name: "alice", Page: new(1)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sim.BindQuery[queryReq](queryRequest(t, tt.url))
			if err != nil {
				t.Fatalf("BindQuery() error = %v", err)
			}
			if !reflect.DeepEqual(*got, tt.want) {
				t.Errorf("BindQuery() = %#v, want %#v", *got, tt.want)
			}
		})
	}
}

func TestBindQueryDashTag(t *testing.T) {
	got, err := sim.BindQuery[struct {
		EmbedPage `query:"-"`
		Name      string `query:"name"`
	}](queryRequest(t, "embeddedPage=3&name=alice"))
	if err != nil {
		t.Fatalf("BindQuery() error = %v", err)
	}
	if got.Page != 0 || got.Name != "alice" {
		t.Errorf("BindQuery() = %+v, want skipped embedded and name=alice", *got)
	}
}

type DefaultPage struct {
	Size int `query:"size,default=10"`
}

func TestBindQueryEmbeddedDefault(t *testing.T) {
	got, err := sim.BindQuery[struct{ *DefaultPage }](queryRequest(t, ""))
	if err != nil {
		t.Fatalf("BindQuery() error = %v", err)
	}
	if got.DefaultPage == nil || got.Size != 10 {
		t.Errorf("BindQuery() DefaultPage = %+v, want allocated with size=10", got.DefaultPage)
	}
}

// default= applies even when other options precede it in the tag.
func TestBindQueryDefaultAfterOption(t *testing.T) {
	got, err := sim.BindQuery[struct {
		Page int `query:"page,opt,default=7"`
	}](queryRequest(t, ""))
	if err != nil {
		t.Fatalf("BindQuery() error = %v", err)
	}
	if got.Page != 7 {
		t.Errorf("BindQuery() Page = %d, want 7", got.Page)
	}
}

func TestBindMap(t *testing.T) {
	tests := []struct {
		name string
		bind func(t *testing.T) (any, error)
		want any
	}{
		{
			name: "query into string map",
			bind: func(t *testing.T) (any, error) {
				t.Helper()
				return sim.BindQuery[map[string]string](queryRequest(t, "a=1&a=2&b=3"))
			},
			want: &map[string]string{"a": "2", "b": "3"},
		},
		{
			name: "form into slice map",
			bind: func(t *testing.T) (any, error) {
				t.Helper()
				return sim.BindForm[map[string][]string](formRequest(t, "a=1&a=2&b=3"))
			},
			want: &map[string][]string{"a": {"1", "2"}, "b": {"3"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.bind(t)
			if err != nil {
				t.Fatalf("bind error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("bind = %#v, want %#v", got, tt.want)
			}
		})
	}
}

type bodyReq struct {
	Name string `json:"name" xml:"name"`
	Age  int    `json:"age" xml:"age"`
}

func TestBindBody(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		bind    func(r *http.Request) (*bodyReq, error)
		want    bodyReq
		wantErr bool
	}{
		{
			name: "json decodes",
			body: `{"name":"alice","age":30}`,
			bind: func(r *http.Request) (*bodyReq, error) { return sim.BindJSON[bodyReq](r) },
			want: bodyReq{Name: "alice", Age: 30},
		},
		{
			name:    "json rejects malformed body",
			body:    `{"name":`,
			bind:    func(r *http.Request) (*bodyReq, error) { return sim.BindJSON[bodyReq](r) },
			wantErr: true,
		},
		{
			name: "json rejects unknown fields on request",
			body: `{"name":"alice","extra":true}`,
			bind: func(r *http.Request) (*bodyReq, error) {
				return sim.BindJSON[bodyReq](r, sim.DisallowUnknownFields())
			},
			wantErr: true,
		},
		{
			name: "xml decodes",
			body: `<bodyReq><name>alice</name><age>30</age></bodyReq>`,
			bind: sim.BindXML[bodyReq],
			want: bodyReq{Name: "alice", Age: 30},
		},
		{
			name:    "xml rejects malformed body",
			body:    `<bodyReq><name>alice`,
			bind:    sim.BindXML[bodyReq],
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(tt.body))
			got, err := tt.bind(r)
			if tt.wantErr {
				if err == nil {
					t.Fatal("bind error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("bind error = %v", err)
			}
			if *got != tt.want {
				t.Errorf("bind = %#v, want %#v", *got, tt.want)
			}
		})
	}
}

func TestBindJSONUseNumber(t *testing.T) {
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/",
		strings.NewReader(`{"n":9007199254740993}`))
	got, err := sim.BindJSON[map[string]any](r, sim.UseNumber())
	if err != nil {
		t.Fatalf("BindJSON() error = %v", err)
	}
	if n := (*got)["n"]; n != json.Number("9007199254740993") {
		t.Errorf("BindJSON() n = %#v, want json.Number", n)
	}
}

type headerReq struct {
	ContentType string   `header:"content-type"`
	RequestID   string   `header:"x-request-id"`
	Languages   []string `header:"accept-language"`
	Missing     string   `header:"x-missing"`
	DefaultVal  string   `header:"x-default,default=xx"`
}

func TestBindHeader(t *testing.T) {
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Request-ID", "req-42")
	r.Header.Add("Accept-Language", "zh-CN")
	r.Header.Add("Accept-Language", "en-US")

	got, err := sim.BindHeader[headerReq](r)
	if err != nil {
		t.Fatalf("BindHeader() error = %v", err)
	}
	want := headerReq{
		ContentType: "application/json",
		RequestID:   "req-42",
		Languages:   []string{"zh-CN", "en-US"},
		DefaultVal:  "xx",
	}
	if !reflect.DeepEqual(*got, want) {
		t.Errorf("BindHeader() = %#v, want %#v", *got, want)
	}
}

type pathReq struct {
	ID   int    `path:"id"`
	Name string `path:"name,default=anon"`
}

func TestBindPath(t *testing.T) {
	tests := []struct {
		name  string
		setup func(r *http.Request)
		want  pathReq
	}{
		{
			name:  "explicit value",
			setup: func(r *http.Request) { r.SetPathValue("id", "42") },
			want:  pathReq{ID: 42, Name: "anon"},
		},
		{
			name:  "empty wildcard takes the default",
			setup: func(r *http.Request) { r.SetPathValue("name", "") },
			want:  pathReq{Name: "anon"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/users/42", nil)
			tt.setup(r)
			got, err := sim.BindPath[pathReq](r)
			if err != nil {
				t.Fatalf("BindPath() error = %v", err)
			}
			if *got != tt.want {
				t.Errorf("BindPath() = %#v, want %#v", *got, tt.want)
			}
		})
	}
}

type formReq struct {
	Name   string                  `form:"name"`
	Bio    string                  `form:"bio,default=anon"`
	Avatar *multipart.FileHeader   `form:"avatar"`
	Docs   []*multipart.FileHeader `form:"docs"`
}

type formFile struct {
	field, name string
}

// multipartRequest builds a multipart POST request holding the "name"
// text field plus the given file parts. The generated media type is
// replaced with ct so callers can vary its case.
func multipartRequest(t *testing.T, ct string, files []formFile) *http.Request {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if err := w.WriteField("name", "alice"); err != nil {
		t.Fatalf("WriteField() error = %v", err)
	}
	for _, f := range files {
		fw, err := w.CreateFormFile(f.field, f.name)
		if err != nil {
			t.Fatalf("CreateFormFile() error = %v", err)
		}
		if _, err = fw.Write([]byte("data")); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", &body)
	r.Header.Set("Content-Type",
		strings.Replace(w.FormDataContentType(), "multipart/form-data", ct, 1))
	return r
}

func TestBindForm(t *testing.T) {
	tests := []struct {
		name       string
		ct         string // multipart media type; empty binds a urlencoded body
		files      []formFile
		wantAvatar string
		wantDocs   []string
	}{
		{name: "urlencoded"},
		{name: "multipart", ct: "multipart/form-data"},
		// Media types are case-insensitive, so a client sending
		// "Multipart/Form-Data" must bind the same way.
		{name: "multipart mixed-case content type", ct: "Multipart/Form-Data"},
		{
			name:       "multipart with files",
			ct:         "multipart/form-data",
			files:      []formFile{{"avatar", "me.png"}, {"docs", "a.txt"}, {"docs", "b.txt"}},
			wantAvatar: "me.png",
			wantDocs:   []string{"a.txt", "b.txt"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := formRequest(t, "name=alice")
			if tt.ct != "" {
				r = multipartRequest(t, tt.ct, tt.files)
			}
			got, err := sim.BindForm[formReq](r)
			if err != nil {
				t.Fatalf("BindForm() error = %v", err)
			}
			// The absent bio field takes its default.
			if got.Name != "alice" || got.Bio != "anon" {
				t.Errorf("BindForm() = %+v, want name=alice and bio=anon", *got)
			}
			var avatar string
			if got.Avatar != nil {
				avatar = got.Avatar.Filename
			}
			if avatar != tt.wantAvatar {
				t.Errorf("BindForm() Avatar = %q, want %q", avatar, tt.wantAvatar)
			}
			docs := make([]string, 0, len(got.Docs))
			for _, fh := range got.Docs {
				docs = append(docs, fh.Filename)
			}
			if !slices.Equal(docs, tt.wantDocs) {
				t.Errorf("BindForm() Docs = %q, want %q", docs, tt.wantDocs)
			}
		})
	}
}

func TestBindFormQueryMerge(t *testing.T) {
	r := formRequest(t, "name=alice")
	r.URL.RawQuery = "q=1"
	got, err := sim.BindForm[struct {
		Name string `form:"name"`
		Q    string `form:"q"`
	}](r)
	if err != nil {
		t.Fatalf("BindForm() error = %v", err)
	}
	if got.Name != "alice" || got.Q != "1" {
		t.Errorf("BindForm() = %+v, want name=alice and q=1", *got)
	}
}

func TestBindFormOversizedURLEncoded(t *testing.T) {
	r := formRequest(t, "name="+strings.Repeat("a", 10<<20))
	if _, err := sim.BindForm[formReq](r); err == nil {
		t.Fatal("BindForm() error = nil, want error for oversized urlencoded body")
	}
}

func TestBindCustomDecoder(t *testing.T) {
	src := sim.DecoderFunc[formReq](func(r *http.Request) (*formReq, error) {
		return &formReq{Name: r.URL.Query().Get("name")}, nil
	})
	got, err := sim.Bind(queryRequest(t, "name=alice"), src)
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}
	if got.Name != "alice" {
		t.Errorf("Bind() Name = %q, want %q", got.Name, "alice")
	}

	failing := sim.DecoderFunc[formReq](func(*http.Request) (*formReq, error) {
		return nil, errDecode
	})
	if _, err = sim.Bind(queryRequest(t, ""), failing); !errors.Is(err, errDecode) {
		t.Errorf("Bind() error = %v, want errDecode", err)
	}
}

type badKindReq struct {
	M map[string]string `query:"m"`
}

func TestBindNilValue(t *testing.T) {
	src := sim.DecoderFunc[int](func(*http.Request) (*int, error) {
		//nolint:nilnil // a nil value from a decoder is the scenario under test
		return nil, nil
	})
	got, err := sim.Bind(queryRequest(t, ""), src)
	if !errors.Is(err, sim.ErrDecodeNil) || got != nil {
		t.Errorf("Bind() = %v, %v, want nil, nil", got, err)
	}
}

type alwaysInvalid struct {
	Name string `query:"name"`
}

func (alwaysInvalid) Validate(context.Context) error { return errInvalid }

type failReader struct{}

func (failReader) Read([]byte) (int, error) { return 0, errRead }

var (
	errInvalid = errors.New("invalid")
	errDecode  = errors.New("decode failed")
	errRead    = errors.New("read failed")
)

// TestBindTargetErrors covers the exported sentinels: a target that
// cannot bind at all, and a field kind that cannot be bound from
// strings.
func TestBindTargetErrors(t *testing.T) {
	tests := []struct {
		name string
		bind func(t *testing.T) error
		want error
	}{
		{
			name: "unsupported field kind",
			bind: func(t *testing.T) error {
				t.Helper()
				_, err := sim.BindQuery[badKindReq](queryRequest(t, "m=x"))
				return err
			},
			want: sim.ErrUnsupportedKind,
		},
		{
			name: "non-struct target",
			bind: func(t *testing.T) error {
				t.Helper()
				_, err := sim.BindQuery[int](queryRequest(t, "x=1"))
				return err
			},
			want: sim.ErrBindTarget,
		},
		{
			// Only BindQuery and BindForm accept map targets.
			name: "map target on header",
			bind: func(t *testing.T) error {
				t.Helper()
				r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
				_, err := sim.BindHeader[map[string]string](r)
				return err
			},
			want: sim.ErrBindTarget,
		},
		{
			name: "map target on path",
			bind: func(t *testing.T) error {
				t.Helper()
				r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
				_, err := sim.BindPath[map[string]string](r)
				return err
			},
			want: sim.ErrBindTarget,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.bind(t); !errors.Is(err, tt.want) {
				t.Errorf("bind error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestBindQueryValueErrors(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"malformed query", "ok=%zz"},
		// An empty value with no default binds as-is, so a non-string
		// field reports a conversion error.
		{"empty value without default", "embeddedPage="},
		{"bad scalar value", "count=many"},
		{"bad slice element", "id=one"},
		{"bad time value", "start=not-a-time"},
		{"bad duration value", "timeout=soon"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := sim.BindQuery[queryReq](queryRequest(t, tt.url)); err == nil {
				t.Errorf("BindQuery(%q) error = nil, want error", tt.url)
			}
		})
	}
}

func TestBindDecoderErrors(t *testing.T) {
	tests := []struct {
		name string
		bind func(t *testing.T) error
	}{
		{
			name: "path",
			bind: func(t *testing.T) error {
				t.Helper()
				r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/users/abc", nil)
				r.SetPathValue("id", "abc")
				_, err := sim.BindPath[pathReq](r)
				return err
			},
		},
		{
			name: "header",
			bind: func(t *testing.T) error {
				t.Helper()
				r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
				r.Header.Set("X-Retry", "soon")
				_, err := sim.BindHeader[struct {
					Retry int `header:"x-retry"`
				}](r)
				return err
			},
		},
		{
			name: "form",
			bind: func(t *testing.T) error {
				t.Helper()
				_, err := sim.BindForm[struct {
					Age int `form:"age"`
				}](formRequest(t, "age=old"))
				return err
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.bind(t); err == nil {
				t.Error("bind error = nil, want error")
			}
		})
	}
}

func TestBindQueryValidates(t *testing.T) {
	_, err := sim.BindQuery[alwaysInvalid](queryRequest(t, "name=x"))
	if !errors.Is(err, errInvalid) {
		t.Errorf("BindQuery() error = %v, want errInvalid", err)
	}
}

func TestBufferBodyReadError(t *testing.T) {
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", failReader{})
	if _, err := sim.BufferBody(r); !errors.Is(err, errRead) {
		t.Errorf("BufferBody() error = %v, want errRead", err)
	}
}

func TestBufferBody(t *testing.T) {
	const payload = `{"name":"alice","age":30}`
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(payload))
	nr, err := sim.BufferBody(r)
	if err != nil {
		t.Fatalf("BufferBody() error = %v", err)
	}
	body, ok := sim.BodyFromContext(nr.Context())
	if !ok || string(body) != payload {
		t.Fatalf("BodyFromContext() = %q, %t, want %q", body, ok, payload)
	}

	for range 5 {
		got, err := sim.BindJSON[bodyReq](nr)
		if err != nil {
			t.Fatalf("BindJSON() error = %v", err)
		}
		if got.Name != "alice" {
			t.Errorf("BindJSON() Name = %q, want %q", got.Name, "alice")
		}
	}
}

func TestBufferBodyForm(t *testing.T) {
	nr, err := sim.BufferBody(formRequest(t, "name=alice"))
	if err != nil {
		t.Fatalf("BufferBody() error = %v", err)
	}
	if _, err = io.Copy(io.Discard, nr.Body); err != nil {
		t.Fatalf("Copy() error = %v", err)
	}
	got, err := sim.BindForm[formReq](nr)
	if err != nil {
		t.Fatalf("BindForm() error = %v", err)
	}
	if got.Name != "alice" {
		t.Errorf("BindForm() Name = %q, want %q", got.Name, "alice")
	}
}

func BenchmarkBufferBody(b *testing.B) {
	for _, size := range []int{1 << 10, 64 << 10, 1 << 20} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			body := bytes.Repeat([]byte("x"), size)
			rd := bytes.NewReader(body)
			r := httptest.NewRequestWithContext(b.Context(), http.MethodPost, "/", rd)
			b.ReportAllocs()
			b.SetBytes(int64(size))
			for b.Loop() {
				rd.Reset(body)
				r.Body = io.NopCloser(rd)
				if _, err := sim.BufferBody(r); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
