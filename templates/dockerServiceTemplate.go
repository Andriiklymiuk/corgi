package templates

var DockerComposeService = `services:
  {{.DockerName}}:
    container_name: {{.DockerName}}
    build:
      context: {{.BuildContext}}
      dockerfile: {{.DockerfilePath}}
{{- if .Target}}
      target: {{.Target}}
{{- end}}
{{- if .BuildArgs}}
      args:
{{- range $k, $v := .BuildArgs}}
        {{$k}}: "{{$v}}"
{{- end}}
{{- end}}
    ports:
      - "{{.Port}}:{{.ContainerPort}}"
    env_file:
      - .env
{{- if .Volumes}}
    volumes:
{{- range .Volumes}}
      - {{.}}
{{- end}}
{{- end}}
{{- if .Command}}
    command: {{.Command}}
{{- end}}
    extra_hosts:
      - "host.docker.internal:host-gateway"
    restart: unless-stopped
    networks:
      - corgi-network

networks:
  corgi-network:
    driver: bridge
`

var MakefileService = `up:
	docker compose up --build
upd:
	docker compose up -d --build
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
followLogs:
	docker logs -f {{.DockerName}}
build:
	docker compose build
help:
	make -qpRr | egrep -e '^[a-z].*:$$' | sed -e 's~:~~g' | sort

.PHONY: up upd down stop id remove logs followLogs build help
`

var MakefileRepoCompose = `COMPOSE := docker compose -f {{.RepoComposeFile}} --env-file {{.EnvFilePath}}
up:
	$(COMPOSE) up --build
upd:
	$(COMPOSE) up -d --build
down:
	$(COMPOSE) down
stop:
	$(COMPOSE) stop
logs:
	$(COMPOSE) logs
followLogs:
	$(COMPOSE) logs -f
build:
	$(COMPOSE) build
help:
	make -qpRr | egrep -e '^[a-z].*:$$' | sed -e 's~:~~g' | sort

.PHONY: up upd down stop logs followLogs build help
`
