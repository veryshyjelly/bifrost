package legacy

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/veryshyjelly/bifrost/internal/identity"
)

func receiptValue(bot string, chat, id int64, raw string) KV {
	return KV{Kind: "telegram-receipt", ID: telegramReceiptKey(telegramKey{bot: bot, chat: chat, id: id}), Value: raw}
}

func messageConfiguration(t *testing.T) (Configuration, *identity.Directory) {
	t.Helper()
	components, _ := fixtureComponents(t, "original-account")
	c, err := ParseConfiguration(components["bridge.env"], components["telegram-api.env"])
	if err != nil {
		t.Fatal(err)
	}
	d, err := identity.New([]identity.Alias{{ID: "999999@lid", Canonical: "12025550123@c.us"}}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return c, d
}

func TestProjectMessagesKeepsRealReceiptsUserObservationsAndPrivateBotScopesSeparate(t *testing.T) {
	c, d := messageConfiguration(t)
	cleanupError := "original cleanup failure"
	groupReceipt := `{"message_id":9001,"chat":{"id":-100},"from":{"id":42,"is_bot":true},"message_thread_id":7,"photo":[{"file_id":"group-file","file_unique_id":"group-unique","width":512,"height":512}],"future_field":{"kept":true}}`
	privateReceipt := `{"message_id":42,"chat":{"id":123},"from":{"id":42,"is_bot":true},"text":"master copy"}`
	userEnvelope := `{"bot":"77","update":{"update_id":1,"message":{"message_id":42,"chat":{"id":123},"from":{"id":123,"is_bot":false},"photo":[{"file_id":"private-file-77","file_unique_id":"private-unique","width":512,"height":512}]}}}`
	format := KV{Kind: "message-presentation", ID: telegramReceiptKey(telegramKey{bot: "master", chat: -100, id: 9001}), Value: `{"bot":"master","sender":"999999@lid","name":"Original linked name","historical":true,"timestamp":1700000000,"historyStyle":"compact","timeZone":"Asia/Kolkata"}`}
	input := MessageEvidence{
		Mappings: []Message{
			{WAChat: "999999@lid", WAID: "group-source", TGChat: "-100", TGID: 9001, Bot: "master", Direction: "in", Media: 1},
			{WAChat: "12025550123@c.us", WAID: "private-owner", TGChat: "123", TGID: 42, Bot: "77", Direction: "out", Media: 1},
			{WAChat: "12025550123@c.us", WAID: "private-master", TGChat: "123", TGID: 42, Bot: "master", Direction: "in"},
			{WAChat: "12025550123@c.us", WAID: "mapping-only", TGChat: "123", TGID: 43, Bot: "77", Direction: "in", Media: 1},
		},
		Observations: []TelegramMessage{
			{Seq: 9007199254740993, Scope: "*", Chat: "-100", Message: 9001, Bot: "master", Thread: 7, Kind: "chat", Date: 1700000000123, Error: &cleanupError},
			{Seq: 9007199254740994, Scope: "77", Chat: "123", Message: 42, Bot: "77", Kind: "command", Date: 1700000000123, Expires: 1700000060123},
		},
		Values: []KV{receiptValue("master", -100, 9001, groupReceipt), receiptValue("master", 123, 42, privateReceipt), format,
			{Kind: "wamessage", ID: "999999@lid:group-source", Value: `{"historical":true,"mentionedIds":["12025550124@c.us",{"_serialized":"888888@lid"}]}`}},
		Jobs: []Job{{ID: 1, Type: "tg", Payload: userEnvelope}},
	}
	result, err := ProjectMessages(c, "original-account", d, input)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Mappings) != 4 || len(result.Observations) != 2 {
		t.Fatal("retained message state lost")
	}
	group := result.Mappings[0]
	if group.Message.WAChat != "12025550123@c.us" || group.Thread != 7 || group.ReceiptOrigin != "cached-receipt" || !bytes.Equal(group.Receipt, []byte(groupReceipt)) || !bytes.Contains(group.Receipt, []byte("future_field")) {
		t.Fatal("group identity, thread or complete raw provider receipt changed")
	}
	if group.Presentation.Name != "Original linked name" || group.Presentation.Sender != "999999@lid" || group.Presentation.HistoryStyle != "compact" || group.Presentation.TimeZone != "Asia/Kolkata" || len(group.MentionedIDs) != 2 {
		t.Fatal("original formatting or legacy mention evidence changed")
	}
	owner := result.Mappings[1]
	if owner.ReceiptOrigin != "mapping-only" || string(owner.Receipt) != "null" || !bytes.Equal(owner.SourcePayload, []byte(userEnvelope)) || owner.Message.Media != 1 {
		t.Fatal("user observation promoted to provider ack or media evidence lost")
	}
	master := result.Mappings[2]
	if string(master.Receipt) != privateReceipt || len(master.SourcePayload) != 0 {
		t.Fatal("overlapping private message IDs crossed bot scope")
	}
	if result.Mappings[3].Thread != 0 || string(result.Mappings[3].Receipt) != "null" || result.Mappings[3].Message.Media != 1 {
		t.Fatal("mapping-only private media fabricated an acknowledgement")
	}
	if result.Observations[0].Observation.Seq != 9007199254740993 || result.Observations[0].Observation.Error == nil || *result.Observations[0].Observation.Error != cleanupError || !bytes.Equal(result.Observations[0].Payload, []byte(groupReceipt)) {
		t.Fatal("original cleanup sequence, error or payload changed")
	}
	if !bytes.Contains(result.Observations[1].Payload, []byte("private-file-77")) || bytes.Contains(result.Observations[1].Payload, []byte("master copy")) {
		t.Fatal("private observation used another bot's receipt")
	}
}

