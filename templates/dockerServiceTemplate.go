package templates

var DockerComposeService = `services:
  {{.ServiceName}}:
    container_name: {{.ServiceName}}
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
	docker stop {{.ServiceName}}
id:
	docker ps -aqf "name={{.ServiceName}}" | awk '{print $1}'
remove:
	docker rm --volumes {{.ServiceName}}
logs:
	docker logs {{.ServiceName}}
build:
	docker compose build {{.ServiceName}}
help:
	make -qpRr | egrep -e '^[a-z].*:$$' | sed -e 's~:~~g' | sort

.PHONY: up down stop id remove logs build help
`
