package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/identity"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
)

// NormalizeIncoming freezes provider identity/content and moves the original
// job, preserving its ID and effect keys. No provider operation occurs here.
// A move behind an active/earlier conversation job releases its claim and
// returns ErrDeferred. Claim and stream changes share a short topology lock.
func (s *Store) NormalizeIncoming(ctx context.Context, job delivery.Job) (identity.Source, error) {
	var source identity.Source
	deferred := false
	err := s.withClaim(ctx, job, func(q *db.Queries, row db.Job) error {
		if row.Kind != "wa" {
			return delivery.ErrInvalid
		}
		if err := q.LockQueueTopology(ctx); err != nil {
			return err
		}
		old, err := q.WhatsAppSource(ctx, db.WhatsAppSourceParams{AccountID: row.AccountID, JobID: row.ID})
		if err == nil {
			if json.Unmarshal(old.Payload, &source) != nil || source.ChatID != old.WhatsappChat || row.Stream != "wa:"+source.ChatID {
				return delivery.ErrInvalid
			}
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		event, err := identity.DecodeSource(jobFrom(row))
		if err != nil {
			return err
		}
		m := event.Message
		if row.Stream != "wa:"+m.ChatID {
			return delivery.ErrInvalid
		}
		if count, err := q.JobEffectCount(ctx, row.ID); err != nil {
			return err
		} else if count != 0 {
			return delivery.ErrInvalid
		}
		source, err = prepareSource(ctx, q, row.AccountID, m)
		if err != nil {
			return err
		}
		chat := source.ChatID
		payload, err := json.Marshal(source)
		if err != nil {
			return err
		}
		if err := q.SaveWhatsAppSource(ctx, db.SaveWhatsAppSourceParams{AccountID: row.AccountID, JobID: row.ID, WhatsappChat: chat, Payload: payload}); err != nil {
			return err
		}
		stream := "wa:" + chat
		if row.Stream == stream {
			return nil
		}
		busy, err := q.ConversationBusy(ctx, db.ConversationBusyParams{AccountID: row.AccountID, Stream: stream, JobID: row.ID})
		if err != nil {
			return err
		}
		if busy {
			deferred = true
			return q.ParkRestreamedJob(ctx, db.ParkRestreamedJobParams{ID: row.ID, Stream: stream})
		}
		return q.RestreamJob(ctx, db.RestreamJobParams{ID: row.ID, Stream: stream})
	})
	if err != nil {
		return identity.Source{}, err
	}
	if deferred {
		return source, delivery.ErrDeferred
	}
	return source, nil
}

func prepareSource(ctx context.Context, q *db.Queries, account string, m sdk.ChatHistoryMessage) (identity.Source, error) {
	if err := q.LockContactDirectory(ctx, account); err != nil {
		return identity.Source{}, err
	}
	rawSender := m.ChatID
	group := strings.HasSuffix(m.ChatID, "@g.us")
	if group {
		rawSender = m.Author
		if rawSender == "" {
			rawSender = m.From
		}
	}
	rawSender = identity.Normalize(rawSender)
	if !m.FromMe && m.SenderPhone != nil {
		phone, valid := identity.PhoneID(*m.SenderPhone)
		if !valid || !identity.IsPerson(rawSender) {
			return identity.Source{}, delivery.ErrInvalid
		}
		if strings.HasSuffix(rawSender, "@lid") {
			got, err := q.SaveContactAlias(ctx, db.SaveContactAliasParams{AccountID: account, Alias: rawSender, Canonical: phone})
			if err != nil {
				return identity.Source{}, err
			}
			if got != phone {
				return identity.Source{}, identity.ErrConflict
			}
		} else if rawSender != phone {
			return identity.Source{}, identity.ErrConflict
		}
	}
	d, err := directory(ctx, q, account)
	if err != nil {
		return identity.Source{}, err
	}
	sender := d.Canonical(rawSender)
	name := ""
	if m.Contact != nil {
		for _, value := range []string{m.Contact.Name, m.Contact.PushName, m.Contact.ShortName} {
			if value != "" {
				name = value
				break
			}
		}
	}
	if name != "" && identity.IsPerson(sender) {
		if err := q.SaveContactProfile(ctx, db.SaveContactProfileParams{AccountID: account, PersonID: sender, Name: name}); err != nil {
			return identity.Source{}, err
		}
		d, err = directory(ctx, q, account)
		if err != nil {
			return identity.Source{}, err
		}
	}
	chat, err := conversationKey(ctx, q, account, m.ChatID, d)
	if err != nil {
		return identity.Source{}, err
	}
	latest, err := q.LatestMessageEdit(ctx, db.LatestMessageEditParams{AccountID: account, WhatsappChat: chat, MessageID: m.ID})
	if err == nil {
		m.Body = latest.Body
		if latest.MentionedIds != nil && json.Unmarshal(latest.MentionedIds, &m.MentionedIDs) != nil {
			return identity.Source{}, delivery.ErrInvalid
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return identity.Source{}, err
	}
	source := identity.Source{ChatID: chat, RawChatID: m.ChatID, MessageID: m.ID, SenderID: sender, Name: d.SenderName(sender), Body: m.Body, Own: m.FromMe, MentionedIDs: m.MentionedIDs}
	if !m.FromMe {
		source.LinkSenderID = sender
	} else {
		self, err := q.SelfIdentity(ctx, account)
		if err == nil {
			source.LinkSenderID = self
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return identity.Source{}, err
		}
	}
	if m.QuotedMessage != nil {
		source.QuotedMessageID = m.QuotedMessage.ID
	}
	if !group {
		source.ConversationName = source.Name
	}
	return source, nil
}

// Call while holding the contact-directory lock when the result is used to
// create/update a route. Identity changes must not split conversation state.
func conversationKey(ctx context.Context, q *db.Queries, account, id string, d *identity.Directory) (string, error) {
	roots, err := q.ExistingConversations(ctx, db.ExistingConversationsParams{AccountID: account, Identities: d.Aliases(id)})
	if err != nil {
		return "", err
	}
	if len(roots) > 1 {
		return "", identity.ErrConflict
	}
	if len(roots) == 1 {
		return roots[0], nil
	}
	return d.Canonical(id), nil
}

func (s *Store) SelfIdentity(ctx context.Context, account string) (string, bool, error) {
	id, err := s.queries.SelfIdentity(ctx, account)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	return id, err == nil, err
}

// SaveSelfIdentity is called only after independently validating the native
// session response. Peer message fields can never establish self identity.
func (s *Store) SaveSelfIdentity(ctx context.Context, job delivery.Job, id string) error {
	verified, valid := identity.PhoneID(identity.Local(id))
	if !valid || verified != id {
		return delivery.ErrInvalid
	}
	return s.withClaim(ctx, job, func(q *db.Queries, row db.Job) error {
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		got, err := q.SaveSelfIdentity(ctx, db.SaveSelfIdentityParams{AccountID: row.AccountID, PersonID: id})
		if err != nil {
			return err
		}
		if got != id {
			return identity.ErrConflict
		}
		return nil
	})
}
