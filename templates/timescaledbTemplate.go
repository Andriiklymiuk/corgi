package templates

var DockerComposeTimescale = `version: "3.9"

services:
  timescaledb-{{.ServiceName}}:
    image: timescale/timescaledb:{{if .Version}}{{.Version}}{{else}}latest-pg14{{end}}
    container_name: timescaledb-{{.ServiceName}}
    environment:
      - POSTGRES_USER={{.User}}
      - POSTGRES_PASSWORD={{.Password}}
      - POSTGRES_DB={{.DatabaseName}}
    ports:
      - "{{.Port}}:5432"
    networks:
      - corgi-network

networks:
  corgi-network:
    driver: bridge
`

var MakefileTimescale = `up:
	docker compose up -d
down:
	docker compose down --volumes
stop:
	docker stop timescaledb-{{.ServiceName}}
id:
	docker ps -aqf "name=timescaledb-{{.ServiceName}}" | awk '{print $1}'
seed:
	cat dump.sql | docker exec -i $(c)  psql -U {{.User}} -d {{.DatabaseName}}
{{if .SeedFromDb.Host}}getDump:
	PGPASSWORD=$(p) pg_dump --host {{.SeedFromDb.Host}} --port {{.SeedFromDb.Port}} --username {{.SeedFromDb.User}} -d {{.SeedFromDb.DatabaseName}} --blobs --no-owner --no-privileges --no-unlogged-table-data --format plain --verbose --file "dump.sql"
{{end}}getSelfDump:
	PGPASSWORD=$(p) pg_dump --host {{.Host}} --port {{.Port}} --username {{.User}} -d {{.DatabaseName}} --blobs --no-owner --no-privileges --no-unlogged-table-data --format plain --verbose --file "dump.sql"
remove:
	docker rm --volumes timescaledb-{{.ServiceName}}
logs:
	docker logs timescaledb-{{.ServiceName}}
help:
	make -qpRr | egrep -e '^[a-z].*:$$' | sed -e 's~:~~g' | sort

.PHONY: up down stop id seed {{if .SeedFromDb.Host}}getDump {{end}}getSelfDump remove logs help
`
