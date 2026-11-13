package config

import (
	"errors"
	"strconv"
	"time"

	"github.com/veryshyjelly/bifrost/internal/providers"
)

// Runtime is loaded only for explicit dispatch. Diagnostic serve retains its
// independent opt-in ingress configuration and requires no provider secrets.
type Runtime struct {
	Config
	OpenWAURL, OpenWAKey, TelegramURL, CloudURL, MasterToken, Callback string
	MediaRoot, TelegramRoot, Python, Chromium                          string
	MaxBytes                                                           int64
	HistoryLimit                                                       int
	DeliveryInterval                                                   time.Duration
	NoSandbox                                                          bool
}

func (Runtime) String() string   { return "runtime configuration (private)" }
func (Runtime) GoString() string { return "config.Runtime{private}" }

func LoadRuntime(getenv func(string) string) (Runtime, error) {
	base, err := Load(func(k string) string {
		if k == "BRIDGE_INGRESS_ENABLED" {
			return "true"
		}
		return getenv(k)
	})
	if err != nil {
		return Runtime{}, err
	}
	if base.AccountID == "" {
		return Runtime{}, errors.New("runtime requires the permanent BIFROST_ACCOUNT_ID")
	}
	value := func(key, fallback string) string {
		if v := getenv(key); v != "" {
			return v
		}
		return fallback
	}
	c := Runtime{Config: base, OpenWAURL: value("OPENWA_URL", "http://openwa:2785"), OpenWAKey: getenv("OPENWA_API_KEY"), TelegramURL: value("TELEGRAM_API_URL", "http://telegram-api:8081"), CloudURL: value("TELEGRAM_CLOUD_URL", "https://api.telegram.org"), MasterToken: getenv("TELEGRAM_MASTER_TOKEN"), Callback: value("BRIDGE_CALLBACK_URL", "http://bifrost:3210"), MediaRoot: value("BRIDGE_MEDIA", "./media"), TelegramRoot: value("TELEGRAM_DATA_ROOT", "/telegram-data"), Python: getenv("STICKER_PYTHON"), Chromium: getenv("STICKER_CHROMIUM"), MaxBytes: 100 << 20, HistoryLimit: 20, DeliveryInterval: 1100 * time.Millisecond}
	if c.OpenWAKey == "" {
		return Runtime{}, errors.New("runtime requires OPENWA_API_KEY")
	}
	for _, url := range []string{c.OpenWAURL, c.TelegramURL, c.CloudURL, c.Callback} {
		if !providers.ValidURL(url) {
			return Runtime{}, errors.New("runtime provider or callback URL is invalid")
		}
	}
	if raw := getenv("MAX_MEDIA_BYTES"); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 1024 || n > 1<<53-1 {
			return Runtime{}, errors.New("MAX_MEDIA_BYTES must be a positive safe byte budget")
		}
		c.MaxBytes = n
	}
	if raw := getenv("HISTORY_LIMIT"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 || n > 2000 {
			return Runtime{}, errors.New("HISTORY_LIMIT must be between 0 and 2000")
		}
		c.HistoryLimit = n
	}
	if raw := getenv("DELIVERY_INTERVAL_MS"); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 32)
		if err != nil || n < 100 {
			return Runtime{}, errors.New("DELIVERY_INTERVAL_MS must be at least 100")
		}
		c.DeliveryInterval = time.Duration(n) * time.Millisecond
	}
	if raw := getenv("STICKER_CHROMIUM_NO_SANDBOX"); raw != "" {
		c.NoSandbox, err = strconv.ParseBool(raw)
		if err != nil {
			return Runtime{}, errors.New("STICKER_CHROMIUM_NO_SANDBOX must be boolean")
		}
	}
	return c, nil
}
