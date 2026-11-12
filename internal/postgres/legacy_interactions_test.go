package postgres

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/legacy"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
	"github.com/veryshyjelly/bifrost/internal/reactions"
)

func legacyInteractionSQL() string {
	return legacyChangeSQL() +
		legacyValueSQL("poll-selection", "999999@lid:poll:source", `[" B ","A"]`) +
		legacyValueSQL("poll-selection", "12345@g.us:cleared", `[]`) +
		legacyValueSQL("custom-reaction-emoji", "12345", `{"emoji":"❤️"}`) +
		legacyValueSQL("outgoing-reaction", legacy.OutgoingReactionKey("fixture-account", "master", "tg:master:201"), `{"emoji":"👍"}`) +
		legacyValueSQL("outgoing-reaction", legacy.OutgoingReactionKey("fixture-account", "master", "tg:master:202"), `{"unsupported":true}`) +
		`INSERT INTO effects VALUES('tg:master:201:react','sending',NULL);`
}

func prepareInteractionFixture(t *testing.T, s *Store) *legacy.Bundle {
	t.Helper()
	b := prepareChangeFixture(t, s, legacyInteractionSQL())
	if _, err := s.ProjectLegacyChanges(t.Context(), b.Account); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestLegacyInteractionsPreserveSelectedOptionsCachesAndPendingReactionResolution(t *testing.T) {
	s := testStore(t)
	b := prepareInteractionFixture(t, s)
	result, err := s.ProjectLegacyInteractions(t.Context(), b.Account)
	if err != nil || result.Polls != 2 || result.Custom != 1 || result.Choices != 2 {
		t.Fatal("interaction projection incomplete", err)
	}
	selection := db.PollSelectionParams{AccountID: b.Account, WhatsappChat: "12025550123@c.us", MessageID: "poll:source"}
	raw, err := s.queries.PollSelection(t.Context(), selection)
	var options []string
	if err != nil || json.Unmarshal(raw, &options) != nil || !reflect.DeepEqual(options, []string{" B ", "A"}) {
		t.Fatal("native poll selection lost exact text/order/alias scope", err)
	}
	if emoji, found, err := s.CustomReactionEmoji(t.Context(), b.Account, "12345"); err != nil || !found || emoji != "❤️" {
		t.Fatal("native custom equivalent cache missing", err)
	}
	if _, found, err := s.CustomReactionEmoji(t.Context(), "another-account", "12345"); err != nil || found {
		t.Fatal("custom equivalent escaped account scope", err)
	}
	if !errors.Is(s.LegacyRuntimeReady(t.Context(), b.Account), ErrLegacyPending) {
		t.Fatal("interaction projection activated incomplete import")
	}
	var jobs, effects int
	if err := s.pool.QueryRow(t.Context(), "SELECT (SELECT count(*) FROM jobs),(SELECT count(*) FROM effects)").Scan(&jobs, &effects); err != nil || jobs != 0 || effects != 0 {
		t.Fatal("frozen resolution became operation evidence", err)
	}
	// Only this isolated consumer fixture marks unrelated import stages done.
	if _, err := s.pool.Exec(t.Context(), "UPDATE legacy_imports SET activated_at=now()"); err != nil {
		t.Fatal(err)
	}
	u := models.Update{ID: 201, MessageReaction: &models.MessageReactionUpdated{Chat: models.Chat{ID: -100, Type: models.ChatTypeSupergroup}, MessageID: 10, User: &models.User{ID: 123}, NewReaction: []models.ReactionType{customReaction("12345")}}}
	payload, _ := json.Marshal(map[string]any{"bot": "master", "update": u})
	if accepted, err := s.Enqueue(t.Context(), delivery.Event{AccountID: b.Account, Dedup: "tg:master:201", Stream: "tg:-100:44", Kind: "tg", Payload: payload}); err != nil || !accepted {
		t.Fatal(err)
	}
	job := claim(t, s)
	c := reactions.Settings{Session: "fixture-session", OwnerID: 123, NoticeTTL: 60}
	input, err := s.OutgoingReactionInput(t.Context(), job, c)
	if err != nil || !input.Mapped || input.FrozenChoice == nil || !input.FrozenChoice.Supported || input.FrozenChoice.Emoji != "👍" {
		t.Fatal("native input ignored exact original frozen resolution", err)
	}
	p := reactions.OutgoingPlan{Key: job.Dedup, Session: c.Session, Input: input, Supported: true, Emoji: "👍", State: "pending", NoticeTTL: c.NoticeTTL}
	wrong := p
	wrong.Emoji = "👎"
	if _, err := s.PrepareOutgoingReaction(t.Context(), job, wrong, c); !errors.Is(err, delivery.ErrInvalid) {
		t.Fatal("caller displaced frozen resolution", err)
	}
	plan, err := s.PrepareOutgoingReaction(t.Context(), job, p, c)
	if err != nil || plan.State != "pending" || plan.Emoji != "👍" {
		t.Fatal("saved resolution confirmed unknown legacy operation", err)
	}
	var sourceState string
	if err := s.pool.QueryRow(t.Context(), "SELECT convert_from(raw_row,'UTF8')::jsonb->>'state' FROM legacy_rows WHERE source_table='effects'").Scan(&sourceState); err != nil || sourceState != "sending" {
		t.Fatal("original uncertain effect evidence changed", err)
	}
	if err := s.queries.SetPollSelection(t.Context(), db.SetPollSelectionParams{AccountID: b.Account, WhatsappChat: selection.WhatsappChat, MessageID: selection.MessageID, Options: []byte(`["Later choice"]`)}); err != nil {
		t.Fatal(err)
	}
	if err := s.queries.SaveCustomReactionEmoji(t.Context(), db.SaveCustomReactionEmojiParams{AccountID: b.Account, CustomID: "12345", Emoji: "👎"}); err != nil {
		t.Fatal(err)
	}
	replay, err := s.ProjectLegacyInteractions(t.Context(), b.Account)
	if err != nil || !reflect.DeepEqual(result, replay) {
		t.Fatal("interaction replay changed initial result", err)
	}
	raw, err = s.queries.PollSelection(t.Context(), selection)
	if err != nil || json.Unmarshal(raw, &options) != nil || !reflect.DeepEqual(options, []string{"Later choice"}) {
		t.Fatal("projection replay reset later poll state", err)
	}
	if emoji, found, err := s.CustomReactionEmoji(t.Context(), b.Account, "12345"); err != nil || !found || emoji != "👎" {
		t.Fatal("projection replay reset later custom cache", err)
	}
}

func TestLegacyInteractionProjectionRollsBackAndRefusesExistingState(t *testing.T) {
	s := testStore(t)
	b := prepareInteractionFixture(t, s)
	_, err := s.pool.Exec(t.Context(), `CREATE FUNCTION reject_interaction_projection() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture failure'; END; $$;
CREATE TRIGGER reject_interaction_projection BEFORE INSERT ON legacy_interaction_projections FOR EACH ROW EXECUTE FUNCTION reject_interaction_projection();`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProjectLegacyInteractions(t.Context(), b.Account); err == nil {
		t.Fatal("failed checkpoint committed")
	}
	for _, table := range []string{"poll_selections", "custom_reaction_emojis", "retained_reaction_choices", "legacy_interaction_projections"} {
		var n int
		if err := s.pool.QueryRow(t.Context(), "SELECT count(*) FROM "+table).Scan(&n); err != nil || n != 0 {
			t.Fatal("partial interaction state survived rollback", table, err)
		}
	}
	if _, err := s.pool.Exec(t.Context(), "DROP TRIGGER reject_interaction_projection ON legacy_interaction_projections"); err != nil {
		t.Fatal(err)
	}
	if err := s.queries.SaveCustomReactionEmoji(t.Context(), db.SaveCustomReactionEmojiParams{AccountID: b.Account, CustomID: "native", Emoji: "👍"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProjectLegacyInteractions(t.Context(), b.Account); !errors.Is(err, ErrLegacyDestination) {
		t.Fatal("existing native interaction state overwritten", err)
	}
}

func TestLegacyReactionChoicesRemainFrozenWithoutRefetchingOrInventingSuccess(t *testing.T) {
	f := newReactionFixture(t)
	if r := f.f.n.incoming(t, "source-image", "Caption"); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.f.n.customEmoji = "👎"
	for _, tc := range []struct {
		update int64
		key    string
		emoji  string
		ok     bool
	}{{301, "tg:master:301", "👍", true}, {302, "tg:master:302", "", false}, {303, "tg:master:303", "", true}} {
		if err := f.f.n.f.store.queries.SaveRetainedReactionChoice(t.Context(), db.SaveRetainedReactionChoiceParams{AccountID: "fixture-account", SourceKey: legacy.OutgoingReactionKey("fixture-account", "master", tc.key), Emoji: tc.emoji, Supported: tc.ok}); err != nil {
			t.Fatal(err)
		}
		if r := f.outgoing(t, tc.update, nil, []models.ReactionType{customReaction("12345")}); r.State != delivery.Done {
			t.Fatal(r)
		}
	}
	f.f.n.mu.Lock()
	defer f.f.n.mu.Unlock()
	if len(f.f.n.sends) != 2 || string(f.f.n.sends[0].Request["emoji"]) != `"👍"` || string(f.f.n.sends[1].Request["emoji"]) != `""` {
		t.Fatal("frozen standard/unsupported/clear resolution changed", f.f.n.sends)
	}
	for _, call := range f.f.n.calls {
		if call.Method == "getCustomEmojiStickers" {
			t.Fatal("frozen resolution refetched from provider")
		}
	}
}
