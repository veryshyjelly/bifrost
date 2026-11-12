package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/identity"
	"github.com/veryshyjelly/bifrost/internal/metadata"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
)

func metadataEnqueue(ctx context.Context, q *db.Queries, account, key, stream, kind string, payload any) (int64, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	id, err := q.Enqueue(ctx, db.EnqueueParams{AccountID: account, Dedup: key, Stream: stream, Kind: kind, Payload: raw})
	if errors.Is(err, pgx.ErrNoRows) {
		return q.MetadataJobByKey(ctx, db.MetadataJobByKeyParams{AccountID: account, Dedup: key})
	}
	return id, err
}
func metadataRunKey(account, session, request string) string {
	b, _ := json.Marshal([]string{account, session, request})
	return metadata.PictureHash(b)[:24]
}
func validRunOptions(c metadata.RunOptions) bool {
	return c.Session != "" && c.OwnerID > 0 && c.Management < 0
}
func admitMetadataRun(ctx context.Context, q *db.Queries, account, key, scope, id, source string, requestJob int64, hasNotify bool, c metadata.RunOptions) (string, error) {
	if !metadata.ValidRunSource(source) {
		return "", delivery.ErrInvalid
	}
	old, err := q.MetadataAdmission(ctx, db.MetadataAdmissionParams{AccountID: account, RequestKey: key})
	if err == nil {
		run, err := q.MetadataRun(ctx, db.MetadataRunParams{AccountID: account, RunID: old})
		if err != nil {
			return "", err
		}
		if run.SessionID != c.Session {
			return "", delivery.ErrInvalid
		}
		return old, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	d, err := directory(ctx, q, account)
	if err != nil {
		return "", err
	}
	if scope == "contact" {
		if !identity.IsPerson(id) {
			return "", delivery.ErrInvalid
		}
		id = d.Canonical(id)
	} else if scope == "group" {
		if !strings.HasSuffix(id, "@g.us") {
			return "", delivery.ErrInvalid
		}
	} else if scope != "all" {
		return "", delivery.ErrInvalid
	}
	active, err := q.MetadataActiveRuns(ctx, db.MetadataActiveRunsParams{AccountID: account, SessionID: c.Session})
	if err != nil {
		return "", err
	}
	runID := ""
	created := false
	for _, run := range active {
		if run.Scope == "all" || run.Scope == scope && d.Canonical(run.SourceID) == id {
			runID = run.RunID
			break
		}
	}
	if runID == "" {
		created = true
		runID = metadataRunKey(account, c.Session, key)
		if err := q.CreateMetadataRun(ctx, db.CreateMetadataRunParams{AccountID: account, RunID: runID, SessionID: c.Session, Scope: scope, SourceID: id, Source: source}); err != nil {
			return "", err
		}
		if _, err := metadataEnqueue(ctx, q, account, "sync-start:"+runID, "metadata-run:"+runID, "sync-start", map[string]string{"run": runID, "session": c.Session}); err != nil {
			return "", err
		}
	}
	if err := q.CreateMetadataAdmission(ctx, db.CreateMetadataAdmissionParams{AccountID: account, RequestKey: key, RunID: runID}); err != nil {
		return "", err
	}
	if requestJob > 0 && (created || hasNotify) {
		if err := q.CreateMetadataRunRequest(ctx, db.CreateMetadataRunRequestParams{AccountID: account, RunID: runID, JobID: requestJob}); err != nil {
			return "", err
		}
	}
	return runID, nil
}
func (s *Store) AdmitMetadataRun(ctx context.Context, j delivery.Job, c metadata.RunOptions) (string, error) {
	if !validRunOptions(c) {
		return "", delivery.ErrInvalid
	}
	var run string
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		var source struct {
			Type string `json:"type"`
			Data struct {
				Scope, ID string
				Source    string
				Notify    json.RawMessage
			} `json:"data"`
		}
		if (row.Kind != "management" && row.Kind != "sync-request") || json.Unmarshal(row.Payload, &source) != nil || source.Type != "sync_metadata" {
			return delivery.ErrInvalid
		}
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		var err error
		trigger := "command"
		if row.Kind == "sync-request" {
			trigger = source.Data.Source
			if trigger != "new" && trigger != "link" && trigger != "bind" || trigger == "link" && source.Data.Scope != "group" || trigger == "bind" && source.Data.Scope != "contact" {
				return delivery.ErrInvalid
			}
		}
		run, err = admitMetadataRun(ctx, q, row.AccountID, row.Dedup, source.Data.Scope, source.Data.ID, trigger, row.ID, len(source.Data.Notify) > 0 && string(source.Data.Notify) != "null", c)
		return err
	})
	return run, err
}
func metadataRunJob(ctx context.Context, q *db.Queries, row db.Job, c metadata.RunOptions) (db.MetadataRun, error) {
	var p struct{ Run, Session string }
	if json.Unmarshal(row.Payload, &p) != nil || p.Run == "" || p.Session != c.Session {
		return db.MetadataRun{}, delivery.ErrInvalid
	}
	run, err := q.MetadataRun(ctx, db.MetadataRunParams{AccountID: row.AccountID, RunID: p.Run})
	if err != nil {
		return run, err
	}
	if run.SessionID != c.Session {
		return run, delivery.ErrInvalid
	}
	return run, nil
}
func metadataSourceKey(d *identity.Directory, kind, id string) string {
	if kind == "contact" {
		person := d.Person(id, nil)
		if person.Registered {
			return "bot:" + person.Bot.ID
		}
		return "contact:" + d.Canonical(id)
	}
	return "group:" + id
}
func metadataTargetSet(ctx context.Context, q *db.Queries, account, run string, d *identity.Directory) (map[string]bool, error) {
	rows, err := q.MetadataRunTargets(ctx, db.MetadataRunTargetsParams{AccountID: account, RunID: run})
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, t := range rows {
		known["id:"+t.Kind+":"+d.Canonical(t.SourceID)] = true
		known["key:"+t.Kind+":"+t.SourceKey] = true
	}
	return known, nil
}
func addMetadataTarget(ctx context.Context, q *db.Queries, account, run, kind, id string, c metadata.RunOptions, d *identity.Directory, known map[string]bool) error {
	if kind == "contact" {
		id = d.Canonical(id)
	}
	key := metadataSourceKey(d, kind, id)
	identityKey, sourceKey := "id:"+kind+":"+id, "key:"+kind+":"+key
	if known[identityKey] || kind == "contact" && known[sourceKey] {
		return nil
	}
	dedup := "sync-target:" + run + ":" + kind + ":" + id
	job, err := metadataEnqueue(ctx, q, account, dedup, "metadata-source:"+c.Session+":"+key, "sync-target", map[string]any{"type": "sync_metadata", "data": map[string]string{"run": run, "session": c.Session, "scope": kind, "id": id}})
	if err != nil {
		return err
	}
	if err := q.CreateMetadataRunTarget(ctx, db.CreateMetadataRunTargetParams{AccountID: account, RunID: run, Kind: kind, SourceID: id, SourceKey: key, JobID: job}); err != nil {
		return err
	}
	known[identityKey], known[sourceKey] = true, true
	return nil
}
func (s *Store) StartMetadataRun(ctx context.Context, j delivery.Job, c metadata.RunOptions) error {
	return s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if row.Kind != "sync-start" || !validRunOptions(c) {
			return delivery.ErrInvalid
		}
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		run, err := metadataRunJob(ctx, q, row, c)
		if err != nil {
			return err
		}
		if run.State != "queued" {
			return nil
		}
		d, err := directory(ctx, q, row.AccountID)
		if err != nil {
			return err
		}
		known, err := metadataTargetSet(ctx, q, row.AccountID, run.RunID, d)
		if err != nil {
			return err
		}
		if run.Scope == "all" {
			routes, err := q.MetadataRoutes(ctx, row.AccountID)
			if err != nil {
				return err
			}
			for _, r := range routes {
				if strings.HasSuffix(r.WhatsappChat, "@g.us") && r.Mode != "muted" && r.Mode != "ignored" {
					if err := addMetadataTarget(ctx, q, row.AccountID, run.RunID, "group", r.WhatsappChat, c, d, known); err != nil {
						return err
					}
				}
			}
			bindings, err := q.IdentityBindings(ctx, row.AccountID)
			if err != nil {
				return err
			}
			for _, b := range bindings {
				p := d.Person(b.ContactID, nil)
				if p.Registered {
					if err := addMetadataTarget(ctx, q, row.AccountID, run.RunID, "contact", p.ID, c, d, known); err != nil {
						return err
					}
				}
			}
		} else if err := addMetadataTarget(ctx, q, row.AccountID, run.RunID, run.Scope, run.SourceID, c, d, known); err != nil {
			return err
		}
		if err := q.SetMetadataRunState(ctx, db.SetMetadataRunStateParams{AccountID: row.AccountID, RunID: run.RunID, State: "running"}); err != nil {
			return err
		}
		_, err = metadataEnqueue(ctx, q, row.AccountID, "sync-seal:"+run.RunID, "metadata-run:"+run.RunID, "sync-seal", map[string]string{"run": run.RunID, "session": c.Session})
		return err
	})
}
func runTarget(ctx context.Context, q *db.Queries, row db.Job, c metadata.RunOptions) (db.MetadataRunTarget, error) {
	if row.Kind != "sync-target" {
		return db.MetadataRunTarget{}, delivery.ErrInvalid
	}
	t, err := q.MetadataRunTarget(ctx, db.MetadataRunTargetParams{AccountID: row.AccountID, JobID: row.ID})
	if err != nil {
		return t, err
	}
	var p struct {
		Type string
		Data struct{ Run, Session, Scope, ID string }
	}
	if json.Unmarshal(row.Payload, &p) != nil || p.Type != "sync_metadata" || p.Data.Run != t.RunID || p.Data.Session != c.Session || p.Data.Scope != t.Kind || p.Data.ID != t.SourceID {
		return t, delivery.ErrInvalid
	}
	run, err := q.MetadataRun(ctx, db.MetadataRunParams{AccountID: row.AccountID, RunID: t.RunID})
	if err != nil {
		return t, err
	}
	if run.SessionID != c.Session {
		return t, delivery.ErrInvalid
	}
	return t, nil
}
func (s *Store) BeginMetadataTarget(ctx context.Context, j delivery.Job, c metadata.RunOptions) (bool, error) {
	terminal, deferred := false, false
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		target, err := runTarget(ctx, q, row, c)
		if err != nil {
			return err
		}
		if target.Outcome != "queued" {
			terminal = true
			return nil
		}
		d, err := directory(ctx, q, row.AccountID)
		if err != nil {
			return err
		}
		busy, err := q.MetadataSourcePredecessor(ctx, db.MetadataSourcePredecessorParams{AccountID: row.AccountID, SessionID: c.Session, Kind: target.Kind, Aliases: d.Aliases(target.SourceID), SourceKey: metadataSourceKey(d, target.Kind, target.SourceID), JobID: row.ID})
		if err != nil {
			return err
		}
		if busy {
			deferred = true
			return q.DeferMetadataJob(ctx, row.ID)
		}
		return nil
	})
	if err == nil && deferred {
		err = delivery.ErrDeferred
	}
	return terminal, err
}
func (s *Store) DiscoverMetadataContacts(ctx context.Context, j delivery.Job, c metadata.RunOptions) error {
	return s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		target, err := runTarget(ctx, q, row, c)
		if err != nil {
			return err
		}
		if target.Kind != "group" {
			return nil
		}
		p, found, err := metadataPlan(ctx, q, row)
		if err != nil {
			return err
		}
		if !found {
			return delivery.ErrIncomplete
		}
		d, err := directory(ctx, q, row.AccountID)
		if err != nil {
			return err
		}
		known, err := metadataTargetSet(ctx, q, row.AccountID, target.RunID, d)
		if err != nil {
			return err
		}
		for _, member := range p.Snapshot.Members {
			person := d.Person(member.ID, nil)
			if person.Registered {
				if err := addMetadataTarget(ctx, q, row.AccountID, target.RunID, "contact", person.ID, c, d, known); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
func (s *Store) CompleteMetadataTarget(ctx context.Context, j delivery.Job, outcome metadata.Outcome, c metadata.RunOptions) error {
	return s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		target, err := runTarget(ctx, q, row, c)
		if err != nil {
			return err
		}
		if target.Outcome != "queued" {
			return nil
		}
		effects, err := q.MetadataJobEffects(ctx, db.MetadataJobEffectsParams{AccountID: row.AccountID, JobID: row.ID})
		if err != nil {
			return err
		}
		for _, effect := range effects {
			if effect.State == "sending" || effect.State == "uncertain" {
				return delivery.ErrUncertain
			}
			if effect.State == "pending" {
				if len(outcome.Warnings) == 0 {
					return delivery.ErrIncomplete
				}
				if _, err := q.DiscardEffect(ctx, db.DiscardEffectParams{AccountID: row.AccountID, EffectKey: effect.EffectKey, JobID: row.ID}); err != nil {
					return err
				}
			}
		}
		if len(outcome.Warnings) == 0 {
			p, found, err := metadataPlan(ctx, q, row)
			if err != nil {
				return err
			}
			if !found || !p.TextComplete {
				return delivery.ErrIncomplete
			}
			photo, found, err := metadataPicturePlan(ctx, q, row)
			if err != nil {
				return err
			}
			if !found || !photo.Complete {
				return delivery.ErrIncomplete
			}
			_, found, err = metadataInspection(ctx, q, row)
			if err != nil {
				return err
			}
			if !found {
				return delivery.ErrIncomplete
			}
		}
		state := "done"
		if len(outcome.Warnings) > 0 {
			state = "warning"
		}
		raw, err := json.Marshal(outcome)
		if err != nil {
			return err
		}
		return q.CompleteMetadataRunTarget(ctx, db.CompleteMetadataRunTargetParams{AccountID: row.AccountID, JobID: row.ID, Outcome: state, Details: raw})
	})
}
func (s *Store) SealMetadataRun(ctx context.Context, j delivery.Job, c metadata.RunOptions) error {
	deferred := false
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if row.Kind != "sync-seal" {
			return delivery.ErrInvalid
		}
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		run, err := metadataRunJob(ctx, q, row, c)
		if err != nil {
			return err
		}
		if run.State == "completed" {
			return nil
		}
		targets, err := q.MetadataRunTargets(ctx, db.MetadataRunTargetsParams{AccountID: row.AccountID, RunID: run.RunID})
		if err != nil {
			return err
		}
		input := metadata.ReportRequest{RunID: run.RunID, Session: c.Session, Source: run.Source}
		for _, t := range targets {
			if t.Outcome == "queued" || t.JobState != "done" {
				deferred = true
				return q.DeferMetadataJob(ctx, row.ID)
			}
			input.TargetJobs = append(input.TargetJobs, t.JobID)
		}
		input.RequestJobs, err = q.MetadataRunRequests(ctx, db.MetadataRunRequestsParams{AccountID: row.AccountID, RunID: run.RunID})
		if err != nil {
			return err
		}
		for _, id := range input.RequestJobs {
			request, err := q.MetadataReportJob(ctx, db.MetadataReportJobParams{AccountID: row.AccountID, ID: id})
			if err != nil {
				return err
			}
			if request.State != "done" {
				deferred = true
				return q.DeferMetadataJob(ctx, row.ID)
			}
		}
		if _, err := metadataEnqueue(ctx, q, row.AccountID, "sync-finish:"+run.RunID, "metadata-notice:"+run.RunID, "control-sync-report", input); err != nil {
			return err
		}
		return q.SetMetadataRunState(ctx, db.SetMetadataRunStateParams{AccountID: row.AccountID, RunID: run.RunID, State: "completed"})
	})
	if err == nil && deferred {
		return delivery.ErrDeferred
	}
	return err
}
func (s *Store) ScheduleMetadataRun(ctx context.Context, account string, c metadata.RunOptions, now time.Time) (string, bool, error) {
	if !validRunOptions(c) || account == "" {
		return "", false, delivery.ErrInvalid
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", false, err
	}
	defer rollback(tx)
	q := s.queries.WithTx(tx)
	identity, err := q.Identity(ctx)
	if err != nil {
		return "", false, err
	}
	if identity.AccountID != account {
		return "", false, delivery.ErrInvalid
	}
	if err := q.LockContactDirectory(ctx, account); err != nil {
		return "", false, err
	}
	minute := now.Truncate(time.Minute).Unix() / 60
	args := db.MetadataCronTickParams{AccountID: account, SessionID: c.Session, Expression: c.Cron, Minute: minute}
	existing, err := q.MetadataCronTick(ctx, args)
	if err == nil {
		return existing, false, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", false, err
	}
	key := "cron:" + c.Session + ":" + c.Cron + ":" + strconv.FormatInt(minute, 10)
	run, err := admitMetadataRun(ctx, q, account, key, "all", "", "cron", 0, false, c)
	if err != nil {
		return "", false, err
	}
	if err := q.CreateMetadataCronTick(ctx, db.CreateMetadataCronTickParams{AccountID: account, SessionID: c.Session, Expression: c.Cron, Minute: minute, RunID: run}); err != nil {
		return "", false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", false, err
	}
	return run, true, nil
}

var _ metadata.RunStore = (*Store)(nil)
