package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/go-telegram/bot/models"
	"github.com/jackc/pgx/v5"
	"github.com/veryshyjelly/bifrost/internal/bots"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/ingress"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
)

func decodeCredential(v db.TelegramCredential) (bots.Credential, error) {
	c := bots.Credential{BotID: v.BotID, Ciphertext: v.Ciphertext, Revision: v.Revision, CreatedNotice: v.CreatedNotice}
	if json.Unmarshal(v.Identity, &c.Identity) != nil || c.Identity.ID <= 0 || !c.Identity.IsBot || (c.BotID != "master" && c.BotID != strconv.FormatInt(c.Identity.ID, 10)) {
		return c, delivery.ErrInvalid
	}
	return c, nil
}
func (s *Store) Credential(ctx context.Context, account, id string) (bots.Credential, bool, error) {
	row, err := s.queries.BotCredential(ctx, db.BotCredentialParams{AccountID: account, BotID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return bots.Credential{}, false, nil
	}
	if err != nil {
		return bots.Credential{}, false, err
	}
	c, err := decodeCredential(row)
	return c, err == nil, err
}
func (s *Store) ManagedRegistration(ctx context.Context, j delivery.Job) (bool, error) {
	found := false
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if row.Kind != "tg" {
			return delivery.ErrInvalid
		}
		_, err := q.BotRegistration(ctx, db.BotRegistrationParams{AccountID: row.AccountID, JobID: row.ID})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		found = err == nil
		return err
	})
	return found, err
}
func saveCredential(ctx context.Context, q *db.Queries, account string, c bots.Credential) error {
	if c.Ciphertext == "" || c.Revision == "" || c.Identity.ID <= 0 || !c.Identity.IsBot || (c.BotID != "master" && c.BotID != strconv.FormatInt(c.Identity.ID, 10)) {
		return delivery.ErrInvalid
	}
	raw, _ := json.Marshal(c.Identity)
	return q.SaveBotCredential(ctx, db.SaveBotCredentialParams{AccountID: account, BotID: c.BotID, Ciphertext: c.Ciphertext, Revision: c.Revision, Identity: raw, CreatedNotice: c.CreatedNotice})
}
func queueBotSetup(ctx context.Context, q *db.Queries, account string, c bots.Credential, settings bots.Settings) error {
	key := "bot-setup:" + c.BotID + ":" + c.Revision + ":" + settings.Configuration
	_, err := metadataEnqueue(ctx, q, account, key, "bot-settings:"+c.BotID, "control-bot-setup", map[string]string{"bot": c.BotID, "revision": c.Revision, "configuration": settings.Configuration})
	return err
}
func (s *Store) RegisterManagedBot(ctx context.Context, j delivery.Job, c bots.Credential, settings bots.Settings) error {
	return s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if row.Kind != "tg" || settings.OwnerID <= 0 || settings.Management >= 0 || settings.Session == "" || settings.Configuration == "" || c.BotID == "master" {
			return delivery.ErrInvalid
		}
		var source struct {
			Bot    string
			Update json.RawMessage
		}
		var u models.Update
		if json.Unmarshal(row.Payload, &source) != nil || source.Bot != "master" || json.Unmarshal(source.Update, &u) != nil || !ingress.AuthorizedUpdate(source.Update, &u, settings.OwnerID) || row.Dedup != "tg:master:"+strconv.FormatInt(u.ID, 10) {
			return delivery.ErrInvalid
		}
		identity, ok := bots.ManagedIdentity(&u, settings.OwnerID)
		if !ok || identity.ID != c.Identity.ID || identity.Username != c.Identity.Username || !identity.IsBot {
			return delivery.ErrInvalid
		}
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		if _, err := q.BotRegistration(ctx, db.BotRegistrationParams{AccountID: row.AccountID, JobID: row.ID}); err == nil {
			return nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err := saveCredential(ctx, q, row.AccountID, c); err != nil {
			return err
		}
		if err := q.PutIngressBot(ctx, db.PutIngressBotParams{AccountID: row.AccountID, BotID: c.BotID, Username: c.Identity.Username}); err != nil {
			return err
		}
		if err := q.SaveBotRegistration(ctx, db.SaveBotRegistrationParams{AccountID: row.AccountID, JobID: row.ID, BotID: c.BotID, Revision: c.Revision}); err != nil {
			return err
		}
		return queueBotSetup(ctx, q, row.AccountID, c, settings)
	})
}

