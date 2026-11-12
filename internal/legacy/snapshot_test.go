package legacy

import (
	"errors"
	"strings"
	"testing"
)

func TestSnapshotPreservesExactJobEffectAndMappingEvidence(t *testing.T) {
	components, _ := fixtureComponents(t, "original-account", `
INSERT INTO jobs VALUES(9007199254740993,'frozen-send-key','wa:peer','wa','{"accountId":"original-account","sessionId":"old-provider-session","data":{"id":"source-id","timestamp":1700000000}}','running',4,1700000012345,'original failure',1700000000123);
INSERT INTO effects VALUES('frozen-send-key:to:master:-100:text:0','sending',NULL);
INSERT INTO effects VALUES('confirmed-before-crash','done','{"message_id":42,"chat":{"id":-100}}');
INSERT INTO aliases VALUES('999999@lid','12025550123@c.us');
INSERT INTO kv VALUES('contact','999999@lid','{"name":"Example","number":"999999","bot":"77"}');
INSERT INTO messages VALUES('12025550123@c.us','source-id','-100',42,'master','in',0);
INSERT INTO messages VALUES('12025550123@c.us','private-source','123',42,'77','out',1);
INSERT INTO telegram_messages VALUES(9007199254740993,'*','-100',42,'master',7,'chat',1700000000000,0,0,NULL);
INSERT INTO telegram_messages VALUES(9007199254740994,'77','123',42,'77',0,'command',1700000000000,1700000060000,0,'cleanup failed');
`)
	archive := writeFixtureBundle(t, components, nil, nil)
	b, err := OpenBundle(t.Context(), archive, BundleOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	var job Job
	var sending Effect
	var mapping Message
	var alias Alias
	report, err := ReadSnapshot(t.Context(), b, SQLiteOptions{}, func(record Record) error {
		if record.Job != nil {
			job = *record.Job
		}
		if record.Effect != nil && record.Effect.State == "sending" {
			sending = *record.Effect
		}
		if record.Message != nil && record.Message.Direction == "out" {
			mapping = *record.Message
		}
		if record.Alias != nil {
			alias = *record.Alias
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.ID != 9007199254740993 || job.Due != 1700000012345 || job.Created != 1700000000123 || job.State != "running" || job.Attempts != 4 || job.Error == nil || *job.Error != "original failure" || !strings.Contains(job.Payload, "old-provider-session") {
		t.Fatal("original job changed")
	}
	if sending.ID != "frozen-send-key:to:master:-100:text:0" || sending.State != "sending" || sending.Result != nil {
		t.Fatal("unknown send evidence changed")
	}
	if mapping.TGID != 42 || mapping.Bot != "77" || mapping.Media != 1 || alias.Canonical != "12025550123@c.us" {
		t.Fatal("scope or explicit alias changed")
	}
	if report.JobStates["running"] != 1 || report.EffectStates["sending"] != 1 || report.EffectStates["done"] != 1 || report.Rows["telegram_messages"] != 2 || report.Domains["contact"] != 1 || report.Digest != b.SQLite.Digest {
		t.Fatal("incomplete snapshot inventory")
	}
}

func TestSnapshotRejectsAmbiguousIdentityScopeAndUnsupportedWork(t *testing.T) {
	cases := []struct {
		name, sql   string
		unsupported bool
	}{
		{"unknown domain", `INSERT INTO kv VALUES('unsupported-private-domain','key','{}');`, true},
		{"unknown job", `INSERT INTO jobs VALUES(1,'key','stream','unsupported','{}','pending',0,0,NULL,1);`, true},
		{"null key", `INSERT INTO kv VALUES(NULL,'key','{}');`, false},
		{"duplicate JSON key", `INSERT INTO kv VALUES('wa-complete','key','{"same":1,"same":2}');`, false},
		{"JSON NUL", `INSERT INTO kv VALUES('wa-complete','key','"\u0000"');`, false},
		{"unknown effect state", `INSERT INTO effects VALUES('key','failed',NULL);`, false},
		{"missing confirmed receipt", `INSERT INTO effects VALUES('key','done',NULL);`, false},
		{"alias collision", `INSERT INTO aliases VALUES('999999@lid','12025550123@c.us'); INSERT INTO aliases VALUES('999999:2@lid','12025550124@c.us');`, false},
		{"LID display is not phone evidence", `INSERT INTO aliases VALUES('999999@lid','999999@lid');`, false},
		{"unregistered bot binding", `INSERT INTO kv VALUES('contact','12025550123@c.us','{"bot":"999"}');`, false},
		{"unregistered migration", `INSERT INTO kv VALUES('migrated','999','true');`, false},
		{"invalid migration proof", `INSERT INTO kv VALUES('migrated','77','{"eligible":true}');`, false},
		{"wrong private scope", `INSERT INTO telegram_messages VALUES(1,'*','123',42,'77',0,'chat',1,0,0,NULL);`, false},
		{"wrong group scope", `INSERT INTO telegram_messages VALUES(1,'77','-100',42,'77',0,'chat',1,0,0,NULL);`, false},
		{"ambiguous group mapping", `INSERT INTO messages VALUES('12025550123@c.us','first','-100',42,'77','in',0); INSERT INTO messages VALUES('12025550124@c.us','second','-100',42,'master','in',0);`, false},
		{"negative attempt count", `INSERT INTO jobs VALUES(1,'key','stream','wa','{}','pending',-1,0,NULL,1);`, false},
		{"unrecoverable job sequence", `INSERT INTO jobs VALUES(9223372036854775807,'key','stream','wa','{}','pending',0,0,NULL,1);`, false},
		{"invalid time", `INSERT INTO jobs VALUES(1,'key','stream','wa','{}','pending',0,-1,NULL,1);`, false},
		{"invalid payload", `INSERT INTO jobs VALUES(1,'key','stream','wa','null','pending',0,0,NULL,1);`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			components, _ := fixtureComponents(t, "original-account", tc.sql)
			b, err := OpenBundle(t.Context(), writeFixtureBundle(t, components, nil, nil), BundleOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer b.Close()
			_, err = ReadSnapshot(t.Context(), b, SQLiteOptions{}, nil)
			expected := ErrInvalid
			if tc.unsupported {
				expected = ErrUnsupported
			}
			if !errors.Is(err, expected) {
				t.Fatalf("expected %v, got %v", expected, err)
			}
		})
	}
}

func TestSnapshotCallbackFailureCannotBecomeSuccessfulValidation(t *testing.T) {
	components, _ := fixtureComponents(t, "original-account")
	b, err := OpenBundle(t.Context(), writeFixtureBundle(t, components, nil, nil), BundleOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	stop := errors.New("destination transaction failed")
	if report, err := ReadSnapshot(t.Context(), b, SQLiteOptions{}, func(Record) error { return stop }); !errors.Is(err, stop) || report.Digest != "" {
		t.Fatal("partial callback treated as validated snapshot")
	}
}
