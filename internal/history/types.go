// Package history coordinates explicit destination-scoped history imports.
package history

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"

	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/conversation"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/identity"
)

type Settings struct {
	Session, Timezone                    string
	OwnerID, Management                  int64
	DefaultCount, CommandTTL, ControlTTL int
}
type Choice struct {
	ID, Session, ChatID, State, Mode, RunID, PlanKey, ReturnKey, BoundContact string
	Target                                                                    control.Target
	Route                                                                     conversation.Route
	Count                                                                     int
	OwnerID, ExpiresAt                                                        int64
}
type Run struct {
	ID, Session, ChatID, Mode, State, Timezone, ReturnKey, LastDay string
	Target                                                         control.Target
	Route                                                          conversation.Route
	Count, Available                                               int
}
type Item struct {
	Index          int
	Message        sdk.ChatHistoryMessage
	Source         identity.Source
	State, PlanKey string
	AdoptedFormat  string
}
type Store interface {
	PrepareHistoryControl(context.Context, delivery.Job, Settings) (control.Plan, error)
	PrepareHistoryPrompt(context.Context, delivery.Job, Settings) (control.Plan, error)
	HistoryFetch(context.Context, delivery.Job, Settings) (Run, bool, error)
	SaveHistoryBatch(context.Context, delivery.Job, []sdk.ChatHistoryMessage, Settings) error
	PrepareHistoryItem(context.Context, delivery.Job, Settings) (Run, Item, bool, error)
	CompleteHistoryItem(context.Context, delivery.Job) error
	PrepareHistoryFinish(context.Context, delivery.Job, Settings) (control.Plan, error)
}
type WhatsApp interface {
	History(context.Context, string, string, int) ([]sdk.ChatHistoryMessage, error)
}
type Delivery interface {
	DeliverHistory(context.Context, delivery.Job, *delivery.Effects, Run, Item) error
}

func Count(args []string, def int) (int, bool) {
	if len(args) == 0 {
		return def, true
	}
	if len(args) != 1 || args[0] == "" {
		return 0, false
	}
	for _, r := range args[0] {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(args[0])
	return n, err == nil && n >= 1 && n <= 2000
}
func local(err error) error {
	if err == nil || errors.Is(err, delivery.ErrClaimLost) || errors.Is(err, delivery.ErrInvalid) || errors.Is(err, delivery.ErrUnsupported) || errors.Is(err, delivery.ErrDeferred) || errors.Is(err, delivery.ErrUncertain) {
		return err
	}
	return delivery.ErrLocalRetry
}

// Ordered visits quote parents before children, including equal/inconsistent
// source timestamps. Cycle detection retains every available message once.
func Ordered(rows []sdk.ChatHistoryMessage) []sdk.ChatHistoryMessage {
	sorted := append([]sdk.ChatHistoryMessage(nil), rows...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Timestamp != sorted[j].Timestamp {
			return sorted[i].Timestamp < sorted[j].Timestamp
		}
		return sorted[i].ID < sorted[j].ID
	})
	byID := map[string]sdk.ChatHistoryMessage{}
	for _, m := range sorted {
		byID[m.ID] = m
	}
	visited, visiting := map[string]bool{}, map[string]bool{}
	out := make([]sdk.ChatHistoryMessage, 0, len(sorted))
	type frame struct {
		message  sdk.ChatHistoryMessage
		expanded bool
	}
	for _, m := range sorted {
		stack := []frame{{message: m}}
		for len(stack) > 0 {
			n := len(stack) - 1
			f := stack[n]
			stack = stack[:n]
			id := f.message.ID
			if visited[id] {
				continue
			}
			if f.expanded {
				delete(visiting, id)
				visited[id] = true
				out = append(out, f.message)
				continue
			}
			visiting[id] = true
			stack = append(stack, frame{message: f.message, expanded: true})
			if q := f.message.QuotedMessage; q != nil {
				if parent, ok := byID[q.ID]; ok && !visited[parent.ID] && !visiting[parent.ID] {
					stack = append(stack, frame{message: parent})
				}
			}
		}
	}
	return out
}
func Source(j delivery.Job) (string, int, error) {
	var v struct {
		Run   string
		Index int
	}
	if json.Unmarshal(j.Payload, &v) != nil || v.Run == "" || v.Index < 0 {
		return "", 0, delivery.ErrInvalid
	}
	return v.Run, v.Index, nil
}
func Callback(data string) bool {
	p := strings.Split(data, ":")
	return len(p) == 3 && p[0] == "history" && (p[1] == "skip" || p[1] == "all" || p[1] == "cancel") && p[2] != ""
}
