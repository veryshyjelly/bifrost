package postgres

import (
	"testing"

	"github.com/veryshyjelly/bifrost/internal/delivery"
)

func TestMetadataResolvedContactKeepsLIDBindingWithoutLearningAlias(t *testing.T) {
	m := newMetadataFixture(t)
	const lid = "12345000000001@lid"
	m.f.bind(t, "200", lid)
	m.contact = `{"id":"12025550123@c.us","name":"Verified name","number":"12345000000001"}`
	m.phoneProof = `{"contactId":"12345000000001@lid","phone":"12025550123"}`
	id := m.enqueue(t, "resolved-contact", "contact", lid)
	if r := m.run(t, id); r.State != delivery.Done {
		t.Fatal(r)
	}
	var aliases int
	if err := m.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM contact_aliases").Scan(&aliases); err != nil || aliases != 0 {
		t.Fatal("metadata learned an identity alias", aliases, err)
	}
	var person, name string
	if err := m.f.store.pool.QueryRow(t.Context(), "SELECT person_id,name FROM contact_profiles WHERE name='Verified name'").Scan(&person, &name); err != nil || person != lid {
		t.Fatal("metadata changed the profile identity", person, err)
	}
	var binding string
	if err := m.f.store.pool.QueryRow(t.Context(), "SELECT contact_id FROM bridge_contact_bots WHERE bot_id='200'").Scan(&binding); err != nil || binding != lid {
		t.Fatal("metadata changed contact binding", binding, err)
	}
}
