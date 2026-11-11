package control

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

func (c *Cleaner) Handle(ctx context.Context, job delivery.Job, e *delivery.Effects, botID string, u *models.Update) error {
	if u == nil || job.Kind != "tg" {
		return delivery.ErrInvalid
	}
	if edited, ok := ParseCommand(u.EditedMessage); ok && (edited.Name == "clear" || edited.Name == "clear_log" || edited.Name == "clear_all") {
		return nil
	}
	m := u.Message
	parsed, ok := ParseCommand(m)
	if !ok || (parsed.Name != "clear" && parsed.Name != "clear_log" && parsed.Name != "clear_all") {
		return delivery.ErrUnsupported
	}
	if m.From == nil || m.From.IsBot || m.From.ID != c.c.OwnerID {
		return nil
	}
	if m.Chat.Type == models.ChatTypePrivate && m.Chat.ID != c.c.OwnerID {
		return delivery.ErrInvalid
	}
	if saved, ok, err := c.replies.Prepared(ctx, job, job.Dedup); err != nil {
		return err
	} else if ok {
		return c.replies.Send(ctx, job, e, saved)
	}
	if parsed.Recipient == "" && m.Chat.Type != models.ChatTypePrivate && botID != "master" {
		return nil
	}
	addressed, err := c.replies.Addressed(ctx, job, botID, parsed.Recipient)
	if err != nil {
		return err
	}
	if !addressed {
		return nil
	}
	scope, err := c.replies.Context(ctx, job, botID, m)
	if err != nil {
		return err
	}
	p := Plan{Key: job.Dedup, Target: scope.Target, Command: &Command{MessageID: int64(m.ID), Date: int64(m.Date) * 1000, TTLSeconds: int(c.c.CommandTTL / time.Second)}, ReplyTTLSeconds: int(c.c.CommandTTL / time.Second)}
	mode := "log"
	if parsed.Name == "clear_all" {
		mode = "all"
	} else if parsed.Name == "clear" && len(parsed.Args) > 0 {
		mode = parsed.Args[0]
	}
	maxArgs := 0
	if parsed.Name == "clear" {
		maxArgs = 1
	}
	text := ""
	if (mode != "log" && mode != "all") || len(parsed.Args) > maxArgs {
		text = "Use /clear log for notices, or /clear all in a management topic."
	} else if mode == "all" && (botID != "master" || m.Chat.ID != c.replies.management) {
		text = "/clear all is available only in the management group, where it clears the current topic. Use /clear log here to remove bot notices."
	} else {
		label := "bot notices in this chat/topic"
		if mode == "all" {
			label = "recorded Telegram messages in this management topic"
		}
		text = "Clearing " + label + " in the background. Telegram limits deletion to messages under 48 hours old."
		p.Clear = &ClearRequest{ID: rand.Text(), Mode: mode, OwnerID: c.c.OwnerID, ManagementChatID: c.replies.management}
	}
	p.Packets = []Packet{{Text: text}}
	return c.replies.Send(ctx, job, e, p)
}
func (c *Cleaner) Handlers() map[string]delivery.Handler {
	return map[string]delivery.Handler{"control-expire": c.Expire, "control-clear": c.Clear}
}
func (c *Cleaner) remove(ctx context.Context, job delivery.Job, e *delivery.Effects, step CleanupStep) error {
	if e == nil {
		return delivery.ErrInvalid
	}
	if err := e.Permit(); err != nil {
		return err
	}
	client, err := c.clients(ctx, job.AccountID, step.Target.BotID)
	if err != nil {
		return err
	}
	ids := make([]int, 0, len(step.Items))
	for _, i := range step.Items {
		ids = append(ids, int(i.MessageID))
	}
	if err := e.Permit(); err != nil {
		return err
	}
	return client.DeleteMessages(ctx, &bot.DeleteMessagesParams{ChatID: step.Target.ChatID, MessageIDs: ids})
}
func deleteStatus(err error) int {
	var p *providers.Error
	if errors.As(err, &p) && p.Service == "telegram" {
		return p.Status
	}
	return 0
}
func (c *Cleaner) Expire(ctx context.Context, job delivery.Job, e *delivery.Effects) error {
	step, err := c.store.PrepareExpiry(ctx, job)
	if err != nil {
		return local(err)
	}
	if step.Outcome != "pending" {
		return nil
	}
	if len(step.Items) == 0 {
		return local(c.store.FinishExpiry(ctx, job, "noop"))
	}
	err = c.remove(ctx, job, e, step)
	outcome := "removed"
	if err != nil {
		if s := deleteStatus(err); s == 400 || s == 403 {
			outcome = "blocked"
		} else {
			return err
		}
	}
	return local(c.store.FinishExpiry(ctx, job, outcome))
}
func (c *Cleaner) Clear(ctx context.Context, job delivery.Job, e *delivery.Effects) error {
	step, err := c.store.PrepareClearStep(ctx, job)
	if err != nil {
		return local(err)
	}
	if step.Kind == "noop" {
		return nil
	}
	if step.Kind == "summary" {
		p := Plan{Key: job.Dedup, Target: step.Run.Target, ReplyTTLSeconds: int(c.c.CommandTTL / time.Second)}
		label := "notices"
		if step.Run.Mode == "all" {
			label = "messages"
		}
		p.Packets = []Packet{{Text: fmt.Sprintf("Cleared %d recorded Telegram %s. %d too old or protected; %d could not be removed.", step.Run.Removed, label, step.Run.Skipped, step.Run.Blocked)}}
		if err := c.replies.Send(ctx, job, e, p); err != nil {
			return err
		}
		return local(c.store.FinishClearRun(ctx, job))
	}
	if step.Outcome == "pending" {
		outcome := "removed"
		if len(step.Items) > 0 {
			err := c.remove(ctx, job, e, step)
			if err != nil {
				switch deleteStatus(err) {
				case 400:
					if step.Run.Single {
						outcome = "blocked"
					} else {
						outcome = "split"
					}
				case 403:
					outcome = "forbidden"
				default:
					return err
				}
			}
		}
		if err := c.store.FinishClearStep(ctx, job, outcome); err != nil {
			return local(err)
		}
		step.Outcome = outcome
	}
	if step.Outcome == "forbidden" {
		return c.replies.Send(ctx, job, e, Plan{Key: job.Dedup, Target: step.Run.Target, ReplyTTLSeconds: int(c.c.CommandTTL / time.Second), Packets: []Packet{{Text: "Telegram refused cleanup. Give Bifrost Delete Messages permission in this group."}}})
	}
	return nil
}
