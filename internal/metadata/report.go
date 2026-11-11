package metadata

import (
	"context"
	"fmt"
	"net/url"

	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/presentation"
)

// ReportRequest is a sealed completion input for the future run coordinator.
// Target jobs must belong to RunID and be terminal with resolved effects.
// Destinations come from the original account-scoped sync request jobs.
type ReportRequest struct {
	RunID, Session, Source  string
	TargetJobs, RequestJobs []int64
	Failures                []TargetFailure
}
type TargetFailure struct {
	JobID   int64
	Warning string
}
type Report struct {
	RunID, Source                                                            string
	FinishedAt                                                               int64
	Targets, Groups, Contacts, Changed, Warnings, MissingBots, SkippedPhotos int
	Quiet                                                                    bool
}
type ReportInput struct {
	Report        Report
	Warnings      []string
	Missing       []BotAdvice
	Permissions   []PermissionAdvice
	Destinations  []control.Target
	RosterChanged bool
}
type PermissionAdvice struct{ Name, URL string }

func ReportPackets(in ReportInput) ([]control.Packet, int, error) {
	text := fmt.Sprintf("WhatsApp metadata sync complete: %d groups, %d contacts, %d fields updated.\n%d warnings; %d contact bots need adding; %d pictures unavailable or hidden.", in.Report.Groups, in.Report.Contacts, in.Report.Changed, len(in.Warnings), len(in.Missing), in.Report.SkippedPhotos)
	unique := map[string]bool{}
	shown := 0
	for _, warning := range in.Warnings {
		if !unique[warning] && shown < 4 {
			text += "\n" + warning
			unique[warning] = true
			shown++
		}
	}
	rows := [][]models.InlineKeyboardButton{}
	for _, missing := range in.Missing[:min(8, len(in.Missing))] {
		rows = append(rows, []models.InlineKeyboardButton{{Text: "Add " + Clip(missing.Name, 40), URL: "https://t.me/" + url.PathEscape(missing.Username) + "?startgroup=bridge"}})
	}
	for _, permission := range in.Permissions[:min(3, len(in.Permissions))] {
		rows = append(rows, []models.InlineKeyboardButton{{Text: "Allow info edits · " + Clip(permission.Name, 30), URL: permission.URL}})
	}
	parts, err := presentation.Split(presentation.Plain(text), 3800)
	if err != nil {
		return nil, 0, err
	}
	packets := []control.Packet{}
	uniqueDest := map[control.Target]bool{}
	for _, target := range in.Destinations {
		if uniqueDest[target] {
			continue
		}
		uniqueDest[target] = true
		for _, part := range parts {
			destination := target
			packet := control.Packet{Text: part.Text, Target: &destination}
			if len(rows) > 0 {
				packet.Markup = &models.InlineKeyboardMarkup{InlineKeyboard: rows}
			}
			packets = append(packets, packet)
		}
	}
	return packets, len(rows), nil
}

type ReportStore interface {
	PrepareMetadataReport(context.Context, delivery.Job, control.SyncSettings) (control.Plan, error)
}
type Reports struct {
	store    ReportStore
	replies  *control.Service
	settings control.SyncSettings
}

func NewReports(store ReportStore, replies *control.Service, c control.SyncSettings) (*Reports, error) {
	if c.CommandTTL == 0 {
		c.CommandTTL = 60
	}
	if c.ControlTTL == 0 {
		c.ControlTTL = 600
	}
	if store == nil || replies == nil || c.Session == "" || c.OwnerID <= 0 || c.Management >= 0 || c.CommandTTL < 5 || c.CommandTTL > 86400 || c.ControlTTL < 5 || c.ControlTTL > 86400 {
		return nil, delivery.ErrInvalid
	}
	return &Reports{store: store, replies: replies, settings: c}, nil
}
func (s *Reports) Send(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
	p, err := s.store.PrepareMetadataReport(ctx, j, s.settings)
	if err != nil {
		return local(err)
	}
	return s.replies.Send(ctx, j, e, p)
}
