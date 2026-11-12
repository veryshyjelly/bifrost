package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/metadata"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
)

func metadataReportTarget(ctx context.Context, q *db.Queries, account string, id int64) (db.Job, error) {
	row, err := q.MetadataReportJob(ctx, db.MetadataReportJobParams{AccountID: account, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return row, delivery.ErrInvalid
	}
	return row, err
}

func metadataReportInput(ctx context.Context, q *db.Queries, row db.Job, in metadata.ReportRequest, c control.SyncSettings) (metadata.ReportInput, error) {
	out := metadata.ReportInput{Report: metadata.Report{RunID: in.RunID, Source: in.Source, FinishedAt: time.Now().UnixMilli()}}
	if in.RunID == "" || in.Session != c.Session || !metadata.ValidRunSource(in.Source) {
		return out, delivery.ErrInvalid
	}
	failures := map[int64]string{}
	for _, f := range in.Failures {
		if f.JobID <= 0 || f.Warning == "" || failures[f.JobID] != "" {
			return out, delivery.ErrInvalid
		}
		failures[f.JobID] = f.Warning
	}
	seen := map[int64]bool{}
	for _, id := range in.TargetJobs {
		if id <= 0 || id == row.ID || seen[id] {
			return out, delivery.ErrInvalid
		}
		seen[id] = true
		target, err := metadataReportTarget(ctx, q, row.AccountID, id)
		if err != nil {
			return out, err
		}
		var source struct {
			Data struct {
				Run     string `json:"run"`
				Session string `json:"session"`
			} `json:"data"`
		}
		scope, _, err := metadataSource(target)
		if err != nil {
			return out, err
		}
		if json.Unmarshal(target.Payload, &source) != nil || source.Data.Run != in.RunID || source.Data.Session != c.Session {
			return out, delivery.ErrInvalid
		}
		if target.State != "done" && target.State != "failed" {
			return out, delivery.ErrLocalRetry
		}
		effects, err := q.MetadataReportEffects(ctx, db.MetadataReportEffectsParams{AccountID: row.AccountID, JobID: id})
		if err != nil {
			return out, err
		}
		for _, e := range effects {
			if e.State == "sending" || e.State == "uncertain" {
				return out, delivery.ErrLocalRetry
			}
			suffix, ok := strings.CutPrefix(e.EffectKey, target.Dedup+":metadata-text:")
			_, numberErr := strconv.ParseUint(suffix, 10, 32)
			if e.State == "done" && (ok && numberErr == nil || e.EffectKey == target.Dedup+":metadata-picture") {
				out.Report.Changed++
			}
		}
		out.Report.Targets++
		if scope == "group" {
			out.Report.Groups++
		} else {
			out.Report.Contacts++
		}
		if target.State == "failed" {
			warning := failures[id]
			if warning == "" {
				warning = "Metadata lookup failed"
			}
			out.Warnings = append(out.Warnings, warning)
			delete(failures, id)
			continue
		}
		runTarget, runErr := q.MetadataRunTarget(ctx, db.MetadataRunTargetParams{AccountID: row.AccountID, JobID: id})
		if runErr != nil && !errors.Is(runErr, pgx.ErrNoRows) {
			return out, runErr
		}
		partial := false
		if runErr == nil {
			if runTarget.RunID != in.RunID || runTarget.Outcome == "queued" {
				return out, delivery.ErrIncomplete
			}
			partial = runTarget.Outcome == "warning"
			if partial {
				var outcome metadata.Outcome
				if json.Unmarshal(runTarget.Details, &outcome) != nil || len(outcome.Warnings) == 0 {
					return out, delivery.ErrInvalid
				}
				out.Warnings = append(out.Warnings, outcome.Warnings...)
			}
		}
		if failures[id] != "" {
			return out, delivery.ErrInvalid
		}
		text, hasText, err := metadataPlan(ctx, q, target)
		if err != nil {
			return out, err
		}
		if !partial && (!hasText || !text.TextComplete) {
			return out, delivery.ErrIncomplete
		}
		if hasText && text.Target.Session != c.Session {
			return out, delivery.ErrInvalid
		}
		photo, hasPhoto, err := metadataPicturePlan(ctx, q, target)
		if err != nil {
			return out, err
		}
		if !partial && (!hasPhoto || !photo.Complete) {
			return out, delivery.ErrIncomplete
		}
		advice, hasAdvice, err := metadataInspection(ctx, q, target)
		if err != nil {
			return out, err
		}
		if !partial && !hasAdvice {
			return out, delivery.ErrIncomplete
		}
		if hasAdvice && (advice.Session != c.Session || advice.OwnerID != c.OwnerID) {
			return out, delivery.ErrInvalid
		}
		out.RosterChanged = out.RosterChanged || hasText && text.RosterChanged
		out.Missing = append(out.Missing, advice.Missing...)
		if advice.PermissionNeeded {
			out.Warnings = append(out.Warnings, metadata.ChangeInfoWarning)
		}
		if advice.PermissionLink != "" {
			out.Permissions = append(out.Permissions, metadata.PermissionAdvice{Name: text.Snapshot.Name, URL: advice.PermissionLink})
		}
		if hasPhoto && photo.Skip == "unavailable" {
			out.Report.SkippedPhotos++
		}
	}
	if len(failures) != 0 {
		return out, delivery.ErrInvalid
	}
	out.Report.Warnings = len(out.Warnings)
	out.Report.MissingBots = len(out.Missing)
	out.Report.Quiet = in.Source == "cron" && out.Report.Changed == 0 && len(out.Warnings) == 0 && !out.RosterChanged
	destinations := []control.Target{}
	for index, id := range in.RequestJobs {
		request, err := metadataReportTarget(ctx, q, row.AccountID, id)
		if err != nil {
			return out, err
		}
		if request.State != "done" {
			return out, delivery.ErrLocalRetry
		}
		var event struct {
			Type string `json:"type"`
			Data struct {
				Scope  string `json:"scope"`
				Notify *struct {
					Bot          string `json:"bot"`
					Chat, Thread int64
				} `json:"notify"`
			} `json:"data"`
		}
		if (request.Kind != "management" && request.Kind != "sync-request") || json.Unmarshal(request.Payload, &event) != nil || event.Type != "sync_metadata" || event.Data.Scope != "all" && event.Data.Scope != "group" && event.Data.Scope != "contact" {
			return out, delivery.ErrInvalid
		}
		if event.Data.Notify == nil {
			if in.Source != "cron" && index == 0 {
				destinations = append(destinations, control.Target{BotID: "master", ChatID: c.Management})
			}
			continue
		}
		notify := event.Data.Notify
		botID := notify.Bot
		if botID == "" {
			botID = "master"
		}
		if notify.Chat == 0 || notify.Thread < 0 || notify.Chat > 0 && (notify.Chat != c.OwnerID || notify.Thread != 0) {
			return out, delivery.ErrInvalid
		}
		if botID != "master" {
			bots, err := q.IdentityBots(ctx, row.AccountID)
			if err != nil {
				return out, err
			}
			known := false
			for _, bot := range bots {
				known = known || bot.BotID == botID
			}
			if !known {
				return out, delivery.ErrInvalid
			}
		}
		destinations = append(destinations, control.Target{BotID: botID, ChatID: notify.Chat, ThreadID: notify.Thread})
	}
	// Non-cron runs use their initial notifier, or management when absent. Cron
	// remains quiet unless work/warnings/roster changed; manual waiters still hear.
	if in.Source != "cron" && len(destinations) == 0 || in.Source == "cron" && !out.Report.Quiet {
		out.Destinations = append(out.Destinations, control.Target{BotID: "master", ChatID: c.Management})
	}
	out.Destinations = append(out.Destinations, destinations...)
	return out, nil
}

func (s *Store) PrepareMetadataReport(ctx context.Context, j delivery.Job, c control.SyncSettings) (control.Plan, error) {
	if !validSyncSettings(c) {
		return control.Plan{}, delivery.ErrInvalid
	}
	var result control.Plan
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if row.Kind != "control-sync-report" {
			return delivery.ErrInvalid
		}
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		if p, found, err := existingGroupPlan(ctx, q, row); err != nil || found {
			result = p
			return err
		}
		var in metadata.ReportRequest
		if json.Unmarshal(row.Payload, &in) != nil {
			return delivery.ErrInvalid
		}
		if row.Dedup != "sync-finish:"+in.RunID {
			return delivery.ErrInvalid
		}
		input, err := metadataReportInput(ctx, q, row, in, c)
		if err != nil {
			return err
		}
		packets, buttons, err := metadata.ReportPackets(input)
		if err != nil {
			return err
		}
		p := control.Plan{Key: row.Dedup, Target: control.Target{BotID: "master", ChatID: c.Management}, Packets: packets, ReplyTTLSeconds: c.CommandTTL}
		if buttons > 0 {
			p.ReplyTTLSeconds = c.ControlTTL
		}
		if len(packets) > 0 {
			p.Target = *packets[0].Target
		}
		raw, err := json.Marshal(input.Report)
		if err != nil {
			return err
		}
		if err := q.CreateMetadataReport(ctx, db.CreateMetadataReportParams{AccountID: row.AccountID, RunID: in.RunID, JobID: row.ID, Payload: raw}); err != nil {
			return err
		}
		if err := saveGroupPlan(ctx, q, row, p); err != nil {
			return err
		}
		result = p
		return nil
	})
	return result, err
}

var _ metadata.ReportStore = (*Store)(nil)
