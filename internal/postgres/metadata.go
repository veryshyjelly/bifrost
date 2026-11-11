package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/identity"
	"github.com/veryshyjelly/bifrost/internal/mentions"
	"github.com/veryshyjelly/bifrost/internal/metadata"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
)

func metadataSource(row db.Job) (string, string, error) {
	var source struct {
		Type string `json:"type"`
		Data struct {
			Scope string `json:"scope"`
			ID    string `json:"id"`
		} `json:"data"`
	}
	if row.Kind != "management" && row.Kind != "sync-target" || json.Unmarshal(row.Payload, &source) != nil || source.Type != "sync_metadata" {
		return "", "", delivery.ErrInvalid
	}
	switch source.Data.Scope {
	case "group":
		if !strings.HasSuffix(source.Data.ID, "@g.us") {
			return "", "", delivery.ErrInvalid
		}
	case "contact":
		if !identity.IsPerson(source.Data.ID) {
			return "", "", delivery.ErrInvalid
		}
	default:
		return "", "", delivery.ErrUnsupported
	}
	return source.Data.Scope, source.Data.ID, nil
}

func metadataPlan(ctx context.Context, q *db.Queries, row db.Job) (metadata.Plan, bool, error) {
	payload, err := q.MetadataTextPlan(ctx, db.MetadataTextPlanParams{AccountID: row.AccountID, JobID: row.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return metadata.Plan{}, false, nil
	}
	if err != nil {
		return metadata.Plan{}, false, err
	}
	var p metadata.Plan
	if json.Unmarshal(payload, &p) != nil || p.JobID != row.ID || p.Key != row.Dedup {
		return p, false, delivery.ErrInvalid
	}
	return p, true, nil
}
func (s *Store) MetadataPlan(ctx context.Context, j delivery.Job) (metadata.Plan, bool, error) {
	var p metadata.Plan
	var found bool
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		var err error
		p, found, err = metadataPlan(ctx, q, row)
		return err
	})
	return p, found, err
}

func metadataTarget(ctx context.Context, q *db.Queries, row db.Job, session string, management int64) (metadata.Target, error) {
	scope, id, err := metadataSource(row)
	if err != nil {
		return metadata.Target{}, err
	}
	if session == "" || management >= 0 {
		return metadata.Target{}, delivery.ErrInvalid
	}
	t := metadata.Target{ID: id, Canonical: id, Session: session, Management: management}
	d, err := directory(ctx, q, row.AccountID)
	if err != nil {
		return t, err
	}
	ids := []string{id}
	if scope == "contact" {
		person := d.Person(id, nil)
		t.Canonical = person.ID
		if person.Registered {
			t.BotID = person.Bot.ID
		}
		// Person.Name is a display label and may fall back to a numeric LID.
		// Metadata fallback must use actual stored profile/route evidence.
		t.Name, err = metadataStoredName(ctx, q, row.AccountID, d, id, t.BotID)
		if err != nil {
			return t, err
		}
		for _, alias := range d.Aliases(id) {
			if alias != id {
				ids = append(ids, alias)
			}
		}
	}
	for _, key := range ids {
		r, err := q.ConversationRoute(ctx, db.ConversationRouteParams{AccountID: row.AccountID, WhatsappChat: key})
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return t, err
		}
		// Multiple independently routed aliases need explicit reconciliation.
		if t.Route != nil {
			return t, identity.ErrConflict
		}
		generation, err := q.MirrorReturnKey(ctx, db.MirrorReturnKeyParams{AccountID: row.AccountID, WhatsappChat: key})
		if err != nil {
			return t, err
		}
		t.Route = &metadata.Route{ID: key, Name: r.Name, ChatID: r.TelegramChatID, ThreadID: r.ThreadID, Retired: r.InboxRetired, ReturnKey: generation}
		if t.Name == "" {
			t.Name = r.Name
		}
		synced, err := q.MetadataTopicName(ctx, db.MetadataTopicNameParams{AccountID: row.AccountID, SessionID: session, SourceID: key, ChatID: r.TelegramChatID, ThreadID: r.ThreadID, ReturnKey: generation})
		if err == nil {
			t.Route.Name = synced
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return t, err
		}
	}
	if t.Route == nil && (scope == "group" || t.BotID == "") {
		return t, delivery.ErrInvalid
	}
	return t, nil
}

