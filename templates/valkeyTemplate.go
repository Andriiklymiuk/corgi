package templates

var DockerComposeValkey = `services:
  valkey-{{.ServiceName}}:
    image: valkey/valkey:{{if .Version}}{{.Version}}{{else}}alpine{{end}}
    container_name: valkey-{{.ServiceName}}
    command: valkey-server{{if .Password}} --requirepass {{.Password}}{{end}}
    ports:
      - "{{.Port}}:6379"
    volumes:
      - valkey_data-{{.ServiceName}}:/data
    restart: unless-stopped
    networks:
      - corgi-network

volumes:
  valkey_data-{{.ServiceName}}:

networks:
  corgi-network:
    driver: bridge
`

var MakefileValkey = `up:
	docker compose up -d
down:
	docker compose down --volumes
stop:
	docker stop valkey-{{.ServiceName}}
restart:
	docker restart valkey-{{.ServiceName}}
id:
	docker ps -aqf "name=valkey-{{.ServiceName}}" | awk '{print $1}'
cli:
	docker exec -it valkey-{{.ServiceName}} valkey-cli{{if .Password}} -a {{.Password}}{{end}}
logs:
	docker logs valkey-{{.ServiceName}}
remove:
	docker rm --volumes valkey-{{.ServiceName}}
seed:
	@echo "Copying dump.rdb into local Docker container..."
	docker cp ./dump.rdb valkey-{{.ServiceName}}:/data/
	@echo "Restarting Valkey service in Docker container..."
	docker restart valkey-{{.ServiceName}}
getDump:
	@echo "Creating Valkey dump..."
	docker exec valkey-{{.ServiceName}} valkey-cli{{if .Password}} -a {{.Password}}{{end}} SAVE
	@echo "Copying dump.rdb to current directory..."
	docker cp valkey-{{.ServiceName}}:/data/dump.rdb ./dump.rdb
help:
	make -qpRr | egrep -e '^[a-z].*:$$' | sed -e 's~:~~g' | sort

.PHONY: up down stop restart id cli logs remove seed getDump help
`
