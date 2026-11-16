// Package changes applies mapped edits and source changes without creating new
// delivery copies or weakening the original effect journal.
package changes

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/mentions"
	"github.com/veryshyjelly/bifrost/internal/presentation"
)

type Settings struct {
	Session             string
	OwnerID, Management int64
	NoticeTTL           int
}

// Destination identifies an existing delivery copy and its frozen text-versus-caption presentation.
type Destination struct {
	Seq       int64
	Target    control.Target
	MessageID int64
	Caption   bool
	Format    presentation.Options
}
type Input struct {
	Event, Session, ChatID, MessageID, Body string
	MentionedIDs                            []string
	TelegramBot                             string
	TelegramMessage                         *models.Message
	Destinations                            []Destination
}
type Step struct {
	Kind        string
	Destination Destination
	Body        presentation.Value
	Edit        *sdk.EditMessageRequest
	Refusal     *control.Notice
	State       string
}

// Plan freezes mapped edit steps and their original input so retries do not create new copies.
type Plan struct {
	Key      string
	Input    Input
	Steps    []Step
	Complete bool `json:"-"`
}

func (p Plan) StepKey(i int) string { return p.Key + ":change:" + strconv.Itoa(i) }

// Request serializes a step without mutable progress or refusal fields for effect-journal comparison.
func (p Plan) Request(i int) (string, json.RawMessage, error) {
	if i < 0 || i >= len(p.Steps) {
		return "", nil, delivery.ErrInvalid
	}
	step := p.Steps[i]
	step.State = ""
	step.Refusal = nil
	raw, err := json.Marshal(step)
	return p.StepKey(i), raw, err
}

// Store separates change admission, frozen plans, per-step receipts and terminal completion.
type Store interface {
	NormalizeChange(context.Context, delivery.Job, string) error
	ChangePlan(context.Context, string, string) (Plan, bool, error)
	EditInput(context.Context, delivery.Job, Settings) (Input, error)
	PrepareChange(context.Context, delivery.Job, Plan, Settings) (Plan, error)
	RecordChange(context.Context, delivery.Job, string, int) error
	RefuseChange(context.Context, delivery.Job, string, int, int, string) error
	CompleteChange(context.Context, delivery.Job, string) error
}
type WhatsApp interface {
	Edit(context.Context, string, sdk.EditMessageRequest) (*sdk.MessageResponse, error)
}
type Telegram interface {
	EditText(context.Context, *bot.EditMessageTextParams) error
	EditCaption(context.Context, *bot.EditMessageCaptionParams) error
}
type Clients func(context.Context, string, string) (Telegram, error)
type Mentions interface {
	Incoming(context.Context, delivery.Job, string, string, []string, int64) (presentation.Value, error)
	Outgoing(context.Context, delivery.Job, string, string, []models.MessageEntity) (mentions.Outgoing, error)
}

func local(err error) error {
	if err == nil || errors.Is(err, delivery.ErrInvalid) || errors.Is(err, delivery.ErrClaimLost) || errors.Is(err, delivery.ErrUncertain) || errors.Is(err, delivery.ErrUnsupported) || errors.Is(err, delivery.ErrDeferred) {
		return err
	}
	return delivery.ErrLocalRetry
}
