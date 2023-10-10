package templates

var DockerComposeCockroach = `version: "3.9"

services:
  cockroach-{{.ServiceName}}:
    image: cockroachdb/cockroach:{{if .Version}}{{.Version}}{{else}}latest{{end}}
    command: start-single-node --insecure
    container_name: cockroach-{{.ServiceName}}
    ports:
      - "{{.Port}}:26257"
    volumes:
      - ./bootstrap:/var/opt/cockroach/startup
      - .:/var/opt/cockroach/backup
    networks:
      - corgi-network

networks:
  corgi-network:
    driver: bridge
`

var MakefileCockroach = `up:
	chmod +x bootstrap/bootstrap.sh && docker-compose up -d && docker exec cockroach-{{.ServiceName}} /var/opt/cockroach/startup/bootstrap.sh
down:
	docker compose down
stop:
	docker stop cockroach-{{.ServiceName}}
id:
	docker ps -aqf "name=cockroach-{{.ServiceName}}" | awk '{print $1}'
seed:
	cat dump.sql | docker exec -i $(id) /cockroach/cockroach sql --insecure -d {{.DatabaseName}} -u {{.User}} -p {{.Password}}
{{if .SeedFromDb.Host}}getDump:
	cockroach dump {{.SeedFromDb.DatabaseName}} --insecure --host={{.SeedFromDb.Host}} --port={{.SeedFromDb.Port}} -u {{.SeedFromDb.User}} > dump.sql
{{end}}getSelfDump:
	cockroach dump {{.DatabaseName}} --insecure --host={{.Host}} --port={{.Port}} -u {{.User}} > dump.sql
remove:
	docker rm cockroach-{{.ServiceName}}
help:
	make -qpRr | egrep -e '^[a-z].*:$$' | sed -e 's~:~~g' | sort

.PHONY: up down stop id remove getDump seed help
`

var BootstrapCockroach = `#!/bin/bash

set -euo pipefail

echo "Waiting for CockroachDB to be ready"
for i in {1..90}; do
  if /cockroach/cockroach sql --insecure -e "SELECT 1" > /dev/null 2>&1; then
    echo "CockroachDB is ready"
    break
  fi
  echo "waiting for CockroachDB..."
  sleep 1
done

echo "Configuring CockroachDB"
echo "==========================="

# Creating the specified database.
/cockroach/cockroach sql --insecure -e 'CREATE DATABASE IF NOT EXISTS "{{.DatabaseName}}"'

# Creating the user.
/cockroach/cockroach sql --insecure -e 'CREATE USER IF NOT EXISTS {{.User}}'

# Granting permissions to the user for the specific database.
/cockroach/cockroach sql --insecure -e 'GRANT ALL ON DATABASE "{{.DatabaseName}}" TO {{.User}}'
`
