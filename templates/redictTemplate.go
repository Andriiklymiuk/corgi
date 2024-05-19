package templates

var DockerComposeRedict = `version: "3.9"

services:
  redict-{{.ServiceName}}:
    image: registry.redict.io/redict:{{if .Version}}{{.Version}}{{else}}latest{{end}}
    container_name: redict-{{.ServiceName}}
    ports:
      - "{{.Port}}:6380"
    volumes:
      - redict_data-{{.ServiceName}}:/data
    environment:
      REDICT_DATABASE_PATH: "/data/db.redict"
      REDICT_LOG_LEVEL: "info"
      # Add other environment variables here as needed.
    restart: unless-stopped

volumes:
  redict_data-{{.ServiceName}}:

networks:
  corgi-network:
    driver: bridge
`

var MakefileRedict = `up:
	docker-compose up -d
down:
	docker-compose down
stop:
	docker stop redict-{{.ServiceName}}
restart:
	docker restart redict-{{.ServiceName}}
logs:
	docker logs redict-{{.ServiceName}}
backup:
	@echo "Backup not directly supported via Docker. Consider backing up the /data directory manually."
restore:
	@echo "Restore by replacing the /data directory contents with your backup."
remove:
	docker rm --volumes redict-{{.ServiceName}}

.PHONY: up down stop restart logs backup restore remove
`
