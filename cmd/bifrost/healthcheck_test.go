package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthcheckUsesConfiguredBindIP(t *testing.T) {
	for _, host := range []string{"", "127.0.0.1", "0.0.0.0", "::1", "::"} {
		t.Run(host, func(t *testing.T) {
			listenIP, network := "127.0.0.1", "tcp4"
			if host == "::1" || host == "::" {
				listenIP, network = "::1", "tcp6"
			}
			listener, err := net.Listen(network, net.JoinHostPort(listenIP, "0"))
			if err != nil {
				t.Skipf("loopback unavailable: %v", err)
			}
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/healthz" {
					t.Errorf("health request path = %s", r.URL.Path)
				}
				w.WriteHeader(http.StatusOK)
			}))
			server.Listener.Close()
			server.Listener = listener
			server.Start()
			t.Cleanup(server.Close)
			_, port, err := net.SplitHostPort(listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("BRIDGE_PORT", port)
			t.Setenv("BRIDGE_BIND_HOST", host)
			if err := run(context.Background(), []string{"healthcheck"}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
