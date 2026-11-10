package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"

	sdk "github.com/rmyndharis/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/providers/openwa"
)

// checkOpenWA is read-only unless the operator explicitly supplies text, chat and a stable key.
func checkOpenWA(ctx context.Context, args []string, output io.Writer) error {
	flags := flag.NewFlagSet("check-openwa", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	session := flags.String("session", os.Getenv("OPENWA_SESSION_ID"), "session override")
	chat := flags.String("chat", "", "explicit destination")
	text := flags.String("text", "", "explicit test message; omit for a read-only check")
	key := flags.String("key", "", "caller-owned stable send idempotency key")
	quote := flags.String("quote", "", "source message ID to quote")
	find := flags.String("find-text", "", "find only this exact text in stored messages, without fetching history or marking read")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return errors.New("invalid check-openwa arguments")
	}
	if *text != "" && *find != "" {
		return errors.New("send and stored-message verification require separate invocations")
	}
	if *text != "" && (*chat == "" || *key == "") {
		return errors.New("a live send requires --text, --chat and --key")
	}
	if *text == "" && (*key != "" || *quote != "") {
		return errors.New("a send key or quote requires explicit text")
	}
	if *find != "" && *chat == "" {
		return errors.New("finding a stored message requires --chat")
	}
	client, err := openwa.New(os.Getenv("OPENWA_URL"), os.Getenv("OPENWA_API_KEY"), nil)
	if err != nil {
		return err
	}
	state, err := client.Session(ctx, *session)
	if err != nil {
		return err
	}
	if state.Restriction != nil && state.Restriction.Kind == "passkey_required" {
		return errors.New("OpenWA requires a WhatsApp passkey to finish pairing")
	}
	if state.Status != "ready" {
		return errors.New("OpenWA session is not ready")
	}
	result := map[string]any{"ready": true}
	if *text != "" {
		receipt, err := client.SendText(ctx, *session, sdk.SendTextRequest{ChatID: *chat, Text: *text, QuotedMessageID: *quote}, *key)
		if err != nil {
			return err
		}
		result["sent"] = true
		result["message_id"] = receipt.MessageID
		result["timestamp"] = receipt.Timestamp
	}
	if *find != "" {
		limit := 100
		inline := false
		messages, err := client.StoredMessages(ctx, *session, &sdk.ListMessagesQuery{ChatID: chat, Limit: &limit, InlineMedia: &inline})
		if err != nil {
			return err
		}
		ids := []string{}
		for _, m := range messages.Messages {
			if m.Body != nil && *m.Body == *find {
				if m.WaMessageID != nil {
					ids = append(ids, *m.WaMessageID)
				} else {
					ids = append(ids, m.ID)
				}
			}
		}
		result["matching_message_ids"] = ids
	}
	return json.NewEncoder(output).Encode(result)
}
