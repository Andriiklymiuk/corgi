package templates

var DockerComposePgvector = `services:
  pgvector-{{.ServiceName}}:
    image: pgvector/pgvector:pg{{if .Version}}{{.Version}}{{else}}16{{end}}
    container_name: pgvector-{{.ServiceName}}
    environment:
      - POSTGRES_USER={{.User}}
      - POSTGRES_PASSWORD={{.Password}}
      - POSTGRES_DB={{.DatabaseName}}
    ports:
      - "{{.Port}}:5432"
    volumes:
      - pgvector-{{.ServiceName}}-data:/var/lib/postgresql/data
    networks:
      - corgi-network
volumes:
  pgvector-{{.ServiceName}}-data:
networks:
  corgi-network:
    driver: bridge
`

var MakefilePgvector = `up:
	docker compose up -d
down:
	docker compose down --volumes
stop:
	docker stop pgvector-{{.ServiceName}}
id:
	docker ps -aqf "name=pgvector-{{.ServiceName}}" | awk '{print $1}'
seed:
	cat dump.sql | docker exec -i $(c) psql -U {{.User}} -d {{.DatabaseName}}
{{if .SeedFromDb.Host}}getDump:
	PGPASSWORD=$(p) pg_dump --host {{.SeedFromDb.Host}} --port {{.SeedFromDb.Port}} --username {{.SeedFromDb.User}} -d {{.SeedFromDb.DatabaseName}} --blobs --no-owner --no-privileges --no-unlogged-table-data --format plain --verbose --file "dump.sql"
{{end}}getSelfDump:
	PGPASSWORD=$(p) pg_dump --host {{.Host}} --port {{.Port}} --username {{.User}} -d {{.DatabaseName}} --blobs --no-owner --no-privileges --no-unlogged-table-data --format plain --verbose --file "dump.sql"
remove:
	docker rm --volumes pgvector-{{.ServiceName}}
logs:
	docker logs pgvector-{{.ServiceName}}
help:
	make -qpRr | egrep -e '^[a-z].*:$$' | sed -e 's~:~~g' | sort
.PHONY: up down stop id seed {{if .SeedFromDb.Host}}getDump {{end}}getSelfDump remove logs help
`
