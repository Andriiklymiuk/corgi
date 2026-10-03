#!/usr/bin/env bash
source "$(dirname "$0")/../_fixtures/common.sh"

start_repo
write_base
commit_all "Initial api"
add_origin
