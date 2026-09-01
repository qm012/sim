// Copyright (c) 2026 The Sim Authors
// Use of this source code is governed by a MIT license
// that can be found in the LICENSE file.

package sim

import (
	"bytes"
	"context"
	"encoding"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	// ErrBindTarget is returned when a bind target is not a non-nil
	// pointer to a struct.
	ErrBindTarget = errors.New("sim: bind target must be a non-nil struct pointer")
	// ErrUnsupportedKind is returned when a field's kind cannot be bound
	// from strings.
	ErrUnsupportedKind = errors.New("unsupported field kind")
)

// Validator defines the interface for validating a decoded request payload.
// Bind calls Validate automatically when the decoded value implements it.
type Validator interface {
	// Validate validates the decoded request data.
	Validate(ctx context.Context) error
}

// Decoder decodes an HTTP request into a *T.
// BindJSON, BindXML, BindQuery, BindForm, BindPath and
// BindHeader use the built-in decoders; custom formats plug in through
// this interface.
type Decoder[T any] interface {
	Decode(r *http.Request) (*T, error)
}

// DecoderFunc adapts an ordinary function to the Decoder interface.
type DecoderFunc[T any] func(r *http.Request) (*T, error)

// Decode implements Decoder.
func (f DecoderFunc[T]) Decode(r *http.Request) (*T, error) {
	return f(r)
}

// Bind decodes the request with src and returns the decoded value.
// If the value implements Validator, Bind validates it against the
// request context before returning; a nil value skips validation.
func Bind[T any](r *http.Request, src Decoder[T]) (*T, error) {
	v, err := src.Decode(r)
	if err != nil {
		return nil, err
	}
	if v == nil {
		//nolint:nilnil // a decoder may return no value with no error; Bind forwards the nil and skips validation
		return nil, nil
	}
	if vd, ok := any(v).(Validator); ok {
		if err = vd.Validate(r.Context()); err != nil {
			return nil, err
		}
	}
	return v, nil
}

type ctxBodyKey struct{}

// BufferBody reads the request body and returns a shallow copy of the
// request whose Body is restored for subsequent reads and whose context
// carries a copy of the body for repeated reads via BodyFromContext. The
// whole body is buffered in memory; to cap it, limit the request body
// beforehand — globally with http.MaxBytesHandler in a wrapper, or per
// handler:
//
//	app.Post("/upload", http.MaxBytesHandler(h, 10<<20))
func BufferBody(r *http.Request) (*http.Request, error) {
	body, err := readBody(r)
	if err != nil {
		return nil, err
	}

	ctx := context.WithValue(r.Context(), ctxBodyKey{}, body)
	nr := r.WithContext(ctx)
	nr.Body = io.NopCloser(bytes.NewReader(body))
	return nr, nil
}

var bodyPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

// readBody copies the request body into a scratch buffer from bodyPool
// and returns an owned clone.
func readBody(r *http.Request) ([]byte, error) {
	buf, ok := bodyPool.Get().(*bytes.Buffer)
	if !ok {
		buf = new(bytes.Buffer)
	}
	buf.Reset()
	defer bodyPool.Put(buf)
	if _, err := io.Copy(buf, r.Body); err != nil {
		return nil, err
	}
	return bytes.Clone(buf.Bytes()), nil
}

// BodyFromContext returns the body cached by BufferBody and reports
// whether the context carried one.
func BodyFromContext(ctx context.Context) ([]byte, bool) {
	v, ok := ctx.Value(ctxBodyKey{}).([]byte)
	return v, ok
}

// requestBody returns the reader a body decoder should consume: the cached
// context body when present, otherwise the raw request body.
func requestBody(r *http.Request) io.Reader {
	if body, ok := BodyFromContext(r.Context()); ok {
		return bytes.NewReader(body)
	}
	return r.Body
}

// JSONDecoderOption configures the JSON decoder.
type JSONDecoderOption func(*json.Decoder)

// UseNumber parses JSON numbers into json.Number instead of float64.
func UseNumber() JSONDecoderOption {
	return func(d *json.Decoder) { d.UseNumber() }
}