// SeedMasterCredential is local startup preparation. It never calls Telegram,
// marks migration eligibility, or starts a consumer.
func (s *Store) SeedMasterCredential(ctx context.Context, account string, c bots.Credential, settings bots.Settings) error {
	if c.BotID != "master" || account == "" || settings.Configuration == "" {
		return delivery.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	q := s.queries.WithTx(tx)
	identity, err := q.Identity(ctx)
	if err != nil {
		return err
	}
	if identity.AccountID != account {
		return delivery.ErrInvalid
	}
	if err := q.LockContactDirectory(ctx, account); err != nil {
		return err
	}
	// Startup must retain imported ciphertext and identity snapshots. Replacing
	// the master token belongs to an explicit credential change, not a restart.
	if saved, err := q.BotCredential(ctx, db.BotCredentialParams{AccountID: account, BotID: "master"}); err == nil {
		previous, err := decodeCredential(saved)
		if err != nil || previous.Revision != c.Revision || previous.Identity.ID != c.Identity.ID {
			return delivery.ErrInvalid
		}
		return queueMasterSetup(ctx, tx, q, account, previous, settings)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	c.CreatedNotice = true
	if err := saveCredential(ctx, q, account, c); err != nil {
		return err
	}
	return queueMasterSetup(ctx, tx, q, account, c, settings)
}

func queueMasterSetup(ctx context.Context, tx pgx.Tx, q *db.Queries, account string, c bots.Credential, settings bots.Settings) error {
	if err := queueBotSetup(ctx, q, account, c, settings); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) QueueBotSetups(ctx context.Context, account string, c bots.Settings) error {
	if account == "" || c.Configuration == "" {
		return delivery.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	q := s.queries.WithTx(tx)
	identity, err := q.Identity(ctx)
	if err != nil {
		return err
	}
	if identity.AccountID != account {
		return delivery.ErrInvalid
	}
	if err := q.LockContactDirectory(ctx, account); err != nil {
		return err
	}
	rows, err := q.BotCredentials(ctx, account)
	if err != nil {
		return err
	}
	for _, row := range rows {
		credential, err := decodeCredential(row)
		if err != nil {
			return err
		}
		if err := queueBotSetup(ctx, q, account, credential, c); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
func setupSource(row db.Job, c bots.Settings) (bots.SetupPlan, error) {
	var p bots.SetupPlan
	var source struct{ Bot, Revision, Configuration string }
	if row.Kind != "control-bot-setup" || json.Unmarshal(row.Payload, &source) != nil || source.Bot == "" || source.Revision == "" || source.Configuration == "" {
		return p, delivery.ErrInvalid
	}
	p.BotID, p.Revision, p.Configuration = source.Bot, source.Revision, source.Configuration
	return p, nil
}
func saveSetup(ctx context.Context, q *db.Queries, row db.Job, p bots.SetupPlan) error {
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return q.SaveBotSetupPlan(ctx, db.SaveBotSetupPlanParams{AccountID: row.AccountID, JobID: row.ID, Payload: raw})
}

func obsoleteBotSetup(ctx context.Context, q *db.Queries, row db.Job) error {
	effects, err := q.MetadataJobEffects(ctx, db.MetadataJobEffectsParams{AccountID: row.AccountID, JobID: row.ID})
	if err != nil {
		return err
	}
	for _, e := range effects {
		if e.State == "sending" || e.State == "uncertain" {
			return delivery.ErrUncertain
		}
		if e.State == "pending" {
			if _, err := q.DiscardEffect(ctx, db.DiscardEffectParams{AccountID: row.AccountID, EffectKey: e.EffectKey, JobID: row.ID}); err != nil {
				return err
			}
		}
	}
	return nil
}
func (s *Store) PrepareBotSetup(ctx context.Context, j delivery.Job, c bots.Settings) (bots.SetupPlan, bool, error) {
	var result bots.SetupPlan
	current := false
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		p, err := setupSource(row, c)
		if err != nil {
			return err
		}
		if p.Configuration != c.Configuration {
			return obsoleteBotSetup(ctx, q, row)
		}
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		credential, err := q.BotCredential(ctx, db.BotCredentialParams{AccountID: row.AccountID, BotID: p.BotID})
		if err != nil {
			return err
		}
		if credential.Revision != p.Revision {
			return obsoleteBotSetup(ctx, q, row)
		}
		current = true
		record, err := q.BotSetupPlan(ctx, db.BotSetupPlanParams{AccountID: row.AccountID, JobID: row.ID})
		if err == nil {
			if json.Unmarshal(record, &result) != nil || result.BotID != p.BotID || result.Revision != p.Revision || result.Configuration != p.Configuration {
				return delivery.ErrInvalid
			}
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		cred, err := decodeCredential(credential)
		if err != nil {
			return err
		}
		p.Identity = cred.Identity
		migrated, err := q.BotLifecycle(ctx, db.BotLifecycleParams{AccountID: row.AccountID, BotID: p.BotID})
		if err == nil {
			var native models.User
			if json.Unmarshal(migrated.Identity, &native) != nil || native.ID != p.Identity.ID || !native.IsBot {
				return delivery.ErrInvalid
			}
			p.Step = 1
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if p.Step == 0 {
			loggedOut, err := q.ConfirmedBotLogout(ctx, db.ConfirmedBotLogoutParams{AccountID: row.AccountID, BotID: p.BotID})
			if err != nil {
				return err
			}
			if loggedOut {
				p.Step = 1
			}
		}
		if p.Step == 0 {
			loggedOut, err := q.RetainedBotLogout(ctx, db.RetainedBotLogoutParams{AccountID: row.AccountID, BotID: p.BotID, CredentialRevision: p.Revision})
			if err != nil {
				return err
			}
			if loggedOut {
				p.Step = 1
			}
		}
		result = p
		return saveSetup(ctx, q, row, p)
	})
	return result, current, err
}
func (s *Store) AdvanceBotSetup(ctx context.Context, j delivery.Job, p bots.SetupPlan, c bots.Settings) error {
	return s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		source, err := setupSource(row, c)
		if err != nil {
			return err
		}
		if p.BotID != source.BotID || p.Revision != source.Revision || p.Configuration != source.Configuration || p.Step < 0 || p.Step > 2 {
			return delivery.ErrInvalid
		}
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		credential, err := q.BotCredential(ctx, db.BotCredentialParams{AccountID: row.AccountID, BotID: p.BotID})
		if err != nil {
			return err
		}
		if credential.Revision != p.Revision {
			return delivery.ErrLocalRetry
		}
		raw, err := q.BotSetupPlan(ctx, db.BotSetupPlanParams{AccountID: row.AccountID, JobID: row.ID})
		if err != nil {
			return err
		}
		var old bots.SetupPlan
		if json.Unmarshal(raw, &old) != nil || old.BotID != p.BotID || old.Revision != p.Revision || old.Configuration != p.Configuration {
			return delivery.ErrInvalid
		}
		if old.Step > p.Step {
			return nil
		}
		if old.Step != p.Step || old.Identity.ID != p.Identity.ID {
			return delivery.ErrInvalid
		}
		if p.Step == 0 {
			effect, err := q.GetEffect(ctx, db.GetEffectParams{AccountID: row.AccountID, EffectKey: row.Dedup + ":cloud-logout"})
			if err != nil {
				return err
			}
			var request struct{ Bot, Revision string }
			if effect.JobID != row.ID || effect.State != "done" || !bytes.Equal(bytes.TrimSpace(effect.Result), []byte("true")) || json.Unmarshal(effect.Request, &request) != nil || request.Bot != p.BotID || request.Revision != p.Revision {
				return delivery.ErrIncomplete
			}
		}
		if p.Step == 1 {
			if !p.Identity.IsBot || p.Identity.Username == "" || p.Identity.ID <= 0 {
				return delivery.ErrInvalid
			}
			identity, _ := json.Marshal(p.Identity)
			if err := q.SaveBotMigration(ctx, db.SaveBotMigrationParams{AccountID: row.AccountID, BotID: p.BotID, Identity: identity, MigratedAt: time.Now().UnixMilli()}); err != nil {
				return err
			}
			if p.BotID != "master" {
				if err := q.PutIngressBot(ctx, db.PutIngressBotParams{AccountID: row.AccountID, BotID: p.BotID, Username: p.Identity.Username}); err != nil {
					return err
				}
			}
		}
		if p.Step == 2 {
			if _, err := metadataEnqueue(ctx, q, row.AccountID, "bot-menu:"+row.Dedup, "bot-settings:"+p.BotID, "control-menu", map[string]string{"bot": p.BotID}); err != nil {
				return err
			}
			if p.BotID != "master" && !credential.CreatedNotice {
				if _, err := metadataEnqueue(ctx, q, row.AccountID, "created-bot:"+p.BotID, "bot-settings:"+p.BotID, "control-bot-created", map[string]string{"bot": p.BotID}); err != nil {
					return err
				}
				if err := q.MarkBotCreatedNotice(ctx, db.MarkBotCreatedNoticeParams{AccountID: row.AccountID, BotID: p.BotID}); err != nil {
					return err
				}
			}
		}
		p.Step++
		return saveSetup(ctx, q, row, p)
	})
}

var _ bots.Store = (*Store)(nil)
