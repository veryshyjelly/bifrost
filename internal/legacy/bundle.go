package legacy

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

var componentNames = []string{"bridge.env", "telegram-api.env", "data.tar.gz", "media.tar.gz", "telegram-data.tar.gz"}

type Manifest struct {
	Format  string            `json:"format"`
	Version int               `json:"version"`
	Created time.Time         `json:"created"`
	Source  ManifestSource    `json:"source"`
	Files   map[string]string `json:"files"`
}

type ManifestSource struct {
	Revision string `json:"revision"`
	Dirty    bool   `json:"dirty"`
	Project  string `json:"project"`
}

// BundleOptions bounds the entire extraction, including compressed component
// files. Defaults allow large retained media volumes without an unbounded disk
// expansion. A caller may lower them before inspecting an untrusted archive.
type BundleOptions struct {
	Scratch          string
	MaxArchiveBytes  int64
	MaxExpandedBytes int64
	MaxFileBytes     int64
	MaxEntries       int64
	SQLite           SQLiteOptions
}

// Bundle owns only its fresh private staging directory. Closing it never
// removes the source archive or any pre-existing directory or Docker volume.
type Bundle struct {
	Manifest Manifest
	Digest   string
	SQLite   SQLiteInfo
	Account  string
	root     string
	config   Configuration
}

func (b *Bundle) Close() error {
	if b == nil || b.root == "" {
		return nil
	}
	err := os.RemoveAll(b.root)
	if err == nil {
		b.root = ""
	}
	return err
}

// Directory returns an owned staged volume, never an archive-supplied path.
func (b *Bundle) Directory(role string) (string, error) {
	if b == nil || b.root == "" || (role != "data" && role != "media" && role != "telegram-data") {
		return "", ErrInvalid
	}
	return filepath.Join(b.root, role), nil
}

func (b *Bundle) Configuration() Configuration { return b.config.clone() }

