package conversation

import (
	"context"
	"errors"
	"testing"
	"unicode/utf16"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

type fixtureStore struct {
	binding Binding
	found   bool
}

func (f *fixtureStore) ConversationRoute(context.Context, string, string) (Route, bool, error) {
	return Route{}, false, nil
}
func (f *fixtureStore) ContactBot(context.Context, string, string) (Binding, bool, error) {
	return f.binding, f.found, nil
}
func (f *fixtureStore) CreateInbox(context.Context, delivery.Job, Route) (Route, error) {
	return Route{}, errors.New("unexpected write")
}

type fixtureTelegram struct {
	role  models.ChatMemberType
	err   error
	calls int
}

func (f *fixtureTelegram) CreateForumTopic(context.Context, *bot.CreateForumTopicParams) (*models.ForumTopic, error) {
	return nil, errors.New("unexpected mutation")
}
func (f *fixtureTelegram) ChatMember(context.Context, *bot.GetChatMemberParams) (*models.ChatMember, error) {
	f.calls++
	return &models.ChatMember{Type: f.role}, f.err
}

func TestTargetSelectionPreservesPrivateStartAndMirrorFallback(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		mode                 Mode
		started, retired     bool
		role                 models.ChatMemberType
		failure              error
		flags                Flags
		wantBot              string
		wantChat, wantThread int64
		wantErr              bool
		calls                int
	}{
		{"private started", Dedicated, true, true, "", nil, Flags{}, "200", 123, 0, false, 0},
		{"private not started", Dedicated, false, false, "", nil, Flags{}, "master", -100, 44, false, 0},
		{"private retired needs restore", Dedicated, false, true, "", nil, Flags{}, "", 0, 0, true, 0},
		{"member available", Group, false, false, models.ChatMemberTypeMember, nil, Flags{}, "200", -100, 44, false, 1},
		{"member admin", Group, false, false, models.ChatMemberTypeAdministrator, nil, Flags{}, "200", -100, 44, false, 1},
		{"member absent", Group, true, false, models.ChatMemberTypeLeft, nil, Flags{}, "master", -100, 44, false, 1},
		{"lookup refusal fallback", Group, true, false, "", &providers.Error{Status: 400}, Flags{}, "master", -100, 44, false, 1},
		{"lookup unavailable", Group, true, false, "", &providers.Error{Status: 503, Retry: true}, Flags{}, "", 0, 0, true, 1},
		{"phone origin group", Group, true, false, "", nil, Flags{FromMe: true}, "master", -100, 44, false, 0},
		{"phone origin private", Dedicated, true, true, "", nil, Flags{FromMe: true}, "200", 123, 0, false, 0},
		{"status through master", Inbox, true, false, "", nil, Flags{Status: true}, "master", -100, 44, false, 0},
		{"forced fallback", Group, true, false, "", nil, Flags{ForceMaster: true}, "master", -100, 44, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &fixtureStore{binding: Binding{BotID: "200", Started: tc.started}, found: true}
			tg := &fixtureTelegram{role: tc.role, err: tc.failure}
			s, err := New(store, tg, -100, 123)
			if err != nil {
				t.Fatal(err)
			}
			target, err := s.SelectTarget(t.Context(), "fixture", Route{ChatID: "peer@c.us", TelegramChatID: -100, ThreadID: 44, Mode: tc.mode, InboxRetired: tc.retired}, "peer@c.us", tc.flags)
			if (err != nil) != tc.wantErr || target.BotID != tc.wantBot || target.ChatID != tc.wantChat || target.ThreadID != tc.wantThread || tg.calls != tc.calls {
				t.Fatal(target, err, tg.calls)
			}
		})
	}
}

func TestSuppressedRoutesNeverInspectMembershipOrSend(t *testing.T) {
	for _, mode := range []Mode{Muted, Ignored} {
		tg := &fixtureTelegram{}
		s, _ := New(&fixtureStore{found: true}, tg, -100, 123)
		target, err := s.SelectTarget(t.Context(), "fixture", Route{ChatID: "group@g.us", TelegramChatID: -200, Mode: mode}, "peer@c.us", Flags{})
		if err != nil || !target.Suppressed || tg.calls != 0 {
			t.Fatal(target, err)
		}
	}
}

func TestTopicNamePreservesUnicodeWithoutSplittingSurrogates(t *testing.T) {
	name := ""
	for i := 0; i < 100; i++ {
		name += "🙂"
	}
	out := topicName(name, "fallback")
	if len(utf16.Encode([]rune(out))) != 128 || len([]rune(out)) != 64 {
		t.Fatal("topic name limit", len([]rune(out)))
	}
	if topicName("", "fallback") != "fallback" {
		t.Fatal("missing fallback")
	}
}
