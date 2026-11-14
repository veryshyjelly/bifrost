package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/history"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
	"github.com/veryshyjelly/bifrost/internal/retained"
)

func loadHistory(ctx context.Context, q *db.Queries, row db.Job, c history.Settings) (history.Run, error) {
	id, _, err := history.Source(jobFrom(row))
	if err != nil {
		return history.Run{}, err
	}
	stored, err := q.HistoryRun(ctx, db.HistoryRunParams{AccountID: row.AccountID, RunID: id})
	if err != nil {
		return history.Run{}, err
	}
	var run history.Run
	if json.Unmarshal(stored.Payload, &run) != nil || run.ID != id || run.Session != c.Session || run.ChatID == "" || (run.Mode != "all" && run.Mode != "skip") {
		return run, delivery.ErrInvalid
	}
	run.State = stored.State
	return run, nil
}
func updateHistory(ctx context.Context, q *db.Queries, account string, run history.Run) error {
	raw, err := json.Marshal(run)
	if err != nil {
		return err
	}
	if err := q.UpdateHistoryRun(ctx, db.UpdateHistoryRunParams{AccountID: account, RunID: run.ID, State: run.State, Payload: raw}); err != nil {
		return err
	}
	n, err := q.RecordHistoryActivity(ctx, db.RecordHistoryActivityParams{AccountID: account, WhatsappChat: run.ChatID, RunID: run.ID, Status: run.State})
	if err != nil {
		return err
	}
	if n != 1 {
		return delivery.ErrLocalRetry
	}
	return nil
}
func (s *Store) HistoryFetch(ctx context.Context, j delivery.Job, c history.Settings) (history.Run, bool, error) {
	var result history.Run
	fetch := false
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if row.Kind != "history" || !validHistorySettings(c) {
			return delivery.ErrInvalid
		}
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		run, err := loadHistory(ctx, q, row, c)
		if err != nil {
			return err
		}
		result = run
		if run.State != "queued" && run.State != "fetching" {
			return nil
		}
		run.State = "fetching"
		if err := updateHistory(ctx, q, row.AccountID, run); err != nil {
			return err
		}
		result, fetch = run, true
		return nil
	})
	return result, fetch, err
}
func (s *Store) SaveHistoryBatch(ctx context.Context, j delivery.Job, batch []sdk.ChatHistoryMessage, c history.Settings) error {
	if batch == nil {
		return delivery.ErrInvalid
	}
	return s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if row.Kind != "history" {
			return delivery.ErrInvalid
		}
		if err := q.LockQueueTopology(ctx); err != nil {
			return err
		}
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		run, err := loadHistory(ctx, q, row, c)
		if err != nil {
			return err
		}
		if run.State == "delivering" || run.State == "complete" || run.State == "failed" {
			return nil
		}
		d, err := directory(ctx, q, row.AccountID)
		if err != nil {
			return err
		}
		unique := map[string]sdk.ChatHistoryMessage{}
		for _, m := range batch {
			if m.ID == "" || m.Type == sdk.MsgRevoked || m.IsStatusBroadcast || m.Kind == "status" || m.Kind == "channel" || m.Kind == "broadcast" {
				continue
			}
			if m.ChatID != "" && d.Canonical(m.ChatID) != d.Canonical(run.ChatID) {
				continue
			}
			m.ChatID = run.ChatID
			m.IsGroup = strings.HasSuffix(run.ChatID, "@g.us")
			unique[m.ID] = m
		}
		rows := make([]sdk.ChatHistoryMessage, 0, len(unique))
		for _, m := range unique {
			rows = append(rows, m)
		}
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].Timestamp != rows[j].Timestamp {
				return rows[i].Timestamp < rows[j].Timestamp
			}
			return rows[i].ID < rows[j].ID
		})
		if len(rows) > run.Count {
			rows = rows[len(rows)-run.Count:]
		}
		rows = history.Ordered(rows)
		for index, m := range rows {
			source, err := prepareSource(ctx, q, row.AccountID, m)
			if err != nil {
				return err
			}
			item := history.Item{Index: index, Message: m, Source: source, State: "pending"}
			job, err := metadataEnqueue(ctx, q, row.AccountID, fmt.Sprintf("history-item:%s:%d", run.ID, index), "history-delivery:"+run.ID, "history-message", map[string]any{"run": run.ID, "index": index, "session": c.Session})
			if err != nil {
				return err
			}
			raw, _ := json.Marshal(item)
			if err := q.CreateHistoryItem(ctx, db.CreateHistoryItemParams{AccountID: row.AccountID, RunID: run.ID, ItemIndex: int32(index), JobID: job, SourceID: m.ID, Payload: raw}); err != nil {
				return err
			}
		}
		if _, err := metadataEnqueue(ctx, q, row.AccountID, "history-finish:"+run.ID, "history-delivery:"+run.ID, "history-finish", map[string]string{"run": run.ID, "session": c.Session}); err != nil {
			return err
		}
		run.Available, run.State = len(rows), "delivering"
		return updateHistory(ctx, q, row.AccountID, run)
	})
}
func loadHistoryItem(ctx context.Context, q *db.Queries, row db.Job) (db.HistoryItem, history.Item, error) {
	stored, err := q.HistoryItem(ctx, db.HistoryItemParams{AccountID: row.AccountID, JobID: row.ID})
	if err != nil {
		return stored, history.Item{}, err
	}
	id, index, err := history.Source(jobFrom(row))
	if err != nil || id != stored.RunID || int(stored.ItemIndex) != index {
		return stored, history.Item{}, delivery.ErrInvalid
	}
	var item history.Item
	if json.Unmarshal(stored.Payload, &item) != nil || item.Message.ID != stored.SourceID || item.Index != index {
		return stored, item, delivery.ErrInvalid
	}
	item.State = stored.State
	item.PlanKey = stored.PlanKey.String
	return stored, item, nil
}
func saveHistoryItem(ctx context.Context, q *db.Queries, row db.Job, item history.Item) error {
	raw, err := json.Marshal(item)
	if err != nil {
		return err
	}
	return q.UpdateHistoryItem(ctx, db.UpdateHistoryItemParams{AccountID: row.AccountID, JobID: row.ID, State: item.State, Payload: raw, PlanKey: pgtype.Text{String: item.PlanKey, Valid: item.PlanKey != ""}})
}
func (s *Store) PrepareHistoryItem(ctx context.Context, j delivery.Job, c history.Settings) (history.Run, history.Item, bool, error) {
	var run history.Run
	var item history.Item
	send := false
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if row.Kind != "history-message" {
			return delivery.ErrInvalid
		}
		if err := q.LockQueueTopology(ctx); err != nil {
			return err
		}
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		var err error
		run, err = loadHistory(ctx, q, row, c)
		if err != nil {
			return err
		}
		if run.State == "failed" || run.State == "complete" {
			return nil
		}
		if run.State != "delivering" {
			return delivery.ErrIncomplete
		}
		_, item, err = loadHistoryItem(ctx, q, row)
		if err != nil {
			return err
		}
		if item.State != "pending" {
			return nil
		}
		revoked, err := q.HistoryMessageRevoked(ctx, db.HistoryMessageRevokedParams{AccountID: row.AccountID, WhatsappChat: run.ChatID, MessageID: item.Message.ID})
		if err != nil {
			return err
		}
		delivered := false
		if run.Mode == "skip" {
			delivered, err = q.HistoryDestinationDelivered(ctx, db.HistoryDestinationDeliveredParams{AccountID: row.AccountID, WhatsappChat: run.ChatID, MessageID: item.Message.ID, ChatID: run.Target.ChatID, ThreadID: run.Target.ThreadID, Scope: relayScope(run.Target.BotID, run.Target.ChatID), RetainedDestinationKey: retained.HistoryDestinationKey(row.AccountID, run.ChatID, item.Message.ID, run.Target.BotID, run.Target.ChatID, run.Target.ThreadID)})
			if err != nil {
				return err
			}
		}
		if revoked || delivered {
			item.State = "skipped"
			return saveHistoryItem(ctx, q, row, item)
		}
		if run.Mode == "skip" && item.PlanKey == "" {
			if err := adoptHistoryPartial(ctx, q, row, run, &item); err != nil {
				return err
			}
		}
		send = true
		return nil
	})
	return run, item, send, err
}

