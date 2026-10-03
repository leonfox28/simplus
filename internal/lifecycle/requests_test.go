package lifecycle

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStopAdmissionDrainsActiveRequestsBeforeResourcesClose(t *testing.T) {
	var requests Requests
	entered, release := make(chan struct{}), make(chan struct{})
	handler := requests.Handler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { close(entered); <-release }))
	done := make(chan struct{})
	go func() { handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil)); close(done) }()
	<-entered
	requests.StopAdmission()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("admission=%d", response.Code)
	}
	waited := make(chan struct{})
	go func() { requests.Wait(); close(waited) }()
	select {
	case <-waited:
		t.Fatal("active handler escaped shutdown barrier")
	default:
	}
	close(release)
	<-done
	<-waited
}
