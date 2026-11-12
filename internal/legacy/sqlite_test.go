package legacy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const sqliteSchema = `
CREATE TABLE kv(kind TEXT,id TEXT,value TEXT NOT NULL,PRIMARY KEY(kind,id));
CREATE TABLE jobs(id INTEGER PRIMARY KEY,dedup TEXT UNIQUE NOT NULL,stream TEXT NOT NULL,type TEXT NOT NULL,payload TEXT NOT NULL,state TEXT NOT NULL DEFAULT 'pending',attempts INTEGER NOT NULL DEFAULT 0,due INTEGER NOT NULL DEFAULT 0,error TEXT,created INTEGER NOT NULL);
CREATE TABLE effects(id TEXT PRIMARY KEY,state TEXT NOT NULL,result TEXT);
CREATE TABLE messages(wa_chat TEXT NOT NULL,wa_id TEXT NOT NULL,tg_chat TEXT NOT NULL,tg_id INTEGER NOT NULL,bot TEXT NOT NULL,direction TEXT NOT NULL,media INTEGER NOT NULL DEFAULT 0,PRIMARY KEY(bot,tg_chat,tg_id));
CREATE TABLE aliases(alias TEXT PRIMARY KEY,canonical TEXT NOT NULL);
CREATE TABLE telegram_messages(seq INTEGER PRIMARY KEY,scope TEXT NOT NULL,chat TEXT NOT NULL,message INTEGER NOT NULL,bot TEXT NOT NULL,thread INTEGER NOT NULL DEFAULT 0,kind TEXT NOT NULL,date INTEGER NOT NULL,expires INTEGER NOT NULL DEFAULT 0,deleted INTEGER NOT NULL DEFAULT 0,error TEXT,UNIQUE(scope,chat,message));
`

func sqliteFixture(t *testing.T, extra string, wal bool) string {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("Python sqlite3 not installed")
	}
	path := filepath.Join(t.TempDir(), "bridge.db")
	schema, _ := json.Marshal(sqliteSchema)
	statements, _ := json.Marshal(extra)
	script := "import sqlite3,sys,json\nc=sqlite3.connect(sys.argv[1])\n"
	if wal {
		script += "c.execute('PRAGMA journal_mode=WAL')\nc.execute('PRAGMA wal_autocheckpoint=0')\n"
	}
	script += "c.executescript(json.loads(" + string(mustJSON(string(schema))) + "))\nc.executescript(json.loads(" + string(mustJSON(string(statements))) + "))\nc.commit()\n"
	if wal {
		script += "print('ready',flush=True)\nsys.stdin.readline()\n"
	}
	script += "c.close()\n"
	writerContext, stopWriter := context.WithCancel(context.Background())
	command := exec.CommandContext(writerContext, python, "-I", "-S", "-c", script, path)
	command.Stderr = io.Discard
	if wal {
		input, err := command.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		output, err := command.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			defer stopWriter()
			input.Close()
			if err := command.Wait(); err != nil {
				t.Error("fixture writer did not close cleanly")
			}
		})
		if line, err := bufio.NewReader(output).ReadString('\n'); err != nil || line != "ready\n" {
			t.Fatal("fixture writer failed to initialize")
		}
	} else {
		defer stopWriter()
		if err := command.Run(); err != nil {
			t.Fatal("fixture SQLite initialization failed")
		}
	}
	return path
}
func mustJSON(value string) []byte { raw, _ := json.Marshal(value); return raw }

