package templates

var DockerComposeImage = `services:
  image-{{.ServiceName}}:
    image: {{.Image}}
    container_name: image-{{.ServiceName}}
{{- if .Port }}
    ports:
      - "{{.Port}}:{{if .ContainerPort}}{{.ContainerPort}}{{else}}{{.Port}}{{end}}"
{{- end }}
{{- if .Environment }}
    environment:
{{- range .Environment }}
      - {{ . }}
{{- end }}
{{- end }}
{{- if .Volumes }}
    volumes:
{{- range .Volumes }}
      - {{ . }}
{{- end }}
{{- end }}
{{- if .Command }}
    command: [{{ range $i, $arg := .Command }}{{ if $i }}, {{ end }}"{{ $arg }}"{{ end }}]
{{- end }}
    restart: unless-stopped
    networks:
      - corgi-network

networks:
  corgi-network:
    driver: bridge
`

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