func metadataStoredName(ctx context.Context, q *db.Queries, account string, d *identity.Directory, id, botID string) (string, error) {
	preferred := []string{id, d.Canonical(id)}
	if botID != "" {
		bound, err := q.ContactForBot(ctx, db.ContactForBotParams{AccountID: account, BotID: botID})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return "", err
		}
		if err == nil {
			preferred = append([]string{bound}, preferred...)
		}
	}
	preferred = append(preferred, d.Aliases(id)...)
	profiles, err := q.IdentityProfiles(ctx, account)
	if err != nil {
		return "", err
	}
	for _, key := range preferred {
		for _, p := range profiles {
			if p.PersonID == key && strings.TrimSpace(p.Name) != "" {
				return p.Name, nil
			}
		}
	}
	return "", nil
}

func (s *Store) MetadataTarget(ctx context.Context, j delivery.Job, session string, management int64) (metadata.Target, error) {
	var target metadata.Target
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		var err error
		target, err = metadataTarget(ctx, q, row, session, management)
		return err
	})
	return target, err
}

// Names may change after snapshot admission; identity, destination and return
// generation must still match. Preferences do not disable an explicit refresh.
func sameMetadataTarget(a, b metadata.Target) bool {
	a.Name = ""
	b.Name = ""
	if a.Route != nil {
		r := *a.Route
		r.Name = ""
		a.Route = &r
	}
	if b.Route != nil {
		r := *b.Route
		r.Name = ""
		b.Route = &r
	}
	return reflect.DeepEqual(a, b)
}

