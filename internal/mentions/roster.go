package mentions

import (
	"context"
	"errors"
	"strings"

	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/identity"
)

// Roster may expose stale cached members to a picker, but never validates an
// outgoing send with stale data after a failed provider refresh.
func (s *Service) Roster(ctx context.Context, job delivery.Job, chat string, allowCached bool) (Roster, error) {
	if !strings.HasSuffix(chat, "@g.us") {
		return Roster{}, delivery.ErrInvalid
	}
	old, found, err := s.store.MentionRoster(ctx, job.AccountID, chat)
	if err != nil {
		return Roster{}, localError(err)
	}
	now := s.now().UnixMilli()
	age := now - old.FetchedAt
	if found && age >= 0 && age < 300000 {
		old.Cached = false
		return old, nil
	}
	info, err := s.wa.Group(ctx, s.session, chat)
	if err == nil && (info == nil || info.ID != chat || strings.TrimSpace(info.Name) == "" || info.Participants == nil) {
		err = ErrRoster
	}
	result := Roster{ChatID: chat, FetchedAt: now}
	if err == nil {
		result.Name = info.Name
		for _, m := range info.Participants {
			if !identity.IsPerson(m.ID) {
				err = ErrRoster
				break
			}
			result.Members = append(result.Members, identity.Member{ID: m.ID, Name: m.Name, IsAdmin: m.IsAdmin, IsSuperAdmin: m.IsSuperAdmin})
		}
	}
	if err != nil {
		if ctx.Err() != nil {
			return Roster{}, ctx.Err()
		}
		if found && allowCached {
			old.Cached = true
			return old, nil
		}
		return Roster{}, ErrRoster
	}
	if err := s.store.SaveMentionRoster(ctx, job, result); err != nil {
		if errors.Is(err, delivery.ErrClaimLost) {
			return Roster{}, err
		}
		return Roster{}, localError(err)
	}
	return result, nil
}
