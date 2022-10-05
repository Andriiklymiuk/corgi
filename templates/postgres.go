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
help:
	make -qpRr | egrep -e '^[a-z].*:$$' | sed -e 's~:~~g' | sort

.PHONY: up down stop id seed help
`