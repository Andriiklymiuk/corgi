package templates

var DockerComposeRedisServer = `services:
  redis-server-{{.ServiceName}}:
    image: redis:{{if .Version}}{{.Version}}-{{end}}alpine
    container_name: redis-server-{{.ServiceName}}
    command: redis-server {{if .Password}}--requirepass {{.Password}}{{end}}
    ports:
      - "{{.Port}}:6379"
    networks:
      - corgi-network

networks:
  corgi-network:
    driver: bridge
`

var MakefileRedisServer = `up:
	docker compose up -d
down:
	docker compose down --volumes
stop:
	docker stop redis-server-{{.ServiceName}}
id:
	docker ps -aqf "name=redis-server-{{.ServiceName}}" | awk '{print $1}'
cli:
	docker exec -it redis-server-{{.ServiceName}} redis-cli -a {{.Password}}
remove:
	docker rm --volumes redis-server-{{.ServiceName}}
logs:
	docker logs redis-server-{{.ServiceName}}
seed:
	@echo "Copying dump.rdb into local Docker container..."
	docker cp ./dump.rdb redis-server-{{.ServiceName}}:/data/
	@echo "Restarting Redis service in Docker container..."
	docker restart redis-server-{{.ServiceName}}
getDump:
	@echo "Creating Redis dump..."
	docker exec redis-server-{{.ServiceName}} redis-cli SAVE
	@echo "Copying dump.rdb to current directory..."
	docker cp redis-server-{{.ServiceName}}:/data/dump.rdb ./dump.rdb
help:
	make -qpRr | egrep -e '^[a-z].*:$$' | sed -e 's~:~~g' | sort

.PHONY: up down stop id cli remove logs seed getDump help
`
