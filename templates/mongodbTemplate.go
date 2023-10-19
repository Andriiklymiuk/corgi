package templates

var DockerComposeMongodb = `version: "3.9"

services:
  mongo-{{.ServiceName}}:
    image: mongo:{{if .Version}}{{.Version}}{{else}}latest{{end}}
    container_name: mongo-{{.ServiceName}}
    environment:
      - MONGO_INITDB_ROOT_USERNAME={{.User}}
      - MONGO_INITDB_ROOT_PASSWORD={{.Password}}
      - MONGO_INITDB_DATABASE={{.DatabaseName}}
    ports:
      - "{{.Port}}:27017"
    networks:
      - corgi-network

networks:
  corgi-network:
    driver: bridge
`

var MakefileMongodb = `up:
	docker compose up -d
down:
	docker compose down --volumes
stop:
	docker stop mongo-{{.ServiceName}}
id:
	docker ps -aqf "name=mongo-{{.ServiceName}}" | awk '{print $1}'
seed:
	cat dump.json | docker exec -i $(c) mongoimport --host localhost --port {{.Port}} --username {{.User}} --password {{.Password}} --db {{.DatabaseName}} --collection myCollection --drop
{{if .SeedFromDb.Host}}getDump:
	mongodump --host {{.SeedFromDb.Host}} --port {{.SeedFromDb.Port}} --username {{.SeedFromDb.User}} --password=$(p) --db {{.SeedFromDb.DatabaseName}} --out "dump-folder"
{{end}}getSelfDump:
	mongodump --host {{.Host}} --port {{.Port}} --username {{.User}} --password=$(p) --db {{.DatabaseName}} --out "dump-folder"
remove:
	docker rm --volumes mongo-{{.ServiceName}}
logs:
	docker logs mongo-{{.ServiceName}}
help:
	make -qpRr | egrep -e '^[a-z].*:$$' | sed -e 's~:~~g' | sort

.PHONY: up down stop id seed {{if .SeedFromDb.Host}}getDump {{end}}getSelfDump remove logs help
`
