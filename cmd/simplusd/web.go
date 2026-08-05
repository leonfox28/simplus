package main

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func applicationHandler(api http.Handler, webRoot string) (http.Handler, error) {
	if api == nil {
		return nil, errors.New("API handler is unavailable")
	}
	if webRoot == "" {
		return api, nil
	}
	if !filepath.IsAbs(webRoot) || filepath.Clean(webRoot) == string(filepath.Separator) {
		return nil, errors.New("SIMPLUS_WEB_ROOT must be a bounded absolute directory")
	}
	root := filepath.Clean(webRoot)
	index := filepath.Join(root, "index.html")
	info, err := os.Stat(index)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("SIMPLUS_WEB_ROOT has no regular index.html")
	}
	files := http.FileServer(http.Dir(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			api.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		clean := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
		if clean == "." {
			clean = "index.html"
		}
		if candidate, statErr := os.Stat(filepath.Join(root, clean)); statErr != nil || candidate.IsDir() {
			clone := r.Clone(r.Context())
			clone.URL.Path = "/"
			files.ServeHTTP(w, clone)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		files.ServeHTTP(w, r)
	}), nil
}
