#!/usr/bin/env bash
source "$(dirname "$0")/../_fixtures/common.sh"

start_repo
mkdir -p orders billing
printf 'module acme\n\ngo 1.22\n' > go.mod
printf 'package orders\n\nimport "acme/billing"\n\nfunc Total(id string) int { return billing.Price(id) }\n\nfunc Lines(id string) []string { return []string{id} }\n' > orders/orders.go
printf 'package billing\n\nimport "acme/orders"\n\nfunc Price(id string) int { return len(orders.Lines(id)) * 100 }\n' > billing/billing.go
commit_all "Orders and billing"
