package relay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-telegram/bot/models"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/attachments"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/conversation"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/history"
	"github.com/veryshyjelly/bifrost/internal/identity"
	"github.com/veryshyjelly/bifrost/internal/presentation"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

func NativeIncomingKey(chat, message string) string {
	return "native-in:" + digest(chat+"\x00"+message)
}

func (n *Native) sender(ctx context.Context, j delivery.Job, e *delivery.Effects, p *NativePlan) error {
	p.ControlTTL = n.c.ControlTTL
	if !p.Source.Own || p.Source.LinkSenderID != "" {
		return nil
	}
	id, found, err := n.store.SelfIdentity(ctx, j.AccountID)
	if err != nil {
		return local(err)
	}
	if !found {
		if err := e.Permit(); err != nil {
			return err
		}
		session, err := n.c.Identity.Session(ctx, n.c.Session)
		if err != nil {
			return err
		}
		if session == nil || session.ID != n.c.Session {
			return providers.InvalidResponse("openwa", false)
		}
		if session.Phone == nil {
			return nil
		}
		var valid bool
		id, valid = identity.PhoneID(*session.Phone)
		if !valid {
			return providers.InvalidResponse("openwa", false)
		}
		if err := n.store.SaveSelfIdentity(ctx, j, id); err != nil {
			return local(err)
		}
	}
	p.Presentation.Sender = id
	return nil
}
func (n *Native) Incoming(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
	decoded, err := identity.DecodeSource(j)
	if err != nil {
		return err
	}
	if decoded.SessionID != n.c.Session {
		return delivery.ErrInvalid
	}
	source, err := n.store.NormalizeIncoming(ctx, j)
	if err != nil {
		return local(err)
	}
	echo, err := n.store.RelayEcho(ctx, j.AccountID, source.ChatID, source.MessageID)
	if err != nil {
		return local(err)
	}
	if echo.Recognized {
		return nil
	}
	if source.Own && echo.Pending {
		return delivery.ErrLocalRetry
	}
	key := NativeIncomingKey(source.ChatID, source.MessageID)
	if p, found, err := n.store.NativePlan(ctx, j.AccountID, key); err != nil {
		return local(err)
	} else if found {
		return n.send(ctx, j, e, p)
	}
	route, known, err := n.store.ConversationRoute(ctx, j.AccountID, source.ChatID)
	if err != nil {
		return local(err)
	}
	if known && (route.Mode == conversation.Muted || route.Mode == conversation.Ignored) {
		return nil
	}
	name := source.ConversationName
	if !known && strings.HasSuffix(source.ChatID, "@g.us") {
		if err := e.Permit(); err != nil {
			return err
		}
		info, err := n.c.Identity.Group(ctx, n.c.Session, source.RawChatID)
		if err != nil {
			return err
		}
		if info == nil || info.ID != source.RawChatID || strings.TrimSpace(info.Name) == "" {
			return providers.InvalidResponse("openwa", false)
		}
		name = info.Name
	}
	if name == "" {
		name = source.Name
	}
	if name == "" {
		name = source.ChatID
	}
	route, err = n.routing.EnsureInbox(ctx, j, e, source.ChatID, name)
	if err != nil {
		return err
	}
	target, err := n.routing.SelectTarget(ctx, j.AccountID, route, source.SenderID, conversation.Flags{FromMe: source.Own})
	if err != nil {
		return err
	}
	if target.Suppressed {
		return nil
	}
	p := NativePlan{Key: key, Direction: "in", Format: "native-packets-v1", Prefix: j.Dedup + ":native", Origin: "live", Session: n.c.Session, OwnerID: n.c.OwnerID, WhatsAppChat: source.ChatID, WhatsAppMessage: source.MessageID, BotID: target.BotID, TelegramChat: target.ChatID, ThreadID: target.ThreadID, Route: route, Source: source, Message: decoded.Message}
	if err := n.sender(ctx, j, e, &p); err != nil {
		return err
	}
	if err := n.render(ctx, j, &p); err != nil {
		return err
	}
	p, err = n.store.PrepareNative(ctx, j, p)
	if err != nil {
		return local(err)
	}
	return n.send(ctx, j, e, p)
}
func (n *Native) DeliverHistory(ctx context.Context, j delivery.Job, e *delivery.Effects, run history.Run, item history.Item) error {
	if item.AdoptedFormat == "text-v1" {
		if n.c.TextResume == nil || item.PlanKey == "" {
			return delivery.ErrInvalid
		}
		return n.c.TextResume.ResumeIncoming(ctx, j, e, item.PlanKey)
	}
	key := "history-native:" + run.ID + ":" + strconv.Itoa(item.Index)
	if item.PlanKey != "" {
		key = item.PlanKey
	}
	if p, found, err := n.store.NativePlan(ctx, j.AccountID, key); err != nil {
		return local(err)
	} else if found {
		return n.send(ctx, j, e, p)
	}
	route := run.Route
	route.TelegramChatID, route.ThreadID, route.InboxRetired = run.Target.ChatID, run.Target.ThreadID, false
	route.Mode = conversation.Group
	if route.TelegramChatID > 0 {
		route.Mode = conversation.Dedicated
	}
	target := conversation.Target{BotID: run.Target.BotID, ChatID: run.Target.ChatID, ThreadID: run.Target.ThreadID}
	if target.BotID == "" {
		var err error
		target, err = n.routing.SelectTarget(ctx, j.AccountID, route, item.Source.SenderID, conversation.Flags{FromMe: item.Source.Own})
		if err != nil {
			return err
		}
	}
	p := NativePlan{Key: key, Direction: "in", Format: "native-packets-v1", Prefix: j.Dedup + ":native", Origin: "history", Session: n.c.Session, OwnerID: n.c.OwnerID, HistoryRun: run.ID, HistoryMode: run.Mode, ReturnKey: run.ReturnKey, WhatsAppChat: run.ChatID, WhatsAppMessage: item.Message.ID, BotID: target.BotID, TelegramChat: target.ChatID, ThreadID: target.ThreadID, Route: run.Route, Source: item.Source, Message: item.Message}
	p.Presentation = presentation.Options{Historical: true, HistoryStyle: "compact", TimeZone: run.Timezone}
	if err := n.sender(ctx, j, e, &p); err != nil {
		return err
	}
	day, label, err := presentation.HistoryDay(item.Message.Timestamp, run.Timezone, p.Presentation.Now)
	if err != nil {
		return err
	}
	if day != run.LastDay {
		p.Day = day
		headerTarget := run.Target
		if headerTarget.BotID == "" {
			headerTarget.BotID = "master"
		}
		p.Packets = append(p.Packets, NativePacket{Kind: "text", Target: headerTarget, Body: label, NoMapping: true})
	}
	if err := n.render(ctx, j, &p); err != nil {
		return err
	}
	p, err = n.store.PrepareNative(ctx, j, p)
	if err != nil {
		return local(err)
	}
	return n.send(ctx, j, e, p)
}
func (n *Native) sourceAsset(ctx context.Context, j delivery.Job, p NativePlan) (attachments.Asset, error) {
	if asset, found, err := n.store.ArchivedAsset(ctx, j.AccountID, p.WhatsAppChat, p.WhatsAppMessage); err != nil {
		return attachments.Asset{}, local(err)
	} else if found {
		if file, err := n.assets.Open(ctx, asset); err == nil {
			file.Close()
			return asset, nil
		}
	}
	botID, message, found, err := n.store.ArchivedMedia(ctx, j.AccountID, p.WhatsAppChat, p.WhatsAppMessage)
	if err != nil {
		return attachments.Asset{}, local(err)
	}
	if found {
		asset, _, err := n.sources.Telegram(ctx, j.AccountID, botID, message)
		if err == nil {
			return asset, nil
		}
		var refusal *providers.Error
		if !errors.Is(err, attachments.ErrAsset) && (!errors.As(err, &refusal) || (refusal.Status != 400 && refusal.Status != 403 && refusal.Status != 404)) {
			return attachments.Asset{}, err
		}
	}
	statusID := ""
	if p.Origin == "status" {
		statusID = p.WhatsAppMessage
	}
	return n.sources.WhatsApp(ctx, p.Message, statusID)
}
func (native *Native) render(ctx context.Context, j delivery.Job, p *NativePlan) error {
	target := control.Target{BotID: p.BotID, ChatID: p.TelegramChat, ThreadID: p.ThreadID}
	source, m := p.Source, p.Message
	format := p.Presentation
	format.Bot, format.Name, format.Own, format.Timestamp = p.BotID, source.Name, source.Own, m.Timestamp
	if format.Sender == "" {
		format.Sender = source.LinkSenderID
	}
	if format.Sender == "" && !source.Own {
		format.Sender = source.SenderID
	}
	if format.Name == "" {
		format.Name = source.SenderID
	}
	if format.Name == "" {
		format.Name = source.ChatID
	}
	p.Presentation = format
	quote := int64(0)
	if m.QuotedMessage != nil && m.QuotedMessage.ID != "" {
		var mapping Mapping
		var found bool
		var err error
		if p.HistoryMode == "all" {
			mapping, found, err = native.store.HistoryQuote(ctx, j.AccountID, p.HistoryRun, m.QuotedMessage.ID, p.BotID, p.TelegramChat, p.ThreadID)
		} else {
			mapping, found, err = native.store.WhatsAppQuote(ctx, j.AccountID, p.WhatsAppChat, m.QuotedMessage.ID, p.BotID, p.TelegramChat, p.ThreadID)
		}
		if err != nil {
			return local(err)
		}
		if found {
			quote = mapping.TelegramMessage
		}
	}
	body := presentation.Plain(source.Body)
	if len(source.MentionedIDs) > 0 {
		if native.c.Mentions == nil {
			return delivery.ErrUnsupported
		}
		var err error
		body, err = native.c.Mentions.Incoming(ctx, j, p.WhatsAppChat, source.Body, source.MentionedIDs, p.TelegramChat)
		if err != nil {
			return err
		}
	}
	render := func(content presentation.Value) (presentation.Value, error) {
		if format.Historical && quote == 0 && m.QuotedMessage != nil && m.QuotedMessage.Body != "" {
			preview := []rune(m.QuotedMessage.Body)
			if len(preview) > 300 {
				preview = preview[:300]
			}
			content = presentation.Concat(presentation.Styled("↪ "+string(preview), models.MessageEntityTypeItalic), presentation.Plain("\n"), content)
		}
		return presentation.Relay(format, content)
	}
	text, err := render(body)
	if err != nil {
		return err
	}
	header, err := presentation.Header(format)
	if err != nil {
		return err
	}
	annotated := func(notice string) (presentation.Value, error) {
		parts := []presentation.Value{body}
		if body.Text != "" {
			parts = append(parts, presentation.Plain("\n"))
		}
		parts = append(parts, presentation.Styled("["+notice+"]", models.MessageEntityTypeItalic))
		return render(presentation.Concat(parts...))
	}
	appendText := func(v presentation.Value, markup *models.InlineKeyboardMarkup) error {
		parts, err := presentation.Split(v, 3800)
		if err != nil {
			return err
		}
		for _, part := range parts {
			if part.Text == "" {
				part = presentation.Plain("[Empty message]")
			}
			p.Packets = append(p.Packets, NativePacket{Kind: "text", Target: target, Body: part, ReplyTo: quote, Markup: markup})
		}
		return nil
	}
	firstCaption := func(v presentation.Value) (presentation.Value, error) {
		parts, err := presentation.Split(v, 1000)
		if err != nil || len(parts) == 0 {
			return presentation.Value{}, delivery.ErrInvalid
		}
		return parts[0], nil
	}
	switch m.Type {
	case sdk.MsgImage, sdk.MsgVideo, sdk.MsgAudio, sdk.MsgVoice, sdk.MsgSticker, sdk.MsgDocument:
		asset, err := native.sourceAsset(ctx, j, *p)
		if err != nil {
			var refusal *providers.Error
			if !errors.As(err, &refusal) || (refusal.Status != 404 && refusal.Status != 413) {
				return err
			}
			reason := "Attachment request failed (404)"
			if refusal.Status == 413 {
				reason = "Attachment exceeds configured bridge media limit"
			} else if refusal.Code == "MEDIA_SOURCE_UNAVAILABLE" {
				reason = "WhatsApp cannot supply this attachment"
			}
			notice, err := annotated(string(m.Type) + " unavailable: " + reason)
			if err != nil {
				return err
			}
			return appendText(notice, nil)
		}
		p.SourceAsset = &asset
		upload := asset
		kind := map[sdk.MessageType]string{sdk.MsgImage: "photo", sdk.MsgVideo: "video", sdk.MsgAudio: "audio", sdk.MsgVoice: "voice", sdk.MsgSticker: "sticker", sdk.MsgDocument: "document"}[m.Type]
		if m.Type == sdk.MsgSticker {
			upload, err = native.codec.Sticker(ctx, asset, "telegram")
		} else if m.Type == sdk.MsgVoice && !strings.Contains(asset.MIME, "ogg") {
			upload, err = native.codec.Transcode(ctx, asset, true, native.c.MaxBytes)
		} else if m.Type == sdk.MsgImage && asset.MIME != "" && !strings.HasPrefix(asset.MIME, "image/jpeg") && !strings.HasPrefix(asset.MIME, "image/png") {
			upload, err = native.codec.Transcode(ctx, asset, false, native.c.MaxBytes)
		}
		if err != nil {
			var refusal *providers.Error
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if !errors.As(err, &refusal) || refusal.Status != 415 {
				if !errors.Is(err, context.DeadlineExceeded) {
					return err
				}
			}
			kind, upload = "document", asset
		}
		separate := (m.Type == sdk.MsgSticker && strings.TrimSpace(text.Text) != "") || presentation.Length(text.Text) > 1000
		if separate {
			if err := appendText(text, nil); err != nil {
				return err
			}
		}
		caption := text
		if presentation.Length(text.Text) > 1000 {
			caption = header
		}
		if m.Type == sdk.MsgSticker {
			caption = presentation.Plain("")
			if kind == "document" {
				caption = presentation.Plain("[Original attachment; native presentation unavailable]")
			}
		}
		caption, err = firstCaption(caption)
		if err != nil {
			return err
		}
		packet := NativePacket{Kind: kind, Target: target, Body: caption, ReplyTo: quote, Asset: &upload}
		if kind != "document" {
			fallback, err := annotated("Original attachment; native presentation unavailable")
			if err != nil {
				return err
			}
			if presentation.Length(fallback.Text) > 1000 {
				if !separate {
					packet.FallbackLabel = &NativePacket{Kind: "text", Target: target, Body: text, ReplyTo: quote}
				}
				fallback, err = render(presentation.Styled("[Original attachment; native presentation unavailable]", models.MessageEntityTypeItalic))
				if err != nil {
					return err
				}
			}
			fallback, err = firstCaption(fallback)
			if err != nil {
				return err
			}
			packet.Fallback = &NativePacket{Kind: "document", Target: target, Body: fallback, ReplyTo: quote, Asset: &asset}
		}
		p.Packets = append(p.Packets, packet)
	case sdk.MsgLocation:
		if m.Location == nil {
			notice, err := annotated("location")
			if err != nil {
				return err
			}
			return appendText(notice, nil)
		}
		if strings.TrimSpace(text.Text) != "" {
			if err := appendText(text, nil); err != nil {
				return err
			}
		}
		p.Packets = append(p.Packets, NativePacket{Kind: "location", Target: target, ReplyTo: quote, Location: m.Location})
	case sdk.MsgContact:
		name := regexp.MustCompile(`(?im)(?:^|\n)FN:(.+)`).FindStringSubmatch(m.Body)
		number := regexp.MustCompile(`(?im)(?:^|\n)TEL[^:]*:([+\d ()-]+)`).FindStringSubmatch(m.Body)
		if strings.Contains(strings.ToUpper(m.Body), "BEGIN:VCARD") && len(name) > 1 && len(number) > 1 {
			if strings.TrimSpace(header.Text) != "" {
				if err := appendText(header, nil); err != nil {
					return err
				}
			}
			p.Packets = append(p.Packets, NativePacket{Kind: "contact", Target: target, ReplyTo: quote, Contact: &NativeContact{Name: strings.TrimSpace(name[1]), Number: strings.TrimSpace(number[1]), VCard: m.Body}})
		} else {
			return appendText(text, nil)
		}
	case sdk.MsgPoll:
		if m.Poll == nil {
			return appendText(text, nil)
		}
		parts := []presentation.Value{presentation.Styled("📊 "+m.Poll.Name, models.MessageEntityTypeBold)}
		for index, value := range m.Poll.Options {
			parts = append(parts, presentation.Plain(fmt.Sprintf("\n%d. %s", index+1, value)))
			if index < 12 {
				p.PollChoices = append(p.PollChoices, PollChoice{Value: value})
			}
		}
		poll, err := render(presentation.Concat(parts...))
		if err != nil {
			return err
		}
		return appendText(poll, nil)
	case sdk.MsgText:
		return appendText(text, nil)
	default:
		label := string(m.Type)
		if label == "" {
			label = "Unsupported message"
		}
		if m.Call != nil && m.Call.Missed {
			label += " · missed"
		}
		notice, err := annotated(label)
		if err != nil {
			return err
		}
		return appendText(notice, nil)
	}
	// The attachment bytes are frozen separately; retaining base64 here would
	// duplicate them in every presentation and recovery record.
	if p.Message.Media != nil {
		copy := *p.Message.Media
		copy.Data = ""
		p.Message.Media = &copy
	}
	return nil
}
func (n *Native) send(ctx context.Context, j delivery.Job, e *delivery.Effects, p NativePlan) error {
	if p.Complete {
		return nil
	}
	if e == nil {
		return delivery.ErrInvalid
	}
	for index := range p.Packets {
		fallback := p.Packets[index].UseFallback
		for {
			if fallback && p.Packets[index].FallbackLabel != nil {
				request, err := json.Marshal(p.Packets[index].FallbackLabel)
				if err != nil {
					return delivery.ErrInvalid
				}
				if _, err := e.Do(p.PartKey(index)+":original-label", request, n.transmit(j)); err != nil {
					return err
				}
				if err := n.store.RecordNative(ctx, j, p.Key, index, "label"); err != nil {
					return local(err)
				}
			}
			key, request, err := p.FrozenPacket(index, fallback)
			if err != nil {
				return err
			}
			_, err = e.Do(key, request, n.transmit(j))
			if err == nil {
				break
			}
			var refusal *providers.Error
			if fallback || p.Packets[index].Fallback == nil || !errors.As(err, &refusal) || refusal.Retry || refusal.Uncertain || (refusal.Status != 400 && refusal.Status != 415) || refusal.Reason == "thread_missing" || refusal.Reason == "reply_missing" {
				return err
			}
			if err := n.store.RefuseNativePacket(ctx, j, p.Key, index); err != nil {
				return local(err)
			}
			fallback = true
			p.Packets[index].UseFallback = true
		}
		if err := n.store.RecordNative(ctx, j, p.Key, index, ""); err != nil {
			return local(err)
		}
	}
	return local(n.store.CompleteNative(ctx, j, p.Key))
}

var _ history.Delivery = (*Native)(nil)

func (n *Native) transmit(j delivery.Job) func(context.Context, json.RawMessage) (json.RawMessage, error) {
	return func(call context.Context, raw json.RawMessage) (json.RawMessage, error) {
		var packet NativePacket
		if json.Unmarshal(raw, &packet) != nil {
			return nil, delivery.ErrInvalid
		}
		var data io.ReadCloser
		if packet.Asset != nil {
			file, err := n.assets.Open(call, *packet.Asset)
			if err != nil {
				return nil, local(err)
			}
			data = file
			defer data.Close()
		}
		client, err := n.clients(call, j.AccountID, packet.Target.BotID)
		if err != nil {
			return nil, local(err)
		}
		result, err := client.SendPacket(call, packet, data)
		if err != nil {
			return nil, err
		}
		if result == nil || result.ID <= 0 || result.Chat.ID != packet.Target.ChatID || (result.MessageThreadID != 0 && int64(result.MessageThreadID) != packet.Target.ThreadID && (packet.Target.ThreadID > 0 || result.IsTopicMessage)) {
			return nil, providers.InvalidResponse("telegram", true)
		}
		return json.Marshal(result)
	}
}
