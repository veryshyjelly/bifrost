package relay

import (
	"context"
	"encoding/json"
	"io"
	"strconv"

	"github.com/go-telegram/bot/models"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/attachments"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/conversation"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/identity"
	"github.com/veryshyjelly/bifrost/internal/presentation"
)

type NativeContact struct{ Name, Number, VCard string }
type NativePacket struct {
	Kind          string
	Target        control.Target
	Body          presentation.Value
	ReplyTo       int64
	Asset         *attachments.Asset
	Location      *sdk.MessageLocation
	Contact       *NativeContact
	Markup        *models.InlineKeyboardMarkup
	NoMapping     bool
	Fallback      *NativePacket
	FallbackLabel *NativePacket
	UseFallback   bool
}
type PollChoice struct{ ID, Value string }
type PollAction struct {
	ID, Session, ChatID, MessageID, Choice string
	Target                                 control.Target
	OwnerID, ExpiresAt                     int64
	Multiple                               bool
}
type NativePlan struct {
	Key                                                              string `json:"key"`
	Direction                                                        string `json:"direction"`
	Format                                                           string
	WhatsAppChat, WhatsAppMessage, BotID                             string
	TelegramChat, ThreadID, OwnerID                                  int64
	Prefix, Origin, HistoryRun, HistoryMode, Day, ReturnKey, Session string
	Route                                                            conversation.Route
	Source                                                           identity.Source
	Message                                                          sdk.ChatHistoryMessage
	Presentation                                                     presentation.Options
	Packets                                                          []NativePacket
	SourceAsset                                                      *attachments.Asset
	PollChoices                                                      []PollChoice
	ControlTTL                                                       int
	Complete                                                         bool `json:"-"`
}

func (p NativePlan) PartKey(part int) string { return p.Prefix + ":packet:" + strconv.Itoa(part) }
func (p NativePlan) FrozenPacket(part int, fallback bool) (string, json.RawMessage, error) {
	if part < 0 || part >= len(p.Packets) {
		return "", nil, delivery.ErrInvalid
	}
	packet := p.Packets[part]
	key := p.PartKey(part)
	if fallback {
		if packet.Fallback == nil {
			return "", nil, delivery.ErrInvalid
		}
		packet = *packet.Fallback
		key += ":original"
	}
	packet.UseFallback = false
	packet.Fallback = nil
	packet.FallbackLabel = nil
	raw, err := json.Marshal(packet)
	return key, raw, err
}

type NativeTelegram interface {
	SendPacket(context.Context, NativePacket, io.Reader) (*models.Message, error)
}
type NativeClients func(context.Context, string, string) (NativeTelegram, error)
type NativeStore interface {
	SelfIdentity(context.Context, string) (string, bool, error)
	SaveSelfIdentity(context.Context, delivery.Job, string) error
	NormalizeIncoming(context.Context, delivery.Job) (identity.Source, error)
	ConversationRoute(context.Context, string, string) (conversation.Route, bool, error)
	RelayEcho(context.Context, string, string, string) (Echo, error)
	WhatsAppQuote(context.Context, string, string, string, string, int64, int64) (Mapping, bool, error)
	NativePlan(context.Context, string, string) (NativePlan, bool, error)
	PrepareNative(context.Context, delivery.Job, NativePlan) (NativePlan, error)
	RecordNative(context.Context, delivery.Job, string, int, string) error
	CompleteNative(context.Context, delivery.Job, string) error
	RefuseNativePacket(context.Context, delivery.Job, string, int) error
	ArchivedAsset(context.Context, string, string, string) (attachments.Asset, bool, error)
	ArchivedMedia(context.Context, string, string, string) (string, *models.Message, bool, error)
	HistoryQuote(context.Context, string, string, string, string, int64, int64) (Mapping, bool, error)
}
type NativeOptions struct {
	Session             string
	OwnerID, Management int64
	MaxBytes            int64
	Mentions            Mentions
	Identity            NativeIdentity
	ControlTTL          int
	TextResume          interface {
		ResumeIncoming(context.Context, delivery.Job, *delivery.Effects, string) error
	}
}
type NativeIdentity interface {
	Session(context.Context, string) (*sdk.SessionResponse, error)
	Group(context.Context, string, string) (*sdk.GroupInfo, error)
}
type Native struct {
	store   NativeStore
	routing Routing
	clients NativeClients
	assets  *attachments.Assets
	sources *attachments.Sources
	codec   *attachments.Codec
	c       NativeOptions
}

func NewNative(store NativeStore, routing Routing, clients NativeClients, assets *attachments.Assets, sources *attachments.Sources, codec *attachments.Codec, c NativeOptions) (*Native, error) {
	if c.ControlTTL == 0 {
		c.ControlTTL = 600
	}
	if store == nil || routing == nil || clients == nil || assets == nil || sources == nil || codec == nil || c.Identity == nil || c.Session == "" || c.OwnerID <= 0 || c.Management >= 0 || c.MaxBytes < 1024 || c.ControlTTL < 5 || c.ControlTTL > 86400 {
		return nil, delivery.ErrInvalid
	}
	return &Native{store: store, routing: routing, clients: clients, assets: assets, sources: sources, codec: codec, c: c}, nil
}
