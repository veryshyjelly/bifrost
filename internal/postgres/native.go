package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/go-telegram/bot/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/veryshyjelly/bifrost/internal/attachments"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/history"
	"github.com/veryshyjelly/bifrost/internal/identity"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
	"github.com/veryshyjelly/bifrost/internal/relay"
)

func nativePlan(ctx context.Context, q *db.Queries, account, key string) (db.RelayPlan, relay.NativePlan, error) {
	row, err := q.NativeDeliveryPlan(ctx, db.NativeDeliveryPlanParams{AccountID: account, PlanKey: key})
	if err != nil {
		return row, relay.NativePlan{}, err
	}
	var p relay.NativePlan
	if json.Unmarshal(row.Payload, &p) != nil || p.Key != key || p.Format != "native-packets-v1" || p.Direction != "in" {
		return row, p, delivery.ErrInvalid
	}
	p.Complete = row.Complete
	return row, p, nil
}
func (s *Store) NativePlan(ctx context.Context, account, key string) (relay.NativePlan, bool, error) {
	_, p, err := nativePlan(ctx, s.queries, account, key)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, false, nil
	}
	return p, err == nil, err
}
func nativePacketValid(p relay.NativePlan, v relay.NativePacket) bool {
	if v.Target.ChatID != p.TelegramChat || v.Target.ThreadID != p.ThreadID || v.Target.BotID == "" || v.ReplyTo < 0 {
		return false
	}
	if v.Target.BotID != p.BotID && !(v.NoMapping && p.Origin == "history" && p.Day != "" && v.Kind == "text" && v.Target.BotID == "master") {
		return false
	}
	if v.NoMapping && (v.Kind != "text" || p.Origin != "history" || p.Day == "") {
		return false
	}
	switch v.Kind {
	case "text":
		if v.Body.Text == "" {
			return false
		}
	case "photo", "video", "animation", "audio", "voice", "document", "sticker":
		if v.Asset == nil || v.Asset.Hash == "" || v.Asset.Size <= 0 {
			return false
		}
	case "location":
		if v.Location == nil {
			return false
		}
	case "contact":
		if v.Contact == nil || v.Contact.Name == "" || v.Contact.Number == "" {
			return false
		}
	default:
		return false
	}
	if v.Fallback != nil {
		copy := *v.Fallback
		if copy.Fallback != nil || copy.FallbackLabel != nil || copy.Kind != "document" || !nativePacketValid(p, copy) {
			return false
		}
	}
	if v.FallbackLabel != nil {
		copy := *v.FallbackLabel
		if copy.Kind != "text" || copy.Fallback != nil || copy.FallbackLabel != nil || !nativePacketValid(p, copy) {
			return false
		}
	}
	return true
}
func prepareNativeScope(ctx context.Context, q *db.Queries, row db.Job, p *relay.NativePlan) error {
	if p.Session == "" || p.Source.ChatID != p.WhatsAppChat || p.Source.MessageID != p.WhatsAppMessage || p.Message.ID != p.WhatsAppMessage {
		return delivery.ErrInvalid
	}
	if p.Origin == "status" {
		return prepareNativeStatusScope(ctx, q, row, p)
	}
	if p.Origin == "live" {
		if row.Kind != "wa" || row.Stream != "wa:"+p.WhatsAppChat || p.Key != relay.NativeIncomingKey(p.WhatsAppChat, p.WhatsAppMessage) {
			return delivery.ErrInvalid
		}
		source, err := q.WhatsAppSource(ctx, db.WhatsAppSourceParams{AccountID: row.AccountID, JobID: row.ID})
		if err != nil {
			return err
		}
		var frozen identity.Source
		if json.Unmarshal(source.Payload, &frozen) != nil || !reflect.DeepEqual(frozen, p.Source) {
			return delivery.ErrInvalid
		}
		decoded, err := identity.DecodeSource(jobFrom(row))
		if err != nil || decoded.SessionID != p.Session || decoded.Message.ID != p.Message.ID || decoded.Message.Type != p.Message.Type {
			return delivery.ErrInvalid
		}
	} else if p.Origin == "history" {
		if row.Kind != "history-message" {
			return delivery.ErrInvalid
		}
		run, err := loadHistory(ctx, q, row, history.Settings{Session: p.Session})
		if err != nil {
			return err
		}
		stored, item, err := loadHistoryItem(ctx, q, row)
		if err != nil {
			return err
		}
		if stored.RunID != p.HistoryRun || run.State != "delivering" || item.State != "pending" || run.Mode != p.HistoryMode || run.Target.ChatID != p.TelegramChat || run.Target.ThreadID != p.ThreadID || !reflect.DeepEqual(item.Source, p.Source) {
			return delivery.ErrInvalid
		}
		if run.Target.BotID != "" && run.Target.BotID != p.BotID {
			return delivery.ErrInvalid
		}
	} else {
		return delivery.ErrUnsupported
	}
	route, err := q.ConversationRoute(ctx, db.ConversationRouteParams{AccountID: row.AccountID, WhatsappChat: p.WhatsAppChat})
	if err != nil {
		return err
	}
	if route.TelegramChatID != p.Route.TelegramChatID || route.ThreadID != p.Route.ThreadID || route.InboxRetired != p.Route.InboxRetired || (p.Origin == "live" && (route.Mode != string(p.Route.Mode) || route.Mode == "muted" || route.Mode == "ignored")) {
		return delivery.ErrLocalRetry
	}
	key, err := q.MirrorReturnKey(ctx, db.MirrorReturnKeyParams{AccountID: row.AccountID, WhatsappChat: p.WhatsAppChat})
	if err != nil {
		return err
	}
	if p.ReturnKey != "" && p.ReturnKey != key {
		return delivery.ErrLocalRetry
	}
	p.ReturnKey = key
	reserved, err := q.ReservedTopic(ctx, db.ReservedTopicParams{AccountID: row.AccountID, TelegramChatID: p.TelegramChat, ThreadID: p.ThreadID})
	if err != nil {
		return err
	}
	if reserved {
		return delivery.ErrLocalRetry
	}
	if p.BotID != "master" {
		d, err := directory(ctx, q, row.AccountID)
		if err != nil {
			return err
		}
		person := d.Person(p.Source.SenderID, nil)
		if p.Origin == "history" && p.TelegramChat > 0 {
			person = d.Person(p.WhatsAppChat, nil)
		}
		if !person.Registered || person.Bot.ID != p.BotID || (p.TelegramChat > 0 && !person.Bot.Started) {
			return delivery.ErrLocalRetry
		}
	}
	return nil
}
func (s *Store) PrepareNative(ctx context.Context, j delivery.Job, p relay.NativePlan) (relay.NativePlan, error) {
	var result relay.NativePlan
	if p.Key == "" || p.Prefix != j.Dedup+":native" || p.Direction != "in" || p.Format != "native-packets-v1" || p.BotID == "" || p.TelegramChat == 0 || p.ThreadID < 0 || p.OwnerID <= 0 || p.ControlTTL < 5 || p.ControlTTL > 86400 || len(p.Packets) == 0 {
		return result, delivery.ErrInvalid
	}
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		if owner, old, err := nativePlan(ctx, q, row.AccountID, p.Key); err == nil {
			if owner.JobID != row.ID {
				return delivery.ErrInvalid
			}
			result = old
			return nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err := prepareNativeScope(ctx, q, row, &p); err != nil {
			return err
		}
		for _, packet := range p.Packets {
			if !nativePacketValid(p, packet) {
				return delivery.ErrInvalid
			}
		}
		if len(p.PollChoices) > 0 {
			if p.Message.Poll == nil || len(p.PollChoices) > 12 {
				return delivery.ErrInvalid
			}
			var buttons [][]models.InlineKeyboardButton
			for index := range p.PollChoices {
				choice := &p.PollChoices[index]
				choice.ID = groupToken()
				text := []rune(choice.Value)
				if len(text) > 64 {
					text = text[:64]
				}
				buttons = append(buttons, []models.InlineKeyboardButton{{Text: string(text), CallbackData: "vote:" + choice.ID}})
			}
			for index := range p.Packets {
				if p.Packets[index].Kind == "text" && !p.Packets[index].NoMapping {
					p.Packets[index].Markup = &models.InlineKeyboardMarkup{InlineKeyboard: buttons}
				}
			}
		}
		raw, err := json.Marshal(p)
		if err != nil {
			return err
		}
		if err := q.PrepareRelay(ctx, db.PrepareRelayParams{AccountID: row.AccountID, PlanKey: p.Key, JobID: row.ID, Direction: "in", WhatsappChat: p.WhatsAppChat, Payload: raw}); err != nil {
			return err
		}
		if p.SourceAsset != nil {
			asset, _ := json.Marshal(p.SourceAsset)
			if err := q.SaveArchivedAsset(ctx, db.SaveArchivedAssetParams{AccountID: row.AccountID, WhatsappChat: p.WhatsAppChat, MessageID: p.WhatsAppMessage, Asset: asset}); err != nil {
				return err
			}
		}
		for _, choice := range p.PollChoices {
			action := relay.PollAction{ID: choice.ID, Session: p.Session, ChatID: p.WhatsAppChat, MessageID: p.WhatsAppMessage, Choice: choice.Value, Target: control.Target{BotID: p.BotID, ChatID: p.TelegramChat, ThreadID: p.ThreadID}, Multiple: p.Message.Poll.AllowMultipleAnswers, ExpiresAt: time.Now().Add(time.Duration(p.ControlTTL) * time.Second).UnixMilli()}
			// Owner is copied from native admission/runtime identity below by the
			// composition's fixed owner rather than inferred from a message sender.
			action.OwnerID = p.OwnerID
			raw, _ := json.Marshal(action)
			if err := q.CreateNativePollAction(ctx, db.CreateNativePollActionParams{AccountID: row.AccountID, ActionID: action.ID, PlanKey: p.Key, Payload: raw}); err != nil {
				return err
			}
		}
		if p.Origin == "history" {
			_, item, err := loadHistoryItem(ctx, q, row)
			if err != nil {
				return err
			}
			item.PlanKey = p.Key
			if err := saveHistoryItem(ctx, q, row, item); err != nil {
				return err
			}
		}
		result = p
		return nil
	})
	return result, err
}
func nativePacket(p relay.NativePlan, index int, role string) (relay.NativePacket, string, []byte, error) {
	if index < 0 || index >= len(p.Packets) {
		return relay.NativePacket{}, "", nil, delivery.ErrInvalid
	}
	if role == "label" {
		if !p.Packets[index].UseFallback || p.Packets[index].FallbackLabel == nil {
			return relay.NativePacket{}, "", nil, delivery.ErrInvalid
		}
		packet := *p.Packets[index].FallbackLabel
		raw, _ := json.Marshal(packet)
		return packet, p.PartKey(index) + ":original-label", raw, nil
	}
	if role != "" {
		return relay.NativePacket{}, "", nil, delivery.ErrInvalid
	}
	key, raw, err := p.FrozenPacket(index, p.Packets[index].UseFallback)
	if err != nil {
		return relay.NativePacket{}, "", nil, err
	}
	var packet relay.NativePacket
	if json.Unmarshal(raw, &packet) != nil {
		return packet, "", nil, delivery.ErrInvalid
	}
	return packet, key, raw, nil
}
func recordNative(ctx context.Context, q *db.Queries, row db.Job, p relay.NativePlan, index int, role string) error {
	packet, key, request, err := nativePacket(p, index, role)
	if err != nil {
		return err
	}
	effect, err := q.GetEffect(ctx, db.GetEffectParams{AccountID: row.AccountID, EffectKey: key})
	if err != nil {
		return err
	}
	if effect.JobID != row.ID || effect.State != "done" || !sameNativePacket(effect.Request, request) {
		return delivery.ErrIncomplete
	}
	var m models.Message
	if json.Unmarshal(effect.Result, &m) != nil || m.ID <= 0 || m.Chat.ID != packet.Target.ChatID || (m.MessageThreadID != 0 && int64(m.MessageThreadID) != packet.Target.ThreadID && (packet.Target.ThreadID > 0 || m.IsTopicMessage)) {
		return delivery.ErrUncertain
	}
	scope := relayScope(packet.Target.BotID, packet.Target.ChatID)
	part := int32(index * 2)
	if role == "label" {
		part++
	}
	if !packet.NoMapping {
		if err := q.InsertRelayMapping(ctx, db.InsertRelayMappingParams{AccountID: row.AccountID, Scope: scope, TelegramChatID: packet.Target.ChatID, TelegramMessageID: int64(m.ID), ThreadID: packet.Target.ThreadID, BotID: packet.Target.BotID, WhatsappChat: p.WhatsAppChat, WhatsappMessageID: p.WhatsAppMessage, Direction: "in", PlanKey: pgtype.Text{String: p.Key, Valid: true}, Part: part, Receipt: effect.Result}); err != nil {
			return err
		}
		saved, err := q.TelegramMapping(ctx, db.TelegramMappingParams{AccountID: row.AccountID, Scope: scope, TelegramChatID: packet.Target.ChatID, TelegramMessageID: int64(m.ID)})
		if err != nil {
			return err
		}
		if saved.PlanKey.String != p.Key || saved.Part != part || saved.WhatsappChat != p.WhatsAppChat || saved.WhatsappMessageID != p.WhatsAppMessage || saved.ThreadID != packet.Target.ThreadID {
			return delivery.ErrInvalid
		}
	}
	date := time.Now().UnixMilli()
	if m.Date > 0 {
		date = int64(m.Date) * 1000
	}
	if err := q.ObserveTelegram(ctx, db.ObserveTelegramParams{AccountID: row.AccountID, Scope: scope, BotID: packet.Target.BotID, ChatID: packet.Target.ChatID, MessageID: int64(m.ID), ThreadID: packet.Target.ThreadID, Kind: "chat", MessageDate: date, Payload: effect.Result}); err != nil {
		return err
	}
	if packet.NoMapping && p.Day != "" && p.HistoryRun != "" {
		return q.RecordHistoryDay(ctx, db.RecordHistoryDayParams{AccountID: row.AccountID, RunID: p.HistoryRun, Day: p.Day})
	}
	return nil
}
func (s *Store) RecordNative(ctx context.Context, j delivery.Job, key string, part int, role string) error {
	return s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		owner, p, err := nativePlan(ctx, q, row.AccountID, key)
		if err != nil {
			return err
		}
		if owner.JobID != row.ID {
			return delivery.ErrInvalid
		}
		return recordNative(ctx, q, row, p, part, role)
	})
}
func (s *Store) CompleteNative(ctx context.Context, j delivery.Job, key string) error {
	return s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		owner, p, err := nativePlan(ctx, q, row.AccountID, key)
		if err != nil {
			return err
		}
		if owner.JobID != row.ID {
			return delivery.ErrInvalid
		}
		for index, packet := range p.Packets {
			if packet.UseFallback && packet.FallbackLabel != nil {
				if err := recordNative(ctx, q, row, p, index, "label"); err != nil {
					return err
				}
			}
			if err := recordNative(ctx, q, row, p, index, ""); err != nil {
				return err
			}
		}
		return q.CompleteRelay(ctx, db.CompleteRelayParams{AccountID: row.AccountID, PlanKey: key})
	})
}
func (s *Store) RefuseNativePacket(ctx context.Context, j delivery.Job, key string, part int) error {
	return s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		owner, p, err := nativePlan(ctx, q, row.AccountID, key)
		if err != nil {
			return err
		}
		if owner.JobID != row.ID || part < 0 || part >= len(p.Packets) || p.Packets[part].Fallback == nil {
			return delivery.ErrInvalid
		}
		effectKey, request, err := p.FrozenPacket(part, false)
		if err != nil {
			return err
		}
		effect, err := q.GetEffect(ctx, db.GetEffectParams{AccountID: row.AccountID, EffectKey: effectKey})
		if err != nil {
			return err
		}
		if p.Packets[part].UseFallback && effect.State == "discarded" {
			return nil
		}
		if effect.JobID != row.ID || effect.State != "pending" || !sameNativePacket(effect.Request, request) {
			return delivery.ErrUncertain
		}
		if _, err := q.DiscardEffect(ctx, db.DiscardEffectParams{AccountID: row.AccountID, JobID: row.ID, EffectKey: effectKey}); err != nil {
			return err
		}
		p.Packets[part].UseFallback = true
		raw, _ := json.Marshal(p)
		return q.UpdateNativePlan(ctx, db.UpdateNativePlanParams{AccountID: row.AccountID, PlanKey: key, Payload: raw})
	})
}

