package agentapi

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestSendTreatsLostOrDamagedReplyAsUnknown(t *testing.T) {
	for _, reply := range []string{"", "{", `{"protocolVersion":999}`, `{"code":"SMS_BACKEND_INVALID"}`} {
		t.Run(reply, func(t *testing.T) {
			var dispatched atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				dispatched.Add(1)
				if reply == "" {
					c, _, _ := w.(http.Hijacker).Hijack()
					c.Close()
					return
				}
				if strings.Contains(reply, "SMS_BACKEND_INVALID") {
					w.WriteHeader(503)
				}
				w.Write([]byte(reply))
			}))
			defer server.Close()
			client := &Client{http: &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "tcp", server.Listener.Addr().String())
			}}}}
			_, err := client.SendSMS(t.Context(), SMSSendRequest{OperationID: "operation-synthetic-1", AgentInstanceID: "01234567-89ab-cdef-0123-456789abcdef", DeviceID: "usb-1-1", DeviceGeneration: 1, ExpectedEquipmentFingerprint: strings.Repeat("a", 64), ExpectedSubscriptionFingerprint: strings.Repeat("b", 64), Destination: "10086", Body: "synthetic"})
			if !errors.Is(err, ErrSMSOutcomeUnknown) || dispatched.Load() != 1 {
				t.Fatalf("dispatches=%d err=%v", dispatched.Load(), err)
			}
		})
	}
}
