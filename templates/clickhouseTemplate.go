package templates

var DockerComposeClickHouse = `version: "3.9"

services:
  clickhouse-{{.ServiceName}}:
    image: clickhouse/clickhouse-server:{{if .Version}}{{.Version}}{{else}}latest{{end}}
    container_name: clickhouse-{{.ServiceName}}
    environment:
      - CLICKHOUSE_USER={{.User}}
      - CLICKHOUSE_PASSWORD={{.Password}}
      - CLICKHOUSE_DB=@@{{.DatabaseName}}@@
    ports:
      - "{{.Port}}:9000"
    networks:
      - corgi-network
    volumes:
      - clickhouse-data:/var/lib/clickhouse
      - ./bootstrap:/docker-entrypoint-initdb.d

networks:
  corgi-network:
    driver: bridge

volumes:
  clickhouse-data:
`

var MakefileClickHouse = `up:
	chmod +x bootstrap/bootstrap.sh && docker compose up -d
down:
	docker compose down --volumes
stop:
	docker stop clickhouse-{{.ServiceName}}
id:
	docker ps -aqf "name=clickhouse-{{.ServiceName}}" | awk '{print $1}'
seed:
	cat dump.sql | docker exec -i $(c) clickhouse-client --multiquery --host=localhost --user={{.User}} --password={{.Password}} --database=@@{{.DatabaseName}}@@
getSelfDump:
	@echo "Starting dump..."
	@echo "SET allow_experimental_database_materialize_mysql = 1;" > dump.sql;  # You might not need this, but sometimes enabling experimental settings can be useful.
	@docker exec -i $$(docker ps -aqf "name=clickhouse-{{.ServiceName}}") \
		clickhouse-client --host=localhost --user={{.User}} --password={{.Password}} --database={{.DatabaseName}} \
		--query="SHOW TABLES" | while read table; do \
		echo "Dumping schema for $$table..."; \
		docker exec -i $$(docker ps -aqf "name=clickhouse-{{.ServiceName}}") \
			clickhouse-client --host=localhost --user={{.User}} --password={{.Password}} --database={{.DatabaseName}} \
			--query="SHOW CREATE TABLE $$table" | tail -n +2 >> dump.sql; \
		echo ";\n" >> dump.sql; \
		echo "Dumping data for $$table..."; \
		docker exec -i $$(docker ps -aqf "name=clickhouse-{{.ServiceName}}") \
			clickhouse-client --host=localhost --user={{.User}} --password={{.Password}} --database={{.DatabaseName}} \
			--query="SELECT * FROM $$table FORMAT TabSeparatedWithNames" >> dump.sql; \
		echo "\n" >> dump.sql; \
		done;
remove:
	docker rm --volumes clickhouse-{{.ServiceName}}
logs:
	docker logs clickhouse-{{.ServiceName}}
help:
	make -qpRr | egrep -e '^[a-z].*:$$' | sed -e 's~:~~g' | sort

.PHONY: up down stop id seed getSelfDump remove logs help
`

var BootstrapClickHouse = `#!/bin/bash

set -euo pipefail

echo "Waiting for ClickHouse to be ready"

# Define a function to test if ClickHouse is ready.
is_clickhouse_ready() {
  if clickhouse-client --host localhost --user={{.User}} --password={{.Password}} --query="SELECT 1" > /dev/null 2>&1; then
    return 0
  else
    echo "Failed to connect. Let's check the error:"
    clickhouse-client --host localhost --user={{.User}} --password={{.Password}} --query="SELECT 1"
    return 1
  fi
}

# Wait for ClickHouse to be ready.
for i in {1..90}; do
  if is_clickhouse_ready; then
    echo "ClickHouse is ready"
    break
  fi
  echo "waiting for ClickHouse..."
  sleep 1
done

echo "ClickHouse connection successful"
echo "==================="
`
