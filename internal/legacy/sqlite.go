// Package legacy validates frozen TypeScript state for explicit migration.
// SQLite is a one-shot source reader; PostgreSQL remains the runtime store.
package legacy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
)

var ErrInvalid = errors.New("legacy state validation failed")
var ErrPython = errors.New("legacy SQLite inspection requires Python with sqlite3")

//go:embed sqlite_reader.py
var sqliteReader []byte

type Row struct {
	Table  string
	Values json.RawMessage
}
type SQLiteInfo struct {
	SQLiteVersion string
	UserVersion   int64
	Counts        map[string]int64
	Digest        string
}
type SQLiteOptions struct {
	Python, Scratch string
	MaxRows         int64
	MaxRowBytes     int
}

var sourceTables = map[string]bool{"kv": true, "jobs": true, "effects": true, "messages": true, "aliases": true, "telegram_messages": true}

func snapshotLine(r *bufio.Reader, limit int) ([]byte, error) {
	var line []byte
	for {
		part, err := r.ReadSlice('\n')
		if len(line)+len(part) > limit {
			return nil, ErrInvalid
		}
		line = append(line, part...)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if err == io.EOF && len(line) > 0 {
			return nil, ErrInvalid
		}
		return line, err
	}
}
func consumeSQLite(r io.Reader, c SQLiteOptions, accept func(Row) error) (SQLiteInfo, error) {
	var info SQLiteInfo
	reader := bufio.NewReaderSize(r, 64<<10)
	hash := sha256.New()
	header, end := false, false
	counts := map[string]int64{}
	var total int64
	for {
		line, err := snapshotLine(reader, c.MaxRowBytes)
		if err == io.EOF {
			break
		}
		if err != nil {
			return info, err
		}
		var record struct {
			Record, Format, SQLiteVersion, Digest, Table string
			Version                                      int
			UserVersion                                  int64
			Counts                                       map[string]int64
			Values                                       json.RawMessage
		}
		if end || json.Unmarshal(line, &record) != nil {
			return info, ErrInvalid
		}
		switch record.Record {
		case "header":
			if header || record.Format != "bifrost-legacy-sqlite" || record.Version != 1 || record.SQLiteVersion == "" || len(record.Counts) != len(sourceTables) {
				return info, ErrInvalid
			}
			for table, n := range record.Counts {
				if !sourceTables[table] || n < 0 || n > c.MaxRows-total {
					return info, ErrInvalid
				}
				total += n
				counts[table] = 0
			}
			info = SQLiteInfo{SQLiteVersion: record.SQLiteVersion, UserVersion: record.UserVersion, Counts: record.Counts}
			header = true
			total = 0
		case "row":
			if !header || !sourceTables[record.Table] || len(record.Values) == 0 || record.Values[0] != '{' || total >= c.MaxRows {
				return info, ErrInvalid
			}
			counts[record.Table]++
			total++
			if counts[record.Table] > info.Counts[record.Table] {
				return info, ErrInvalid
			}
			hash.Write(line)
			if accept != nil {
				if err := accept(Row{Table: record.Table, Values: record.Values}); err != nil {
					return info, err
				}
			}
		case "end":
			if !header || !reflect.DeepEqual(counts, info.Counts) || !reflect.DeepEqual(record.Counts, info.Counts) || record.Digest != hex.EncodeToString(hash.Sum(nil)) {
				return info, ErrInvalid
			}
			info.Digest = record.Digest
			end = true
		default:
			return info, ErrInvalid
		}
	}
	if !end {
		return info, ErrInvalid
	}
	return info, nil
}

// ReadStagedSQLite reads a frozen, owned extraction or other quiesced source
// view, including its WAL. It must not be used to copy a live writer's files.
// Callbacks can receive rows before final validation; callers must commit only
// after this function succeeds. No archived executable or environment is used.
func ReadStagedSQLite(ctx context.Context, path string, c SQLiteOptions, accept func(Row) error) (SQLiteInfo, error) {
	var info SQLiteInfo
	if ctx == nil || path == "" {
		return info, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return info, err
	}
	if c.MaxRows == 0 {
		c.MaxRows = 1_000_000
	}
	if c.MaxRowBytes == 0 {
		c.MaxRowBytes = 64 << 20
	}
	if c.MaxRows <= 0 || c.MaxRowBytes < 1024 || c.MaxRowBytes > 1_000_000_000 {
		return info, ErrInvalid
	}
	if c.Python == "" {
		c.Python = "python3"
	}
	entry, err := os.Lstat(path)
	if err != nil || !entry.Mode().IsRegular() {
		return info, ErrInvalid
	}
	file, err := os.Open(path)
	if err != nil {
		return info, ErrInvalid
	}
	stat, err := file.Stat()
	var magic [16]byte
	_, readErr := io.ReadFull(file, magic[:])
	file.Close()
	if err != nil || !stat.Mode().IsRegular() || readErr != nil || !bytes.Equal(magic[:], []byte("SQLite format 3\x00")) {
		return info, ErrInvalid
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return info, ErrInvalid
	}
	python, err := exec.LookPath(c.Python)
	if err != nil {
		return info, ErrPython
	}
	scratch, err := os.MkdirTemp(c.Scratch, ".bifrost-sqlite-reader-")
	if err != nil {
		return info, ErrInvalid
	}
	defer os.RemoveAll(scratch)
	script := filepath.Join(scratch, "reader.py")
	if err := os.WriteFile(script, sqliteReader, 0o600); err != nil {
		return info, ErrInvalid
	}
	call, cancel := context.WithCancel(ctx)
	defer cancel()
	command := exec.CommandContext(call, python, "-I", "-S", "-X", "utf8", script, path, strconv.FormatInt(c.MaxRows, 10), strconv.Itoa(c.MaxRowBytes))
	command.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "LANG=C.UTF-8", "PYTHONDONTWRITEBYTECODE=1"}
	command.Stderr = io.Discard
	stdout, err := command.StdoutPipe()
	if err != nil {
		return info, ErrPython
	}
	if err := command.Start(); err != nil {
		if ctx.Err() != nil {
			return info, ctx.Err()
		}
		return info, ErrPython
	}
	info, readErr = consumeSQLite(stdout, c, accept)
	if readErr != nil {
		cancel()
	}
	waitErr := command.Wait()
	if ctx.Err() != nil {
		return info, ctx.Err()
	}
	if readErr != nil {
		return info, readErr
	}
	if waitErr != nil {
		return info, ErrInvalid
	}
	return info, nil
}
