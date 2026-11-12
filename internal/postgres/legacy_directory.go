package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/veryshyjelly/bifrost/internal/identity"
	"github.com/veryshyjelly/bifrost/internal/legacy"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
)

type LegacyDirectoryProjection struct {
	Account       string `json:"account"`
	ArchiveDigest string `json:"archive_digest"`
	Aliases       int    `json:"aliases"`
	Profiles      int    `json:"profiles"`
	Bindings      int    `json:"bindings"`
	Tokens        int    `json:"tokens"`
	Rosters       int    `json:"rosters"`
}

// ProjectLegacyDirectory preserves the original scoped identity and mention
// capability. It requires projected credentials and leaves import activation
// blocked. The selected source profile remains distinct from routing identity.
func (s *Store) ProjectLegacyDirectory(ctx context.Context, account string) (LegacyDirectoryProjection, error) {
	var result LegacyDirectoryProjection
	if account == "" {
		return result, legacy.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer rollback(tx)
	q := s.queries.WithTx(tx)
	if err = q.LockIdentity(ctx); err != nil {
		return result, err
	}
	saved, err := q.Identity(ctx)
	if err != nil {
		return result, err
	}
	if saved.AccountID != account {
		return result, ErrLegacyDestination
	}
	checkpoint, err := q.LockLegacyImport(ctx, account)
	if err != nil {
		return result, err
	}
	previous, err := q.LegacyDirectoryProjection(ctx, account)
	if err == nil {
		if json.Unmarshal(previous, &result) != nil || result.Account != account || result.ArchiveDigest != checkpoint.ArchiveDigest {
			return result, legacy.ErrInvalid
		}
		return result, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	if checkpoint.ActivatedAt.Valid {
		return result, legacy.ErrInvalid
	}
	if _, err = q.LegacyBotProjection(ctx, account); err != nil {
		return result, err
	}
	if err = q.LockContactDirectory(ctx, account); err != nil {
		return result, err
	}
	conflict, err := q.LegacyDirectoryProjectionConflict(ctx, account)
	if err != nil {
		return result, err
	}
	if conflict {
		return result, ErrLegacyDestination
	}
	values, err := q.LegacyDirectoryValues(ctx, account)
	if err != nil {
		return result, err
	}
	input := make([]legacy.KV, len(values))
	for i, v := range values {
		input[i] = legacy.KV{Kind: v.Kind, ID: v.SourceID, Value: v.SourceValue}
	}
	sourceAliases, err := q.LegacySourceAliases(ctx, account)
	if err != nil {
		return result, err
	}
	aliases := make([]legacy.Alias, len(sourceAliases))
	for i, a := range sourceAliases {
		aliases[i] = legacy.Alias{Alias: a.Alias, Canonical: a.Canonical}
	}
	sourceBots, err := q.IdentityBots(ctx, account)
	if err != nil {
		return result, err
	}
	bots := make([]identity.Bot, len(sourceBots))
	for i, b := range sourceBots {
		bots[i] = identity.Bot{ID: b.BotID, Username: b.Username, Started: b.Started}
	}
	projected, err := legacy.ProjectDirectory(account, input, aliases, bots)
	if err != nil {
		return result, err
	}
	for _, a := range projected.Aliases {
		got, err := q.SaveContactAlias(ctx, db.SaveContactAliasParams{AccountID: account, Alias: a.ID, Canonical: a.Canonical})
		if err != nil {
			return LegacyDirectoryProjection{}, err
		}
		if got != a.Canonical {
			return LegacyDirectoryProjection{}, legacy.ErrInvalid
		}
	}
	for _, p := range projected.Profiles {
		if err = q.SaveContactProfile(ctx, db.SaveContactProfileParams{AccountID: account, PersonID: p.ID, Name: p.Name, MetadataSynced: p.Synced}); err != nil {
			return LegacyDirectoryProjection{}, err
		}
	}
	for _, b := range projected.Bindings {
		if err = q.SaveLegacyContactBinding(ctx, db.SaveLegacyContactBindingParams{AccountID: account, ContactID: b.PersonID, BotID: b.BotID, SourceProfile: b.SourceProfile}); err != nil {
			return LegacyDirectoryProjection{}, err
		}
	}
	keys := make([]string, 0, len(projected.Tokens))
	for key := range projected.Tokens {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		token := projected.Tokens[key]
		got, err := q.SaveMentionToken(ctx, db.SaveMentionTokenParams{AccountID: account, Token: key, ChatID: token.ChatID, PersonID: token.PersonID})
		if err != nil {
			return LegacyDirectoryProjection{}, err
		}
		if got.ChatID != token.ChatID || got.PersonID != token.PersonID {
			return LegacyDirectoryProjection{}, legacy.ErrInvalid
		}
	}
	for _, roster := range projected.Rosters {
		payload, err := json.Marshal(roster)
		if err != nil {
			return LegacyDirectoryProjection{}, legacy.ErrInvalid
		}
		if err = q.SaveMentionRoster(ctx, db.SaveMentionRosterParams{AccountID: account, ChatID: roster.ChatID, Payload: payload, FetchedAt: roster.FetchedAt}); err != nil {
			return LegacyDirectoryProjection{}, err
		}
	}
	if _, err = directory(ctx, q, account); err != nil {
		return LegacyDirectoryProjection{}, err
	}
	result = LegacyDirectoryProjection{Account: account, ArchiveDigest: checkpoint.ArchiveDigest, Aliases: len(projected.Aliases), Profiles: len(projected.Profiles), Bindings: len(projected.Bindings), Tokens: len(projected.Tokens), Rosters: len(projected.Rosters)}
	raw, _ := json.Marshal(result)
	if err = q.SaveLegacyDirectoryProjection(ctx, db.SaveLegacyDirectoryProjectionParams{AccountID: account, Result: raw}); err != nil {
		return LegacyDirectoryProjection{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return LegacyDirectoryProjection{}, err
	}
	return result, nil
}
