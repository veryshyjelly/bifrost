package main

import (
	"context"
	"io"
	"testing"
)

func TestLiveCheckRequiresExplicitSendDestinationAndStableKey(t *testing.T) {
	for _, args := range [][]string{{"--text", "test"}, {"--text", "test", "--chat", "fixture@g.us"}, {"--key", "key"}, {"--find-text", "test"}, {"--quote", "id"}} {
		if err := checkOpenWA(context.Background(), args, io.Discard); err == nil {
			t.Fatal("unsafe live check arguments accepted")
		}
	}
}
