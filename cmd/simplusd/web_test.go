package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestApplicationHandlerServesSPAAndKeepsAPISeparate(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<title>Simplus</title>"), 0600); err != nil {
		t.Fatal(err)
	}
	api := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(`{"ok":true}`)) })
	handler, err := applicationHandler(api, root)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/dashboard"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK || response.Body.String() != "<title>Simplus</title>" {
			t.Fatalf("%s status=%d body=%q", path, response.Code, response.Body.String())
		}
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/test", nil))
	if response.Body.String() != `{"ok":true}` {
		t.Fatalf("api body=%q", response.Body.String())
	}
}
