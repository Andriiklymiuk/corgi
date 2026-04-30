package templates

// DockerComposeImage renders a generic docker-compose for stateless services
// shipped as a public image (gotenberg, mailhog, jaeger, etc.). The container
// listens on `ContainerPort` (defaults to `Port` if unset) and is exposed on
// the host's `Port`. Optional environment entries pass through verbatim.
var DockerComposeImage = `services:
  image-{{.ServiceName}}:
    image: {{.Image}}
    container_name: image-{{.ServiceName}}
{{- if .Port }}
    ports:
      - "{{.Port}}:{{if .ContainerPort}}{{.ContainerPort}}{{else}}{{.Port}}{{end}}"
{{- end }}
    restart: unless-stopped
    networks:
      - corgi-network

networks:
  corgi-network:
    driver: bridge
`

// MakefileImage matches the postgres-driver Makefile shape (up/down/stop/id/...)
// so corgi's lifecycle commands work identically across drivers.
var MakefileImage = `up:
	docker compose up -d
down:
	docker compose down --volumes
stop:
	docker stop image-{{.ServiceName}}
remove:
	docker rm --volumes image-{{.ServiceName}}
logs:
	docker logs image-{{.ServiceName}}
id:
	docker ps -aqf "name=image-{{.ServiceName}}" | awk '{print $1}'
help:
	@make -qpRr | egrep -e '^[a-z].*:$$' | sed -e 's~:~~g' | sort

.PHONY: up down stop remove logs id help
`