func validMetadataChanges(t metadata.Target, snapshot metadata.Snapshot, changes []metadata.Change) bool {
	seen := map[string]bool{}
	for _, c := range changes {
		if seen[c.Kind] {
			return false
		}
		seen[c.Kind] = true
		switch c.Kind {
		case "topic":
			if t.Route == nil || t.Route.Retired || t.Route.ChatID != t.Management || t.Route.ThreadID <= 1 || c.BotID != "master" || c.ChatID != t.Route.ChatID || c.ThreadID != t.Route.ThreadID || c.Text != metadata.Clip(snapshot.Name, 128) {
				return false
			}
		case "title", "description":
			if snapshot.Scope != "group" || t.Route == nil || t.Route.ChatID == t.Management || t.Route.ChatID >= 0 || c.BotID != "master" || c.ChatID != t.Route.ChatID || c.ThreadID != 0 {
				return false
			}
			if c.Kind == "title" && c.Text != metadata.Clip(snapshot.Name, 128) {
				return false
			}
			if c.Kind == "description" && (snapshot.Description == nil || c.Text != metadata.Clip(*snapshot.Description, 255)) {
				return false
			}
		case "bot-name", "bot-description":
			if snapshot.Scope != "contact" || t.BotID == "" || c.BotID != t.BotID || c.ChatID != 0 || c.ThreadID != 0 {
				return false
			}
			expected := metadata.Clip(snapshot.Name, 64)
			if c.Kind == "bot-description" {
				expected = metadata.Clip("WhatsApp bridge for "+snapshot.Name+". Replies are sent through your WhatsApp account.", 512)
			}
			if c.Text != expected {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func (s *Store) PrepareMetadata(ctx context.Context, j delivery.Job, target metadata.Target, snapshot metadata.Snapshot, changes []metadata.Change, warning string) (metadata.Plan, error) {
	var result metadata.Plan
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		if p, found, err := metadataPlan(ctx, q, row); err != nil || found {
			result = p
			return err
		}
		scope, id, err := metadataSource(row)
		if err != nil {
			return err
		}
		if snapshot.ID != id || snapshot.Scope != scope || snapshot.FetchedAt <= 0 || strings.TrimSpace(snapshot.Name) == "" || !validMetadataChanges(target, snapshot, changes) {
			return delivery.ErrInvalid
		}
		if scope == "group" {
			if snapshot.Members == nil {
				return delivery.ErrInvalid
			}
			for _, m := range snapshot.Members {
				if !identity.IsPerson(m.ID) {
					return delivery.ErrInvalid
				}
			}
		} else if snapshot.Members != nil || snapshot.Description != nil {
			return delivery.ErrInvalid
		}
		current, err := metadataTarget(ctx, q, row, target.Session, target.Management)
		if err != nil {
			return err
		}
		if !sameMetadataTarget(target, current) {
			return metadata.ErrTarget
		}
		b, err := json.Marshal(snapshot)
		if err != nil {
			return err
		}
		rosterChanged := false
		if scope == "group" {
			old, err := q.MetadataSnapshot(ctx, db.MetadataSnapshotParams{AccountID: row.AccountID, SessionID: target.Session, SourceID: target.Canonical})
			if errors.Is(err, pgx.ErrNoRows) {
				rosterChanged = true
			} else if err != nil {
				return err
			} else {
				var prior metadata.Snapshot
				if json.Unmarshal(old.Payload, &prior) != nil {
					return delivery.ErrInvalid
				}
				d, err := directory(ctx, q, row.AccountID)
				if err != nil {
					return err
				}
				rosterChanged = !reflect.DeepEqual(metadataMemberKeys(d, prior.Members), metadataMemberKeys(d, snapshot.Members))
			}
		}
		n, err := q.SaveMetadataSnapshot(ctx, db.SaveMetadataSnapshotParams{AccountID: row.AccountID, SessionID: target.Session, SourceID: target.Canonical, JobID: row.ID, FetchedAt: snapshot.FetchedAt, Payload: b})
		if err != nil {
			return err
		}
		if n != 1 {
			return metadata.ErrTarget
		}
		if scope == "group" {
			roster := mentions.Roster{ChatID: id, Name: snapshot.Name, Members: snapshot.Members, FetchedAt: snapshot.FetchedAt}
			b, _ := json.Marshal(roster)
			if err := q.SaveMentionRoster(ctx, db.SaveMentionRosterParams{AccountID: row.AccountID, ChatID: id, Payload: b, FetchedAt: snapshot.FetchedAt}); err != nil {
				return err
			}
		} else {
			d, err := directory(ctx, q, row.AccountID)
			if err != nil {
				return err
			}
			for _, alias := range d.Aliases(id) {
				if err := q.SaveContactProfile(ctx, db.SaveContactProfileParams{AccountID: row.AccountID, PersonID: alias, Name: snapshot.Name, MetadataSynced: true}); err != nil {
					return err
				}
			}
		}
		if r := target.Route; r != nil {
			if r.ChatID == target.Management && r.ThreadID > 1 {
				if err := q.RememberMetadataTopicName(ctx, db.RememberMetadataTopicNameParams{AccountID: row.AccountID, SessionID: target.Session, SourceID: r.ID, ChatID: r.ChatID, ThreadID: r.ThreadID, ReturnKey: r.ReturnKey, Name: r.Name}); err != nil {
					return err
				}
			}
			if err := q.RenameMetadataRoute(ctx, db.RenameMetadataRouteParams{AccountID: row.AccountID, WhatsappChat: r.ID, Name: snapshot.Name}); err != nil {
				return err
			}
		}
		result = metadata.Plan{Key: row.Dedup, JobID: row.ID, Target: target, Snapshot: snapshot, Changes: changes, Warning: warning, RosterChanged: rosterChanged}
		b, err = json.Marshal(result)
		if err != nil {
			return err
		}
		return q.CreateMetadataTextPlan(ctx, db.CreateMetadataTextPlanParams{AccountID: row.AccountID, JobID: row.ID, Payload: b})
	})
	return result, err
}

func metadataCurrent(ctx context.Context, q *db.Queries, row db.Job, p metadata.Plan) (bool, error) {
	target, err := metadataTarget(ctx, q, row, p.Target.Session, p.Target.Management)
	if errors.Is(err, delivery.ErrInvalid) || errors.Is(err, identity.ErrConflict) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !sameMetadataTarget(target, p.Target) {
		return false, nil
	}
	snapshot, err := q.MetadataSnapshot(ctx, db.MetadataSnapshotParams{AccountID: row.AccountID, SessionID: p.Target.Session, SourceID: p.Target.Canonical})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return snapshot.JobID == row.ID, nil
}
func (s *Store) MetadataTextCurrent(ctx context.Context, j delivery.Job) (bool, error) {
	current := false
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		p, found, err := metadataPlan(ctx, q, row)
		if err != nil {
			return err
		}
		if !found {
			return delivery.ErrInvalid
		}
		current, err = metadataCurrent(ctx, q, row, p)
		return err
	})
	return current, err
}

