package templates

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// SupabasePorts holds the API/DB/Studio/Inbucket ports actually configured
// in the project's supabase/config.toml. Falls back to the supabase CLI
// stock defaults (54321..54324) when fields are missing or the file is
// unreadable.
type SupabasePorts struct {
	API      int // [api].port
	DB       int // [db].port
	Studio   int // [studio].port
	Inbucket int // [inbucket].port
}

// ReadSupabasePorts parses supabase/config.toml relative to projectRoot.
// Pass the corgi project root (CorgiComposePathDir) so it works regardless
// of the user's cwd or `corgi run -f /other/path/...`. Empty projectRoot
// falls back to cwd. Missing or unparseable sections fall back to the
// supabase CLI defaults so consumers always get a sensible URL.
func ReadSupabasePorts(projectRoot string) SupabasePorts {
	defaults := SupabasePorts{API: 54321, DB: 54322, Studio: 54323, Inbucket: 54324}
	tomlPath := "supabase/config.toml"
	if projectRoot != "" {
		tomlPath = projectRoot + "/supabase/config.toml"
	}
	data, err := os.ReadFile(tomlPath)
	if err != nil {
		return defaults
	}
	sectionRe := regexp.MustCompile(`^\s*\[([^\]]+)\]\s*$`)
	portRe := regexp.MustCompile(`^\s*port\s*=\s*(\d+)`)

	got := defaults
	current := ""
	for _, line := range strings.Split(string(data), "\n") {
		if m := sectionRe.FindStringSubmatch(line); m != nil {
			current = strings.TrimSpace(m[1])
			continue
		}
		if m := portRe.FindStringSubmatch(line); m != nil {
			p, _ := strconv.Atoi(m[1])
			if p == 0 {
				continue
			}
			switch current {
			case "api":
				got.API = p
			case "db":
				got.DB = p
			case "studio":
				got.Studio = p
			case "inbucket":
				got.Inbucket = p
			}
		}
	}
	return got
}

// SignSupabaseJWT signs the canonical supabase HS256 JWT for a given role
// (anon | service_role) using the provided secret. Output matches what
// `supabase status` reports for projects with the same JWT secret.
func SignSupabaseJWT(secret, role string) string {
	header := `{"alg":"HS256","typ":"JWT"}`
	payload := fmt.Sprintf(`{"iss":"supabase-demo","role":"%s","exp":1983812996}`, role)

	enc := base64.RawURLEncoding
	h := enc.EncodeToString([]byte(header))
	p := enc.EncodeToString([]byte(payload))
	signing := h + "." + p

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signing))
	sig := enc.EncodeToString(mac.Sum(nil))
	return signing + "." + sig
}

// Stock supabase local-dev seeds. ANON_KEY / SERVICE_ROLE_KEY are derived
// at emission time via SignSupabaseJWT, not hardcoded.
const (
	SupabaseJWTSecret     = "super-secret-jwt-token-with-at-least-32-characters-long"
	SupabaseS3AccessKeyID = "625729a08b95bf1b7ff351a663f3a23c"
	SupabaseS3AccessKey   = "850181e4652dd023b7a98c58ae0d2d34bd487ee0cc3254aed6eda37307425907"
	SupabaseS3Region      = "local"
)

// MakefileSupabase wraps the supabase CLI. Runs from project root so
// supabase/config.toml is found in its conventional location.
var MakefileSupabase = `ROOT := $(shell git rev-parse --show-toplevel 2>/dev/null || cd ../../.. && pwd)

up:
	@command -v supabase >/dev/null 2>&1 || { \
		echo "supabase CLI not found."; \
		if [ -t 0 ] && command -v brew >/dev/null 2>&1; then \
			printf "Install via 'brew install supabase/tap/supabase'? [y/N] "; \
			read ans; \
			case "$$ans" in \
				[yY]|[yY][eE][sS]) brew install supabase/tap/supabase || exit 1 ;; \
				*) echo "Aborted. Install manually: brew install supabase/tap/supabase"; exit 1 ;; \
			esac; \
		else \
			echo "Install: brew install supabase/tap/supabase"; \
			exit 1; \
		fi; \
	}
	@cd "$(ROOT)" && [ -f supabase/config.toml ] || { \
		echo "→ supabase/config.toml missing — running 'supabase init'..."; \
		supabase init; \
	}
	@cd "$(ROOT)" && if supabase status >/dev/null 2>&1; then \
		echo "✓ supabase already running"; \
	else \
		echo "→ starting supabase (first run pulls images; can take several minutes)..."; \
		supabase start; \
	fi
	@bash bootstrap/bootstrap.sh

down:
	@cd "$(ROOT)" && supabase stop --no-backup 2>/dev/null || true

stop: down

logs:
	@cd "$(ROOT)" && supabase status

id:
	@docker ps --filter "name=supabase_" --format '{{"{{.ID}}"}}' | head -1

remove:
	@cd "$(ROOT)" && supabase stop --no-backup 2>/dev/null || true

bootstrap:
	@bash bootstrap/bootstrap.sh

help:
	@make -qpRr | egrep -e '^[a-z].*:$$' | sed -e 's~:~~g' | sort

.PHONY: up down stop logs id remove help
`