// DisallowUnknownFields makes decoding fail when the JSON contains fields
// that do not match any field of T.
func DisallowUnknownFields() JSONDecoderOption {
	return func(d *json.Decoder) { d.DisallowUnknownFields() }
}

// jsonSource returns a Decoder that decodes the request body as JSON.
func jsonSource[T any](opts ...JSONDecoderOption) Decoder[T] {
	return DecoderFunc[T](func(r *http.Request) (*T, error) {
		var t T
		d := json.NewDecoder(requestBody(r))
		for _, opt := range opts {
			opt(d)
		}
		if err := d.Decode(&t); err != nil {
			return nil, err
		}
		return &t, nil
	})
}

// xmlSource returns a Decoder that decodes the request body as XML.
func xmlSource[T any]() Decoder[T] {
	return DecoderFunc[T](func(r *http.Request) (*T, error) {
		var t T
		//nolint:gosec // decoding the untrusted request body is BindXML's purpose
		if err := xml.NewDecoder(requestBody(r)).Decode(&t); err != nil {
			return nil, err
		}
		return &t, nil
	})
}

// querySource returns a Decoder that binds the URL query into T using
// `query` struct tags. The query is parsed with url.ParseQuery rather
// than http.Request.URL.Query, which drops malformed pairs and their
// error, so a bad query fails the bind the same way a bad form body
// does.
func querySource[T any]() Decoder[T] {
	return DecoderFunc[T](func(r *http.Request) (*T, error) {
		values, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			return nil, err
		}
		var t T
		if err = bindValues(&t, "query", values, nil); err != nil {
			return nil, err
		}
		return &t, nil
	})
}

// defaultMaxFormMemory caps in-memory multipart file parts at 32 MiB,
// matching the default used by http.Request.FormValue; larger parts
// spill to temporary files. See BindForm for the full binding behavior
// and oversized-upload guidance.
const defaultMaxFormMemory = 32 << 20 // 32 MiB

// formSource returns a Decoder that binds form values (query plus request
// body form) into T using `form` struct tags. Both urlencoded and multipart
// bodies are parsed; multipart file parts bind into *multipart.FileHeader
// or []*multipart.FileHeader fields.
func formSource[T any]() Decoder[T] {
	return DecoderFunc[T](func(r *http.Request) (*T, error) {
		if err := parseForm(r); err != nil {
			return nil, err
		}
		var files func(name string) ([]*multipart.FileHeader, bool)
		if mf := r.MultipartForm; mf != nil {
			files = func(name string) ([]*multipart.FileHeader, bool) {
				fhs, ok := mf.File[name]
				return fhs, ok && len(fhs) > 0
			}
		}
		var t T
		if err := bindValues(&t, "form", r.Form, files); err != nil {
			return nil, err
		}
		return &t, nil
	})
}

// parseForm parses both urlencoded and multipart form bodies, populating
// r.Form with the text fields of either. ParseForm runs first so its
// errors (e.g. an oversized urlencoded body) stop the bind, where
// ParseMultipartForm alone would keep parsing the body alongside a
// ParseForm failure; the media type match is then left to
// ParseMultipartForm, which honors case-insensitive Content-Type
// values.
func parseForm(r *http.Request) error {
	// The form parsers read r.Body directly instead of going through
	// requestBody, so restore a buffered body for them; without this a
	// request whose body was already consumed binds empty values.
	if body, ok := BodyFromContext(r.Context()); ok && r.PostForm == nil {
		r.Body = io.NopCloser(bytes.NewReader(body))
	}
	if err := r.ParseForm(); err != nil {
		return err
	}
	//nolint:gosec // memory is capped by defaultMaxFormMemory; oversized bodies are rejected upstream
	if err := r.ParseMultipartForm(defaultMaxFormMemory); err != nil {
		if errors.Is(err, http.ErrNotMultipart) {
			return nil
		}
		return err
	}
	return nil
}

// pathSource returns a Decoder that binds path values (r.PathValue) into T
// using `path` struct tags. r.PathValue returns "" both for a missing
// wildcard and for one matching an empty value (e.g. {rest...}), so an
// empty path value is treated as absent and triggers default=.
func pathSource[T any]() Decoder[T] {
	return DecoderFunc[T](func(r *http.Request) (*T, error) {
		var t T
		lookup := func(name string) ([]string, bool) {
			v := r.PathValue(name)
			return []string{v}, v != ""
		}
		if err := bindParams(&t, "path", lookup, nil); err != nil {
			return nil, err
		}
		return &t, nil
	})
}

