package templates

var DockerComposeYugabytedb = `version: "3.9"

services:
  yugabyte-{{.ServiceName}}:
    image: yugabytedb/yugabyte:{{if .Version}}{{.Version}}{{else}}latest{{end}}
    container_name: yugabytedb-{{.ServiceName}}
    command: |
      bash -c '
      mkdir -p /var/ybinit
      echo "create user $${POSTGRES_USER} password '$${POSTGRES_PASSWORD}' " > /var/ybinit/01-usr.sql
      echo "create database $${POSTGRES_DB:-$${POSTGRES_USER}}             " > /var/ybinit/02-db.sql
      # start YugabyteDB
      bin/yugabyted start --daemon=false --initial_scripts_dir=/var/ybinit --tserver_flags=ysql_enable_auth=true
      '
    ports:
      - 7050:7000
      - {{.Port}}:5433
      - 9000:9000
      - 9042:9042
      - 15433:15433
    volumes:
      - yugabyte_db:/var/lib/yugabyteql/data
    environment:
      - POSTGRES_USER={{.User}}
      - POSTGRES_PASSWORD={{.Password}}
      - POSTGRES_DB={{.DatabaseName}}
    networks:
      - corgi-network

volumes:
  yugabyte_db:

networks:
  corgi-network:
    driver: bridge
`

var MakefileYugabytedb = `up:
	docker compose up -d
down:
	docker compose down --volumes
stop:
	docker stop yugabytedb-{{.ServiceName}}
id:
	docker ps -aqf "name=yugabytedb-*-{{.ServiceName}}" | awk '{print $1}'
seed:
	cat dump.sql | docker exec -i $(c) psql -U {{.User}} -d {{.DatabaseName}} -h yugabytedb-tserver
getSelfDump:
	PGPASSWORD=$(p) pg_dump --host yugabytedb-tserver --port {{.Port}} --username {{.User}} -d {{.DatabaseName}} --blobs --no-owner --no-privileges --no-unlogged-table-data --format plain --verbose --file "dump.sql"
remove:
	docker rm --volumes yugabytedb-{{.ServiceName}}
logs:
	docker logs yugabytedb-{{.ServiceName}}
help:
	make -qpRr | egrep -e '^[a-z].*:$$' | sed -e 's~:~~g' | sort

.PHONY: up down stop id seed getSelfDump remove logs logs help
`
