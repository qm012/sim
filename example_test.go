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