// headerSource returns a Decoder that binds request headers into T using
// `header` struct tags. Header names are matched case-insensitively.
func headerSource[T any]() Decoder[T] {
	return DecoderFunc[T](func(r *http.Request) (*T, error) {
		var t T
		lookup := func(name string) ([]string, bool) {
			vs := r.Header.Values(name)
			return vs, len(vs) > 0
		}
		if err := bindParams(&t, "header", lookup, nil); err != nil {
			return nil, err
		}
		return &t, nil
	})
}

// BindJSON decodes the request body as JSON into a *T and validates it
// when T implements Validator. Only the first JSON value is decoded;
// trailing content is not rejected.
func BindJSON[T any](r *http.Request, opts ...JSONDecoderOption) (*T, error) {
	return Bind(r, jsonSource[T](opts...))
}

// BindXML decodes the request body as XML into a *T and validates it
// when T implements Validator.
func BindXML[T any](r *http.Request) (*T, error) {
	return Bind(r, xmlSource[T]())
}

// BindQuery binds the URL query into a *T using `query` struct tags and
// validates it when T implements Validator. A malformed query string is
// rejected instead of silently dropping the affected keys. T may also be
// map[string]string or map[string][]string, which receive the query
// values directly. The Binding section of the package documentation
// describes the shared struct-tag rules.
func BindQuery[T any](r *http.Request) (*T, error) {
	return Bind(r, querySource[T]())
}

// BindForm binds form values — the URL query plus the request body form —
// into a *T using `form` struct tags and validates it when T implements
// Validator. T may also be map[string]string or map[string][]string,
// which receive the form values directly. The Binding section of the
// package documentation describes the shared struct-tag rules.
//
// Both urlencoded and multipart bodies are parsed with a fixed 32 MiB
// memory cap; multipart file parts bind into *multipart.FileHeader or
// []*multipart.FileHeader fields. A body buffered with BufferBody is
// parsed from the cached copy. Parsing populates r.Form, r.PostForm
// and r.MultipartForm in place.
// To reject oversized uploads, limit the body with http.MaxBytesHandler —
// globally in a wrapper or per handler — before the request reaches
// BindForm.
func BindForm[T any](r *http.Request) (*T, error) {
	return Bind(r, formSource[T]())
}

// BindPath binds path values into a *T using `path` struct tags and
// validates it when T implements Validator. A wildcard that is missing
// or that matched an empty value counts as absent. Map targets are not
// supported, because path wildcards cannot be enumerated. The Binding
// section of the package documentation describes the shared struct-tag
// rules.
func BindPath[T any](r *http.Request) (*T, error) {
	return Bind(r, pathSource[T]())
}

// BindHeader binds request headers into a *T using `header` struct tags
// and validates it when T implements Validator. Header names are matched
// case-insensitively; map targets are not supported. The Binding section
// of the package documentation describes the shared struct-tag rules.
func BindHeader[T any](r *http.Request) (*T, error) {
	return Bind(r, headerSource[T]())
}

// bindValues binds values into dst using the given struct tag: map targets
// take the values directly, struct targets go through bindParams.
func bindValues(
	dst any,
	tag string,
	values url.Values,
	files func(name string) ([]*multipart.FileHeader, bool),
) error {
	if tryBindMap(dst, values) {
		return nil
	}
	return bindParams(dst, tag, urlValuesLookup(values), files)
}

// bindParams fills the exported fields of dst (a non-nil pointer to a
// struct) from values supplied by lookup, keyed by the given struct tag.
// lookup reports whether the key is present; a present key may still
// carry empty values, which count as absent for default=. When files is
// non-nil, *multipart.FileHeader and []*multipart.FileHeader fields bind
// from it instead; when it is nil they stay unset.
func bindParams(
	dst any,
	tag string,
	lookup func(name string) ([]string, bool),
	files func(name string) ([]*multipart.FileHeader, bool),
) error {
	v := reflect.ValueOf(dst)
	if v.Kind() != reflect.Pointer || v.IsNil() || v.Elem().Kind() != reflect.Struct {
		return ErrBindTarget
	}
	_, err := bindStruct(v.Elem(), tag, lookup, files)
	return err
}

