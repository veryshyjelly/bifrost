package postgres

import (
	"context"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/conversation"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
	"github.com/veryshyjelly/bifrost/internal/relay"
)

func nativeRepairRoute(ctx context.Context, q *db.Queries, row db.Job, p relay.NativePlan) error {
	r, err := q.ConversationRoute(ctx, db.ConversationRouteParams{AccountID: row.AccountID, WhatsappChat: p.WhatsAppChat})
	if err != nil {
		return err
	}
	key, err := q.MirrorReturnKey(ctx, db.MirrorReturnKeyParams{AccountID: row.AccountID, WhatsappChat: p.WhatsAppChat})
	if err != nil {
		return err
	}
	if r.TelegramChatID != p.Route.TelegramChatID || r.ThreadID != p.Route.ThreadID || r.InboxRetired != p.Route.InboxRetired || r.Mode != string(p.Route.Mode) || key != p.ReturnKey {
		return relay.ErrNativeRepairUnsafe
	}
	busy, err := q.RouteTransitionBusy(ctx, db.RouteTransitionBusyParams{AccountID: row.AccountID, WhatsappChat: p.WhatsAppChat, JobID: row.ID})
	if err != nil {
		return err
	}
	if busy {
		return delivery.ErrLocalRetry
	}
	active, err := q.ActiveHistory(ctx, db.ActiveHistoryParams{AccountID: row.AccountID, WhatsappChat: p.WhatsAppChat})
	if err != nil {
		return err
	}
	if active {
		return delivery.ErrLocalRetry
	}
	return nil
}

// nativeRepairUnsent rejects even confirmed effects whose mapping transaction
// has not committed yet. Source mappings from another plan also prevent a move.
func nativeRepairUnsent(ctx context.Context, q *db.Queries, row db.Job, p relay.NativePlan, discard bool) error {
	mapped, err := q.NativeSourceMapped(ctx, db.NativeSourceMappedParams{AccountID: row.AccountID, WhatsappChat: p.WhatsAppChat, WhatsappMessageID: p.WhatsAppMessage})
	if err != nil {
		return err
	}
	if mapped {
		return relay.ErrNativeRepairUnsafe
	}
	effects, err := q.MetadataJobEffects(ctx, db.MetadataJobEffectsParams{AccountID: row.AccountID, JobID: row.ID})
	if err != nil {
		return err
	}
	for _, effect := range effects {
		if !strings.HasPrefix(effect.EffectKey, p.Prefix+":packet:") {
			continue
		}
		if effect.State != "pending" && effect.State != "discarded" {
			return relay.ErrNativeRepairUnsafe
		}
	}
	if discard {
		for _, effect := range effects {
			if strings.HasPrefix(effect.EffectKey, p.Prefix+":packet:") && effect.State == "pending" {
				n, err := q.DiscardEffect(ctx, db.DiscardEffectParams{AccountID: row.AccountID, JobID: row.ID, EffectKey: effect.EffectKey})
				if err != nil {
					return err
				}
				if n != 1 {
					return delivery.ErrClaimLost
				}
			}
		}
	}
	return nil
}

