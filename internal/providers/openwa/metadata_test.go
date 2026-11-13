package openwa

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMetadataGroupPreservesDescriptionPresenceAndRejectsIncompleteRosters(t *testing.T) {
	for _, tc := range []struct {
		name, extra, participants string
		want                      *string
		invalid                   bool
	}{
		{name: "absent", participants: `[]`},
		{name: "null", extra: `,"description":null`, participants: `[]`},
		{name: "empty", extra: `,"description":""`, participants: `[]`, want: new("")},
		{name: "available", extra: `,"description":"About"`, participants: `[{"id":"12345000000001@lid","number":"2025550123"}]`, want: new("About")},
		{name: "missing roster", participants: `null`, invalid: true},
		{name: "bad member", participants: `[{"id":"not-a-person"}]`, invalid: true},
		{name: "bad description", extra: `,"description":false`, participants: `[]`, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "GET" || r.URL.Path != "/api/sessions/session/groups/123@g.us" {
					t.Error("unexpected source mutation or path", r.Method, r.URL.Path)
				}
				fmt.Fprintf(w, `{"id":"123@g.us","name":"Group","participants":%s%s}`, tc.participants, tc.extra)
			}))
			defer server.Close()
			c, err := New(server.URL, "fixture-key", nil)
			if err != nil {
				t.Fatal(err)
			}
			group, err := c.GroupMetadata(t.Context(), "session", "123@g.us")
			if (err != nil) != tc.invalid {
				t.Fatal(group, err)
			}
			if !tc.invalid && ((group.Description == nil) != (tc.want == nil) || tc.want != nil && *group.Description != *tc.want) {
				t.Fatal("description availability changed", group)
			}
			if calls != 1 {
				t.Fatal("hidden retry", calls)
			}
		})
	}
}

func TestMetadataContactReadsNativeRecordWithoutInferringLIDPhone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/sessions/session/contacts/12345000000001@lid" {
			t.Error("unexpected source mutation", r.Method, r.URL.Path)
		}
		fmt.Fprint(w, `{"id":"12345000000001@lid","name":null,"pushName":"Friend","number":"12345000000001"}`)
	}))
	defer server.Close()
	c, err := New(server.URL, "fixture-key", nil)
	if err != nil {
		t.Fatal(err)
	}
	contact, err := c.Contact(t.Context(), "session", "12345000000001@lid")
	if err != nil || contact.ID != "12345000000001@lid" || contact.Name != nil || contact.PushName == nil || *contact.PushName != "Friend" {
		t.Fatal(contact, err)
	}
}

func TestMetadataCanonicalContactRequiresIndependentPhoneProof(t *testing.T) {
	for _, outcome := range []string{"match", "missing", "wrong scope", "wrong phone", "phone failure", "reverse mismatch"} {
		t.Run(outcome, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "GET" {
					t.Error("metadata mutated source")
				}
				if calls == 1 {
					fmt.Fprint(w, `{"id":"12025550123@c.us","name":"Verified","number":"12345000000001"}`)
					return
				}
				if r.URL.Path != "/api/sessions/session/contacts/12345000000001@lid/phone" {
					t.Error("proof queried a different contact")
				}
				switch outcome {
				case "match":
					fmt.Fprint(w, `{"contactId":"12345000000001@lid","phone":"12025550123"}`)
				case "wrong scope":
					fmt.Fprint(w, `{"contactId":"99999999999999@lid","phone":"12025550123"}`)
				case "wrong phone":
					fmt.Fprint(w, `{"contactId":"12345000000001@lid","phone":"12025550124"}`)
				case "phone failure":
					w.WriteHeader(503)
					fmt.Fprint(w, `{}`)
				default:
					fmt.Fprint(w, `{"contactId":"12345000000001@lid","phone":null}`)
				}
			}))
			defer server.Close()
			c, err := New(server.URL, "fixture-key", nil)
			if err != nil {
				t.Fatal(err)
			}
			id := "12345000000001@lid"
			if outcome == "reverse mismatch" {
				id = "12025550124@c.us"
			}
			record, err := c.Contact(t.Context(), "session", id)
			if outcome == "match" {
				if err != nil || record.ID != id || record.Name == nil || *record.Name != "Verified" {
					t.Fatal(record, err)
				}
			} else if err == nil {
				t.Fatal("unproved identity accepted")
			}
			want := 2
			if outcome == "reverse mismatch" {
				want = 1
			}
			if calls != want {
				t.Fatal("unexpected lookup or replay", calls)
			}
		})
	}
}
