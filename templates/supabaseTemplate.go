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

// Parses a supabase config.toml. Arg can be either a direct .toml path or
// a project root (resolves to <root>/supabase/config.toml). Empty falls back
// to cwd. Missing sections use supabase CLI defaults.
func ReadSupabasePorts(pathOrRoot string) SupabasePorts {
	defaults := SupabasePorts{API: 54321, DB: 54322, Studio: 54323, Inbucket: 54324}
	var tomlPath string
	switch {
	case strings.HasSuffix(pathOrRoot, ".toml"):
		tomlPath = pathOrRoot
	case pathOrRoot == "":
		tomlPath = "supabase/config.toml"
	default:
		tomlPath = pathOrRoot + "/supabase/config.toml"
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

// Wraps the supabase CLI. WORKDIR is the service folder when configTomlPath
// is set (corgi owns config.toml), or project root otherwise (legacy).
// DESIRED_*_PORT come from yaml port:/dbPort:/studioPort:/inbucketPort:;
// "0" = noop. Patches happen before `supabase start` so bind ports match
// the env corgi emits.
var MakefileSupabase = `{{if .ConfigTomlPath -}}
WORKDIR := $(dir $(realpath $(firstword $(MAKEFILE_LIST))))
{{- else -}}
WORKDIR := $(shell git rev-parse --show-toplevel 2>/dev/null || (cd ../../.. && pwd))
{{- end}}
DESIRED_API_PORT := {{.Port}}
DESIRED_DB_PORT := {{.DbPort}}
DESIRED_STUDIO_PORT := {{.StudioPort}}
DESIRED_INBUCKET_PORT := {{.InbucketPort}}

# In-place patch [<section>].port in config.toml. Args: section, desired port.
define _patch_supabase_port
	@if [ "$(2)" != "0" ] && [ -n "$(2)" ]; then \
		cd "$(WORKDIR)" && awk -v sec="$(1)" -v p="$(2)" ' \
			/^\[/{ section=$$0 } \
			section=="["sec"]" && /^port[[:space:]]*=/{ \
				if ($$0 != "port = " p) { \
					print "port = " p; changed=1; next \
				} \
			} \
			{ print } \
			END{ if (changed) print "→ patched ["sec"].port=" p " in supabase/config.toml" > "/dev/stderr" } \
		' supabase/config.toml > supabase/config.toml.corgi-tmp && \
			mv supabase/config.toml.corgi-tmp supabase/config.toml; \
	fi
endef

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
	@cd "$(WORKDIR)" && [ -f supabase/config.toml ] || { \
		echo "→ supabase/config.toml missing — running 'supabase init'..."; \
		supabase init; \
	}
	$(call _patch_supabase_port,api,$(DESIRED_API_PORT))
	$(call _patch_supabase_port,db,$(DESIRED_DB_PORT))
	$(call _patch_supabase_port,studio,$(DESIRED_STUDIO_PORT))
	$(call _patch_supabase_port,inbucket,$(DESIRED_INBUCKET_PORT))
	@cd "$(WORKDIR)" && if supabase status >/dev/null 2>&1; then \
		echo "✓ supabase already running"; \
	else \
		echo "→ starting supabase (first run pulls images; can take several minutes)..."; \
		supabase start; \
	fi
	@bash bootstrap/bootstrap.sh

down:
	@cd "$(WORKDIR)" && supabase stop --no-backup 2>/dev/null || true

stop: down

logs:
	@cd "$(WORKDIR)" && supabase status

id:
	@docker ps --filter "name=supabase_" --format '{{"{{.ID}}"}}' | head -1

remove:
	@cd "$(WORKDIR)" && supabase stop --no-backup 2>/dev/null || true

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

# WORKDIR matches Makefile's: service folder when configTomlPath is set,
# project root otherwise.
{{if .ConfigTomlPath -}}
WORKDIR="$(cd "$(dirname "$0")/.." && pwd)"
{{- else -}}
WORKDIR="$(git rev-parse --show-toplevel 2>/dev/null || (cd ../../.. && pwd))"
{{- end}}
cd "$WORKDIR"
ROOT="$(git rev-parse --show-toplevel 2>/dev/null || (cd ../../.. && pwd))"

if ! command -v supabase >/dev/null 2>&1; then
    echo "supabase CLI not found — skipping bootstrap"
    exit 0
fi

# jq is optional. New users still get created without it; only the
# password / user_metadata reconcile pass on existing users is skipped.
HAVE_JQ=0
if command -v jq >/dev/null 2>&1; then
    HAVE_JQ=1
else
    echo "⚠ jq not found — auth user reconcile will be skipped (install: brew install jq)"
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
# Reconcile loop: POST creates new users (422 swallowed if already there);
# follow-up PUT keeps password + user_metadata in sync with corgi-compose.yml
# on every re-run. PUT path is gated on $HAVE_JQ from the prerequisite check
# above — without jq, new users still get created but edits don't propagate.
{{range .AuthUsers}}
echo "  upsert auth user: {{.Email}}"
metadata='{{.MetadataJSON}}'
curl -sS -o /dev/null -X POST "$API_URL/auth/v1/admin/users" \
    -H "apikey: $SERVICE_ROLE_KEY" \
    -H "Authorization: Bearer $SERVICE_ROLE_KEY" \
    -H "Content-Type: application/json" \
    -d "$(printf '{"email":"%s","password":"%s","email_confirm":true,"user_metadata":%s}' \
            "{{.Email}}" "{{.Password}}" "$metadata")" || true

if [ "$HAVE_JQ" = "1" ]; then
    # gotrue admin API has no server-side email filter, so list (cap at 1000 —
    # plenty for local dev seeds) and pick the matching id client-side.
    user_id=$(curl -sS "$API_URL/auth/v1/admin/users?per_page=1000" \
        -H "apikey: $SERVICE_ROLE_KEY" \
        -H "Authorization: Bearer $SERVICE_ROLE_KEY" \
        | jq -r --arg e "{{.Email}}" '.users[]? | select(.email == $e) | .id' | head -1)
    if [ -n "$user_id" ]; then
        curl -sS -o /dev/null -X PUT "$API_URL/auth/v1/admin/users/$user_id" \
            -H "apikey: $SERVICE_ROLE_KEY" \
            -H "Authorization: Bearer $SERVICE_ROLE_KEY" \
            -H "Content-Type: application/json" \
            -d "$(printf '{"password":"%s","user_metadata":%s}' \
                    "{{.Password}}" "$metadata")" || true
    fi
fi
{{end}}
echo "  auth users: $(($(date +%s) - USERS_TS))s"

echo "✓ supabase bootstrap done in $(($(date +%s) - START_TS))s"
`
