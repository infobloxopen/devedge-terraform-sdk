package tfkit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestClientCRUDAndBearer(t *testing.T) {
	ctx := context.Background()
	var (
		mu         sync.Mutex
		seenBearer string
		gotBody    map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		seenBearer = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodPost:
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "w1", "displayName": gotBody["displayName"]})
		case http.MethodGet:
			if strings.HasSuffix(r.URL.Path, "/missing") {
				http.Error(w, `{"message":"nope"}`, http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "w1", "displayName": "Gadget"})
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok-123")

	// Create round-trips the body and decodes the response.
	var out map[string]any
	if err := c.DoCreate(ctx, "/v1/widgets", map[string]any{"displayName": "Gadget"}, &out); err != nil {
		t.Fatalf("DoCreate: %v", err)
	}
	if out["id"] != "w1" || out["displayName"] != "Gadget" {
		t.Fatalf("create response = %v", out)
	}
	if seenBearer != "tok-123" {
		t.Fatalf("bearer not attached, saw %q", seenBearer)
	}
	if gotBody["displayName"] != "Gadget" {
		t.Fatalf("create body not sent: %v", gotBody)
	}

	// Read decodes a resource.
	var read map[string]any
	if err := c.DoRead(ctx, "/v1/widgets/w1", &read); err != nil {
		t.Fatalf("DoRead: %v", err)
	}
	if read["displayName"] != "Gadget" {
		t.Fatalf("read response = %v", read)
	}

	// Non-2xx surfaces as an error carrying the status + body.
	err := c.DoRead(ctx, "/v1/widgets/missing", &read)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected 404 error, got %v", err)
	}
}

func TestMappingHelpers(t *testing.T) {
	// Set* skip null/unknown and write known values.
	body := map[string]any{}
	SetString(body, "a", types.StringValue("x"))
	SetString(body, "skip_null", types.StringNull())
	SetString(body, "skip_unknown", types.StringUnknown())
	SetInt64(body, "n", types.Int64Value(7))
	SetBool(body, "b", types.BoolValue(true))
	if body["a"] != "x" || body["n"] != int64(7) || body["b"] != true {
		t.Fatalf("Set* wrote wrong values: %v", body)
	}
	if _, ok := body["skip_null"]; ok {
		t.Fatalf("null value should not be written")
	}
	if _, ok := body["skip_unknown"]; ok {
		t.Fatalf("unknown value should not be written")
	}

	// Get* tolerate JSON's float64 numerics and missing keys.
	data := map[string]any{"s": "hi", "n": float64(42), "b": true}
	if GetString(data, "s").ValueString() != "hi" {
		t.Fatalf("GetString")
	}
	if GetInt64(data, "n").ValueInt64() != 42 {
		t.Fatalf("GetInt64 from float64")
	}
	if !GetBool(data, "b").ValueBool() {
		t.Fatalf("GetBool")
	}
	if !GetString(data, "absent").IsNull() {
		t.Fatalf("absent string should be null")
	}
	if !GetInt64(data, "absent").IsNull() {
		t.Fatalf("absent int should be null")
	}

	// MaskKeys is sorted + deterministic.
	mask := MaskKeys(map[string]any{"z": 1, "a": 1, "m": 1})
	if len(mask) != 3 || mask[0] != "a" || mask[2] != "z" {
		t.Fatalf("MaskKeys not sorted: %v", mask)
	}
}