func (s *Store) CompleteMetadataText(ctx context.Context, j delivery.Job) error {
	return s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		p, found, err := metadataPlan(ctx, q, row)
		if err != nil {
			return err
		}
		if !found {
			return delivery.ErrInvalid
		}
		if p.TextComplete {
			return nil
		}
		current, err := metadataCurrent(ctx, q, row, p)
		if err != nil {
			return err
		}
		if !current {
			return metadata.ErrTarget
		}
		for i, c := range p.Changes {
			effect, err := q.GetEffect(ctx, db.GetEffectParams{AccountID: row.AccountID, EffectKey: p.ChangeKey(i)})
			if err != nil {
				return err
			}
			if effect.State == "discarded" && p.TextRefusals[i] != "" {
				continue
			}
			if effect.State != "done" || !bytes.Equal(bytes.TrimSpace(effect.Result), []byte("true")) {
				return delivery.ErrIncomplete
			}
			var actual struct {
				Session string
				Target  metadata.Target
				Change  metadata.Change
			}
			if json.Unmarshal(effect.Request, &actual) != nil || actual.Session != p.Target.Session || !reflect.DeepEqual(actual.Target, p.Target) || actual.Change != c {
				return delivery.ErrInvalid
			}
			if c.Kind == "topic" {
				r := p.Target.Route
				if err := q.SetMetadataTopicName(ctx, db.SetMetadataTopicNameParams{AccountID: row.AccountID, SessionID: p.Target.Session, SourceID: r.ID, ChatID: r.ChatID, ThreadID: r.ThreadID, ReturnKey: r.ReturnKey, Name: p.Snapshot.Name}); err != nil {
					return err
				}
			}
		}
		p.TextComplete = true
		b, err := json.Marshal(p)
		if err != nil {
			return err
		}
		return q.SaveMetadataTextPlan(ctx, db.SaveMetadataTextPlanParams{AccountID: row.AccountID, JobID: row.ID, Payload: b})
	})
}

var _ metadata.Store = (*Store)(nil)

func (s *Store) RefuseMetadataText(ctx context.Context, j delivery.Job, part int, warning string) error {
	if warning == "" {
		return delivery.ErrInvalid
	}
	return s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		p, found, err := metadataPlan(ctx, q, row)
		if err != nil {
			return err
		}
		if !found || part < 0 || part >= len(p.Changes) {
			return delivery.ErrInvalid
		}
		effect, err := q.GetEffect(ctx, db.GetEffectParams{AccountID: row.AccountID, EffectKey: p.ChangeKey(part)})
		if err != nil {
			return err
		}
		if effect.JobID != row.ID {
			return delivery.ErrInvalid
		}
		if effect.State == "discarded" && p.TextRefusals[part] != "" {
			return nil
		}
		if effect.State != "pending" {
			return delivery.ErrUncertain
		}
		if _, err := q.DiscardEffect(ctx, db.DiscardEffectParams{AccountID: row.AccountID, JobID: row.ID, EffectKey: effect.EffectKey}); err != nil {
			return err
		}
		if p.TextRefusals == nil {
			p.TextRefusals = map[int]string{}
		}
		p.TextRefusals[part] = warning
		raw, err := json.Marshal(p)
		if err != nil {
			return err
		}
		return q.SaveMetadataTextPlan(ctx, db.SaveMetadataTextPlanParams{AccountID: row.AccountID, JobID: row.ID, Payload: raw})
	})
}

func metadataMemberKeys(d *identity.Directory, members []identity.Member) []string {
	keys := make([]string, 0, len(members))
	for _, m := range members {
		keys = append(keys, d.Canonical(m.ID)+":"+strconv.FormatBool(m.IsAdmin)+":"+strconv.FormatBool(m.IsSuperAdmin))
	}
	sort.Strings(keys)
	return keys
}
