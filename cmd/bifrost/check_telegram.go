package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/veryshyjelly/bifrost/internal/providers/telegram"
)

func checkTelegram(ctx context.Context, args []string, output io.Writer) error {
	if len(args) != 0 {
		return errors.New("check-telegram takes no arguments and performs no mutations")
	}
	client, err := telegram.New(os.Getenv("TELEGRAM_API_URL"), os.Getenv("TELEGRAM_MASTER_TOKEN"), nil)
	if err != nil {
		return err
	}
	if _, err := client.Me(ctx); err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(map[string]bool{"authenticated": true})
}