func TestReadStagedSQLiteIncludesUncheckpointedWALAndExactIntegers(t *testing.T) {
	source := sqliteFixture(t, `INSERT INTO kv VALUES('identity','account','{"id":"fixture-account"}'); INSERT INTO jobs(id,dedup,stream,type,payload,created) VALUES(9007199254740993,'original-key','wa:peer','wa','{}',1700000000000);`, true)
	stage := t.TempDir()
	path := filepath.Join(stage, "bridge.db")
	for _, suffix := range []string{"", "-wal", "-shm"} {
		raw, err := os.ReadFile(source + suffix)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path+suffix, raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	before := map[string][32]byte{}
	for _, suffix := range []string{"", "-wal"} {
		raw, err := os.ReadFile(path + suffix)
		if err != nil {
			t.Fatal(err)
		}
		before[suffix] = sha256.Sum256(raw)
	}
	var job string
	info, err := ReadStagedSQLite(t.Context(), path, SQLiteOptions{}, func(row Row) error {
		if row.Table == "jobs" {
			decoder := json.NewDecoder(bytes.NewReader(row.Values))
			decoder.UseNumber()
			var values map[string]any
			if err := decoder.Decode(&values); err != nil {
				return err
			}
			job = values["id"].(json.Number).String()
		}
		return nil
	})
	if err != nil || info.Counts["jobs"] != 1 || info.Counts["kv"] != 1 || job != "9007199254740993" || len(info.Digest) != 64 {
		t.Fatal(info, job, err)
	}
	for suffix, hash := range before {
		raw, err := os.ReadFile(path + suffix)
		if err != nil || sha256.Sum256(raw) != hash {
			t.Fatal("reader changed frozen database or WAL", suffix, err)
		}
	}
}

func TestReadStagedSQLiteRejectsViewsBlobColumnsAndRowLimits(t *testing.T) {
	for _, tc := range []struct {
		name, sql string
		limit     int64
	}{{"view", `DROP TABLE jobs; CREATE VIEW jobs AS SELECT 1 AS id;`, 100}, {"blob", `INSERT INTO kv VALUES('bot','200',X'616263');`, 100}, {"row-limit", `INSERT INTO kv VALUES('identity','account','{}');`, 1}} {
		t.Run(tc.name, func(t *testing.T) {
			path := sqliteFixture(t, tc.sql, false)
			maximum := tc.limit
			if tc.name == "row-limit" {
				maximum = 1
				if _, err := ReadStagedSQLite(t.Context(), path, SQLiteOptions{MaxRows: maximum}, nil); err != nil {
					t.Fatal("valid boundary refused", err)
				}
				// Add another row with a separate source; the first source stays untouched.
				path = sqliteFixture(t, tc.sql+`INSERT INTO kv VALUES('route','peer','{}');`, false)
			}
			if _, err := ReadStagedSQLite(t.Context(), path, SQLiteOptions{MaxRows: maximum}, nil); !errors.Is(err, ErrInvalid) {
				t.Fatal(err)
			}
		})
	}
}

func TestReadStagedSQLiteCancellationAndProtocolValidation(t *testing.T) {
	path := sqliteFixture(t, `INSERT INTO kv VALUES('identity','account','{}');`, false)
	stop := errors.New("fixture consumer stop")
	if _, err := ReadStagedSQLite(t.Context(), path, SQLiteOptions{}, func(Row) error { return stop }); !errors.Is(err, stop) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := ReadStagedSQLite(ctx, path, SQLiteOptions{}, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	bad := `{"record":"header","format":"bifrost-legacy-sqlite","version":1,"sqliteVersion":"fixture","counts":{"kv":0,"jobs":0,"effects":0,"messages":0,"aliases":0,"telegram_messages":0}}` + "\n" + `{"record":"end","counts":{"kv":0,"jobs":0,"effects":0,"messages":0,"aliases":0,"telegram_messages":0},"digest":"wrong"}` + "\n"
	if _, err := consumeSQLite(strings.NewReader(bad), SQLiteOptions{MaxRows: 10, MaxRowBytes: 4096}, nil); !errors.Is(err, ErrInvalid) {
		t.Fatal("accepted bad footer", err)
	}
	if _, err := consumeSQLite(strings.NewReader(strings.Repeat("x", 5000)), SQLiteOptions{MaxRows: 10, MaxRowBytes: 4096}, nil); !errors.Is(err, ErrInvalid) {
		t.Fatal("accepted oversized record", err)
	}
}
