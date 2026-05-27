package httpapi

import (
	"net/http/httptest"
	"testing"
)

func TestParseExpectResponse_HeaderOverridesBody(t *testing.T) {
	r := httptest.NewRequest("POST", "/v1/device/dev/op", nil)
	r.Header.Set("X-Expect-Response", "false")

	requestData := map[string]any{
		"__expect_response": true,
	}

	got, err := parseExpectResponse(r, requestData)
	if err != nil {
		t.Fatalf("parseExpectResponse returned error: %v", err)
	}
	if got {
		t.Fatalf("expected false, got true")
	}
	if _, ok := requestData["__expect_response"]; !ok {
		t.Fatalf("header mode should not mutate __expect_response in body")
	}
}

func TestParseExpectResponse_BodyFallbackAndCleanup(t *testing.T) {
	r := httptest.NewRequest("POST", "/v1/device/dev/op", nil)

	requestData := map[string]any{
		"__expect_response": "No",
	}

	got, err := parseExpectResponse(r, requestData)
	if err != nil {
		t.Fatalf("parseExpectResponse returned error: %v", err)
	}
	if got {
		t.Fatalf("expected false, got true")
	}
	if _, ok := requestData["__expect_response"]; ok {
		t.Fatalf("expected __expect_response to be removed from command body")
	}
}

func TestParseExpectResponse_DefaultTrue(t *testing.T) {
	r := httptest.NewRequest("POST", "/v1/device/dev/op", nil)

	requestData := map[string]any{"foo": "bar"}
	got, err := parseExpectResponse(r, requestData)
	if err != nil {
		t.Fatalf("parseExpectResponse returned error: %v", err)
	}
	if !got {
		t.Fatalf("expected true, got false")
	}
}

func TestParseExpectResponse_InvalidValues(t *testing.T) {
	t.Run("invalid header", func(t *testing.T) {
		r := httptest.NewRequest("POST", "/v1/device/dev/op", nil)
		r.Header.Set("X-Expect-Response", "maybe")

		_, err := parseExpectResponse(r, map[string]any{})
		if err == nil {
			t.Fatalf("expected error for invalid header")
		}
	})

	t.Run("invalid body type", func(t *testing.T) {
		r := httptest.NewRequest("POST", "/v1/device/dev/op", nil)

		_, err := parseExpectResponse(r, map[string]any{"__expect_response": 1})
		if err == nil {
			t.Fatalf("expected error for invalid body type")
		}
	})

	t.Run("invalid body text", func(t *testing.T) {
		r := httptest.NewRequest("POST", "/v1/device/dev/op", nil)

		_, err := parseExpectResponse(r, map[string]any{"__expect_response": "later"})
		if err == nil {
			t.Fatalf("expected error for invalid body text")
		}
	})
}