func TestProjectMessagesUsesConfirmedScopedEffectsAndNeverBorrowsAnUncertainReceipt(t *testing.T) {
	c, d := messageConfiguration(t)
	receipt := `{"message_id":42,"chat":{"id":-100},"from":{"id":888,"is_bot":true},"photo":[{"file_id":"old-master-file","file_unique_id":"old-unique","width":512,"height":512}]}`
	mapping := Message{WAChat: "12025550123@c.us", WAID: "source", TGChat: "-100", TGID: 42, Bot: "master", Direction: "in", Media: 1}
	input := MessageEvidence{Mappings: []Message{mapping}, Values: []KV{
		{Kind: "delivery-target", ID: "root", Value: `{"bot":"77","destination":"123"}`},
		{Kind: "delivery-target", ID: "root:to:77:123:prefix", Value: `{"bot":"master","destination":"-100","thread":7}`},
	}, Effects: []Effect{{ID: "root:to:77:123:prefix:to:master:-100:media", State: "done", Result: &receipt}}}
	result, err := ProjectMessages(c, "original-account", d, input)
	if err != nil || result.Mappings[0].Thread != 7 || result.Mappings[0].ReceiptOrigin != "confirmed-effect" || string(result.Mappings[0].Receipt) != receipt {
		t.Fatal("historical logical-master scope or frozen request thread lost", err)
	}
	input.Effects[0].State = "uncertain"
	if _, err := ProjectMessages(c, "original-account", d, input); !errors.Is(err, ErrMappingEvidence) {
		t.Fatal("unknown effect adopted as location/receipt evidence")
	}
	input.Observations = []TelegramMessage{{Seq: 1, Scope: "*", Chat: "-100", Message: 42, Bot: "master", Thread: 7, Kind: "chat"}}
	result, err = ProjectMessages(c, "original-account", d, input)
	if err != nil || result.Mappings[0].Thread != 7 || string(result.Mappings[0].Receipt) != "null" || result.Mappings[0].ReceiptOrigin != "mapping-only" {
		t.Fatal("known local mapping adopted an uncertain provider receipt")
	}
}

func TestProjectMessagesRefusesWrongBotReceiptsAndConflictingThreadEvidence(t *testing.T) {
	c, d := messageConfiguration(t)
	for _, tc := range []struct {
		mapping     Message
		receipt     string
		observation *TelegramMessage
	}{
		{mapping: Message{WAChat: "12025550123@c.us", WAID: "source", TGChat: "123", TGID: 42, Bot: "77", Direction: "in"}, receipt: `{"message_id":42,"chat":{"id":123},"from":{"id":42,"is_bot":true}}`},
		{mapping: Message{WAChat: "12025550123@c.us", WAID: "source", TGChat: "123", TGID: 42, Bot: "77", Direction: "in"}, receipt: `{"message_id":42,"chat":{"id":999},"from":{"id":77,"is_bot":true}}`},
		{mapping: Message{WAChat: "12025550123@c.us", WAID: "source", TGChat: "-100", TGID: 42, Bot: "master", Direction: "in"}, receipt: `{"message_id":42,"chat":{"id":-100},"message_thread_id":9}`, observation: &TelegramMessage{Seq: 1, Scope: "*", Chat: "-100", Message: 42, Bot: "master", Thread: 7, Kind: "chat"}},
	} {
		chat, _ := strconv.ParseInt(tc.mapping.TGChat, 10, 64)
		input := MessageEvidence{Mappings: []Message{tc.mapping}, Values: []KV{receiptValue(tc.mapping.Bot, chat, tc.mapping.TGID, tc.receipt)}}
		if tc.observation != nil {
			input.Observations = []TelegramMessage{*tc.observation}
		}
		if _, err := ProjectMessages(c, "original-account", d, input); !errors.Is(err, ErrMappingEvidence) {
			t.Fatal("conflicting scoped message evidence accepted")
		}
	}
}

