package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-telegram/bot/models"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/attachments"
	"github.com/veryshyjelly/bifrost/internal/bots"
	"github.com/veryshyjelly/bifrost/internal/bridge"
	"github.com/veryshyjelly/bifrost/internal/changes"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/conversation"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/history"
	"github.com/veryshyjelly/bifrost/internal/ingress"
	"github.com/veryshyjelly/bifrost/internal/mentions"
	"github.com/veryshyjelly/bifrost/internal/metadata"
	"github.com/veryshyjelly/bifrost/internal/operations"
	"github.com/veryshyjelly/bifrost/internal/polls"
	"github.com/veryshyjelly/bifrost/internal/postgres"
	"github.com/veryshyjelly/bifrost/internal/providers/openwa"
	"github.com/veryshyjelly/bifrost/internal/providers/telegram"
	"github.com/veryshyjelly/bifrost/internal/reactions"
	"github.com/veryshyjelly/bifrost/internal/relay"
	"github.com/veryshyjelly/bifrost/internal/revocations"
	"github.com/veryshyjelly/bifrost/internal/sourceactions"
	"github.com/veryshyjelly/bifrost/internal/statuses"
)

type runtimeServiceOptions struct {
	Account, Session, OpenWAURL, OpenWAKey, TelegramURL, CloudURL, MasterToken, Callback, AdminToken string
	MediaRoot, TelegramRoot, Cron, Timezone                                                          string
	Owner, Management, MaxBytes                                                                      int64
	CommandTTL, ControlTTL                                                                           int
	HistoryLimit                                                                                     int
	Key                                                                                              [32]byte
	Codec                                                                                            attachments.CodecOptions
	Transport                                                                                        http.RoundTripper
}
type runtimeServices struct {
	Handlers      map[string]delivery.Handler
	Bots          *bots.Service
	Metadata      *metadata.Coordinator
	Pool          *bots.Pool
	Configuration string
}

type runtimePool struct {
	*bots.Pool
	store         *postgres.Store
	configuration string
}

func (p runtimePool) Local(ctx context.Context, account, bot string) (*telegram.Client, error) {
	ready, err := p.store.RuntimeBotReady(ctx, account, bot, p.configuration, false)
	if err != nil {
		return nil, err
	}
	if !ready {
		return nil, delivery.ErrLocalRetry
	}
	return p.Pool.Local(ctx, account, bot)
}

