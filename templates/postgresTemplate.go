package templates

var DockerComposePostgres = `version: "3.8"

services:
  postgres:
    image: postgres:11.5-alpine
    container_name: postgres-{{.ServiceName}}
    logging:
      driver: none
    environment:
      - POSTGRES_USER={{.User}}
      - POSTGRES_PASSWORD={{.Password}}
      - POSTGRES_DB={{.DatabaseName}}
    ports:
      - "{{.Port}}:5432"
`

var MakefilePostgres = `up:
	docker compose up -d
down:
	docker compose down    
stop:
	docker stop postgres-{{.ServiceName}}
id:
	docker ps -aqf "name=postgres-{{.ServiceName}}" | awk '{print $1}'
seed:
	cat dump.sql | docker exec -i $(c)  psql -U {{.User}} -d {{.DatabaseName}}
{{if .SeedFromDb.Host}}getDump:
	PGPASSWORD=$(p) pg_dump --host {{.SeedFromDb.Host}} --port {{.SeedFromDb.Port}} --username {{.SeedFromDb.User}} -d {{.SeedFromDb.DatabaseName}} --blobs --no-owner --no-privileges --no-unlogged-table-data --format plain --verbose --file "dump.sql"
{{end}}help:
	make -qpRr | egrep -e '^[a-z].*:$$' | sed -e 's~:~~g' | sort

.PHONY: up down stop id seed {{if .SeedFromDb.Host}}getDump{{end}}help
`