// A skip import resumes a known partial live delivery rather than posting new
// copies. Queue admission and both claims are fenced before ownership changes;
// in-flight and unknown effects always require review and are never adopted.
func adoptHistoryPartial(ctx context.Context, q *db.Queries, row db.Job, run history.Run, item *history.Item) error {
	plans, err := q.HistoryPartialPlans(ctx, db.HistoryPartialPlansParams{AccountID: row.AccountID, WhatsappChat: run.ChatID, MessageID: item.Message.ID, ChatID: run.Target.ChatID, ThreadID: run.Target.ThreadID})
	if err != nil {
		return err
	}
	if len(plans) == 0 {
		return nil
	}
	if len(plans) > 1 {
		return delivery.ErrUncertain
	}
	plan := plans[0]
	var shape struct{ BotID, Format string }
	if json.Unmarshal(plan.Payload, &shape) != nil {
		return delivery.ErrInvalid
	}
	if run.Target.ChatID > 0 && shape.BotID != run.Target.BotID {
		return nil
	}
	previous, err := q.LockHistoryPartialJob(ctx, plan.JobID)
	if err != nil {
		return err
	}
	if previous.AccountID != row.AccountID || previous.Kind != "wa" || previous.ID == row.ID {
		return delivery.ErrUncertain
	}
	if previous.State == "running" {
		return delivery.ErrLocalRetry
	}
	if previous.State != "pending" && previous.State != "failed" {
		return delivery.ErrUncertain
	}
	unknown, err := q.HasUncertainEffect(ctx, previous.ID)
	if err != nil {
		return err
	}
	if unknown {
		return delivery.ErrUncertain
	}
	sending, err := q.HasSendingEffect(ctx, previous.ID)
	if err != nil {
		return err
	}
	if sending {
		return delivery.ErrUncertain
	}
	count, err := q.HistoryPartialPlanCount(ctx, previous.ID)
	if err != nil {
		return err
	}
	if count != 1 {
		return delivery.ErrUncertain
	}
	if shape.Format != "" && shape.Format != "native-packets-v1" {
		return delivery.ErrInvalid
	}
	if err := q.AdoptHistoryPartialEffects(ctx, db.AdoptHistoryPartialEffectsParams{AccountID: row.AccountID, NewJobID: row.ID, OldJobID: previous.ID}); err != nil {
		return err
	}
	if err := q.AdoptHistoryPartialPlan(ctx, db.AdoptHistoryPartialPlanParams{AccountID: row.AccountID, PlanKey: plan.PlanKey, JobID: row.ID}); err != nil {
		return err
	}
	if err := q.RetireHistoryPartialJob(ctx, previous.ID); err != nil {
		return err
	}
	item.PlanKey = plan.PlanKey
	item.AdoptedFormat = shape.Format
	if shape.Format == "" {
		item.AdoptedFormat = "text-v1"
	}
	return saveHistoryItem(ctx, q, row, *item)
}
func (s *Store) CompleteHistoryItem(ctx context.Context, j delivery.Job) error {
	return s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if row.Kind != "history-message" {
			return delivery.ErrInvalid
		}
		_, item, err := loadHistoryItem(ctx, q, row)
		if err != nil {
			return err
		}
		if item.State != "pending" {
			return nil
		}
		if item.PlanKey == "" {
			return delivery.ErrIncomplete
		}
		plan, err := q.HistoryDeliveryPlan(ctx, db.HistoryDeliveryPlanParams{AccountID: row.AccountID, PlanKey: item.PlanKey})
		if err != nil {
			return err
		}
		if plan.JobID != row.ID || !plan.Complete {
			return delivery.ErrIncomplete
		}
		unfinished, err := q.UnfinishedEffects(ctx, row.ID)
		if err != nil {
			return err
		}
		if unfinished != 0 {
			return delivery.ErrIncomplete
		}
		item.State = "done"
		return saveHistoryItem(ctx, q, row, item)
	})
}
func (s *Store) PrepareHistoryFinish(ctx context.Context, j delivery.Job, c history.Settings) (control.Plan, error) {
	var result control.Plan
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if row.Kind != "history-finish" {
			return delivery.ErrInvalid
		}
		if err := q.LockQueueTopology(ctx); err != nil {
			return err
		}
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		if p, found, err := existingGroupPlan(ctx, q, row); err != nil || found {
			result = p
			return err
		}
		run, err := loadHistory(ctx, q, row, c)
		if err != nil {
			return err
		}
		target := run.Target
		if target.BotID == "" {
			target.BotID = "master"
		}
		p := control.Plan{Key: row.Dedup, Target: target, ReplyTTLSeconds: c.CommandTTL}
		if run.State != "failed" {
			items, err := q.HistoryItems(ctx, db.HistoryItemsParams{AccountID: row.AccountID, RunID: run.ID})
			if err != nil {
				return err
			}
			if len(items) != run.Available {
				return delivery.ErrIncomplete
			}
			for _, item := range items {
				if item.State == "pending" || item.JobState != "done" {
					return delivery.ErrIncomplete
				}
			}
			mode := "Already delivered messages were skipped."
			if run.Mode == "all" {
				mode = "Previously delivered messages were included."
			}
			p.Packets = []control.Packet{{Text: fmt.Sprintf("History ready: %d available messages checked, oldest first. %s WhatsApp-deleted messages remain excluded.", run.Available, mode)}}
			run.State = "complete"
			if err := updateHistory(ctx, q, row.AccountID, run); err != nil {
				return err
			}
		}
		if err := saveGroupPlan(ctx, q, row, p); err != nil {
			return err
		}
		result = p
		return nil
	})
	return result, err
}

