package templates

var DockerComposePostgis = `services:
  postgis-{{.ServiceName}}:
    image: postgis/postgis:{{if .Version}}{{.Version}}-{{else}}17-3.5-{{end}}alpine
    container_name: postgis-{{.ServiceName}}
    environment:
      - POSTGRES_USER={{.User}}
      - POSTGRES_PASSWORD={{.Password}}
      - POSTGRES_DB={{.DatabaseName}}
    ports:
      - "{{.Port}}:5432"
    volumes:
      - postgis-{{.ServiceName}}-data:/var/lib/postgresql/data
    networks:
      - corgi-network
volumes:
  postgis-{{.ServiceName}}-data:
networks:
  corgi-network:
    driver: bridge
`

var MakefilePostgis = `up:
	docker compose up -d
down:
	docker compose down --volumes
stop:
	docker stop postgis-{{.ServiceName}}
id:
	docker ps -aqf "name=postgis-{{.ServiceName}}" | awk '{print $1}'
seed:
	cat dump.sql | docker exec -i $(c) psql -U {{.User}} -d {{.DatabaseName}}
{{if .SeedFromDb.Host}}getDump:
	PGPASSWORD=$(p) pg_dump --host {{.SeedFromDb.Host}} --port {{.SeedFromDb.Port}} --username {{.SeedFromDb.User}} -d {{.SeedFromDb.DatabaseName}} --blobs --no-owner --no-privileges --no-unlogged-table-data --format plain --verbose --file "dump.sql"
{{end}}getSelfDump:
	PGPASSWORD=$(p) pg_dump --host {{.Host}} --port {{.Port}} --username {{.User}} -d {{.DatabaseName}} --blobs --no-owner --no-privileges --no-unlogged-table-data --format plain --verbose --file "dump.sql"
remove:
	docker rm --volumes postgis-{{.ServiceName}}
logs:
	docker logs postgis-{{.ServiceName}}
help:
	make -qpRr | egrep -e '^[a-z].*:$$' | sed -e 's~:~~g' | sort
.PHONY: up down stop id seed {{if .SeedFromDb.Host}}getDump {{end}}getSelfDump remove logs help
`
