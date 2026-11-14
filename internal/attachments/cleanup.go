package attachments

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Cleanup removes abandoned work only inside this implementation's owned
// directory. Frozen content-addressed assets and retained media remain intact.
func (a *Assets) Cleanup(ctx context.Context, now time.Time) error {
	entries, err := os.ReadDir(a.root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !strings.HasPrefix(entry.Name(), ".codec-") && !strings.HasPrefix(entry.Name(), ".incoming-") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.ModTime().Before(now.Add(-24*time.Hour)) || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		path := filepath.Join(a.root, entry.Name())
		if strings.HasPrefix(entry.Name(), ".codec-") && info.IsDir() {
			err = os.RemoveAll(path)
		} else if info.Mode().IsRegular() {
			err = os.Remove(path)
		}
		if err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
