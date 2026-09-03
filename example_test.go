// Copyright (c) 2026 The Sim Authors
// Use of this source code is governed by a MIT license
// that can be found in the LICENSE file.

package sim_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/qm012/sim"
)

// errBadSignature is returned when X-Signature does not match the body's HMAC.
var errBadSignature = errors.New("bad signature")

// webhookEvent is the payload carried by a signed JSON webhook.
type webhookEvent struct {
	EventID string `json:"event_id"`
	Type    string `json:"type"`
	// A real webhook event carries more fields (timestamp, actor, data, ...);
	// this example decodes only the two it uses, and JSON ignores the rest.
}

// ExampleDecoderFunc plugs a custom format into Bind: a JSON webhook whose
// X-Signature header is verified over the raw body bytes before the payload
// is decoded. The decoder reads the body once and reuses those bytes for both
// the HMAC check and the JSON decode.
func ExampleDecoderFunc() {
	secret := []byte("s3cret")

	// verify yields an event only when X-Signature matches the HMAC-SHA256 of
	// the raw body; a mismatch fails before any JSON is parsed. The Decoder is
	// stateless per request, so one value serves every call.
	verify := sim.DecoderFunc[webhookEvent](func(r *http.Request) (*webhookEvent, error) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, fmt.Errorf("read body: %w", err)
		}
		mac := hmac.New(sha256.New, secret)
		_, _ = mac.Write(raw)
		if !hmac.Equal([]byte(r.Header.Get("X-Signature")), []byte(hex.EncodeToString(mac.Sum(nil)))) {
			return nil, errBadSignature
		}
		var e webhookEvent
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode event: %w", err)
		}
		return &e, nil
	})

	body := `{"event_id":"evt_9f3a","type":"order.paid"}`
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(body))
	sig := hex.EncodeToString(mac.Sum(nil))
	ctx := context.Background()

	// A correctly signed webhook decodes into the event.
	ok := httptest.NewRequestWithContext(ctx, http.MethodPost, "/webhook", strings.NewReader(body))
	ok.Header.Set("X-Signature", sig)
	e, _ := sim.Bind(ok, verify)
	fmt.Println("valid:", e.Type, e.EventID)

	// A tampered signature is rejected before the payload is used.
	bad := httptest.NewRequestWithContext(ctx, http.MethodPost, "/webhook", strings.NewReader(body))
	bad.Header.Set("X-Signature", "deadbeef")
	_, err := sim.Bind(bad, verify)
	fmt.Println("tampered:", err)

	// Output:
	// valid: order.paid evt_9f3a
	// tampered: bad signature
}

type exampleUser struct {
	XMLName xml.Name `json:"-" xml:"user"`
	Name    string   `json:"name" xml:"name"`
	Age     int      `json:"age" xml:"age"`
}

func ExampleJSON() {
	w := httptest.NewRecorder()
	_ = sim.JSON(w, http.StatusOK, exampleUser{Name: "alice", Age: 30})
	fmt.Println(w.Code, w.Header().Get("Content-Type"))
	fmt.Print(w.Body)
	// Output:
	// 200 application/json; charset=utf-8
	// {"name":"alice","age":30}
}

func ExampleJSON_escapeForHTML() {
	data := map[string]string{"url": "a<b&c"}

	escaped := httptest.NewRecorder()
	_ = sim.JSON(escaped, http.StatusOK, data)
	fmt.Print(escaped.Body)

	raw := httptest.NewRecorder()
	_ = sim.JSON(raw, http.StatusOK, data, sim.EscapeForHTML(false))
	fmt.Print(raw.Body)
	// Output:
	// {"url":"a\u003cb\u0026c"}
	// {"url":"a<b&c"}
}

func ExampleJSON_indented() {
	w := httptest.NewRecorder()
	_ = sim.JSON(w, http.StatusOK, exampleUser{Name: "alice", Age: 30}, sim.Indented(true))
	fmt.Print(w.Body)
	// Output:
	// {
	//     "name": "alice",
	//     "age": 30
	// }
}

func ExampleXML() {
	w := httptest.NewRecorder()
	_ = sim.XML(w, http.StatusOK, exampleUser{Name: "alice", Age: 30})
	fmt.Println(w.Header().Get("Content-Type"))
	fmt.Print(w.Body)
	// Output:
	// application/xml; charset=utf-8
	// <?xml version="1.0" encoding="UTF-8"?>
	// <user><name>alice</name><age>30</age></user>
}

func ExampleText() {
	w := httptest.NewRecorder()
	_ = sim.Text(w, http.StatusOK, "hello, sim")
	fmt.Println(w.Header().Get("Content-Type"))
	fmt.Print(w.Body)
	// Output:
	// text/plain; charset=utf-8
	// hello, sim
}

func ExampleBytes() {
	w := httptest.NewRecorder()
	_ = sim.Bytes(w, http.StatusOK, "text/csv", []byte("name,age\nalice,30\n"))
	fmt.Println(w.Header().Get("Content-Type"))
	fmt.Print(w.Body)
	// Output:
	// text/csv
	// name,age
	// alice,30
}

func ExampleStream() {
	w := httptest.NewRecorder()
	_ = sim.Stream(w, http.StatusOK, "text/plain", strings.NewReader("chunked body"))
	fmt.Print(w.Body)
	// Output: chunked body
}

func ExampleAttachment() {
	w := httptest.NewRecorder()
	_ = sim.Attachment(w, "report.txt", strings.NewReader("hello"))
	fmt.Println(w.Header().Get("Content-Disposition"))
	fmt.Print(w.Body)
	// Output:
	// attachment; filename=report.txt
	// hello
}
