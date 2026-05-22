package templates

var DockerComposeService = `services:
  {{.DockerName}}:
    container_name: {{.DockerName}}
    build:
      context: ../../..
      dockerfile: Dockerfile
    ports:
      - "{{.Port}}:${DOCKERFILE_PORT}"
    env_file:
      - .env
    volumes:
      - ../../../:/app
      - /app/node_modules
      - /app/dist
    restart: unless-stopped
    networks:
      - corgi-network

networks:
  corgi-network:
    driver: bridge
`

var MakefileService = `up:
	docker compose up
down:
	docker compose down --volumes
stop:
	docker stop {{.DockerName}}
id:
	docker ps -aqf "name={{.DockerName}}" | awk '{print $1}'
remove:
	docker rm --volumes {{.DockerName}}
logs:
	docker logs {{.DockerName}}
build:
	docker compose build {{.DockerName}}
help:
	make -qpRr | egrep -e '^[a-z].*:$$' | sed -e 's~:~~g' | sort

.PHONY: up down stop id remove logs build help
`
