package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/bots"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/ingress"
	tg "github.com/veryshyjelly/bifrost/internal/providers/telegram"
	"github.com/veryshyjelly/bifrost/internal/secrets"
)

type botFixture struct {
	f                      *relayFixture
	service                *bots.Service
	pool                   *bots.Pool
	mu                     sync.Mutex
	calls                  []string
	token                  string
	dropLogout, refuseHook bool
	settings               bots.Settings
}

func newBotFixture(t *testing.T) *botFixture {
	t.Helper()
	b := &botFixture{f: newRelayFixture(t, nil, true), token: "200:fixture-token"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		defer b.mu.Unlock()
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		b.calls = append(b.calls, method)
		var parseErr error
		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
			parseErr = r.ParseMultipartForm(1 << 20)
		} else {
			parseErr = r.ParseForm()
		}
		if err := parseErr; err != nil {
			t.Error(err)
		}
		switch method {
		case "getManagedBotToken":
			fmt.Fprintf(w, `{"ok":true,"result":%q}`, b.token)
		case "getMe":
			id := strings.Split(strings.TrimPrefix(strings.Split(r.URL.Path, "/")[1], "bot"), ":")[0]
			fmt.Fprintf(w, `{"ok":true,"result":{"id":%s,"is_bot":true,"first_name":"Fixture","username":"bot_%s"}}`, id, id)
		case "logout":
			if b.dropLogout {
				conn, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
				} else {
					conn.Close()
				}
				return
			}
			fmt.Fprint(w, `{"ok":true,"result":true}`)
		case "setMyCommands", "setChatMenuButton":
			fmt.Fprint(w, `{"ok":true,"result":true}`)
		case "setWebhook":
			if r.FormValue("drop_pending_updates") == "true" || r.FormValue("secret_token") != ingress.TelegramSecret("200", strings.Repeat("a", 32)) {
				t.Error("unsafe webhook settings")
			}
			if b.refuseHook {
				fmt.Fprint(w, `{"ok":false,"error_code":429,"parameters":{"retry_after":1}}`)
				return
			}
			fmt.Fprint(w, `{"ok":true,"result":true}`)
		default:
			t.Error("unexpected bot operation", method)
			w.WriteHeader(500)
		}
	}))
	t.Cleanup(server.Close)
	var err error
	b.pool, err = bots.NewPool(b.f.store, [32]byte{}, "fixture-account", server.URL, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	master, err := tg.New(server.URL, "999:master", nil)
	if err != nil {
		t.Fatal(err)
	}
	clients := func(ctx context.Context, account, id string) (bots.Telegram, error) {
		if id == "master" {
			return master, nil
		}
		return b.pool.Local(ctx, account, id)
	}
	b.settings = bots.Settings{Session: "fixture-session", Callback: "http://bifrost:3210", OwnerID: 123, Management: -100, CommandTTL: 60, ControlTTL: 600}
	b.service, err = bots.New(b.f.store, clients, clients, bots.Options{Settings: b.settings, AdminToken: strings.Repeat("a", 32)})
	if err != nil {
		t.Fatal(err)
	}
	b.settings.Configuration = bots.Digest([]string{b.settings.Callback, ingress.TelegramSecret("master", strings.Repeat("a", 32))})
	return b
}
func (b *botFixture) register(t *testing.T, id int64) int64 {
	t.Helper()
	u := models.Update{ID: id, ManagedBot: &models.ManagedBotUpdated{User: &models.User{ID: 123}, Bot: &models.User{ID: 200, IsBot: true, Username: "bot_200"}}}
	if !admitPicker(t, b.f, "master", u) {
		t.Fatal("native registration not admitted")
	}
	key := "tg:master:" + strconv.FormatInt(id, 10)
	var job int64
	if err := b.f.store.pool.QueryRow(t.Context(), "SELECT id FROM jobs WHERE dedup=$1", key).Scan(&job); err != nil {
		t.Fatal(err)
	}
	if r := b.f.run(t, job, "tg", func(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
		return b.service.Handle(ctx, j, e, "master", &u)
	}); r.State != delivery.Done {
		t.Fatal(r)
	}
	var setup int64
	if err := b.f.store.pool.QueryRow(t.Context(), "SELECT id FROM jobs WHERE kind='control-bot-setup' ORDER BY id DESC LIMIT 1").Scan(&setup); err != nil {
		t.Fatal(err)
	}
	return setup
}
func (b *botFixture) count(method string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for _, c := range b.calls {
		if c == method {
			n++
		}
	}
	return n
}
func TestManagedBotRegistrationEncryptsAndCompletesExplicitNativeSetup(t *testing.T) {
	b := newBotFixture(t)
	setup := b.register(t, 900)
	c, found, err := b.f.store.Credential(t.Context(), "fixture-account", "200")
	if err != nil || !found {
		t.Fatal(found, err)
	}
	cipher, _ := secrets.New([32]byte{})
	plain, err := cipher.Open(c.Ciphertext)
	if err != nil || plain != b.token || strings.Contains(c.Ciphertext, b.token) {
		t.Fatal("credential encryption failed", err)
	}
	var migrated int
	if err := b.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM telegram_bot_lifecycle").Scan(&migrated); err != nil || migrated != 0 {
		t.Fatal("registration established migration eligibility", migrated, err)
	}
	if r := b.f.run(t, setup, "control-bot-setup", b.service.Setup); r.State != delivery.Done {
		t.Fatal(r)
	}
	if b.count("logout") != 1 || b.count("getMe") != 1 || b.count("setWebhook") != 1 {
		t.Fatal("native setup steps missing")
	}
	if err := b.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM telegram_bot_lifecycle").Scan(&migrated); err != nil || migrated != 1 {
		t.Fatal(migrated, err)
	}
	var raw []byte
	if err := b.f.store.pool.QueryRow(t.Context(), "SELECT payload FROM telegram_setup_plans").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var p bots.SetupPlan
	if json.Unmarshal(raw, &p) != nil || p.Step != 3 || strings.Contains(string(raw), b.token) {
		t.Fatal("invalid setup journal")
	}
	menus, err := control.NewMenus(b.f.store, func(ctx context.Context, account, id string) (control.MenuTelegram, error) {
		return b.pool.Local(ctx, account, id)
	}, b.settings.OwnerID, b.settings.Management)
	if err != nil {
		t.Fatal(err)
	}
	var menu int64
	if err := b.f.store.pool.QueryRow(t.Context(), "SELECT id FROM jobs WHERE kind='control-menu'").Scan(&menu); err != nil {
		t.Fatal(err)
	}
	if r := b.f.run(t, menu, "control-menu", menus.Install); r.State != delivery.Done {
		t.Fatal("native setup's menu did not install", r)
	}
	if b.count("setMyCommands") != 1 || b.count("setChatMenuButton") != 1 || b.count("logout") != 1 {
		t.Fatal("native setup menus missing or migration repeated")
	}
	var installed int64
	if err := b.f.store.pool.QueryRow(t.Context(), "SELECT menu_installed_at FROM telegram_bot_lifecycle WHERE bot_id='200'").Scan(&installed); err != nil || installed == 0 {
		t.Fatal("menu completion was not recorded", err)
	}
}
func TestManagedBotUnknownCloudLogoutBlocksLocalMigration(t *testing.T) {
	b := newBotFixture(t)
	b.dropLogout = true
	setup := b.register(t, 901)
	if r := b.f.run(t, setup, "control-bot-setup", b.service.Setup); r.State != delivery.Uncertain {
		t.Fatal(r)
	}
	if b.count("getMe") != 0 || b.count("setWebhook") != 0 {
		t.Fatal("unknown cloud logout advanced setup")
	}
	var migrated int
	if err := b.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM telegram_bot_lifecycle").Scan(&migrated); err != nil || migrated != 0 {
		t.Fatal(migrated, err)
	}
}
func TestManagedBotWebhookRetryNeverRepeatsConfirmedLogout(t *testing.T) {
	b := newBotFixture(t)
	b.refuseHook = true
	setup := b.register(t, 902)
	if r := b.f.run(t, setup, "control-bot-setup", b.service.Setup); r.State != delivery.Pending {
		t.Fatal(r)
	}
	b.mu.Lock()
	b.refuseHook = false
	b.mu.Unlock()
	b.f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE id=$1", setup)
	if r := b.f.run(t, setup, "control-bot-setup", b.service.Setup); r.State != delivery.Done {
		t.Fatal(r)
	}
	if b.count("logout") != 1 || b.count("getMe") != 1 || b.count("setWebhook") != 2 {
		t.Fatal("confirmed setup repeated")
	}
}
func TestManagedBotRegistrationRollbackLeavesNoCredentialOrSetup(t *testing.T) {
	b := newBotFixture(t)
	if _, err := b.f.store.pool.Exec(t.Context(), `CREATE FUNCTION refuse_credential_registration() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture registration'; END $$; CREATE TRIGGER refuse_registration BEFORE INSERT ON telegram_registrations FOR EACH ROW EXECUTE FUNCTION refuse_credential_registration()`); err != nil {
		t.Fatal(err)
	}
	u := models.Update{ID: 903, ManagedBot: &models.ManagedBotUpdated{User: &models.User{ID: 123}, Bot: &models.User{ID: 200, IsBot: true, Username: "bot_200"}}}
	admitPicker(t, b.f, "master", u)
	var id int64
	b.f.store.pool.QueryRow(t.Context(), "SELECT id FROM jobs WHERE dedup='tg:master:903'").Scan(&id)
	if r := b.f.run(t, id, "tg", func(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
		return b.service.Handle(ctx, j, e, "master", &u)
	}); r.State != delivery.Pending {
		t.Fatal(r)
	}
	for _, table := range []string{"telegram_credentials", "telegram_registrations"} {
		var count int
		if err := b.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatal(table, count, err)
		}
	}
}