func (s *Store) BeginNativeRepair(ctx context.Context, j delivery.Job, key string, r relay.NativeRepair) (relay.NativePlan, error) {
	var result relay.NativePlan
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		owner, p, err := nativePlan(ctx, q, row.AccountID, key)
		if err != nil {
			return err
		}
		if owner.JobID != row.ID || row.Kind != "wa" || row.Stream != "wa:"+p.WhatsAppChat || p.Origin != "live" || p.Complete || p.Content == nil {
			return relay.ErrNativeRepairUnsafe
		}
		if p.Repair != nil {
			result = p
			return nil
		}
		if p.Repairs >= 2 || r.Prefix != row.Dedup+":native:repair:"+strconv.Itoa(p.Repairs+1) || r.Management >= 0 {
			return delivery.ErrInvalid
		}
		if err := nativeRepairRoute(ctx, q, row, p); err != nil {
			return err
		}
		if err := nativeRepairUnsent(ctx, q, row, p, true); err != nil {
			return err
		}
		switch r.Reason {
		case "inbox_required":
			if !p.Route.InboxRetired || p.BotID != "master" || !r.CreateTopic {
				return delivery.ErrInvalid
			}
		case "member_forbidden":
			if p.BotID == "master" || r.Target.BotID != "master" || r.CreateTopic != p.Route.InboxRetired {
				return delivery.ErrInvalid
			}
			if !r.CreateTopic && (r.Target.ChatID != p.Route.TelegramChatID || r.Target.ThreadID != p.Route.ThreadID) {
				return delivery.ErrInvalid
			}
		case "thread_missing":
			if p.BotID != "master" || p.TelegramChat != r.Management || p.Route.TelegramChatID != r.Management || p.ThreadID <= 0 {
				return delivery.ErrInvalid
			}
			if !r.CreateTopic {
				if p.Route.Mode != conversation.Dedicated || r.Target.BotID == "master" || r.Target.ChatID != p.OwnerID || r.Target.ThreadID != 0 {
					return delivery.ErrInvalid
				}
				d, err := directory(ctx, q, row.AccountID)
				if err != nil {
					return err
				}
				person := d.Person(p.Source.SenderID, nil)
				if !person.Registered || !person.Bot.Started || person.Bot.ID != r.Target.BotID {
					return delivery.ErrLocalRetry
				}
			}
		default:
			return delivery.ErrInvalid
		}
		if r.CreateTopic {
			if r.Target.BotID != "master" || r.Target.ChatID != r.Management || r.Target.ThreadID != 0 || p.Route.TelegramChatID != r.Management || r.TopicName != conversation.TopicName(p.Route.Name, p.WhatsAppChat) || r.TopicKey != r.Prefix+":topic" {
				return delivery.ErrInvalid
			}
			if err := q.LockRouteTransition(ctx, db.LockRouteTransitionParams{AccountID: row.AccountID, WhatsappChat: p.WhatsAppChat, JobID: row.ID}); err != nil {
				return err
			}
		}
		p.Repair = &r
		p.Repairs++
		raw, _ := json.Marshal(p)
		if err := q.UpdateNativePlan(ctx, db.UpdateNativePlanParams{AccountID: row.AccountID, PlanKey: p.Key, Payload: raw}); err != nil {
			return err
		}
		result = p
		return nil
	})
	return result, err
}

