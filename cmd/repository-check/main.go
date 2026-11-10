// Command repository-check audits tracked and new files without printing secrets.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var credential = regexp.MustCompile(`\b\d{6,}:[A-Za-z0-9_-]{30,}\b|-----BEGIN (?:[A-Z]+ )?PRIVATE KEY-----`)
var databaseFile = regexp.MustCompile(`\.(db|sqlite)(?:$|[.-])`)
var envFile = regexp.MustCompile(`\.env(?:\.|$)`)

func forbiddenPath(name string) bool {
	for _, part := range strings.Split(filepath.ToSlash(name), "/") {
		switch part {
		case "data", "media", "backups", "vendor", "node_modules", ".serena":
			return true
		}
		if strings.HasPrefix(part, ".bifrost-export-") || strings.HasPrefix(part, ".bifrost-import-") {
			return true
		}
	}
	base := filepath.Base(name)
	return strings.HasSuffix(base, ".bifrost.tar.gz") || databaseFile.MatchString(base) ||
		(!strings.HasSuffix(base, ".example") && (base == ".env" || strings.HasPrefix(base, ".env.") || envFile.MatchString(base)))
}

func hasCredential(content []byte, secrets [][]byte) bool {
	if credential.Match(content) {
		return true
	}
	for _, secret := range secrets {
		if len(secret) >= 16 && bytes.Contains(content, secret) {
			return true
		}
	}
	return false
}

func git(root string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	return cmd.Output()
}

func privateValues(root string) ([][]byte, error) {
	names := map[string]bool{
		"BRIDGE_ENCRYPTION_KEY": true, "BRIDGE_ADMIN_TOKEN": true,
		"OPENWA_WEBHOOK_SECRET": true, "OPENWA_API_KEY": true,
		"TELEGRAM_MASTER_TOKEN": true, "TELEGRAM_API_HASH": true,
		"N8N_ENCRYPTION_KEY": true, "POSTGRES_PASSWORD": true, "DATABASE_URL": true,
	}
	var secrets [][]byte
	for _, name := range []string{"bridge.env", "telegram-api.env", "n8n.env", ".go.env"} {
		content, err := os.ReadFile(filepath.Join(root, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("cannot read private configuration %s", name)
		}
		for _, line := range bytes.Split(content, []byte{'\n'}) {
			key, value, ok := bytes.Cut(line, []byte{'='})
			if ok && names[string(key)] && len(value) >= 16 {
				secrets = append(secrets, value)
			}
		}
	}
	return secrets, nil
}

func run(staged bool) error {
	location, err := git("", "rev-parse", "--show-toplevel")
	if err != nil {
		return fmt.Errorf("cannot find Git repository")
	}
	root := strings.TrimSpace(string(location))
	secrets, err := privateValues(root)
	if err != nil {
		return err
	}
	args := []string{"ls-files", "--cached", "--others", "--exclude-standard", "-z"}
	if staged {
		args = []string{"diff", "--cached", "--name-only", "--diff-filter=ACMR", "-z"}
	}
	paths, err := git(root, args...)
	if err != nil {
		return fmt.Errorf("cannot enumerate repository files")
	}
	seen := make(map[string]bool)
	inspected, failures := 0, 0
	for _, name := range strings.Split(string(paths), "\x00") {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		path := filepath.Join(root, name)
		info, statErr := os.Lstat(path)
		if !staged && os.IsNotExist(statErr) {
			continue // A tracked worktree deletion is not a private artifact.
		}
		inspected++
		var content []byte
		if staged {
			mode, modeErr := git(root, "ls-files", "--stage", "--", name)
			if modeErr != nil || bytes.HasPrefix(mode, []byte("120000 ")) || forbiddenPath(name) {
				fmt.Println("Private/runtime path or unreadable staged entry:", name)
				failures++
				continue
			}
			content, err = git(root, "show", ":"+name)
		} else {
			if statErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || forbiddenPath(name) {
				fmt.Println("Private/runtime path or unreadable entry:", name)
				failures++
				continue
			}
			content, err = os.ReadFile(path)
		}
		if err != nil || hasCredential(content, secrets) {
			fmt.Println("Cannot inspect file or possible credential:", name)
			failures++
		}
	}
	if failures > 0 {
		return fmt.Errorf("repository check failed (%d files); no credential values were printed", failures)
	}
	fmt.Printf("Repository check passed: %d files inspected.\n", inspected)
	return nil
}

func main() {
	staged := flag.Bool("staged", false, "inspect added and modified staged content")
	flag.Parse()
	if err := run(*staged); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
