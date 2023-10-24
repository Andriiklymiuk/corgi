package templates

var DockerComposeFauna = `version: "3.9"

services:
  faunadb-{{.ServiceName}}:
    image: fauna/faunadb:{{if .Version}}{{.Version}}{{else}}latest{{end}}
    container_name: faunadb-{{.ServiceName}}
    ports:
      - "{{.Port}}:8443"
    volumes:
      - faunadb-data:/var/lib/faunadb
    networks:
      - corgi-network

networks:
  corgi-network:
    driver: bridge

volumes:
  faunadb-data:
`

var MakefileFauna = `up:
	docker-compose up -d
down:
	docker-compose down --volumes
stop:
	docker stop faunadb-{{.ServiceName}}
id:
	docker ps -aqf "name=faunadb-{{.ServiceName}}" | awk '{print $1}'
logs:
	docker logs faunadb-{{.ServiceName}}
help:
	make -qpRr | egrep -e '^[a-z].*:$$' | sed -e 's~:~~g' | sort

.PHONY: up down stop id logs help
`