func TestProjectMessagesPreservesMainChatWithIncidentalReplyThread(t *testing.T) {
	c, d := messageConfiguration(t)
	raw := `{"message_id":42,"chat":{"id":-100,"type":"supergroup"},"message_thread_id":9001,"text":"Reply in the main chat"}`
	input := MessageEvidence{Mappings: []Message{{WAChat: "12025550123@c.us", WAID: "source", TGChat: "-100", TGID: 42, Bot: "master", Direction: "in"}}, Observations: []TelegramMessage{{Seq: 1, Scope: "*", Chat: "-100", Message: 42, Bot: "master", Thread: 0, Kind: "chat"}}, Values: []KV{receiptValue("master", -100, 42, raw)}}
	r, err := ProjectMessages(c, "original-account", d, input)
	if err != nil || r.Mappings[0].Thread != 0 || string(r.Mappings[0].Receipt) != raw {
		t.Fatal("incidental reply thread replaced proven main-chat scope or original receipt", err)
	}
	input.Values = []KV{receiptValue("master", -100, 42, strings.Replace(raw, `"message_thread_id":9001`, `"is_topic_message":true,"message_thread_id":9001`, 1))}
	if _, err = ProjectMessages(c, "original-account", d, input); !errors.Is(err, ErrMappingEvidence) {
		t.Fatal("real forum topic conflict accepted", err)
	}
}

func TestProjectMessagesPreservesFallbackHistoryAndPermanentSelfIdentity(t *testing.T) {
	c, d := messageConfiguration(t)
	// A saved account can differ from the provider connection even when an old
	// environment did not yet contain BIFROST_ACCOUNT_ID.
	c = c.clone()
	delete(c.bridge, "BIFROST_ACCOUNT_ID")
	scope := legacyDirectoryScope("original-account")
	input := MessageEvidence{Mappings: []Message{{WAChat: "12025550123@c.us", WAID: "source", TGChat: "123", TGID: 42, Bot: "77", Direction: "out"}},
		Values: []KV{{Kind: "reaction-self", ID: scope, Value: `{"sender":"12025550999@c.us"}`}},
		Jobs:   []Job{{ID: 2, Type: "wa", Payload: `{"event":"message.received","historical":false,"data":{"id":"source","chatId":"12025550123@c.us","timestamp":200}}`}, {ID: 1, Type: "wa", Payload: `{"event":"message.sent","historical":true,"data":{"id":"source","chatId":"12025550123@c.us","timestamp":100,"name":"Not the fallback name","mentionedIds":[]}}`}},
	}
	result, err := ProjectMessages(c, "original-account", d, input)
	if err != nil {
		t.Fatal(err)
	}
	format := result.Mappings[0].Presentation
	if !format.Own || !format.Historical || format.Timestamp != 100 || format.Name != "WhatsApp" || format.Sender != "12025550999@c.us" || result.Mappings[0].MentionedIDs == nil || len(result.Mappings[0].MentionedIDs) != 0 {
		t.Fatal("fallback presentation used current connection or replaced original history evidence")
	}
	if strings.Contains(string(result.Mappings[0].Receipt), "12025550999") {
		t.Fatal("source identity fabricated a provider receipt")
	}
	if _, err := json.Marshal(result); err != nil {
		t.Fatal(err)
	}
}

func TestProjectMessagesNeverDerivesSelfPhoneFromLIDDisplay(t *testing.T) {
	c, d := messageConfiguration(t)
	input := MessageEvidence{Values: []KV{{Kind: "reaction-self", ID: legacyDirectoryScope("original-account"), Value: `{"sender":"999999@lid"}`}}}
	if _, err := ProjectMessages(c, "original-account", d, input); !errors.Is(err, ErrInvalid) {
		t.Fatal("LID display identity became verified self phone evidence")
	}
}
