package templates

var DockerComposeInfluxDB = `version: "3.9"

services:
  influxdb-{{.ServiceName}}:
    image: influxdb:{{if .Version}}{{.Version}}{{else}}latest{{end}}
    container_name: influxdb-{{.ServiceName}}
    environment:
      - DOCKER_INFLUXDB_INIT_MODE=setup
      - DOCKER_INFLUXDB_INIT_USERNAME={{.User}}
      - DOCKER_INFLUXDB_INIT_PASSWORD={{.Password}}
      - DOCKER_INFLUXDB_INIT_ADMIN_TOKEN={{.Password}}
      - DOCKER_INFLUXDB_INIT_ORG=corgi
      - DOCKER_INFLUXDB_INIT_BUCKET={{.DatabaseName}}
    volumes:
      - influxdb-data:/var/lib/influxdb2
      - influxdb-data:/etc/influxdb2
    ports:
      - "{{.Port}}:8086"
    networks:
      - corgi-network

volumes:
  influxdb-data:

networks:
  corgi-network:
    driver: bridge
`

var MakefileInfluxDB = `up:
	docker compose up -d
down:
	docker compose down --volumes
stop:
	docker stop influxdb-{{.ServiceName}}
id:
	docker ps -aqf "name=influxdb-{{.ServiceName}}" | awk '{print $1}'
remove:
	docker rm --volumes influxdb-{{.ServiceName}}
logs:
	docker logs influxdb-{{.ServiceName}}
help:
	make -qpRr | egrep -e '^[a-z].*:$$' | sed -e 's~:~~g' | sort

.PHONY: up down stop id remove logs help
`