// bindStruct fills the exported fields of s, reporting whether any field
// was set.
func bindStruct(
	s reflect.Value,
	tag string,
	lookup func(name string) ([]string, bool),
	files func(name string) ([]*multipart.FileHeader, bool),
) (bool, error) {
	plan := planFor(s.Type(), tag)
	var isSet bool
	for i := range plan.fields {
		// Plans are immutable once built, so fields bind through a
		// pointer instead of copying the plan per field.
		fp := &plan.fields[i]
		set, err := bindField(s.Field(fp.index), fp, tag, lookup, files)
		if err != nil {
			return false, err
		}
		isSet = isSet || set
	}
	return isSet, nil
}

// bindField binds one field from its cached plan and reports whether it
// was set.
func bindField(
	fv reflect.Value,
	fp *fieldPlan,
	tag string,
	lookup func(name string) ([]string, bool),
	files func(name string) ([]*multipart.FileHeader, bool),
) (bool, error) {
	if fp.embedded {
		// Recurse; a nil embedded pointer stays nil unless a field
		// inside it binds, which includes binding from default=.
		ptr := fv
		if fp.embPtr {
			if fv.IsNil() {
				ptr = reflect.New(fv.Type().Elem())
			}
		} else {
			ptr = fv.Addr()
		}
		set, err := bindStruct(ptr.Elem(), tag, lookup, files)
		if err != nil {
			return false, err
		}
		if fp.embPtr && fv.IsNil() && set {
			fv.Set(ptr)
		}
		return set, nil
	}
	if fp.isFile || fp.isFileSlice {
		if files == nil {
			return false, nil
		}
		fhs, ok := files(fp.name)
		if !ok {
			return false, nil
		}
		if fp.isFile {
			fv.Set(reflect.ValueOf(fhs[0]))
		} else {
			fv.Set(reflect.ValueOf(fhs))
		}
		return true, nil
	}
	raw, ok := lookup(fp.name)
	if !ok || emptyValues(raw) {
		if fp.hasDef {
			raw = []string{fp.def}
		} else if !ok || len(raw) == 0 {
			return false, nil
		}
		// Present but empty values without a default bind as-is, so
		// non-string fields report a conversion error.
	}
	if err := setField(fv, fp, raw); err != nil {
		return false, fmt.Errorf("sim: bind field %s: %w", fp.goName, err)
	}
	return true, nil
}

// emptyValues reports whether raw carries nothing to bind: no values at
// all, or only empty strings. Such a result counts as absent, so a
// default= option applies to "?k=" and "?k=&k=" alike.
func emptyValues(raw []string) bool {
	for _, s := range raw {
		if s != "" {
			return false
		}
	}
	return true
}

var (
	textUnmarshalerType = reflect.TypeFor[encoding.TextUnmarshaler]()
	durationType        = reflect.TypeFor[time.Duration]()
	fileHeaderType      = reflect.TypeFor[*multipart.FileHeader]()
	fileHeaderSliceType = reflect.TypeFor[[]*multipart.FileHeader]()

	// plans caches parsed field layouts per (type, tag); entries are
	// bounded by the distinct (struct type, tag) pairs the program binds.
	plansMu sync.RWMutex
	plans   = make(map[planKey]*structPlan)
)

// planFor returns the plan for typ and tag, building and caching it on
// first use.
func planFor(typ reflect.Type, tag string) *structPlan {
	key := planKey{typ, tag}
	plansMu.RLock()
	p, ok := plans[key]
	plansMu.RUnlock()
	if ok {
		return p
	}
	plansMu.Lock()
	defer plansMu.Unlock()
	if p, ok = plans[key]; ok {
		return p
	}
	p = buildPlan(typ, tag)
	plans[key] = p
	return p
}

