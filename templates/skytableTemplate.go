package templates

var DockerComposeSkytable = `version: "3.9"
      
services:
  skytable-{{.ServiceName}}:
    image: skytable/skytable:{{if .Version}}{{.Version}}{{else}}latest{{end}}
    ports:
      - '{{.Port}}:2003'
    container_name: skytable-{{.ServiceName}}
    volumes:
      - ./data:/data
    command: ["/skytable/skytable --datadir /data"]
    networks:
      - corgi-network

networks:
  corgi-network:
    driver: bridge
`

var MakefileSkytable = `up:
	docker compose up -d
down:
	docker compose down --volumes
stop:
	docker stop skytable-{{.ServiceName}}
id:
	docker ps -aqf "name=skytable-{{.ServiceName}}" | awk '{print $1}'
backup:
	@echo "Creating Skytable snapshot..."
	docker exec skytable-{{.ServiceName}} skytable --snapshot
	@echo "Copying snapshot.sky to current directory..."
	docker cp skytable-{{.ServiceName}}:/data/snapshot.sky ./snapshot.sky
restore:
	@echo "Restoring Skytable snapshot..."
	docker cp ./snapshot.sky skytable-{{.ServiceName}}:/data/snapshot.sky
	@echo "Restarting Skytable service in Docker container..."
	docker restart skytable-{{.ServiceName}}
remove:
	docker rm --volumes skytable-{{.ServiceName}}
logs:
	docker logs skytable-{{.ServiceName}}
help:
	make -qpRr | egrep -e '^[a-z].*:$$' | sed -e 's~:~~g' | sort

.PHONY: up down stop id backup restore remove logs help
`
