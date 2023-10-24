package templates

var DockerComposeMeiliSearch = `version: "3.9"

services:
  meilisearch-{{.ServiceName}}:
    image: getmeili/meilisearch:{{if .Version}}{{.Version}}{{else}}latest{{end}}
    container_name: meilisearch-{{.ServiceName}}
    environment:
      - MEILI_MASTER_KEY={{.Password}}
    ports:
      - "{{.Port}}:7700"
    volumes:
      - meilisearch-{{.ServiceName}}-volume:/data.ms

volumes:
  meilisearch-{{.ServiceName}}-volume:

networks:
  default:
    driver: bridge
`

var MakefileMeiliSearch = `up:
	docker-compose up -d
down:
	docker-compose down --volumes
stop:
	docker stop meilisearch-{{.ServiceName}}
logs:
	docker logs meilisearch-{{.ServiceName}}
help:
	make -qpRr | egrep -e '^[a-z].*:$$' | sed -e 's~:~~g' | sort

.PHONY: up down stop logs help
`
