package bots

import (
	"github.com/go-telegram/bot/models"
	"testing"
)

func TestManagedIdentityRejectsIncompleteAndForeignNativeUpdates(t *testing.T) {
	for _, u := range []*models.Update{nil, {}, {ManagedBot: &models.ManagedBotUpdated{}}, {Message: &models.Message{ManagedBotCreated: &models.ManagedBotCreated{}}}, {ManagedBot: &models.ManagedBotUpdated{User: &models.User{ID: 124}, Bot: &models.User{ID: 200, IsBot: true}}}} {
		if _, ok := ManagedIdentity(u, 123); ok {
			t.Fatal("invalid managed update accepted")
		}
	}
	u := &models.Update{ManagedBot: &models.ManagedBotUpdated{User: &models.User{ID: 123}, Bot: &models.User{ID: 200, IsBot: true}}}
	if id, ok := ManagedIdentity(u, 123); !ok || id.ID != 200 {
		t.Fatal(id, ok)
	}
	for _, s := range []string{"", "201:wrong", "200:bad/token", "200:bad token"} {
		if ValidToken(s, 200) {
			t.Fatal("bad token accepted")
		}
	}
	if !ValidToken("200:fixture-token", 200) {
		t.Fatal("valid token refused")
	}
}
