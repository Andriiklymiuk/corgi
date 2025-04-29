package templates

var DockerComposeDragonfly = `services:
  dragonfly-{{.ServiceName}}:
    image: 'docker.dragonflydb.io/dragonflydb/dragonfly:{{if .Version}}{{.Version}}{{else}}latest{{end}}'
    container_name: dragonfly-{{.ServiceName}}
    ulimits:
      memlock: -1
    ports:
      - '{{.Port}}:6379'
    # For better performance, consider 'host' mode instead of 'port' to avoid docker NAT.
    # 'host' mode is NOT currently supported in Swarm Mode.
    # https://docs.docker.com/compose/compose-file/compose-file-v3/#network_mode
    # network_mode: "host"
    volumes:
      - dragonfly_data-{{.ServiceName}}:/data

volumes:
  dragonfly_data-{{.ServiceName}}:

networks:
  corgi-network:
    driver: bridge
`

var MakefileDragonfly = `up:
	docker-compose up -d
down:
	docker-compose down
stop:
	docker stop dragonfly-{{.ServiceName}}
restart:
	docker restart dragonfly-{{.ServiceName}}
logs:
	docker logs dragonfly-{{.ServiceName}}
backup:
	docker exec dragonfly-{{.ServiceName}} dragonfly --save
restore:
	@echo "Restoration process needs manual handling of the data file"
remove:
	docker rm --volumes dragonfly-{{.ServiceName}}

.PHONY: up down stop restart logs backup restore remove
`
