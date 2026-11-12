package legacy

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/veryshyjelly/bifrost/internal/secrets"
)

type fixtureEntry struct {
	name string
	body []byte
	kind byte
	link string
}

func fixtureTar(t *testing.T, entries []fixtureEntry, tail []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	z := gzip.NewWriter(&out)
	tw := tar.NewWriter(z)
	for _, entry := range entries {
		kind := entry.kind
		if kind == 0 {
			kind = tar.TypeReg
		}
		if err := tw.WriteHeader(&tar.Header{Name: entry.name, Typeflag: kind, Mode: 0777, Size: int64(len(entry.body)), Linkname: entry.link}); err != nil {
			t.Fatal(err)
		}
		if len(entry.body) != 0 {
			if _, err := tw.Write(entry.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := z.Write(tail); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func fixtureComponents(t *testing.T, savedAccount string, extra ...string) (map[string][]byte, string) {
	t.Helper()
	key := [32]byte{1, 2, 3}
	cipher, _ := secrets.New(key)
	sealed, _ := cipher.Seal("77:preserve-original-token")
	bot, _ := json.Marshal(map[string]any{"token": sealed, "username": "fixture_bot", "ready": true})
	identity, _ := json.Marshal(map[string]any{"id": savedAccount})
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	source := sqliteFixture(t, "INSERT INTO kv VALUES('identity','account',"+quote(string(identity))+"); INSERT INTO kv VALUES('bot','77',"+quote(string(bot))+");"+strings.Join(extra, "\n"), false)
	db, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	bridge := "BRIDGE_ENCRYPTION_KEY=" + hex.EncodeToString(key[:]) + "\nBRIDGE_ADMIN_TOKEN=" + strings.Repeat("a", 32) + "\nOPENWA_WEBHOOK_SECRET=" + strings.Repeat("w", 32) + "\nTELEGRAM_MASTER_TOKEN=42:original-master-token\nTELEGRAM_OWNER_ID=123\nTELEGRAM_MANAGEMENT_CHAT_ID=-100\nTELEGRAM_API_ID=12345\nTELEGRAM_API_HASH=" + strings.Repeat("a", 32) + "\nBIFROST_ACCOUNT_ID=original-account\nOPENWA_SESSION_ID=rotated-connection\n"
	return map[string][]byte{
		"bridge.env":           []byte(bridge),
		"telegram-api.env":     []byte("TELEGRAM_API_ID=12345\nTELEGRAM_API_HASH=" + strings.Repeat("a", 32) + "\n"),
		"data.tar.gz":          fixtureTar(t, []fixtureEntry{{name: ".", kind: tar.TypeDir}, {name: "./bridge.db", body: db}}, nil),
		"media.tar.gz":         fixtureTar(t, []fixtureEntry{{name: ".", kind: tar.TypeDir}, {name: "./nested/source.webp", body: []byte("original frozen bytes")}}, nil),
		"telegram-data.tar.gz": fixtureTar(t, []fixtureEntry{{name: ".", kind: tar.TypeDir}, {name: "42/cache/file", body: []byte("retained local API file")}}, nil),
	}, sealed
}

func writeFixtureBundle(t *testing.T, components map[string][]byte, alter func(*Manifest), extra []fixtureEntry) string {
	t.Helper()
	m := Manifest{Format: "bifrost", Version: 1, Created: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Source: ManifestSource{Revision: "checkpoint-typescript-baseline", Project: "legacy-bifrost"}, Files: map[string]string{}}
	entries := []fixtureEntry{{name: ".", kind: tar.TypeDir}}
	for _, name := range componentNames {
		sum := sha256.Sum256(components[name])
		m.Files[name] = hex.EncodeToString(sum[:])
		entries = append(entries, fixtureEntry{name: "./" + name, body: components[name]})
	}
	if alter != nil {
		alter(&m)
	}
	raw, _ := json.Marshal(m)
	entries = append(entries, fixtureEntry{name: "manifest.json", body: raw})
	entries = append(entries, extra...)
	file := filepath.Join(t.TempDir(), "private-export.tar.gz")
	if err := os.WriteFile(file, fixtureTar(t, entries, nil), 0600); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestBundlePreservesOriginalIdentitySecretsAndVolumes(t *testing.T) {
	components, sealed := fixtureComponents(t, "original-account")
	archive := writeFixtureBundle(t, components, nil, nil)
	before, _ := os.ReadFile(archive)
	scratch := t.TempDir()
	b, err := OpenBundle(t.Context(), archive, BundleOptions{Scratch: scratch})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	if b.Account != "original-account" || b.SQLite.Counts["kv"] != 2 || len(b.Digest) != 64 {
		t.Fatalf("invalid safe summary: account=%q, counts=%v", b.Account, b.SQLite.Counts)
	}
	if sum := sha256.Sum256(before); b.Digest != hex.EncodeToString(sum[:]) {
		t.Fatal("archive digest mismatch")
	}
	config := b.Configuration()
	if config.Value("OPENWA_SESSION_ID") != "rotated-connection" || config.MasterID() != 42 || config.OwnerID() != 123 || config.ManagementChatID() != -100 {
		t.Fatal("configuration changed")
	}
	bridge, telegram := config.Files()
	if !bytes.Equal(bridge, components["bridge.env"]) || !bytes.Equal(telegram, components["telegram-api.env"]) {
		t.Fatal("original private files changed")
	}
	if strings.Contains(fmt.Sprintf("%v %#v", config, config), config.Value("TELEGRAM_MASTER_TOKEN")) {
		t.Fatal("configuration formatting exposed credentials")
	}
	media, _ := b.Directory("media")
	data, err := os.ReadFile(filepath.Join(media, "nested", "source.webp"))
	if err != nil || string(data) != "original frozen bytes" {
		t.Fatal("media not preserved")
	}
	stat, _ := os.Stat(filepath.Join(media, "nested", "source.webp"))
	if stat.Mode().Perm() != 0600 {
		t.Fatal("archived executable permissions retained")
	}
	dbdir, _ := b.Directory("data")
	found := false
	_, err = ReadStagedSQLite(t.Context(), filepath.Join(dbdir, "bridge.db"), SQLiteOptions{}, func(row Row) error {
		if bytes.Contains(row.Values, []byte(sealed)) {
			found = true
		}
		return nil
	})
	if err != nil || !found {
		t.Fatal("original encrypted bot credential changed")
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Directory("media"); !errors.Is(err, ErrInvalid) {
		t.Fatal("closed bundle exposed stale path")
	}
	children, _ := os.ReadDir(scratch)
	after, _ := os.ReadFile(archive)
	if len(children) != 0 || !bytes.Equal(before, after) {
		t.Fatal("cleanup affected source or leaked stage")
	}
}

func TestBundleRejectsTamperingAndUnsafeArchivesWithoutLeakingStages(t *testing.T) {
	components, _ := fixtureComponents(t, "original-account")
	cases := []struct {
		name     string
		edit     func(map[string][]byte)
		manifest func(*Manifest)
		extra    []fixtureEntry
		opts     BundleOptions
	}{
		{name: "checksum", manifest: func(m *Manifest) { m.Files["media.tar.gz"] = strings.Repeat("0", 64) }},
		{name: "unexpected outer file", extra: []fixtureEntry{{name: "execute.py", body: []byte("untrusted")}}},
		{name: "duplicate normalized path", extra: []fixtureEntry{{name: "././bridge.env", body: []byte("duplicate")}}},
		{name: "source identity conflict", edit: func(c map[string][]byte) {
			c["bridge.env"] = bytes.ReplaceAll(c["bridge.env"], []byte("BIFROST_ACCOUNT_ID=original-account"), []byte("BIFROST_ACCOUNT_ID=other-account"))
		}},
		{name: "wrong original key", edit: func(c map[string][]byte) {
			c["bridge.env"] = bytes.ReplaceAll(c["bridge.env"], []byte("BRIDGE_ENCRYPTION_KEY=010203"), []byte("BRIDGE_ENCRYPTION_KEY=040506"))
		}},
		{name: "mismatched API account", edit: func(c map[string][]byte) {
			c["telegram-api.env"] = bytes.ReplaceAll(c["telegram-api.env"], []byte("12345"), []byte("99999"))
		}},
		{name: "duplicate secret", edit: func(c map[string][]byte) {
			c["bridge.env"] = append(bytes.Clone(c["bridge.env"]), []byte("BRIDGE_ADMIN_TOKEN=second-secret\n")...)
		}},
		{name: "expanded byte limit", opts: BundleOptions{MaxExpandedBytes: 1024}},
		{name: "entry limit", opts: BundleOptions{MaxEntries: 7}},
		{name: "archive byte limit", opts: BundleOptions{MaxArchiveBytes: 10}},
		{name: "per-file limit", opts: BundleOptions{MaxFileBytes: 512}},
	}
	for _, entry := range []fixtureEntry{
		{name: "../outside", body: []byte("escape")},
		{name: "/absolute", body: []byte("escape")},
		{name: "nested/../outside", body: []byte("escape")},
		{name: "nested\\outside", body: []byte("escape")},
		{name: "link", kind: tar.TypeSymlink, link: "../../outside"},
		{name: "link", kind: tar.TypeLink, link: "outside"},
		{name: "device", kind: tar.TypeFifo},
	} {
		cases = append(cases, struct {
			name     string
			edit     func(map[string][]byte)
			manifest func(*Manifest)
			extra    []fixtureEntry
			opts     BundleOptions
		}{name: "unsafe " + entry.name, edit: func(c map[string][]byte) { c["media.tar.gz"] = fixtureTar(t, []fixtureEntry{entry}, nil) }})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			copy := make(map[string][]byte)
			for k, v := range components {
				copy[k] = bytes.Clone(v)
			}
			if tc.edit != nil {
				tc.edit(copy)
			}
			archive := writeFixtureBundle(t, copy, tc.manifest, tc.extra)
			before, _ := os.ReadFile(archive)
			tc.opts.Scratch = t.TempDir()
			b, err := OpenBundle(t.Context(), archive, tc.opts)
			if b != nil {
				_ = b.Close()
				t.Fatal("invalid bundle accepted")
			}
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("expected safe validation error, got %v", err)
			}
			children, _ := os.ReadDir(tc.opts.Scratch)
			after, _ := os.ReadFile(archive)
			if len(children) != 0 || !bytes.Equal(before, after) {
				t.Fatal("source changed or failed stage leaked")
			}
		})
	}
}

func TestBundleRejectsGzipCorruptionTrailingMembersAndHiddenTarData(t *testing.T) {
	components, _ := fixtureComponents(t, "original-account")
	valid := writeFixtureBundle(t, components, nil, nil)
	raw, _ := os.ReadFile(valid)
	corrupt := bytes.Clone(raw)
	corrupt[len(corrupt)-8] ^= 1
	second := append(bytes.Clone(raw), fixtureTar(t, nil, nil)...)
	trailing := append(bytes.Clone(raw), byte('x'))
	for name, data := range map[string][]byte{"checksum": corrupt, "truncated": raw[:len(raw)-5], "second member": second, "trailing byte": trailing} {
		t.Run(name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "export.gz")
			_ = os.WriteFile(file, data, 0600)
			if b, err := OpenBundle(t.Context(), file, BundleOptions{}); err == nil {
				_ = b.Close()
				t.Fatal("damaged gzip accepted")
			}
		})
	}
	for name, data := range map[string][]byte{
		"hidden tar":     fixtureTar(t, nil, []byte("hidden")),
		"padding bomb":   fixtureTar(t, nil, make([]byte, (1<<20)+1)),
		"duplicate root": fixtureTar(t, []fixtureEntry{{name: ".", kind: tar.TypeDir}, {name: "./", kind: tar.TypeDir}}, nil),
		"file parent":    fixtureTar(t, []fixtureEntry{{name: "parent", body: []byte("file")}, {name: "parent/child", body: []byte("child")}}, nil),
	} {
		t.Run(name, func(t *testing.T) {
			copy := make(map[string][]byte)
			for k, v := range components {
				copy[k] = v
			}
			copy["media.tar.gz"] = data
			file := writeFixtureBundle(t, copy, nil, nil)
			if b, err := OpenBundle(t.Context(), file, BundleOptions{}); err == nil {
				_ = b.Close()
				t.Fatal("unsafe tar accepted")
			}
		})
	}
}

func TestLegacyJSONRejectsDuplicateNestedKeysAndExtraValues(t *testing.T) {
	for _, raw := range []string{`{"id":"a","id":"b"}`, `{"id":"a"} {"id":"b"}`, `{"id":{"nested":1,"nested":2}}`, `{"unknown":true}`, strings.Repeat("[", 66) + strings.Repeat("]", 66)} {
		var value struct {
			ID string `json:"id"`
		}
		if decodeStrictJSON([]byte(raw), &value) == nil {
			t.Fatal("ambiguous JSON accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := OpenBundle(ctx, "not-opened", BundleOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost")
	}
}
