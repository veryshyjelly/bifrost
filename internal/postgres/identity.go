package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/identity"
	"github.com/veryshyjelly/bifrost/internal/mentions"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
)

func directory(ctx context.Context, q *db.Queries, account string) (*identity.Directory, error) {
	aliasRows, err := q.IdentityAliases(ctx, account)
	if err != nil {
		return nil, err
	}
	profiles, err := q.IdentityProfiles(ctx, account)
	if err != nil {
		return nil, err
	}
	botRows, err := q.IdentityBots(ctx, account)
	if err != nil {
		return nil, err
	}
	bound, err := q.IdentityBindings(ctx, account)
	if err != nil {
		return nil, err
	}
	aliases := []identity.Alias{}
	contacts := []identity.Profile{}
	bots := []identity.Bot{}
	bindings := []identity.Binding{}
	for _, a := range aliasRows {
		aliases = append(aliases, identity.Alias{ID: a.Alias, Canonical: a.Canonical})
	}
	for _, p := range profiles {
		contacts = append(contacts, identity.Profile{ID: p.PersonID, Name: p.Name, Synced: p.MetadataSynced})
	}
	for _, b := range botRows {
		bots = append(bots, identity.Bot{ID: b.BotID, Username: b.Username, Started: b.Started})
	}
	for _, b := range bound {
		bindings = append(bindings, identity.Binding{PersonID: b.ContactID, BotID: b.BotID})
	}
	return identity.New(aliases, contacts, bots, bindings)
}

// IdentityDirectory uses one consistent read snapshot; bindings cannot be mixed
// with aliases or usernames from another point in a concurrent registration.
func (s *Store) IdentityDirectory(ctx context.Context, account string) (*identity.Directory, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer rollback(tx)
	d, err := directory(ctx, s.queries.WithTx(tx), account)
	if err != nil {
		return nil, err
	}
	return d, tx.Commit(ctx)
}

// SaveContactIdentity accepts provider-verified LID/phone evidence, never a
// display number. Callers must authenticate the webhook or validate the native
// Contacts.Phone response before calling it. It never rekeys routes or receipts.
func (s *Store) SaveContactIdentity(ctx context.Context, job delivery.Job, alias, phoneID string, profile identity.Profile) error {
	alias = identity.Normalize(alias)
	phoneID = identity.Normalize(phoneID)
	if alias != "" {
		verified, ok := identity.PhoneID(identity.Local(phoneID))
		if !identity.IsPerson(alias) || !strings.HasSuffix(alias, "@lid") || !ok || verified != phoneID {
			return delivery.ErrInvalid
		}
	} else if phoneID != "" {
		return delivery.ErrInvalid
	}
	if profile.ID != "" && !identity.IsPerson(profile.ID) {
		return delivery.ErrInvalid
	}
	return s.withClaim(ctx, job, func(q *db.Queries, row db.Job) error {
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		if alias != "" {
			got, err := q.SaveContactAlias(ctx, db.SaveContactAliasParams{AccountID: row.AccountID, Alias: alias, Canonical: phoneID})
			if err != nil {
				return err
			}
			if got != phoneID {
				return identity.ErrConflict
			}
		}
		if profile.ID != "" {
			if err := q.SaveContactProfile(ctx, db.SaveContactProfileParams{AccountID: row.AccountID, PersonID: profile.ID, Name: profile.Name, MetadataSynced: profile.Synced}); err != nil {
				return err
			}
		}
		// A new alias must not merge two separately bound bots silently.
		_, err := directory(ctx, q, row.AccountID)
		return err
	})
}

func (s *Store) MentionRoster(ctx context.Context, account, chat string) (mentions.Roster, bool, error) {
	r, err := s.queries.MentionRoster(ctx, db.MentionRosterParams{AccountID: account, ChatID: chat})
	if errors.Is(err, pgx.ErrNoRows) {
		return mentions.Roster{}, false, nil
	}
	if err != nil {
		return mentions.Roster{}, false, err
	}
	var roster mentions.Roster
	if json.Unmarshal(r.Payload, &roster) != nil || roster.ChatID != chat || roster.FetchedAt != r.FetchedAt {
		return mentions.Roster{}, false, delivery.ErrInvalid
	}
	return roster, true, nil
}
func (s *Store) SaveMentionRoster(ctx context.Context, job delivery.Job, r mentions.Roster) error {
	if !strings.HasSuffix(r.ChatID, "@g.us") || r.Name == "" || r.FetchedAt <= 0 || r.Cached {
		return delivery.ErrInvalid
	}
	for _, m := range r.Members {
		if !identity.IsPerson(m.ID) {
			return delivery.ErrInvalid
		}
	}
	payload, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return s.withClaim(ctx, job, func(q *db.Queries, row db.Job) error {
		return q.SaveMentionRoster(ctx, db.SaveMentionRosterParams{AccountID: row.AccountID, ChatID: r.ChatID, Payload: payload, FetchedAt: r.FetchedAt})
	})
}
func (s *Store) MentionToken(ctx context.Context, account, key string) (mentions.Token, bool, error) {
	r, err := s.queries.MentionToken(ctx, db.MentionTokenParams{AccountID: account, Token: key})
	if errors.Is(err, pgx.ErrNoRows) {
		return mentions.Token{}, false, nil
	}
	return mentions.Token{ChatID: r.ChatID, PersonID: r.PersonID}, err == nil, err
}

var mentionTokenKey = regexp.MustCompile(`^[a-f0-9]{8,64}$`)

func (s *Store) SaveMentionToken(ctx context.Context, job delivery.Job, key string, t mentions.Token) (mentions.Token, error) {
	if !mentionTokenKey.MatchString(key) || !strings.HasSuffix(t.ChatID, "@g.us") || !identity.IsPerson(t.PersonID) {
		return mentions.Token{}, delivery.ErrInvalid
	}
	var result mentions.Token
	err := s.withClaim(ctx, job, func(q *db.Queries, row db.Job) error {
		r, err := q.SaveMentionToken(ctx, db.SaveMentionTokenParams{AccountID: row.AccountID, Token: key, ChatID: t.ChatID, PersonID: t.PersonID})
		result = mentions.Token{ChatID: r.ChatID, PersonID: r.PersonID}
		return err
	})
	return result, err
}

var _ mentions.Store = (*Store)(nil)