// JSONB normalizes object key order and whitespace. Compare the typed frozen
// request while retaining exact integer identities and presentation entities.
func sameNativePacket(a, b json.RawMessage) bool {
	var left, right relay.NativePacket
	return json.Unmarshal(a, &left) == nil && json.Unmarshal(b, &right) == nil && reflect.DeepEqual(left, right)
}
func (s *Store) ArchivedAsset(ctx context.Context, account, chat, message string) (attachments.Asset, bool, error) {
	raw, err := s.queries.ArchivedAsset(ctx, db.ArchivedAssetParams{AccountID: account, WhatsappChat: chat, MessageID: message})
	if errors.Is(err, pgx.ErrNoRows) {
		return attachments.Asset{}, false, nil
	}
	if err != nil {
		return attachments.Asset{}, false, err
	}
	var asset attachments.Asset
	if json.Unmarshal(raw, &asset) != nil {
		return asset, false, delivery.ErrInvalid
	}
	return asset, true, nil
}
func (s *Store) ArchivedMedia(ctx context.Context, account, chat, message string) (string, *models.Message, bool, error) {
	rows, err := s.queries.ArchivedMedia(ctx, db.ArchivedMediaParams{AccountID: account, WhatsappChat: chat, WhatsappMessageID: message})
	if err != nil {
		return "", nil, false, err
	}
	for _, row := range rows {
		var m models.Message
		if json.Unmarshal(row.Receipt, &m) == nil {
			if _, found := attachments.SelectTelegram(&m); found {
				return row.BotID, &m, true, nil
			}
		}
		var source struct {
			Bot    string
			Update models.Update
		}
		if json.Unmarshal(row.Source, &source) == nil && source.Bot == row.BotID && source.Update.Message != nil {
			m = *source.Update.Message
			if int64(m.ID) == row.TelegramMessageID && m.Chat.ID == row.TelegramChatID {
				if _, found := attachments.SelectTelegram(&m); found {
					return row.BotID, &m, true, nil
				}
			}
		}
	}
	return "", nil, false, nil
}
func (s *Store) HistoryQuote(ctx context.Context, account, run, source, bot string, chat, thread int64) (relay.Mapping, bool, error) {
	row, err := s.queries.HistoryQuote(ctx, db.HistoryQuoteParams{AccountID: account, RunID: run, SourceID: source, TelegramChatID: chat, ThreadID: thread, Scope: relayScope(bot, chat)})
	if errors.Is(err, pgx.ErrNoRows) {
		return relay.Mapping{}, false, nil
	}
	if err != nil {
		return relay.Mapping{}, false, err
	}
	return relay.Mapping{WhatsAppChat: row.WhatsappChat, WhatsAppMessage: row.WhatsappMessageID, BotID: row.BotID, Direction: row.Direction, TelegramChat: row.TelegramChatID, TelegramMessage: row.TelegramMessageID, ThreadID: row.ThreadID, Deleted: row.Deleted}, true, nil
}

var _ relay.NativeStore = (*Store)(nil)