// buildPlan computes the bind layout of typ for tag: exported fields,
// their bind keys and defaults, and their conversion plans.
func buildPlan(typ reflect.Type, tag string) *structPlan {
	plan := &structPlan{fields: make([]fieldPlan, 0, typ.NumField())}
	for i := range typ.NumField() {
		if fp, ok := planField(typ.Field(i), i, tag); ok {
			plan.fields = append(plan.fields, fp)
		}
	}
	return plan
}

// planField computes the bind layout of one struct field and reports
// whether the field binds at all.
func planField(sf reflect.StructField, index int, tag string) (fieldPlan, bool) {
	// Unexported fields never bind; for an anonymous field this also
	// skips a struct whose type name is unexported.
	if !sf.IsExported() {
		return fieldPlan{}, false
	}
	name, opts, _ := strings.Cut(sf.Tag.Get(tag), ",")
	if name == "-" {
		// A "-" field never binds, embedded or not.
		return fieldPlan{}, false
	}
	// Conversion plans are computed on the dereferenced field: a field and
	// a pointer to it bind the same way.
	t := sf.Type
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	// A type that decodes itself from text binds as a single value, even
	// when it is anonymous or a slice of bytes, as net.IP is.
	selfDecoding := reflect.PointerTo(t).Implements(textUnmarshalerType)
	// Other anonymous struct fields recurse with the same tag; a tag name
	// on them is ignored.
	if sf.Anonymous && !selfDecoding && isEmbeddable(sf.Type) {
		return fieldPlan{
			index:    index,
			goName:   sf.Name,
			embedded: true,
			embPtr:   sf.Type.Kind() == reflect.Pointer,
		}, true
	}
	if name == "" {
		name = sf.Name
	}
	fp := fieldPlan{index: index, goName: sf.Name, name: name}
	if def, ok := tagDefault(opts); ok {
		fp.def, fp.hasDef = def, true
	}
	switch {
	case sf.Type == fileHeaderType:
		fp.isFile = true
	case sf.Type == fileHeaderSliceType:
		fp.isFileSlice = true
	case !selfDecoding && t.Kind() == reflect.Slice:
		planSlice(&fp, t.Elem())
	default:
		fp.scalar = buildScalar(t)
	}
	return fp, true
}

// planSlice fills the slice part of fp for a slice field with the given
// element type.
func planSlice(fp *fieldPlan, et reflect.Type) {
	fp.isSlice = true
	if et.Kind() == reflect.Uint8 {
		// []byte binds the raw value instead of one element per value.
		fp.byteSlice = true
		return
	}
	// Pointer elements (e.g. []*int) are allocated and parsed from their
	// string form, one per value.
	if et.Kind() == reflect.Pointer {
		fp.elemPtr = true
		et = et.Elem()
	}
	fp.scalar = buildScalar(et)
}

// isEmbeddable reports whether an anonymous field recurses into the
// struct it contains.
func isEmbeddable(t reflect.Type) bool {
	return t.Kind() == reflect.Struct ||
		t.Kind() == reflect.Pointer && t.Elem().Kind() == reflect.Struct
}

// urlValuesLookup adapts url.Values to the lookup function used by
// bindParams.
func urlValuesLookup(values url.Values) func(name string) ([]string, bool) {
	return func(name string) ([]string, bool) {
		vs, ok := values[name]
		return vs, ok && len(vs) > 0
	}
}

// tagDefault extracts the `default=value` option from struct tag
// options; the value keeps everything after the first "=" up to the next
// option, so it cannot contain a comma.
func tagDefault(opts string) (string, bool) {
	for opts != "" {
		var opt string
		opt, opts, _ = strings.Cut(opts, ",")
		k, v, _ := strings.Cut(opt, "=")
		if k == "default" {
			return v, true
		}
	}
	return "", false
}

// tryBindMap fills dst when it points to a map[string]string or
// map[string][]string and reports whether it did: string maps take the
// last value per key, slice maps copy every value. Other types are left
// untouched.
func tryBindMap(dst any, values url.Values) bool {
	switch m := dst.(type) {
	case *map[string]string:
		if *m == nil {
			*m = make(map[string]string, len(values))
		}
		for k, v := range values {
			if len(v) > 0 {
				(*m)[k] = v[len(v)-1]
			}
		}
		return true
	case *map[string][]string:
		if *m == nil {
			*m = make(map[string][]string, len(values))
		}
		for k, v := range values {
			(*m)[k] = slices.Clone(v)
		}
		return true
	}
	return false
}

