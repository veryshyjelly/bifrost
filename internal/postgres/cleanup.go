package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
)

func saveStep(ctx context.Context, q *db.Queries, account string, id int64, step control.CleanupStep, create bool) error {
	data, err := json.Marshal(step)
	if err != nil {
		return err
	}
	if create {
		return q.CreateCleanupStep(ctx, db.CreateCleanupStepParams{AccountID: account, JobID: id, Payload: data})
	}
	return q.UpdateCleanupStep(ctx, db.UpdateCleanupStepParams{AccountID: account, JobID: id, Payload: data})
}
func loadStep(ctx context.Context, q *db.Queries, row db.Job) (control.CleanupStep, bool, error) {
	data, err := q.CleanupStep(ctx, db.CleanupStepParams{AccountID: row.AccountID, JobID: row.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return control.CleanupStep{}, false, nil
	}
	if err != nil {
		return control.CleanupStep{}, false, err
	}
	var step control.CleanupStep
	if json.Unmarshal(data, &step) != nil || step.Key != row.Dedup {
		return step, false, delivery.ErrInvalid
	}
	return step, true, nil
}
func loadClear(ctx context.Context, q *db.Queries, account, id string) (control.ClearRun, error) {
	data, err := q.ClearRun(ctx, db.ClearRunParams{AccountID: account, ClearID: id})
	if err != nil {
		return control.ClearRun{}, err
	}
	var run control.ClearRun
	if json.Unmarshal(data, &run) != nil || run.ID != id {
		return run, delivery.ErrInvalid
	}
	return run, nil
}
func updateClear(ctx context.Context, q *db.Queries, account string, run control.ClearRun) error {
	data, err := json.Marshal(run)
	if err != nil {
		return err
	}
	return q.UpdateClearRun(ctx, db.UpdateClearRunParams{AccountID: account, ClearID: run.ID, Payload: data})
}
func enqueueClear(ctx context.Context, q *db.Queries, account string, run control.ClearRun) error {
	key := "control-clear:" + run.ID + ":" + strconv.FormatInt(run.Cursor, 10) + ":" + strconv.FormatBool(run.Single)
	payload, _ := json.Marshal(struct {
		ID     string `json:"id"`
		Cursor int64  `json:"cursor"`
		Single bool   `json:"single"`
	}{run.ID, run.Cursor, run.Single})
	_, err := q.Enqueue(ctx, db.EnqueueParams{AccountID: account, Dedup: key, Stream: "control-clear:" + run.ID, Kind: "control-clear", Payload: payload})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	return err
}
func queueClear(ctx context.Context, q *db.Queries, account string, p control.Plan) error {
	r := p.Clear
	if r == nil {
		return nil
	}
	if r.ID == "" || r.OwnerID <= 0 || (r.Mode != "log" && r.Mode != "all") || (r.Mode == "all" && (p.Target.BotID != "master" || p.Target.ChatID != r.ManagementChatID)) {
		return delivery.ErrInvalid
	}
	cutoff, err := q.ClearCutoff(ctx, account)
	if err != nil {
		return err
	}
	run := control.ClearRun{ID: r.ID, Target: p.Target, Scope: relayScope(p.Target.BotID, p.Target.ChatID), Mode: r.Mode, Cutoff: cutoff, Status: "queued"}
	data, err := json.Marshal(run)
	if err != nil {
		return err
	}
	if err := q.CreateClearRun(ctx, db.CreateClearRunParams{AccountID: account, ClearID: run.ID, Payload: data}); err != nil {
		return err
	}
	if err := q.SnapshotClearItems(ctx, db.SnapshotClearItemsParams{AccountID: account, ClearID: run.ID, Scope: run.Scope, ChatID: p.Target.ChatID, ThreadID: p.Target.ThreadID, Cutoff: cutoff, Mode: run.Mode}); err != nil {
		return err
	}
	return enqueueClear(ctx, q, account, run)
}

func (s *Store) PrepareExpiry(ctx context.Context, job delivery.Job) (control.CleanupStep, error) {
	var result control.CleanupStep
	err := s.withClaim(ctx, job, func(q *db.Queries, row db.Job) error {
		if row.Kind != "control-expire" {
			return delivery.ErrInvalid
		}
		old, known, err := loadStep(ctx, q, row)
		if err != nil {
			return err
		}
		if known && old.Outcome != "pending" {
			result = old
			return nil
		}
		var p struct {
			Scope   string `json:"scope"`
			Chat    int64  `json:"chat"`
			Message int64  `json:"message"`
			Bot     string `json:"bot"`
		}
		if json.Unmarshal(row.Payload, &p) != nil || p.Chat == 0 || p.Message <= 0 || p.Scope == "" || relayScope(p.Bot, p.Chat) != p.Scope {
			return delivery.ErrInvalid
		}
		step := control.CleanupStep{Key: row.Dedup, Kind: "expire", Scope: p.Scope, Target: control.Target{BotID: p.Bot, ChatID: p.Chat}, Outcome: "noop"}
		if p.Chat < 0 {
			step.Target.BotID = "master"
		}
		o, err := q.CleanupObservation(ctx, db.CleanupObservationParams{AccountID: row.AccountID, Scope: p.Scope, ChatID: p.Chat, MessageID: p.Message})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err == nil && !o.Deleted && (o.Kind == "log" || o.Kind == "command") && o.ExpiresAt > 0 {
			if o.ExpiresAt > time.Now().UnixMilli() {
				return control.WaitUntil{Deadline: time.UnixMilli(o.ExpiresAt)}
			}
			relay, err := q.IsRelayObservation(ctx, db.IsRelayObservationParams{AccountID: row.AccountID, Scope: p.Scope, ChatID: p.Chat, MessageID: p.Message})
			if err != nil {
				return err
			}
			if !relay {
				step.Outcome = "pending"
				step.Target.ThreadID = o.ThreadID
				step.Items = []control.CleanupItem{{Seq: o.Seq, MessageID: o.MessageID}}
			}
		}
		result = step
		return saveStep(ctx, q, row.AccountID, row.ID, step, !known)
	})
	return result, err
}
func (s *Store) FinishExpiry(ctx context.Context, job delivery.Job, outcome string) error {
	if outcome != "removed" && outcome != "blocked" && outcome != "noop" {
		return delivery.ErrInvalid
	}
	return s.withClaim(ctx, job, func(q *db.Queries, row db.Job) error {
		step, known, err := loadStep(ctx, q, row)
		if err != nil {
			return err
		}
		if !known || step.Kind != "expire" {
			return delivery.ErrInvalid
		}
		if step.Outcome != "pending" {
			return nil
		}
		ids := []int64{}
		for _, i := range step.Items {
			ids = append(ids, i.Seq)
		}
		if outcome == "removed" {
			if err := q.MarkCleanupDeleted(ctx, db.MarkCleanupDeletedParams{AccountID: row.AccountID, Column2: ids}); err != nil {
				return err
			}
		}
		if outcome == "blocked" {
			if err := q.MarkCleanupBlocked(ctx, db.MarkCleanupBlockedParams{AccountID: row.AccountID, Column2: ids, CleanupError: "Telegram could not remove this notice"}); err != nil {
				return err
			}
		}
		step.Outcome = outcome
		return saveStep(ctx, q, row.AccountID, row.ID, step, false)
	})
}

func (s *Store) PrepareClearStep(ctx context.Context, job delivery.Job) (control.CleanupStep, error) {
	var result control.CleanupStep
	err := s.withClaim(ctx, job, func(q *db.Queries, row db.Job) error {
		if row.Kind != "control-clear" {
			return delivery.ErrInvalid
		}
		old, known, err := loadStep(ctx, q, row)
		if err != nil {
			return err
		}
		if known {
			result = old
			return nil
		}
		var p struct {
			ID     string `json:"id"`
			Cursor int64  `json:"cursor"`
			Single bool   `json:"single"`
		}
		if json.Unmarshal(row.Payload, &p) != nil || p.ID == "" {
			return delivery.ErrInvalid
		}
		run, err := loadClear(ctx, q, row.AccountID, p.ID)
		if err != nil {
			return err
		}
		result = control.CleanupStep{Key: row.Dedup, ClearID: run.ID, Run: run, Kind: "noop", Outcome: "complete"}
		if run.Status == "completed" || run.Status == "failed" || p.Cursor != run.Cursor || p.Single != run.Single {
			return nil
		}
		limit := int32(100)
		if run.Single {
			limit = 1
		}
		rows, err := q.ClearItemRows(ctx, db.ClearItemRowsParams{AccountID: row.AccountID, ClearID: run.ID, ObservationSeq: run.Cursor, Limit: limit})
		if err != nil {
			return err
		}
		step := control.CleanupStep{Key: row.Dedup, ClearID: run.ID, Run: run, Scope: run.Scope, Target: run.Target, Kind: "clear", Outcome: "pending"}
		if step.Target.ChatID < 0 {
			step.Target.BotID = "master"
		}
		if len(rows) == 0 {
			step.Kind = "summary"
			result = step
			return saveStep(ctx, q, row.AccountID, row.ID, step, true)
		}
		// Select fixed rows first. A new relay after this read cannot add its
		// message to the selection. Unknown own-bot logs must wait for receipts.
		for _, o := range rows {
			if run.Mode == "log" && o.Kind == "log" && o.ExpiresAt == 0 && !o.Deleted {
				blocked, err := q.UnresolvedRelayInTarget(ctx, db.UnresolvedRelayInTargetParams{AccountID: row.AccountID, Scope: run.Scope, ChatID: run.Target.ChatID, ThreadID: run.Target.ThreadID})
				if err != nil {
					return err
				}
				if blocked {
					return delivery.ErrLocalRetry
				}
				break
			}
		}
		for _, o := range rows {
			step.Cursor = o.Seq
			if o.Deleted {
				step.Gone = append(step.Gone, o.Seq)
				continue
			}
			if run.Mode == "log" {
				relay, err := q.IsRelayObservation(ctx, db.IsRelayObservationParams{AccountID: row.AccountID, Scope: run.Scope, ChatID: o.ChatID, MessageID: o.MessageID})
				if err != nil {
					return err
				}
				if relay || (o.Kind != "log" && o.Kind != "command") {
					step.Gone = append(step.Gone, o.Seq)
					continue
				}
			}
			if o.Kind == "protected" || o.MessageDate <= time.Now().Add(-48*time.Hour).UnixMilli() {
				step.Skipped = append(step.Skipped, o.Seq)
				continue
			}
			step.Items = append(step.Items, control.CleanupItem{Seq: o.Seq, MessageID: o.MessageID})
		}
		result = step
		return saveStep(ctx, q, row.AccountID, row.ID, step, true)
	})
	return result, err
}
func (s *Store) FinishClearStep(ctx context.Context, job delivery.Job, outcome string) error {
	if outcome != "removed" && outcome != "split" && outcome != "blocked" && outcome != "forbidden" {
		return delivery.ErrInvalid
	}
	return s.withClaim(ctx, job, func(q *db.Queries, row db.Job) error {
		step, known, err := loadStep(ctx, q, row)
		if err != nil {
			return err
		}
		if !known || step.Kind != "clear" {
			return delivery.ErrInvalid
		}
		if step.Outcome != "pending" {
			return nil
		}
		run, err := loadClear(ctx, q, row.AccountID, step.ClearID)
		if err != nil {
			return err
		}
		ids := []int64{}
		for _, i := range step.Items {
			ids = append(ids, i.Seq)
		}
		switch outcome {
		case "split":
			if run.Single {
				return delivery.ErrInvalid
			}
			run.Single = true
		case "forbidden":
			run.Status = "failed"
		default:
			if outcome == "removed" {
				if err := q.MarkCleanupDeleted(ctx, db.MarkCleanupDeletedParams{AccountID: row.AccountID, Column2: ids}); err != nil {
					return err
				}
				run.Removed += int64(len(ids))
			} else {
				run.Blocked += int64(len(ids))
				if err := q.MarkCleanupBlocked(ctx, db.MarkCleanupBlockedParams{AccountID: row.AccountID, Column2: ids, CleanupError: "Telegram could not remove this message"}); err != nil {
					return err
				}
			}
			run.Skipped += int64(len(step.Skipped))
			run.Cursor = step.Cursor
			run.Status = "running"
		}
		step.Outcome = outcome
		if err := saveStep(ctx, q, row.AccountID, row.ID, step, false); err != nil {
			return err
		}
		if err := updateClear(ctx, q, row.AccountID, run); err != nil {
			return err
		}
		if outcome == "forbidden" {
			return nil
		}
		return enqueueClear(ctx, q, row.AccountID, run)
	})
}
func (s *Store) FinishClearRun(ctx context.Context, job delivery.Job) error {
	return s.withClaim(ctx, job, func(q *db.Queries, row db.Job) error {
		step, known, err := loadStep(ctx, q, row)
		if err != nil {
			return err
		}
		if !known || step.Kind != "summary" {
			return delivery.ErrInvalid
		}
		p, err := q.ControlPlan(ctx, db.ControlPlanParams{AccountID: row.AccountID, PlanKey: row.Dedup})
		if err != nil {
			return err
		}
		if !p.Complete {
			return delivery.ErrIncomplete
		}
		run, err := loadClear(ctx, q, row.AccountID, step.ClearID)
		if err != nil {
			return err
		}
		run.Status = "completed"
		if err := updateClear(ctx, q, row.AccountID, run); err != nil {
			return err
		}
		step.Outcome = "complete"
		return saveStep(ctx, q, row.AccountID, row.ID, step, false)
	})
}

var _ control.CleanupStore = (*Store)(nil)
