package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/go-telegram/bot/models"
	"github.com/jackc/pgx/v5"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
)

// RecordBotMigration is a fenced persistence boundary for a completed local API
// migration/import. It neither migrates a bot nor installs a webhook or menu.
func (s *Store) RecordBotMigration(ctx context.Context, job delivery.Job, bot string, identity models.User) error {
	if bot == "" || identity.ID <= 0 || !identity.IsBot || identity.Username == "" || (bot != "master" && strconv.FormatInt(identity.ID, 10) != bot) {
		return delivery.ErrInvalid
	}
	return s.withClaim(ctx, job, func(q *db.Queries, row db.Job) error {
		if row.Kind != "tg" && row.Kind != "management" {
			return delivery.ErrInvalid
		}
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		if bot != "master" {
			if _, err := q.IngressBot(ctx, db.IngressBotParams{AccountID: row.AccountID, BotID: bot}); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return delivery.ErrInvalid
				}
				return err
			}
		}
		old, err := q.BotLifecycle(ctx, db.BotLifecycleParams{AccountID: row.AccountID, BotID: bot})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err == nil {
			var previous models.User
			if json.Unmarshal(old.Identity, &previous) != nil || previous.ID != identity.ID {
				return delivery.ErrInvalid
			}
		}
		payload, _ := json.Marshal(identity)
		return q.SaveBotMigration(ctx, db.SaveBotMigrationParams{AccountID: row.AccountID, BotID: bot, Identity: payload, MigratedAt: time.Now().UnixMilli()})
	})
}

func (s *Store) QueueMenus(ctx context.Context, account, run string) (int, error) {
	if account == "" || run == "" || len(run) > 128 {
		return 0, delivery.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	q := s.queries.WithTx(tx)
	if err := q.LockContactDirectory(ctx, account); err != nil {
		return 0, err
	}
	bots, err := q.MigratedBots(ctx, account)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, bot := range bots {
		payload, _ := json.Marshal(map[string]string{"bot": bot})
		_, err := q.Enqueue(ctx, db.EnqueueParams{AccountID: account, Dedup: "control-menu:" + run + ":" + bot, Stream: "control-menu:" + bot, Kind: "control-menu", Payload: payload})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return 0, err
		}
		if err == nil {
			count++
		}
	}
	return count, tx.Commit(ctx)
}

func menuSource(ctx context.Context, q *db.Queries, row db.Job) (string, error) {
	var source struct {
		Bot string `json:"bot"`
	}
	if row.Kind != "control-menu" || json.Unmarshal(row.Payload, &source) != nil || source.Bot == "" || row.Stream != "control-menu:"+source.Bot {
		return "", delivery.ErrInvalid
	}
	if _, err := q.BotLifecycle(ctx, db.BotLifecycleParams{AccountID: row.AccountID, BotID: source.Bot}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", delivery.ErrInvalid
		}
		return "", err
	}
	if source.Bot != "master" {
		if _, err := q.IngressBot(ctx, db.IngressBotParams{AccountID: row.AccountID, BotID: source.Bot}); err != nil {
			return "", delivery.ErrInvalid
		}
	}
	return source.Bot, nil
}
func loadMenu(ctx context.Context, q *db.Queries, row db.Job) (control.MenuPlan, error) {
	b, err := q.MenuPlan(ctx, db.MenuPlanParams{AccountID: row.AccountID, JobID: row.ID})
	if err != nil {
		return control.MenuPlan{}, err
	}
	var p control.MenuPlan
	if json.Unmarshal(b, &p) != nil || p.BotID == "" || p.Done < 0 || p.Done > p.Steps() {
		return p, delivery.ErrInvalid
	}
	return p, nil
}
func (s *Store) MenuPlan(ctx context.Context, job delivery.Job) (control.MenuPlan, bool, error) {
	var p control.MenuPlan
	known := false
	err := s.withClaim(ctx, job, func(q *db.Queries, row db.Job) error {
		bot, err := menuSource(ctx, q, row)
		if err != nil {
			return err
		}
		p, err = loadMenu(ctx, q, row)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if p.BotID != bot {
			return delivery.ErrInvalid
		}
		known = true
		return nil
	})
	return p, known, err
}
func (s *Store) PrepareMenu(ctx context.Context, job delivery.Job, p control.MenuPlan) (control.MenuPlan, error) {
	if p.OwnerID <= 0 || p.ManagementChatID >= 0 || p.Done != 0 || p.Identity.ID <= 0 || !p.Identity.IsBot || p.Identity.Username == "" || len(p.Commands) == 0 {
		return control.MenuPlan{}, delivery.ErrInvalid
	}
	var result control.MenuPlan
	err := s.withClaim(ctx, job, func(q *db.Queries, row db.Job) error {
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		bot, err := menuSource(ctx, q, row)
		if err != nil {
			return err
		}
		if bot != p.BotID {
			return delivery.ErrInvalid
		}
		old, err := loadMenu(ctx, q, row)
		if err == nil {
			result = old
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		state, err := q.BotLifecycle(ctx, db.BotLifecycleParams{AccountID: row.AccountID, BotID: bot})
		if err != nil {
			return err
		}
		var identity models.User
		if json.Unmarshal(state.Identity, &identity) != nil || identity.ID != p.Identity.ID {
			return delivery.ErrInvalid
		}
		b, _ := json.Marshal(p)
		if err := q.PrepareMenu(ctx, db.PrepareMenuParams{AccountID: row.AccountID, JobID: row.ID, Payload: b}); err != nil {
			return err
		}
		b, _ = json.Marshal(p.Identity)
		if err := q.SaveTelegramIdentity(ctx, db.SaveTelegramIdentityParams{AccountID: row.AccountID, BotID: bot, Identity: b}); err != nil {
			return err
		}
		if bot != "master" {
			if err := q.PutIngressBot(ctx, db.PutIngressBotParams{AccountID: row.AccountID, BotID: bot, Username: p.Identity.Username}); err != nil {
				return err
			}
		}
		result = p
		return nil
	})
	return result, err
}
func (s *Store) AdvanceMenu(ctx context.Context, job delivery.Job, step int) error {
	return s.withClaim(ctx, job, func(q *db.Queries, row db.Job) error {
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		bot, err := menuSource(ctx, q, row)
		if err != nil {
			return err
		}
		p, err := loadMenu(ctx, q, row)
		if err != nil {
			return err
		}
		if p.BotID != bot || step < 0 || step >= p.Steps() || p.Done < step {
			return delivery.ErrInvalid
		}
		if p.Done > step {
			return nil
		}
		p.Done++
		b, _ := json.Marshal(p)
		if err := q.UpdateMenu(ctx, db.UpdateMenuParams{AccountID: row.AccountID, JobID: row.ID, Payload: b}); err != nil {
			return err
		}
		if p.Done == p.Steps() {
			return q.MarkMenuInstalled(ctx, db.MarkMenuInstalledParams{AccountID: row.AccountID, BotID: bot, MenuInstalledAt: time.Now().UnixMilli()})
		}
		return nil
	})
}

var _ control.MenuStore = (*Store)(nil)
