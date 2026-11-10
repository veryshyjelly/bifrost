package mentions

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/identity"
	"github.com/veryshyjelly/bifrost/internal/presentation"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

const alice = "12025550123@c.us"
const bob = "12025550124@c.us"
const lid = "987654321@lid"
const group = "123@g.us"

type fixture struct {
	directory               *identity.Directory
	rosters                 map[string]Roster
	tokens                  map[string]Token
	members                 []sdk.GroupParticipant
	memberType              models.ChatMemberType
	groupErr, memberErr     error
	groupCalls, memberCalls int
	s                       *Service
	job                     delivery.Job
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{rosters: map[string]Roster{}, tokens: map[string]Token{}, memberType: models.ChatMemberTypeMember, job: delivery.Job{Event: delivery.Event{AccountID: "s1"}}}
	var err error
	f.directory, err = identity.New(nil, []identity.Profile{{ID: alice, Name: "Alice"}, {ID: bob, Name: "Bob"}, {ID: lid, Name: "Hidden"}}, []identity.Bot{{ID: "42", Username: "alice_bot"}}, []identity.Binding{{PersonID: alice, BotID: "42"}})
	if err != nil {
		t.Fatal(err)
	}
	f.members = []sdk.GroupParticipant{{ID: alice, Name: "Alice"}, {ID: bob, Name: "Bob"}, {ID: lid, Name: "Hidden"}}
	f.s, err = New(f, f, f, "session")
	if err != nil {
		t.Fatal(err)
	}
	f.s.now = func() time.Time { return time.UnixMilli(1_800_000_000_000) }
	return f
}
func (f *fixture) IdentityDirectory(context.Context, string) (*identity.Directory, error) {
	return f.directory, nil
}
func (f *fixture) MentionRoster(_ context.Context, _ string, chat string) (Roster, bool, error) {
	r, ok := f.rosters[chat]
	return r, ok, nil
}
func (f *fixture) SaveMentionRoster(_ context.Context, _ delivery.Job, r Roster) error {
	f.rosters[r.ChatID] = r
	return nil
}
func (f *fixture) MentionToken(_ context.Context, _ string, key string) (Token, bool, error) {
	r, ok := f.tokens[key]
	return r, ok, nil
}
func (f *fixture) SaveMentionToken(_ context.Context, _ delivery.Job, key string, r Token) (Token, error) {
	if old, ok := f.tokens[key]; ok {
		return old, nil
	}
	f.tokens[key] = r
	return r, nil
}
func (f *fixture) Group(context.Context, string, string) (*sdk.GroupInfo, error) {
	f.groupCalls++
	if f.groupErr != nil {
		return nil, f.groupErr
	}
	return &sdk.GroupInfo{ID: group, Name: "Group", Participants: f.members}, nil
}
func (f *fixture) ChatMember(context.Context, *bot.GetChatMemberParams) (*models.ChatMember, error) {
	f.memberCalls++
	if f.memberErr != nil {
		return nil, f.memberErr
	}
	return &models.ChatMember{Type: f.memberType}, nil
}

