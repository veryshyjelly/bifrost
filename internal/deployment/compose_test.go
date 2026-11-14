// Package deployment checks the self-contained stack without starting live accounts.
package deployment

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"net/url"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestComposeContainsCompleteIsolatedStack(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("Docker Compose is required for deployment validation")
	}
	cmd := exec.Command("docker", "compose", "-p", "bifrost-go", "--env-file", ".go.env.example", "config", "--format", "json")
	cmd.Dir = "../.."
	data, err := cmd.Output()
	if err != nil {
		t.Fatal("Compose configuration did not validate")
	}
	var stack struct {
		Name     string `json:"name"`
		Services map[string]struct {
			Environment   map[string]string `json:"environment"`
			ContainerName string            `json:"container_name"`
			Build         struct {
				Context string `json:"context"`
			} `json:"build"`
		} `json:"services"`
		Volumes map[string]struct {
			External bool `json:"external"`
		} `json:"volumes"`
		Networks map[string]struct {
			External bool `json:"external"`
		} `json:"networks"`
	}
	if err := json.Unmarshal(data, &stack); err != nil {
		t.Fatal(err)
	}
	if stack.Name != "bifrost-go" {
		t.Fatal("stack can collide with baseline project")
	}
	for _, name := range []string{"bifrost", "postgres", "openwa", "redis", "telegram-api"} {
		service, ok := stack.Services[name]
		if !ok {
			t.Fatalf("missing stack component %s", name)
		}
		if service.ContainerName != "" {
			t.Fatalf("global container name on %s", name)
		}
	}
	for name, v := range stack.Volumes {
		if v.External {
			t.Fatalf("external volume required: %s", name)
		}
	}
	for name, v := range stack.Networks {
		if v.External {
			t.Fatalf("external network required: %s", name)
		}
	}
	wa := stack.Services["openwa"]
	if wa.Environment["DATABASE_TYPE"] != "postgres" || wa.Environment["DATABASE_HOST"] != "postgres" || wa.Environment["REDIS_HOST"] != "redis" {
		t.Fatal("OpenWA datastores not wired")
	}
	if wa.Environment["DATABASE_SYNCHRONIZE"] != "false" {
		t.Fatal("unsafe OpenWA schema synchronization")
	}
	if wa.Environment["SSRF_ALLOWED_HOSTS"] != "bifrost" {
		t.Fatal("bridge callback hostname not allowlisted")
	}
	if wa.Build.Context == "" {
		t.Fatal("OpenWA requires an external prebuilt image")
	}
}

func TestSharedPostgresRolesCannotAccessOtherApplicationDatabase(t *testing.T) {
	first := os.Getenv("BIFROST_TEST_DATABASE_URL")
	second := os.Getenv("BIFROST_TEST_OPENWA_DATABASE_URL")
	if first == "" || second == "" {
		t.Skip("set both isolated app database URLs to test role separation")
	}
	for _, pair := range [][2]string{{first, second}, {second, first}} {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		connection, err := pgx.Connect(ctx, pair[0])
		if err != nil {
			cancel()
			t.Fatal("cannot connect as app role")
		}
		var privileged bool
		err = connection.QueryRow(ctx, "SELECT rolsuper OR rolcreatedb OR rolcreaterole OR rolreplication FROM pg_roles WHERE rolname=current_user").Scan(&privileged)
		_ = connection.Close(ctx)
		if err != nil || privileged {
			cancel()
			t.Fatal("app database user is privileged")
		}
		own, err := url.Parse(pair[0])
		if err != nil {
			cancel()
			t.Fatal("invalid test URL")
		}
		other, err := url.Parse(pair[1])
		if err != nil {
			cancel()
			t.Fatal("invalid test URL")
		}
		own.Path = other.Path
		connection, err = pgx.Connect(ctx, own.String())
		if err == nil {
			_ = connection.Close(ctx)
			cancel()
			t.Fatal("app role connected to the other database")
		}
		var refused *pgconn.PgError
		if !errors.As(err, &refused) || refused.Code != "42501" {
			cancel()
			t.Fatal("cross-database connection did not fail with permission denied")
		}
		cancel()
	}
}