// BootstrapSupabase seeds buckets (Storage API) and auth users (Admin API)
// after `supabase start`. Idempotent — duplicates skipped via curl `|| true`.
var BootstrapSupabase = `#!/usr/bin/env bash
set -euo pipefail

# corgi-managed wrapper for the supabase CLI runs from the project root
# (where supabase/config.toml lives). Re-derive that here.
ROOT="$(git rev-parse --show-toplevel 2>/dev/null || cd ../../.. && pwd)"
cd "$ROOT"

if ! command -v supabase >/dev/null 2>&1; then
    echo "supabase CLI not found — skipping bootstrap"
    exit 0
fi

# Pull live keys from supabase. Handles custom JWT secrets transparently.
eval "$(supabase status -o env 2>/dev/null)" || {
    echo "⚠ supabase status failed — bootstrap skipped"
    exit 0
}

if [ -z "${SERVICE_ROLE_KEY:-}" ] || [ -z "${API_URL:-}" ]; then
    echo "⚠ SERVICE_ROLE_KEY / API_URL missing — bootstrap skipped"
    exit 0
fi

START_TS=$(date +%s)
echo "configuring supabase (api=$API_URL)"
echo "==================="

# Write captured live keys to .env-supabase-runtime so consumers can override
# corgi-emitted hardcoded fallbacks if supabase rotates internal seeds.
RUNTIME_ENV="$ROOT/.env-supabase-runtime"
{
    echo "# Auto-captured by corgi supabase driver bootstrap. Do not edit."
    echo "SUPABASE_URL=$API_URL"
    echo "SUPABASE_ANON_KEY=$ANON_KEY"
    echo "SUPABASE_SERVICE_ROLE_KEY=$SERVICE_ROLE_KEY"
    echo "SUPABASE_JWT_SECRET=$JWT_SECRET"
    echo "SUPABASE_DB_URL=$DB_URL"
    echo "SUPABASE_S3_PROTOCOL_ACCESS_KEY_ID=${S3_PROTOCOL_ACCESS_KEY_ID:-}"
    echo "SUPABASE_S3_PROTOCOL_ACCESS_KEY_SECRET=${S3_PROTOCOL_ACCESS_KEY_SECRET:-}"
    echo "SUPABASE_S3_PROTOCOL_REGION=${S3_PROTOCOL_REGION:-local}"
    echo "SUPABASE_STORAGE_S3_URL=${STORAGE_S3_URL:-}"
} > "$RUNTIME_ENV"
echo "  wrote $RUNTIME_ENV"

BUCKETS_TS=$(date +%s)
{{range .Buckets}}
echo "  create storage bucket: {{.}}"
curl -sS -o /dev/null -X POST "$API_URL/storage/v1/bucket" \
    -H "apikey: $SERVICE_ROLE_KEY" \
    -H "Authorization: Bearer $SERVICE_ROLE_KEY" \
    -H "Content-Type: application/json" \
    -d '{"id":"{{.}}","name":"{{.}}","public":false}' || true
{{end}}
echo "  buckets: $(($(date +%s) - BUCKETS_TS))s"

USERS_TS=$(date +%s)
{{range .AuthUsers}}
echo "  create auth user: {{.Email}}"
metadata='{{.MetadataJSON}}'
curl -sS -o /dev/null -X POST "$API_URL/auth/v1/admin/users" \
    -H "apikey: $SERVICE_ROLE_KEY" \
    -H "Authorization: Bearer $SERVICE_ROLE_KEY" \
    -H "Content-Type: application/json" \
    -d "$(printf '{"email":"%s","password":"%s","email_confirm":true,"user_metadata":%s}' \
            "{{.Email}}" "{{.Password}}" "$metadata")" || true
{{end}}
echo "  auth users: $(($(date +%s) - USERS_TS))s"

echo "✓ supabase bootstrap done in $(($(date +%s) - START_TS))s"
`
