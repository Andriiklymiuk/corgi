package templates

var DockerComposeScylla = `version: "3.9"

services:
  scylla-{{.ServiceName}}:
    image: scylladb/scylla:{{if .Version}}{{.Version}}{{else}}latest{{end}}
    container_name: scylla-{{.ServiceName}}
    ports:
      - "{{.Port}}:9042"
    volumes:
      - ./data:/var/lib/scylla
      - ./bootstrap:/etc/scylla-init
    networks:
      - corgi-network

networks:
  corgi-network:
    driver: bridge
`

var MakefileScylla = `up:
	chmod +x bootstrap/bootstrap.sh && docker compose up -d && docker exec scylla-{{.ServiceName}} /etc/scylla-init/bootstrap.sh
down:
	docker compose down --volumes    
stop:
	docker stop scylla-{{.ServiceName}}
id:
	docker ps -aqf "name=scylla-{{.ServiceName}}" | awk '{print $1}'
seed:
	cat dump.cql | docker exec -i $(id) cqlsh -u {{.User}} -p {{.Password}}
getSelfDump:
	echo "USE {{.DatabaseName}};" > dump.cql && \
	docker exec $(id) cqlsh -u {{.User}} -p {{.Password}} -e "DESCRIBE KEYSPACE {{.DatabaseName}}" >> dump.cql && \
	for table in $$(docker exec $(id) cqlsh -u {{.User}} -p {{.Password}} -e "DESCRIBE TABLES FROM {{.DatabaseName}}"); do \
	    echo "COPY {{.DatabaseName}}.$$table TO STDOUT;" | docker exec -i $(id) cqlsh -u {{.User}} -p {{.Password}} >> dump.cql; \
	done
remove:
	docker rm --volumes scylla-{{.ServiceName}}
logs:
	docker logs scylla-{{.ServiceName}}
help:
	make -qpRr | egrep -e '^[a-z].*:$$' | sed -e 's~:~~g' | sort

.PHONY: up down stop id seed getSelfDump remove logs help
`

var BootstrapScylla = `#!/usr/bin/env bash

set -euo pipefail

echo "Waiting for ScyllaDB to be ready"

# Define a function to test if ScyllaDB is ready.
is_scylla_ready() {
  if cqlsh -e "SELECT now() FROM system.local;" > /dev/null 2>&1; then
    return 0
  else
    return 1
  fi
}

# Wait for ScyllaDB to be ready.
for i in {1..90}; do
  if is_scylla_ready; then
    echo "ScyllaDB is ready"
    break
  fi
  echo "ScyllaDb is not ready yet, waiting"
  sleep 1
done

echo -e "\e[32mScyllaDB created successfully\e[0m"
echo -e "\e[32m===================\e[0m"
`

// This template doesn't create user (by default scylla is allowing all auth)
// for now it is fine, but need to change authenticator in scylla.yaml in the future
// https://opensource.docs.scylladb.com/stable/operating-scylla/security/runtime-authentication.html