// OpenBundle validates a legacy v1 export without changing any existing
// resource. Archived scripts are never executed. WAL inspection happens only
// on the owned staged copy. The caller must close the bundle on success.
func OpenBundle(ctx context.Context, filename string, options BundleOptions) (_ *Bundle, err error) {
	if ctx == nil {
		return nil, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c, err := normalizedBundleOptions(options)
	if err != nil {
		return nil, err
	}
	before, err := os.Lstat(filename)
	if err != nil || !before.Mode().IsRegular() || before.Size() > c.MaxArchiveBytes {
		return nil, ErrInvalid
	}
	f, err := os.Open(filename)
	if err != nil {
		return nil, ErrInvalid
	}
	defer f.Close()
	after, err := f.Stat()
	if err != nil || !os.SameFile(before, after) {
		return nil, ErrInvalid
	}
	root, err := os.MkdirTemp(c.Scratch, ".bifrost-legacy-*")
	if err != nil {
		return nil, ErrInvalid
	}
	b := &Bundle{root: root}
	defer func() {
		if err != nil {
			_ = b.Close()
		}
	}()
	budget := extractionBudget{bytes: c.MaxExpandedBytes, entries: c.MaxEntries, file: c.MaxFileBytes}
	outer := filepath.Join(root, "components")
	if err := os.Mkdir(outer, 0700); err != nil {
		return nil, ErrInvalid
	}
	archiveHash := sha256.New()
	allowed := map[string]bool{"manifest.json": true}
	for _, name := range componentNames {
		allowed[name] = true
	}
	if err := extractGzipTar(ctx, io.TeeReader(io.LimitReader(f, c.MaxArchiveBytes+1), archiveHash), outer, &budget, allowed); err != nil {
		return nil, err
	}
	b.Digest = hex.EncodeToString(archiveHash.Sum(nil))
	raw, err := os.ReadFile(filepath.Join(outer, "manifest.json"))
	if err != nil || decodeStrictJSON(raw, &b.Manifest) != nil || !validManifest(b.Manifest) {
		return nil, ErrInvalid
	}
	for _, name := range componentNames {
		sum, err := hashFile(ctx, filepath.Join(outer, name))
		if err != nil || sum != b.Manifest.Files[name] {
			return nil, ErrInvalid
		}
	}
	bridge, err := os.ReadFile(filepath.Join(outer, "bridge.env"))
	if err != nil {
		return nil, ErrInvalid
	}
	telegram, err := os.ReadFile(filepath.Join(outer, "telegram-api.env"))
	if err != nil {
		return nil, ErrInvalid
	}
	b.config, err = parseConfiguration(bridge, telegram)
	if err != nil {
		return nil, err
	}
	for _, role := range []string{"data", "media", "telegram-data"} {
		destination := filepath.Join(root, role)
		if err := os.Mkdir(destination, 0700); err != nil {
			return nil, ErrInvalid
		}
		f, err := os.Open(filepath.Join(outer, role+".tar.gz"))
		if err != nil {
			return nil, ErrInvalid
		}
		err = extractGzipTar(ctx, f, destination, &budget, nil)
		_ = f.Close()
		if err != nil {
			return nil, err
		}
	}
	var savedIdentity string
	b.SQLite, err = ReadStagedSQLite(ctx, filepath.Join(root, "data", "bridge.db"), c.SQLite, func(row Row) error {
		if row.Table != "kv" {
			return nil
		}
		var kv struct{ Kind, ID, Value string }
		if json.Unmarshal(row.Values, &kv) != nil {
			return ErrInvalid
		}
		if kv.Kind == "identity" && kv.ID == "account" {
			var identity struct {
				ID string `json:"id"`
			}
			if decodeStrictJSON([]byte(kv.Value), &identity) != nil || !validAccount(identity.ID) {
				return ErrInvalid
			}
			savedIdentity = identity.ID
		}
		if kv.Kind == "bot" {
			return b.config.validateBot(kv.ID, []byte(kv.Value))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	b.Account, err = b.config.account(savedIdentity)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func normalizedBundleOptions(c BundleOptions) (BundleOptions, error) {
	if c.MaxArchiveBytes == 0 {
		c.MaxArchiveBytes = 64 << 30
	}
	if c.MaxExpandedBytes == 0 {
		c.MaxExpandedBytes = 256 << 30
	}
	if c.MaxFileBytes == 0 {
		c.MaxFileBytes = 64 << 30
	}
	if c.MaxEntries == 0 {
		c.MaxEntries = 1_000_000
	}
	if c.MaxArchiveBytes < 1 || c.MaxArchiveBytes > 1<<60 || c.MaxExpandedBytes < 1 || c.MaxExpandedBytes > 1<<60 || c.MaxFileBytes < 1 || c.MaxFileBytes > 1<<60 || c.MaxEntries < 1 || c.MaxEntries > 100_000_000 {
		return c, ErrInvalid
	}
	return c, nil
}

type extractionBudget struct{ bytes, entries, file int64 }

// extractGzipTar fully consumes the gzip member, verifies its checksum and
// rejects a second member or hidden nonzero data after the tar terminator.
func extractGzipTar(ctx context.Context, source io.Reader, destination string, budget *extractionBudget, allowed map[string]bool) error {
	compressed := bufio.NewReader(source)
	z, err := gzip.NewReader(compressed)
	if err != nil {
		return ErrInvalid
	}
	defer z.Close()
	z.Multistream(false)
	expanded := &budgetReader{ctx: ctx, r: z, budget: budget}
	tr := tar.NewReader(expanded)
	seen := make(map[string]byte)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			if expanded.zeroTail < 1024 {
				return ErrInvalid
			}
			break
		}
		if err != nil {
			return archiveError(ctx)
		}
		budget.entries--
		if budget.entries < 0 || (h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeDir) || h.Linkname != "" {
			return ErrInvalid
		}
		name := path.Clean(h.Name)
		if h.Name == "" || strings.ContainsAny(h.Name, "\\\x00") || path.IsAbs(h.Name) || strings.Contains(h.Name, ":") {
			return ErrInvalid
		}
		for _, part := range strings.Split(h.Name, "/") {
			if part == ".." {
				return ErrInvalid
			}
		}
		if _, duplicate := seen[name]; duplicate {
			return ErrInvalid
		}
		seen[name] = h.Typeflag
		if name == "." {
			if h.Typeflag != tar.TypeDir || h.Size != 0 {
				return ErrInvalid
			}
			continue
		}
		if !filepath.IsLocal(filepath.FromSlash(name)) || (allowed != nil && (!allowed[name] || h.Typeflag != tar.TypeReg)) {
			return ErrInvalid
		}
		maxFile := budget.file
		if allowed != nil && (name == "manifest.json" || strings.HasSuffix(name, ".env")) {
			maxFile = min(maxFile, 1<<20)
		}
		if h.Size < 0 || h.Size > maxFile || h.Size > budget.bytes {
			return ErrInvalid
		}
		target := filepath.Join(destination, filepath.FromSlash(name))
		if h.Typeflag == tar.TypeDir {
			if h.Size != 0 || os.MkdirAll(target, 0700) != nil {
				return ErrInvalid
			}
			continue
		}
		if os.MkdirAll(filepath.Dir(target), 0700) != nil {
			return ErrInvalid
		}
		f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return ErrInvalid
		}
		n, copyErr := io.Copy(f, tr)
		closeErr := f.Close()
		if copyErr != nil || closeErr != nil || n != h.Size {
			return archiveError(ctx)
		}
	}
	// Ordinary tar writers pad with zero blocks. Bound that padding separately
	// so a gzip bomb cannot hide behind the tar end-of-archive marker.
	padding := make([]byte, 32<<10)
	var size int64
	for {
		n, err := expanded.Read(padding)
		size += int64(n)
		if size > 1<<20 {
			return ErrInvalid
		}
		for _, b := range padding[:n] {
			if b != 0 {
				return ErrInvalid
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return archiveError(ctx)
		}
	}
	if _, err := compressed.ReadByte(); err != io.EOF {
		return ErrInvalid
	}
	if allowed != nil {
		for name := range allowed {
			if seen[name] != tar.TypeReg {
				return ErrInvalid
			}
		}
	}
	return nil
}

func archiveError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return ErrInvalid
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

// Count archive headers, transparent PAX metadata and padding as well as file
// contents. tar.Reader may consume extension records inside Next.
type budgetReader struct {
	ctx      context.Context
	r        io.Reader
	budget   *extractionBudget
	zeroTail int64
}

func (r *budgetReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if int64(len(p)) > r.budget.bytes+1 {
		p = p[:r.budget.bytes+1]
	}
	n, err := r.r.Read(p)
	r.budget.bytes -= int64(n)
	if r.budget.bytes < 0 {
		return 0, ErrInvalid
	}
	for _, b := range p[:n] {
		if b == 0 {
			r.zeroTail++
		} else {
			r.zeroTail = 0
		}
	}
	return n, err
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

func hashFile(ctx context.Context, filename string) (string, error) {
	f, err := os.Open(filename)
	if err != nil {
		return "", ErrInvalid
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, contextReader{ctx, f}); err != nil {
		return "", archiveError(ctx)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func validManifest(m Manifest) bool {
	if m.Format != "bifrost" || m.Version != 1 || m.Created.IsZero() || len(m.Files) != len(componentNames) || m.Source.Project == "" || len(m.Source.Project) > 256 || len(m.Source.Revision) > 128 {
		return false
	}
	for _, name := range componentNames {
		digest, err := hex.DecodeString(m.Files[name])
		if err != nil || len(digest) != sha256.Size || strings.ToLower(m.Files[name]) != m.Files[name] {
			return false
		}
	}
	return true
}
