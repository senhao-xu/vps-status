package webui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerServesIndex(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()

	Handler().ServeHTTP(response, request)

	result := response.Result()
	t.Cleanup(func() { _ = result.Body.Close() })
	if result.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", result.StatusCode, http.StatusOK)
	}

	body, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if !strings.Contains(string(body), "VPS Status") {
		t.Fatalf("response does not contain the application title: %q", body)
	}
}

func TestHandlerFallsBackToIndex(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/future/client/route", nil)
	response := httptest.NewRecorder()

	Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
}
