package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/veryshyjelly/bifrost/internal/recovery"
)

func reviewRequest(ctx context.Context, base, token, method, path string, body []byte) ([]byte, error) {
	u, err := url.Parse(base)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || len(token) < 32 {
		return nil, errors.New("set a valid review server URL and BRIDGE_ADMIN_TOKEN")
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(base, "/")+path, bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("cannot construct review request")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(req)
	if err != nil {
		return nil, errors.New("review server request failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("review request failed (HTTP %d)", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 || !json.Valid(raw) {
		return nil, errors.New("invalid review server response")
	}
	return raw, nil
}

// recoveryCLI validates the operator's decision before calling the bridge and
// checks returned scope before printing evidence. It does not write persistence
// directly or infer absence of delivery from a missing response.
func recoveryCLI(ctx context.Context, command string, args []string, out io.Writer) error {
	port := os.Getenv("BRIDGE_PORT")
	if port == "" {
		port = "3210"
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	base := flags.String("url", "http://127.0.0.1:"+port, "local bridge API URL")
	var job *int64
	var action *string
	var confirm *bool
	var file *string
	switch command {
	case "review":
		job = flags.Int64("job", 0, "job ID")
		action = flags.String("action", "", "retry or skip")
		confirm = flags.Bool("confirm-no-delivery", false, "explicit absence confirmation")
	case "inspect-topic":
		job = flags.Int64("job", 0, "job ID")
	case "reconcile-topic":
		file = flags.String("file", "", "reviewed topic decision JSON")
	default:
		return errors.New("unknown recovery command")
	}
	if err := flags.Parse(args); errors.Is(err, flag.ErrHelp) {
		usage := map[string]string{"review": "review --job ID --action retry|skip [--confirm-no-delivery]", "inspect-topic": "inspect-topic --job ID", "reconcile-topic": "reconcile-topic --file reviewed-decision.json"}
		_, err := fmt.Fprintln(out, "bifrost "+usage[command]+" [--url http://127.0.0.1:3210]")
		return err
	} else if err != nil || flags.NArg() != 0 {
		return errors.New("invalid recovery CLI arguments")
	}
	method, path := "POST", "/v1/review"
	var body []byte
	switch command {
	case "review":
		if *job <= 0 || (*action != "retry" && *action != "skip") {
			return errors.New("choose a positive --job and --action retry or skip")
		}
		body, _ = json.Marshal(struct {
			Job     int64  `json:"job"`
			Action  string `json:"action"`
			Confirm bool   `json:"confirmNoDelivery"`
		}{*job, *action, *confirm})
	case "inspect-topic":
		if *job <= 0 {
			return errors.New("choose a positive --job")
		}
		method, path = "GET", "/v1/topic-review?job="+strconv.FormatInt(*job, 10)
	case "reconcile-topic":
		if *file == "" {
			return errors.New("supply a reviewed topic decision with --file")
		}
		input, err := os.Open(*file)
		if err != nil {
			return errors.New("cannot read topic decision file")
		}
		raw, readErr := io.ReadAll(io.LimitReader(input, (16<<10)+1))
		input.Close()
		if readErr != nil || len(raw) > 16<<10 {
			return errors.New("topic decision file exceeds 16 KiB or cannot be read")
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		var d recovery.TopicDecision
		if decoder.Decode(&d) != nil || decoder.Decode(new(any)) != io.EOF || !d.Valid() {
			return errors.New("supply one complete explicitly confirmed topic decision")
		}
		if account := os.Getenv("BIFROST_ACCOUNT_ID"); account != "" && account != d.AccountID {
			return errors.New("topic decision account differs from BIFROST_ACCOUNT_ID")
		}
		body, _ = json.Marshal(d)
		path = "/v1/topic-review"
	}
	raw, err := reviewRequest(ctx, *base, os.Getenv("BRIDGE_ADMIN_TOKEN"), method, path, body)
	if err != nil {
		return err
	}
	var result any
	switch command {
	case "inspect-topic":
		var snapshot recovery.TopicSnapshot
		if json.Unmarshal(raw, &snapshot) != nil || snapshot.Job != *job || snapshot.AccountID == "" || snapshot.Effect == "" {
			return errors.New("invalid topic inspection response")
		}
		if account := os.Getenv("BIFROST_ACCOUNT_ID"); account != "" && account != snapshot.AccountID {
			return errors.New("review server account differs from BIFROST_ACCOUNT_ID")
		}
		result = snapshot
	case "reconcile-topic":
		var decision recovery.TopicDecision
		_ = json.Unmarshal(body, &decision)
		var applied recovery.TopicResult
		if json.Unmarshal(raw, &applied) != nil || applied.Job != decision.Job || applied.Effect != decision.Effect || applied.Action != decision.Action {
			return errors.New("invalid topic reconciliation response")
		}
		result = applied
	default:
		var accepted struct {
			Accepted bool `json:"accepted"`
		}
		if json.Unmarshal(raw, &accepted) != nil || !accepted.Accepted {
			return errors.New("review was not accepted")
		}
		result = accepted
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}