// composeRuntimeServices constructs provider adapters and every workflow. It
// does not start a worker, migrate tokens, register hooks or contact providers.
// The explicit runtime lifecycle must establish readiness before dispatch.
func composeRuntimeServices(store *postgres.Store, c runtimeServiceOptions) (*runtimeServices, error) {
	if store == nil || c.Account == "" || c.Session == "" || c.Owner <= 0 || c.Management >= 0 || c.CommandTTL < 5 || c.ControlTTL < 5 {
		return nil, delivery.ErrInvalid
	}
	wa, err := openwa.New(c.OpenWAURL, c.OpenWAKey, c.Transport)
	if err != nil {
		return nil, err
	}
	basePool, err := bots.NewPool(store, c.Key, c.Account, c.TelegramURL, c.CloudURL, c.Transport)
	if err != nil {
		return nil, err
	}
	configuration := bots.Digest([]string{strings.TrimRight(c.Callback, "/"), ingress.TelegramSecret("master", c.AdminToken)})
	pool := runtimePool{Pool: basePool, store: store, configuration: configuration}
	master, err := telegram.New(c.TelegramURL, c.MasterToken, c.Transport)
	if err != nil {
		return nil, err
	}
	assets, err := attachments.NewAssets(c.MediaRoot)
	if err != nil {
		return nil, err
	}
	codec, err := attachments.NewCodec(assets, c.Codec)
	if err != nil {
		return nil, err
	}
	sources, err := attachments.NewSources(assets, wa, func(ctx context.Context, a, b string) (attachments.Telegram, error) { return pool.Local(ctx, a, b) }, c.Session, c.TelegramRoot, c.MaxBytes)
	if err != nil {
		return nil, err
	}
	routing, err := conversation.New(store, master, c.Management, c.Owner)
	if err != nil {
		return nil, err
	}
	mention, err := mentions.New(store, wa, master, c.Session)
	if err != nil {
		return nil, err
	}
	replies, err := control.New(store, func(ctx context.Context, a, b string) (control.Telegram, error) { return pool.Local(ctx, a, b) }, c.Management)
	if err != nil {
		return nil, err
	}
	relayOptions := relay.Options{SessionID: c.Session, OwnerID: c.Owner, ManagementChatID: c.Management, Mentions: mention}
	text, err := relay.New(store, routing, func(ctx context.Context, a, b string) (relay.Telegram, error) { return pool.Local(ctx, a, b) }, wa, relayOptions)
	if err != nil {
		return nil, err
	}
	native, err := relay.NewNative(store, routing, func(ctx context.Context, a, b string) (relay.NativeTelegram, error) { return pool.Local(ctx, a, b) }, assets, sources, codec, relay.NativeOptions{Session: c.Session, OwnerID: c.Owner, Management: c.Management, MaxBytes: c.MaxBytes, Mentions: mention, Identity: wa, ControlTTL: c.ControlTTL, TextResume: text})
	if err != nil {
		return nil, err
	}
	outbound, err := relay.NewOutbound(store, wa, assets, sources, codec, replies, relayOptions)
	if err != nil {
		return nil, err
	}
	historyCount := c.HistoryLimit
	if historyCount == 0 {
		historyCount = 20
	}
	hist, err := history.New(store, wa, native, replies, history.Settings{Session: c.Session, Timezone: c.Timezone, OwnerID: c.Owner, Management: c.Management, DefaultCount: historyCount, CommandTTL: c.CommandTTL, ControlTTL: c.ControlTTL})
	if err != nil {
		return nil, err
	}
	edits, err := changes.NewEdits(store, wa, func(ctx context.Context, a, b string) (changes.Telegram, error) { return pool.Local(ctx, a, b) }, mention, changes.Settings{Session: c.Session, OwnerID: c.Owner, Management: c.Management, NoticeTTL: c.CommandTTL})
	if err != nil {
		return nil, err
	}
	votes, err := polls.New(store, wa, replies, polls.Settings{Session: c.Session, OwnerID: c.Owner, NoticeTTL: c.CommandTTL})
	if err != nil {
		return nil, err
	}
	react, err := reactions.New(store, routing, func(ctx context.Context, a, b string) (reactions.Telegram, error) { return pool.Local(ctx, a, b) }, wa, reactions.Settings{Session: c.Session, OwnerID: c.Owner, NoticeTTL: c.CommandTTL})
	if err != nil {
		return nil, err
	}
	revoke, err := revocations.New(store, func(ctx context.Context, a, b string) (revocations.Telegram, error) { return pool.Local(ctx, a, b) }, replies, revocations.Settings{Session: c.Session, OwnerID: c.Owner, ControlTTL: c.ControlTTL, NoticeTTL: c.CommandTTL})
	if err != nil {
		return nil, err
	}
	actions, err := sourceactions.New(store, wa, replies, sourceactions.Settings{Session: c.Session, OwnerID: c.Owner, Management: c.Management, CommandTTL: c.CommandTTL, NoticeTTL: c.CommandTTL})
	if err != nil {
		return nil, err
	}
	botSettings := bots.Settings{Session: c.Session, Callback: c.Callback, OwnerID: c.Owner, Management: c.Management, CommandTTL: c.CommandTTL, ControlTTL: c.ControlTTL}
	managed, err := bots.New(store, func(ctx context.Context, a, b string) (bots.Telegram, error) { return basePool.Local(ctx, a, b) }, func(ctx context.Context, a, b string) (bots.Telegram, error) { return pool.Cloud(ctx, a, b) }, bots.Options{Settings: botSettings, AdminToken: c.AdminToken, Key: c.Key})
	if err != nil {
		return nil, err
	}
	provision, err := bots.NewProvisioner(store, replies, master, botSettings)
	if err != nil {
		return nil, err
	}
	commandTTL, controlTTL := time.Duration(c.CommandTTL)*time.Second, time.Duration(c.ControlTTL)*time.Second
	start, err := control.NewStarter(replies, control.StartOptions{OwnerID: c.Owner, CommandTTL: commandTTL, ControlTTL: controlTTL})
	if err != nil {
		return nil, err
	}
	syncer, err := control.NewSync(store, replies, control.SyncOptions{Session: c.Session, OwnerID: c.Owner, CommandTTL: commandTTL, ControlTTL: controlTTL})
	if err != nil {
		return nil, err
	}
	mirrors, err := control.NewMirrors(store, replies, master, control.MirrorOptions{OwnerID: c.Owner, CommandTTL: commandTTL, ControlTTL: controlTTL})
	if err != nil {
		return nil, err
	}
	groups, err := control.NewGroups(store, replies, master, control.GroupOptions{OwnerID: c.Owner, CommandTTL: commandTTL, ControlTTL: controlTTL})
	if err != nil {
		return nil, err
	}
	operator, err := control.NewOperator(store, replies, control.OperatorOptions{OwnerID: c.Owner, CommandTTL: commandTTL})
	if err != nil {
		return nil, err
	}
	picker, err := mentions.NewPicker(mention, replies, mentions.PickerOptions{OwnerID: c.Owner, CommandTTL: commandTTL, ControlTTL: controlTTL})
	if err != nil {
		return nil, err
	}
	cleaner, err := control.NewCleaner(store, replies, func(ctx context.Context, a, b string) (control.DeleteTelegram, error) { return pool.Local(ctx, a, b) }, control.CleanupOptions{OwnerID: c.Owner, CommandTTL: commandTTL})
	if err != nil {
		return nil, err
	}
	menus, err := control.NewMenus(store, func(ctx context.Context, a, b string) (control.MenuTelegram, error) { return pool.Local(ctx, a, b) }, c.Owner, c.Management)
	if err != nil {
		return nil, err
	}
	retirer, err := conversation.NewRetirer(store, master, c.Management)
	if err != nil {
		return nil, err
	}
	meta, err := metadata.New(store, wa, func(ctx context.Context, a, b string) (metadata.Telegram, error) { return pool.Local(ctx, a, b) }, c.Session, c.Management)
	if err != nil {
		return nil, err
	}
	pictures, err := metadata.NewPictures(store, wa, func(ctx context.Context, a, b string) (metadata.PictureTelegram, error) { return pool.Local(ctx, a, b) }, metadata.NewCDN(c.Transport), c.Session, c.Management, c.MaxBytes)
	if err != nil {
		return nil, err
	}
	syncSettings := control.SyncSettings{Session: c.Session, OwnerID: c.Owner, Management: c.Management, CommandTTL: c.CommandTTL, ControlTTL: c.ControlTTL}
	advice, err := metadata.NewInspector(store, master, syncSettings)
	if err != nil {
		return nil, err
	}
	coordinator, err := metadata.NewCoordinator(store, meta, pictures, advice, metadata.RunOptions{Session: c.Session, OwnerID: c.Owner, Management: c.Management, Cron: c.Cron, Timezone: c.Timezone, CommandTTL: c.CommandTTL, ControlTTL: c.ControlTTL})
	if err != nil {
		return nil, err
	}
	reports, err := metadata.NewReports(store, replies, syncSettings)
	if err != nil {
		return nil, err
	}
	statusSettings := statuses.Settings{Session: c.Session, OwnerID: c.Owner, Management: c.Management, CommandTTL: c.CommandTTL, ControlTTL: c.ControlTTL, NoticeTTL: c.CommandTTL}
	archive, err := statuses.New(store, wa, master, native, statusSettings)
	if err != nil {
		return nil, err
	}
	statusControls, err := statuses.NewControls(store, replies, func(ctx context.Context, a, b string) (statuses.ControlTelegram, error) { return pool.Local(ctx, a, b) }, archive, statusSettings)
	if err != nil {
		return nil, err
	}
	ops, err := operations.New(store, replies, func(ctx context.Context, a, b string) (operations.Telegram, error) { return pool.Local(ctx, a, b) }, operations.Settings{Session: c.Session, OwnerID: c.Owner, Management: c.Management, CommandTTL: c.CommandTTL, ControlTTL: c.ControlTTL, NoticeTTL: c.CommandTTL, SyncCron: c.Cron, SyncTimezone: c.Timezone, Configured: true})
	if err != nil {
		return nil, err
	}
	options := bridge.Options{AccountID: c.Account, SessionID: c.Session, OwnerID: c.Owner, Extensions: bridge.Extensions{
		PassiveTopics: statusControls.Handle, Incoming: native.Incoming, MentionPicker: picker.Handle, MentionRefusal: picker.Refusal,
		OwnerCommands: runtimeTelegramChain(managed.Handle, provision.Handle, votes.Callback, revoke.Callback, actions.Handle, ops.Handle, syncer.Handle, hist.Handle, groups.Handle, start.Handle, mirrors.Handle, operator.Handle),
		Telegram:      runtimeTelegramChain(edits.Telegram, react.Outgoing, cleaner.Handle, outbound.Handle),
		WhatsApp:      map[sdk.WebhookEvent]delivery.Handler{sdk.EventMessageEdited: edits.WhatsApp, sdk.EventMessageReaction: react.Incoming, sdk.EventMessageRevoked: revoke.Incoming, sdk.EventPollVote: votes.Observe, sdk.EventStatusReceived: archive.Receive, sdk.EventSessionStatus: ops.SessionEvent, sdk.EventSessionDisconnected: ops.SessionEvent, sdk.EventMessageFailed: ops.SessionEvent},
		Management:    map[string]delivery.Handler{"operator_action": runtimeDeliveryChain(actions.Manage, hist.Manage, provision.Manage, operator.Manage), "link_group": mirrors.Manage, "return_to_inbox": mirrors.Manage, "new_conversation": groups.Header, "sync_metadata": coordinator.Admit, "session_status": ops.Management, "delivery_problem": ops.Management},
		ControlJobs:   map[string]delivery.Handler{"control-menu": menus.Install, "control-retire-inbox": retirer.Retire, "control-notice": replies.Notice, "control-bot-created": provision.Created, "control-source-action": actions.Run, "control-poll-vote": votes.Vote, "control-revoke": revoke.Process},
	}}
	dispatch, err := bridge.New(store, text, wa, options)
	if err != nil {
		return nil, err
	}
	handlers := dispatch.Handlers()
	for _, extra := range []map[string]delivery.Handler{managed.Handlers(), cleaner.Handlers(), hist.Handlers(), coordinator.Handlers(), archive.Handlers(), statusControls.Handlers(), {"control-sync-report": reports.Send}} {
		for kind, h := range extra {
			if handlers[kind] != nil {
				return nil, errors.New("duplicate runtime job handler")
			}
			handlers[kind] = h
		}
	}
	return &runtimeServices{Handlers: handlers, Bots: managed, Metadata: coordinator, Pool: basePool, Configuration: configuration}, nil
}

func runtimeTelegramChain(handlers ...bridge.TelegramHandler) bridge.TelegramHandler {
	return func(ctx context.Context, j delivery.Job, e *delivery.Effects, b string, u *models.Update) error {
		for _, h := range handlers {
			if err := h(ctx, j, e, b, u); !errors.Is(err, delivery.ErrUnsupported) {
				return err
			}
		}
		return delivery.ErrUnsupported
	}
}
func runtimeDeliveryChain(handlers ...delivery.Handler) delivery.Handler {
	return func(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
		for _, h := range handlers {
			if err := h(ctx, j, e); !errors.Is(err, delivery.ErrUnsupported) {
				return err
			}
		}
		return delivery.ErrUnsupported
	}
}
