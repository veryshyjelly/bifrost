// Package metadata refreshes provider metadata through fenced snapshots and
// frozen Telegram updates. It never writes to WhatsApp.
package metadata

import (
	"context"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/identity"
)

// Group retains the unavailable/empty description distinction lost by the
// SDK's GroupInfo.Description string field. All other fields use native models.
type Group struct {
	sdk.GroupInfo
	Description *string `json:"description"`
}

// Snapshot freezes source metadata and membership before destination updates are planned.
type Snapshot struct {
	ID, Scope, Name string
	Description     *string
	Members         []identity.Member
	FetchedAt       int64
}

type Route struct {
	ID, Name, ReturnKey string
	ChatID, ThreadID    int64
	Retired             bool
}

type Target struct {
	ID, Canonical, Name, BotID, Session string
	Management                          int64
	Route                               *Route
}

type Change struct {
	Kind, BotID, Text string
	ChatID, ThreadID  int64
}

// Plan covers text metadata only. TextComplete is not completion of a full
// metadata run: permission controls, orchestration and scheduling have
// their own remaining workflows before sync_metadata can be enabled.
type Plan struct {
	Key           string
	JobID         int64
	Target        Target
	Snapshot      Snapshot
	Changes       []Change
	Warning       string
	TextComplete  bool
	RosterChanged bool           `json:",omitempty"`
	TextRefusals  map[int]string `json:",omitempty"`
}

// Store freezes metadata text plans and records their per-change refusals and stage completion.
type Store interface {
	MetadataPlan(context.Context, delivery.Job) (Plan, bool, error)
	MetadataTarget(context.Context, delivery.Job, string, int64) (Target, error)
	PrepareMetadata(context.Context, delivery.Job, Target, Snapshot, []Change, string) (Plan, error)
	MetadataTextCurrent(context.Context, delivery.Job) (bool, error)
	CompleteMetadataText(context.Context, delivery.Job) error
	RefuseMetadataText(context.Context, delivery.Job, int, string) error
}

type WhatsApp interface {
	GroupMetadata(context.Context, string, string) (*Group, error)
	Contact(context.Context, string, string) (*sdk.ContactRecord, error)
}

type Telegram interface {
	Me(context.Context) (*models.User, error)
	Chat(context.Context, *bot.GetChatParams) (*models.ChatFullInfo, error)
	ChatMember(context.Context, *bot.GetChatMemberParams) (*models.ChatMember, error)
	MyDescription(context.Context) (*models.BotDescription, error)
	EditForumTopic(context.Context, *bot.EditForumTopicParams) error
	SetChatTitle(context.Context, *bot.SetChatTitleParams) error
	SetChatDescription(context.Context, *bot.SetChatDescriptionParams) error
	SetMyName(context.Context, *bot.SetMyNameParams) error
	SetMyDescription(context.Context, *bot.SetMyDescriptionParams) error
}

type Clients func(context.Context, string, string) (Telegram, error)
type Service struct {
	store      Store
	wa         WhatsApp
	clients    Clients
	session    string
	management int64
	now        func() time.Time
}

func New(store Store, wa WhatsApp, clients Clients, session string, management int64) (*Service, error) {
	if store == nil || wa == nil || clients == nil || session == "" || management >= 0 {
		return nil, delivery.ErrInvalid
	}
	return &Service{store: store, wa: wa, clients: clients, session: session, management: management, now: time.Now}, nil
}