func TestIncomingTagsUseExplicitMetadataNativeEntitiesAndUTF16(t *testing.T) {
	f := newFixture(t)
	body := "😀 Hi @12025550123, @12025550123! Literal @12025550124"
	r, err := f.s.Incoming(t.Context(), f.job, group, body, []string{alice}, -200)
	if err != nil {
		t.Fatal(err)
	}
	if r.Text != "😀 Hi @Alice, @Alice! Literal @12025550124" || len(r.Entities) != 2 || f.memberCalls != 1 {
		t.Fatal(r, f.memberCalls)
	}
	for _, e := range r.Entities {
		v, err := presentation.Slice(r, e.Offset, e.Offset+e.Length)
		if err != nil || v.Text != "@Alice" || e.Type != models.MessageEntityTypeTextMention || e.User.ID != 42 {
			t.Fatal(e, err)
		}
	}
	if r.Entities[0].Offset != 6 {
		t.Fatal("offset is not UTF-16", r.Entities[0])
	}
}
func TestIncomingAbsentBotsAndPrivacyIDsUseCorrectLinks(t *testing.T) {
	f := newFixture(t)
	f.memberType = models.ChatMemberTypeLeft
	r, err := f.s.Incoming(t.Context(), f.job, group, "@12025550123 @12025550124 @987654321", []string{alice, bob, lid}, -200)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"https://t.me/alice_bot", "https://wa.me/12025550124", "https://wa.me/"}
	for i, e := range r.Entities {
		if e.URL != want[i] {
			t.Fatal(r)
		}
	}
	f.memberErr = &providers.Error{Service: "telegram", Status: 403}
	if _, err := f.s.Incoming(t.Context(), f.job, group, "@12025550123", []string{alice}, -200); err != nil {
		t.Fatal(err)
	}
	f.memberErr = &providers.Error{Service: "telegram", Status: 429, Retry: true}
	if _, err := f.s.Incoming(t.Context(), f.job, group, "@12025550123", []string{alice}, -200); !errors.Is(err, f.memberErr) {
		t.Fatal("rate refusal hidden", err)
	}
}
func TestIncomingAliasesAmbiguityAndUnicodeBoundaries(t *testing.T) {
	f := newFixture(t)
	var err error
	f.directory, err = identity.New([]identity.Alias{{ID: lid, Canonical: alice}}, []identity.Profile{{ID: alice, Name: "Alice"}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	r, err := f.s.Incoming(t.Context(), f.job, group, "@987654321 @12025550123 x@12025550123 é@12025550123 @12025550123é @@12025550123", []string{lid}, -200)
	if err != nil || r.Text != "@Alice @Alice x@12025550123 é@12025550123 @12025550123é @@12025550123" {
		t.Fatal(r, err)
	}
	r, err = f.s.Incoming(t.Context(), f.job, group, "@12025550123", []string{alice, "12025550123@lid"}, -200)
	if err != nil || len(r.Entities) != 0 {
		t.Fatal(r, err)
	}
}
func TestOutgoingNamesUsernamesAndCopiedTokensPreserveExactIDs(t *testing.T) {
	f := newFixture(t)
	token, err := f.s.Token(t.Context(), f.job, group, bob)
	if err != nil {
		t.Fatal(err)
	}
	text := "😀 @Alice + @ALICE_BOT and " + token + " @someone_else"
	r, err := f.s.Outgoing(t.Context(), f.job, group, text, []models.MessageEntity{{Type: models.MessageEntityTypeTextMention, Offset: 3, Length: 6, User: &models.User{ID: 42}}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Text != "😀 @12025550123 + @12025550123 and @12025550124 @someone_else" || !reflect.DeepEqual(r.Mentions, []string{alice, bob}) || f.groupCalls != 1 {
		t.Fatal(r, f.groupCalls)
	}
	// The label may change without changing the copied token's person.
	renamed := strings.Replace(token, "Bob", "Renamed", 1)
	r, err = f.s.Outgoing(t.Context(), f.job, group, renamed, nil)
	if err != nil || !reflect.DeepEqual(r.Mentions, []string{bob}) {
		t.Fatal(r, err)
	}
}
func TestOutgoingExcludesEmailURLsCodeAndUnrelatedUsernames(t *testing.T) {
	f := newFixture(t)
	text := "x@alice_bot https://t.me/@alice_bot @alice_bot @12025550123 @other"
	r, err := f.s.Outgoing(t.Context(), f.job, group, text, []models.MessageEntity{{Type: models.MessageEntityTypeCode, Offset: strings.LastIndex(text, "@alice_bot"), Length: 10}})
	if err != nil || r.Text != text || len(r.Mentions) != 0 || f.groupCalls != 0 {
		t.Fatal(r, err)
	}
	for _, kind := range []models.MessageEntityType{models.MessageEntityTypeCode, models.MessageEntityTypePre, models.MessageEntityTypeURL, models.MessageEntityTypeTextLink, models.MessageEntityTypeEmail} {
		r, err := f.s.Outgoing(t.Context(), f.job, group, "@alice_bot", []models.MessageEntity{{Type: kind, Offset: 0, Length: 10}})
		if err != nil || len(r.Mentions) != 0 {
			t.Fatal(kind, r, err)
		}
	}
}
func TestInvalidForeignAndDepartedTokensRefuseBeforeSending(t *testing.T) {
	f := newFixture(t)
	token, err := f.s.Token(t.Context(), f.job, group, bob)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"@{Bob|deadbeef}", "@{broken", token} {
		chat := group
		if text == token {
			chat = "other@g.us"
		}
		if _, err := f.s.Outgoing(t.Context(), f.job, chat, text, nil); !errors.Is(err, ErrTag) {
			t.Fatal(text, err)
		}
	}
	f.members = f.members[:1]
	if _, err := f.s.Outgoing(t.Context(), f.job, group, token, nil); !errors.Is(err, ErrDeparted) {
		t.Fatal(err)
	}
}
func TestRosterExpiryFallbackAndFutureClockDoNotAuthorizeStaleSends(t *testing.T) {
	f := newFixture(t)
	f.rosters[group] = Roster{ChatID: group, Name: "old", Members: []identity.Member{{ID: alice}}, FetchedAt: f.s.now().UnixMilli() - 300000}
	f.groupErr = errors.New("private provider body")
	r, err := f.s.Roster(t.Context(), f.job, group, true)
	if err != nil || !r.Cached {
		t.Fatal(r, err)
	}
	if _, err := f.s.Outgoing(t.Context(), f.job, group, "@alice_bot", nil); !errors.Is(err, ErrRoster) {
		t.Fatal(err)
	}
	r.FetchedAt = f.s.now().UnixMilli() + 1
	r.Cached = false
	f.rosters[group] = r
	if _, err := f.s.Outgoing(t.Context(), f.job, group, "@alice_bot", nil); !errors.Is(err, ErrRoster) {
		t.Fatal("future cache authorized send", err)
	}
	f.groupErr = nil
	if _, err := f.s.Outgoing(t.Context(), f.job, group, "@alice_bot", nil); err != nil {
		t.Fatal(err)
	}
}
func TestUTF16EntityCannotSplitSurrogatePair(t *testing.T) {
	f := newFixture(t)
	if _, err := f.s.Outgoing(t.Context(), f.job, group, "😀 @alice_bot", []models.MessageEntity{{Type: models.MessageEntityTypeTextMention, Offset: 1, Length: 1, User: &models.User{ID: 42}}}); !errors.Is(err, delivery.ErrInvalid) {
		t.Fatal(err)
	}
}

func TestCopiedTokenExtendsCollisionsAndFollowsLaterVerifiedAlias(t *testing.T) {
	f := newFixture(t)
	encoded, _ := json.Marshal([]string{digest(f.job.AccountID), group, lid})
	prefix := digest(string(encoded))[:8]
	f.tokens[prefix] = Token{ChatID: "other@g.us", PersonID: alice}
	token, err := f.s.Token(t.Context(), f.job, group, lid)
	if err != nil {
		t.Fatal(err)
	}
	match := tokenKey.FindStringSubmatch(token)
	if len(match) != 2 || len(match[1]) != 10 {
		t.Fatal("short-key collision not extended", token)
	}
	f.directory, err = identity.New([]identity.Alias{{ID: lid, Canonical: bob}}, []identity.Profile{{ID: bob, Name: "Renamed Bob"}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	r, err := f.s.Outgoing(t.Context(), f.job, group, token, nil)
	if err != nil || !reflect.DeepEqual(r.Mentions, []string{bob}) || r.Text != "@12025550124" {
		t.Fatal(r, err)
	}
}
