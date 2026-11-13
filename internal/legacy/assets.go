package legacy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"unicode/utf8"
)

var ErrRestoredAssets = errors.New("restored legacy assets do not match the frozen archive")

type AssetTree struct {
	Files, Bytes int64
	Digest       string
}
type AssetVerification struct {
	Account       string    `json:"account"`
	ArchiveDigest string    `json:"archive_digest"`
	Media         AssetTree `json:"media"`
	Telegram      AssetTree `json:"telegram"`
}

type assetReader struct {
	ctx context.Context
	io.Reader
}

func (r assetReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(p)
}

// assetTree reads only regular files beneath an OS-enforced directory root.
// Ownership, modes and timestamps may change during volume restoration; paths,
// directory structure and every file byte must remain identical.
func assetTree(ctx context.Context, path string) (AssetTree, error) {
	var result AssetTree
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return result, ErrRestoredAssets
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return result, ErrRestoredAssets
	}
	defer root.Close()
	hash := sha256.New()
	encoder := json.NewEncoder(hash)
	entries := 0
	err = fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return ErrRestoredAssets
		}
		if !utf8.ValidString(name) {
			return ErrRestoredAssets
		}
		entries++
		if entries > 1_000_000 {
			return ErrRestoredAssets
		}
		if entry.IsDir() {
			return encoder.Encode([]any{name, "directory"})
		}
		if !entry.Type().IsRegular() {
			return ErrRestoredAssets
		}
		file, err := root.Open(name)
		if err != nil {
			return ErrRestoredAssets
		}
		before, err := file.Stat()
		if err != nil || !before.Mode().IsRegular() {
			file.Close()
			return ErrRestoredAssets
		}
		content := sha256.New()
		n, err := io.Copy(content, assetReader{ctx: ctx, Reader: file})
		after, statErr := file.Stat()
		closeErr := file.Close()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return ErrRestoredAssets
		}
		if statErr != nil || closeErr != nil || before.Size() != n || after.Size() != n || !before.ModTime().Equal(after.ModTime()) || !os.SameFile(before, after) {
			return ErrRestoredAssets
		}
		if result.Bytes > int64(^uint64(0)>>1)-n {
			return ErrRestoredAssets
		}
		result.Files++
		result.Bytes += n
		return encoder.Encode([]any{name, "file", n, hex.EncodeToString(content.Sum(nil))})
	})
	if err != nil {
		return AssetTree{}, err
	}
	result.Digest = hex.EncodeToString(hash.Sum(nil))
	return result, nil
}

// VerifyRestoredAssets compares mounted/restored media and Telegram API trees
// with their owned archive copies. Call while their writers are stopped. It
// performs no restoration, configuration mutation, provider call or activation.
func VerifyRestoredAssets(ctx context.Context, bundle *Bundle, mediaRoot, telegramRoot string) (AssetVerification, error) {
	var result AssetVerification
	if ctx == nil || bundle == nil || bundle.root == "" || bundle.Account == "" || len(bundle.Digest) != 64 {
		return result, ErrInvalid
	}
	result.Account, result.ArchiveDigest = bundle.Account, bundle.Digest
	for _, role := range []struct {
		name, path string
		dest       *AssetTree
	}{{"media", mediaRoot, &result.Media}, {"telegram-data", telegramRoot, &result.Telegram}} {
		source, err := bundle.Directory(role.name)
		if err != nil {
			return AssetVerification{}, err
		}
		expected, err := assetTree(ctx, source)
		if err != nil {
			return AssetVerification{}, err
		}
		actual, err := assetTree(ctx, role.path)
		if err != nil {
			return AssetVerification{}, err
		}
		if expected != actual {
			return AssetVerification{}, ErrRestoredAssets
		}
		*role.dest = actual
	}
	return result, nil
}