var _ history.Store = (*Store)(nil)

func historyJob(kind string) bool {
	return kind == "history" || kind == "history-message" || kind == "history-finish"
}
func failHistoryJob(ctx context.Context, q *db.Queries, row db.Job) error {
	if !historyJob(row.Kind) {
		return nil
	}
	if err := q.FailHistoryJob(ctx, row.ID); err != nil {
		return err
	}
	return q.SyncHistoryActivity(ctx)
}
func resumeReviewedHistory(ctx context.Context, q *db.Queries, row db.Job) error {
	if !historyJob(row.Kind) {
		return nil
	}
	if err := q.LockQueueTopology(ctx); err != nil {
		return err
	}
	if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
		return err
	}
	id, _, err := history.Source(jobFrom(row))
	if err != nil {
		return err
	}
	record, err := q.HistoryRun(ctx, db.HistoryRunParams{AccountID: row.AccountID, RunID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if record.State != "failed" {
		return nil
	}
	var run history.Run
	if json.Unmarshal(record.Payload, &run) != nil || run.ID != id {
		return delivery.ErrInvalid
	}
	active, err := q.ActiveHistoryRun(ctx, db.ActiveHistoryRunParams{AccountID: row.AccountID, SessionID: run.Session, WhatsappChat: run.ChatID})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err == nil {
		var other history.Run
		if json.Unmarshal(active, &other) != nil || other.ID != run.ID {
			return delivery.ErrInvalid
		}
	}
	run.State = "delivering"
	if row.Kind == "history" {
		run.State = "fetching"
	}
	return updateHistory(ctx, q, row.AccountID, run)
}