func (s *Store) CommitNativeRepair(ctx context.Context, j delivery.Job, candidate relay.NativePlan) (relay.NativePlan, error) {
	var result relay.NativePlan
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		owner, p, err := nativePlan(ctx, q, row.AccountID, candidate.Key)
		if err != nil {
			return err
		}
		if owner.JobID != row.ID {
			return delivery.ErrInvalid
		}
		if p.Repair == nil {
			if p.Prefix != candidate.Prefix {
				return delivery.ErrInvalid
			}
			result = p
			return nil
		}
		r := *p.Repair
		previousReturn := p.ReturnKey
		if err := nativeRepairRoute(ctx, q, row, p); err != nil {
			return err
		}
		if err := nativeRepairUnsent(ctx, q, row, p, false); err != nil {
			return err
		}
		if candidate.Repair == nil || !reflect.DeepEqual(candidate.Repair, p.Repair) || candidate.Prefix != r.Prefix || candidate.Repairs != p.Repairs || !reflect.DeepEqual(candidate.Content, p.Content) || !reflect.DeepEqual(candidate.Source, p.Source) || !reflect.DeepEqual(candidate.Message, p.Message) || !reflect.DeepEqual(candidate.SourceAsset, p.SourceAsset) || !reflect.DeepEqual(candidate.PreparedUpload, p.PreparedUpload) || candidate.PreparedKind != p.PreparedKind || candidate.MediaNotice != p.MediaNotice || !reflect.DeepEqual(candidate.PollChoices, p.PollChoices) {
			return delivery.ErrInvalid
		}
		target := r.Target
		if r.CreateTopic {
			effect, err := q.GetEffect(ctx, db.GetEffectParams{AccountID: row.AccountID, EffectKey: r.TopicKey})
			if err != nil {
				return err
			}
			var topic models.ForumTopic
			if effect.JobID != row.ID || effect.State != "done" || json.Unmarshal(effect.Result, &topic) != nil || topic.MessageThreadID <= 1 {
				return delivery.ErrUncertain
			}
			target.ThreadID = int64(topic.MessageThreadID)
			reserved, err := q.ReservedTopic(ctx, db.ReservedTopicParams{AccountID: row.AccountID, TelegramChatID: target.ChatID, ThreadID: target.ThreadID})
			if err != nil {
				return err
			}
			if reserved {
				return delivery.ErrInvalid
			}
			other, err := q.OtherTopicRoute(ctx, db.OtherTopicRouteParams{AccountID: row.AccountID, TelegramChatID: target.ChatID, ThreadID: target.ThreadID, WhatsappChat: p.WhatsAppChat})
			if err != nil {
				return err
			}
			if other {
				return delivery.ErrInvalid
			}
			if err := q.RestoreNativeInbox(ctx, db.RestoreNativeInboxParams{AccountID: row.AccountID, WhatsappChat: p.WhatsAppChat, ThreadID: target.ThreadID}); err != nil {
				return err
			}
			p.Route.ThreadID, p.Route.InboxRetired = target.ThreadID, false
			p.ReturnKey = r.TopicKey
			if err := q.SaveMirrorReturn(ctx, db.SaveMirrorReturnParams{AccountID: row.AccountID, WhatsappChat: p.WhatsAppChat, ReturnKey: p.ReturnKey}); err != nil {
				return err
			}
			if err := q.RememberInbox(ctx, db.RememberInboxParams{AccountID: row.AccountID, WhatsappChat: p.WhatsAppChat, TelegramChatID: target.ChatID, ThreadID: target.ThreadID}); err != nil {
				return err
			}
			payload, _ := json.Marshal(map[string]any{"id": r.TopicKey, "type": "new_conversation", "data": map[string]any{"chatId": p.WhatsAppChat, "tgChat": target.ChatID, "thread": target.ThreadID, "returnKey": p.ReturnKey}, "at": time.Now().UnixMilli()})
			if err := enqueueTransitionEvent(ctx, q, row.AccountID, r.TopicKey, payload); err != nil {
				return err
			}
			if err := q.UnlockRouteTransition(ctx, db.UnlockRouteTransitionParams{AccountID: row.AccountID, WhatsappChat: p.WhatsAppChat, JobID: row.ID}); err != nil {
				return err
			}
		}
		if candidate.BotID != target.BotID || candidate.TelegramChat != target.ChatID || candidate.ThreadID != target.ThreadID || !reflect.DeepEqual(candidate.Route, p.Route) || len(candidate.Packets) == 0 {
			return delivery.ErrInvalid
		}
		if target.BotID != "master" {
			d, err := directory(ctx, q, row.AccountID)
			if err != nil {
				return err
			}
			person := d.Person(p.Source.SenderID, nil)
			if !person.Registered || !person.Bot.Started || person.Bot.ID != target.BotID {
				return delivery.ErrLocalRetry
			}
		}
		// Rendering may change the destination, attribution bot, quote and packet
		// shape. Every other frozen input remains byte-for-byte semantically equal.
		expected := p
		expected.BotID, expected.TelegramChat, expected.ThreadID = target.BotID, target.ChatID, target.ThreadID
		expected.Prefix, expected.Packets, expected.ReturnKey = r.Prefix, candidate.Packets, previousReturn
		expected.Presentation.Bot = target.BotID
		if !reflect.DeepEqual(candidate, expected) {
			return delivery.ErrInvalid
		}
		candidate.ReturnKey = p.ReturnKey
		candidate.Repair = nil
		for _, packet := range candidate.Packets {
			if !nativePacketValid(candidate, packet) {
				return delivery.ErrInvalid
			}
		}
		// These controls have not been exposed: no packet receipt exists.
		rawTarget, _ := json.Marshal(target)
		if err := q.RetargetNativePollAction(ctx, db.RetargetNativePollActionParams{AccountID: row.AccountID, PlanKey: p.Key, Target: rawTarget}); err != nil {
			return err
		}
		raw, _ := json.Marshal(candidate)
		if err := q.UpdateNativePlan(ctx, db.UpdateNativePlanParams{AccountID: row.AccountID, PlanKey: p.Key, Payload: raw}); err != nil {
			return err
		}
		result = candidate
		return nil
	})
	return result, err
}

var _ relay.NativeRepairStore = (*Store)(nil)
