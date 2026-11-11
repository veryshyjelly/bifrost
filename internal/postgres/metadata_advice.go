package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/metadata"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
)

func metadataInspection(ctx context.Context, q *db.Queries, row db.Job) (metadata.Inspection, bool, error) {
	raw, err := q.MetadataInspection(ctx, db.MetadataInspectionParams{AccountID: row.AccountID, JobID: row.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return metadata.Inspection{}, false, nil
	}
	if err != nil {
		return metadata.Inspection{}, false, err
	}
	var result metadata.Inspection
	if json.Unmarshal(raw, &result) != nil || result.AccountID != row.AccountID || result.JobID != row.ID {
		return result, false, delivery.ErrInvalid
	}
	return result, true, nil
}
func (s *Store) MetadataInspection(ctx context.Context, j delivery.Job) (metadata.Inspection, bool, error) {
	var result metadata.Inspection
	var found bool
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		var err error
		result, found, err = metadataInspection(ctx, q, row)
		return err
	})
	return result, found, err
}

func metadataAdviceContext(ctx context.Context, q *db.Queries, row db.Job) (metadata.AdviceContext, error) {
	result := metadata.AdviceContext{}
	p, found, err := metadataPlan(ctx, q, row)
	if err != nil {
		return result, err
	}
	if !found || !p.TextComplete {
		return result, delivery.ErrIncomplete
	}
	photo, found, err := metadataPicturePlan(ctx, q, row)
	if err != nil {
		return result, err
	}
	if !found || !photo.Complete {
		return result, delivery.ErrIncomplete
	}
	current, err := metadataCurrent(ctx, q, row, p)
	if err != nil {
		return result, err
	}
	if !current {
		return result, metadata.ErrTarget
	}
	result.Plan = p
	result.PermissionNeeded = p.Warning != "" || photo.Skip == "permissions"
	if p.Snapshot.Scope != "group" || p.Target.Route == nil || p.Target.Route.ChatID == p.Target.Management {
		return result, nil
	}
	d, err := directory(ctx, q, row.AccountID)
	if err != nil {
		return result, err
	}
	for _, member := range p.Snapshot.Members {
		person := d.Person(member.ID, nil)
		if !person.Registered || person.Bot.Username == "" {
			continue
		}
		name, err := metadataStoredName(ctx, q, row.AccountID, d, member.ID, person.Bot.ID)
		if err != nil {
			return result, err
		}
		if name == "" {
			name = person.Bot.Username
		}
		result.Candidates = append(result.Candidates, metadata.BotAdvice{PersonID: member.ID, ID: person.Bot.ID, Username: person.Bot.Username, Name: name})
	}
	return result, nil
}
func (s *Store) MetadataAdviceContext(ctx context.Context, j delivery.Job) (metadata.AdviceContext, error) {
	var result metadata.AdviceContext
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		var err error
		result, err = metadataAdviceContext(ctx, q, row)
		return err
	})
	return result, err
}
func (s *Store) PrepareMetadataInspection(ctx context.Context, j delivery.Job, in metadata.Inspection, c control.SyncSettings) (metadata.Inspection, error) {
	if !validSyncSettings(c) {
		return metadata.Inspection{}, delivery.ErrInvalid
	}
	var result metadata.Inspection
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		if old, found, err := metadataInspection(ctx, q, row); err != nil || found {
			result = old
			return err
		}
		context, err := metadataAdviceContext(ctx, q, row)
		if err != nil {
			return err
		}
		if in.AccountID != row.AccountID || in.JobID != row.ID || in.OwnerID != c.OwnerID || in.Session != c.Session || context.Plan.Target.Session != c.Session || context.Plan.Target.Management != c.Management || !reflect.DeepEqual(in.Target, context.Plan.Target) || in.Permission != nil || in.PermissionLink != "" {
			return delivery.ErrInvalid
		}
		allowed := append([]metadata.BotAdvice(nil), context.Candidates...)
		if !reflect.DeepEqual(in.Candidates, context.Candidates) {
			return delivery.ErrLocalRetry
		}
		for _, missing := range in.Missing {
			found := -1
			for i, candidate := range allowed {
				if candidate == missing {
					found = i
					break
				}
			}
			if found < 0 {
				return delivery.ErrLocalRetry
			}
			allowed = append(allowed[:found], allowed[found+1:]...)
		}
		if in.PermissionNeeded {
			r := in.Target.Route
			if r == nil || r.ChatID >= 0 || r.ChatID == c.Management || r.ThreadID != 0 || !strings.HasSuffix(in.Target.ID, "@g.us") || in.MasterUsername == "" {
				return delivery.ErrInvalid
			}
			permission := control.SyncPermission{ID: groupToken(), AccountID: row.AccountID, Session: c.Session, OwnerID: c.OwnerID, WhatsAppChat: r.ID, ChatID: r.ChatID, ReturnKey: r.ReturnKey, ExpiresAt: time.Now().Add(24 * time.Hour).UnixMilli()}
			b, _ := json.Marshal(permission)
			if err := q.CreateMetadataPermission(ctx, db.CreateMetadataPermissionParams{AccountID: row.AccountID, ActionID: permission.ID, Payload: b}); err != nil {
				return err
			}
			in.Permission = &permission
			in.PermissionLink = metadata.PermissionLink(in.MasterUsername, permission.ID)
		}
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		if err := q.CreateMetadataInspection(ctx, db.CreateMetadataInspectionParams{AccountID: row.AccountID, JobID: row.ID, Payload: b}); err != nil {
			return err
		}
		result = in
		return nil
	})
	return result, err
}

var _ metadata.AdviceStore = (*Store)(nil)
