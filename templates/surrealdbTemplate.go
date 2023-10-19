package templates

var DockerComposeSurrealDB = `version: "3.9"

services:
  surrealdb-{{.ServiceName}}:
    image: surrealdb/surrealdb:{{if .Version}}{{.Version}}{{else}}latest{{end}}
    container_name: surrealdb-{{.ServiceName}}
    ports:
      - "{{.Port}}:8000"
    entrypoint: 
      - /surreal 
      - start 
      - --user
      - {{.User}}
      - --pass
      - {{.Password}}
    networks:
      - corgi-network

networks:
  corgi-network:
    driver: bridge
`

var MakefileSurrealDB = `up:
	docker compose up -d
down:
	docker compose down --volumes
stop:
	docker stop surrealdb-{{.ServiceName}}
id:
	docker ps -aqf "name=surrealdb-{{.ServiceName}}" | awk '{print $1}'
remove:
	docker rm --volumes surrealdb-{{.ServiceName}}
logs:
	docker logs surrealdb-{{.ServiceName}}
help:
	make -qpRr | egrep -e '^[a-z].*:$$' | sed -e 's~:~~g' | sort

.PHONY: up down stop id remove logs help
`
