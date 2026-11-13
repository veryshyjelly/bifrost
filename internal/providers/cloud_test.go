package providers

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestCloudTransportUsesFreshHTTP2ConnectionsAndPreservesRequest(t *testing.T) {
	var calls, connections atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.ProtoMajor != 2 || r.Method != http.MethodPost {
			t.Error("cloud request lost HTTP/2 or method")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil || string(body) != "fixture" {
			t.Error("cloud payload changed", err)
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"ok":false,"error_code":503}`))
	}))
	server.EnableHTTP2 = true
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	server.StartTLS()
	defer server.Close()
	client := HTTPClient(&cloudTransport{base: server.Client().Transport.(*http.Transport)})
	for range 2 {
		r, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+"/logout", bytes.NewReader([]byte("fixture")))
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if r.GetBody == nil {
			t.Fatal("transport changed caller's frozen request")
		}
	}
	if calls.Load() != 2 || connections.Load() != 2 {
		t.Fatal("cloud request retried or reused a connection", calls.Load(), connections.Load())
	}
}