// setField assigns raw to the field; scalars and []byte take the last
// value, other slices take every value.
func setField(f reflect.Value, fp *fieldPlan, raw []string) error {
	if f.Kind() == reflect.Pointer {
		if f.IsNil() {
			f.Set(reflect.New(f.Type().Elem()))
		}
		f = f.Elem()
	}
	if fp.isSlice {
		// []byte binds the raw string instead of parsing it per element.
		if fp.byteSlice {
			f.SetBytes([]byte(raw[len(raw)-1]))
			return nil
		}
		slice := reflect.MakeSlice(f.Type(), len(raw), len(raw))
		for i, s := range raw {
			elem := slice.Index(i)
			if fp.elemPtr {
				// MakeSlice leaves pointer elements nil; allocate first.
				elem.Set(reflect.New(elem.Type().Elem()))
				elem = elem.Elem()
			}
			if err := fp.scalar.parse(elem, s); err != nil {
				return err
			}
		}
		f.Set(slice)
		return nil
	}
	return fp.scalar.parse(f, raw[len(raw)-1])
}

// planKey identifies a cached bind plan by struct type and tag.
type planKey struct {
	typ reflect.Type
	tag string
}

// structPlan is the cached bind layout of one struct type and tag.
type structPlan struct {
	fields []fieldPlan
}

// fieldPlan is the cached bind layout of one exported struct field.
type fieldPlan struct {
	index       int    // field index in the struct
	goName      string // Go field name, used in errors
	name        string // bind key: tag name or field name
	def         string // default= option value
	hasDef      bool   // default= option present
	embedded    bool   // anonymous struct or *struct field: recurse
	embPtr      bool   // embedded field is a pointer
	isFile      bool   // *multipart.FileHeader
	isFileSlice bool   // []*multipart.FileHeader
	isSlice     bool   // binds every value into a slice
	byteSlice   bool   // []byte fast path
	elemPtr     bool   // slice element is a pointer: allocate per element
	scalar      scalarPlan
}

// scalarPlan is the cached conversion plan of one scalar type.
type scalarPlan struct {
	kind          reflect.Kind
	isDuration    bool // converts via time.ParseDuration
	textUnmarshal bool // *T implements encoding.TextUnmarshaler
}

// buildScalar computes the conversion plan of one scalar type.
func buildScalar(t reflect.Type) scalarPlan {
	return scalarPlan{
		kind:          t.Kind(),
		isDuration:    t == durationType,
		textUnmarshal: reflect.PointerTo(t).Implements(textUnmarshalerType),
	}
}

// parse assigns s to f, which must be settable and of the plan's type.
// Types whose pointer implements encoding.TextUnmarshaler decode
// themselves; the remaining kinds parse their string form.
func (sp scalarPlan) parse(f reflect.Value, s string) error {
	if sp.textUnmarshal {
		// buildScalar verified *T implements encoding.TextUnmarshaler,
		// so the assertion cannot fail.
		u, _ := reflect.TypeAssert[encoding.TextUnmarshaler](f.Addr())
		return u.UnmarshalText([]byte(s))
	}
	//nolint:exhaustive // the remaining kinds fall through to the unsupported-kind error
	switch sp.kind {
	case reflect.String:
		f.SetString(s)
	case reflect.Bool:
		b, err := strconv.ParseBool(s)
		if err != nil {
			return err
		}
		f.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if sp.isDuration {
			d, err := time.ParseDuration(s)
			if err != nil {
				return err
			}
			f.SetInt(int64(d))
			return nil
		}
		n, err := strconv.ParseInt(s, 10, f.Type().Bits())
		if err != nil {
			return err
		}
		f.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(s, 10, f.Type().Bits())
		if err != nil {
			return err
		}
		f.SetUint(n)
	case reflect.Float32, reflect.Float64:
		n, err := strconv.ParseFloat(s, f.Type().Bits())
		if err != nil {
			return err
		}
		f.SetFloat(n)
	default:
		return fmt.Errorf("%w %s", ErrUnsupportedKind, sp.kind)
	}
	return nil
}
