package ingress

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"time"

	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
)

// MaxBody bounds every admitted HTTP body before parsing or durable storage.
const MaxBody = 2 << 20

var botPath = regexp.MustCompile(`^/webhooks/telegram/([a-zA-Z0-9_-]+)$`)

// Service authenticates and admits callbacks through Store. It implements
// http.Handler but does not bind a listener or start delivery itself.
type Service struct {
	c Options
	s Store
}

// New validates account and authentication settings without accepting traffic.
func New(c Options, store Store) (*Service, error) {
	if store == nil || c.AccountID == "" || c.SessionID == "" || len(c.WebhookSecret) < 32 || len(c.AdminToken) < 32 || c.OwnerID <= 0 || c.ManagementChatID >= 0 || c.MasterBotID <= 0 {
		return nil, errors.New("invalid ingress configuration")
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	return &Service{c: c, s: store}, nil
}

// TelegramSecret is compatible with the existing per-bot callback secret.
func TelegramSecret(bot, admin string) string {
	mac := hmac.New(sha256.New, []byte(admin))
	mac.Write([]byte(bot))
	return hex.EncodeToString(mac.Sum(nil))
}

func reply(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

type refusal struct {
	status  int
	message string
}

func (r *refusal) Error() string { return r.message }
func invalid() error             { return &refusal{400, "Invalid event"} }

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	bot := botPath.FindStringSubmatch(r.URL.Path)
	if r.Method != http.MethodPost || (bot == nil && r.URL.Path != "/webhooks/openwa" && r.URL.Path != "/v1/management") {
		reply(w, 404, map[string]string{"error": "Not found"})
		return
	}
	if bot != nil && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Telegram-Bot-Api-Secret-Token")), []byte(TelegramSecret(bot[1], s.c.AdminToken))) != 1 {
		reply(w, 401, map[string]string{"error": "Invalid Telegram signature"})
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBody))
	if err != nil {
		status := 400
		var size *http.MaxBytesError
		if errors.As(err, &size) {
			status = 413
		}
		reply(w, status, map[string]string{"error": "Invalid webhook body"})
		return
	}
	if r.URL.Path == "/webhooks/openwa" && !sdk.VerifyWebhookSignature(raw, r.Header.Get("X-OpenWA-Signature"), s.c.WebhookSecret) {
		reply(w, 401, map[string]string{"error": "Invalid OpenWA signature"})
		return
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		reply(w, 400, map[string]string{"error": "Expected one JSON object"})
		return
	}
	var accepted bool
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	switch {
	case bot != nil:
		accepted, err = s.telegram(ctx, bot[1], raw, object)
	case r.URL.Path == "/webhooks/openwa":
		accepted, err = s.openwa(ctx, raw, object)
	default:
		accepted, err = s.management(ctx, raw)
	}
	if err != nil {
		var denied *refusal
		if errors.As(err, &denied) {
			reply(w, denied.status, map[string]string{"error": denied.message})
		} else {
			// Never expose database, SDK or payload descriptions.
			reply(w, 503, map[string]string{"error": "Event was not durably accepted"})
		}
		return
	}
	reply(w, 200, map[string]bool{"accepted": accepted})
}
