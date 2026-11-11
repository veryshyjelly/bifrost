// Package attachments freezes media bytes independently of provider credentials.
package attachments

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"github.com/veryshyjelly/bifrost/internal/providers"
)

type Asset struct {
	Hash, Name, MIME, StickerFormat string
	Size                            int64
}
type Assets struct{ root string }

var hashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var ErrAsset = errors.New("frozen attachment missing or changed; restore its media archive")

func NewAssets(root string) (*Assets, error) {
	if root == "" {
		return nil, ErrAsset
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, ErrAsset
	}
	if err := os.MkdirAll(absolute, 0700); err != nil {
		return nil, ErrAsset
	}
	absolute, err = filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, ErrAsset
	}
	owned := filepath.Join(absolute, "bifrost-go-assets")
	if err := os.MkdirAll(owned, 0700); err != nil {
		return nil, ErrAsset
	}
	resolved, err := filepath.EvalSymlinks(owned)
	if err != nil || resolved != owned {
		return nil, ErrAsset
	}
	return &Assets{root: owned}, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
func TooLarge() error {
	return &providers.Error{Service: "media", Status: 413, Code: "MEDIA_TOO_LARGE"}
}
func (a *Assets) Save(ctx context.Context, r io.Reader, name, mime string, max int64) (Asset, error) {
	if ctx == nil || r == nil || max <= 0 || max > 1<<53-1 {
		return Asset{}, ErrAsset
	}
	file, err := os.CreateTemp(a.root, ".incoming-")
	if err != nil {
		return Asset{}, ErrAsset
	}
	temp := file.Name()
	defer os.Remove(temp)
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(contextReader{ctx, r}, max+1))
	if err != nil {
		return Asset{}, err
	}
	if size > max {
		return Asset{}, TooLarge()
	}
	if size == 0 {
		return Asset{}, ErrAsset
	}
	if err := file.Sync(); err != nil {
		return Asset{}, ErrAsset
	}
	if err := file.Close(); err != nil {
		return Asset{}, ErrAsset
	}
	asset := Asset{Hash: hex.EncodeToString(hash.Sum(nil)), Name: filepath.Base(name), MIME: mime, Size: size}
	if asset.Name == "" || asset.Name == "." || asset.Name == string(filepath.Separator) {
		asset.Name = "attachment.bin"
	}
	dest := filepath.Join(a.root, asset.Hash)
	if err := os.Link(temp, dest); err != nil && !errors.Is(err, os.ErrExist) {
		return Asset{}, ErrAsset
	}
	if dir, err := os.Open(a.root); err == nil {
		err = dir.Sync()
		dir.Close()
		if err != nil {
			return Asset{}, ErrAsset
		}
	} else {
		return Asset{}, ErrAsset
	}
	verify, err := a.Open(ctx, asset)
	if err != nil {
		return Asset{}, err
	}
	verify.Close()
	return asset, nil
}
func (a *Assets) Path(v Asset) (string, error) {
	if !hashPattern.MatchString(v.Hash) || v.Size <= 0 {
		return "", ErrAsset
	}
	return filepath.Join(a.root, v.Hash), nil
}
func (a *Assets) Open(ctx context.Context, v Asset) (*os.File, error) {
	path, err := a.Path(v)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() != v.Size {
		return nil, ErrAsset
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, ErrAsset
	}
	hash := sha256.New()
	n, err := io.Copy(hash, contextReader{ctx, file})
	if err != nil || n != v.Size || hex.EncodeToString(hash.Sum(nil)) != v.Hash {
		file.Close()
		return nil, ErrAsset
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		file.Close()
		return nil, ErrAsset
	}
	return file, nil
}
