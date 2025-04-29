package templates

// TODO: change to latest version from unstable, when it is available
var DockerComposeValkey = `services:
  valkey-{{.ServiceName}}:
    image: valkey/valkey:{{if .Version}}{{.Version}}{{else}}unstable{{end}}
    container_name: valkey-{{.ServiceName}}
    ports:
      - "{{.Port}}:8080"
    environment:
      VALKEY_ENV_VAR: "example"
      # Define other environment variables here as needed.
    volumes:
      - valkey_data-{{.ServiceName}}:/data
    restart: unless-stopped

volumes:
  valkey_data-{{.ServiceName}}:

networks:
  corgi-network:
    driver: bridge
`

var MakefileValkey = `up:
	docker-compose up -d
down:
	docker-compose down
stop:
	docker stop valkey-{{.ServiceName}}
restart:
	docker restart valkey-{{.ServiceName}}
logs:
	docker logs valkey-{{.ServiceName}}
remove:
	docker rm --volumes valkey-{{.ServiceName}}

.PHONY: up down stop restart logs remove
`
