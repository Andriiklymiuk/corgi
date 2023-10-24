package templates

var DockerComposeDgraph = `version: "3.9"

services:
  dgraph-zero-{{.ServiceName}}:
    image: dgraph/dgraph:{{if .Version}}{{.Version}}{{else}}latest{{end}}
    container_name: zero-{{.ServiceName}}
    ports:
      - "5080:5080"
      - "6080:6080"
    command: dgraph zero --my=dgraph-zero-{{.ServiceName}}:5080
    networks:
      - corgi-network
    volumes:
      - zero-volume:/dgraph

  dgraph-alpha-{{.ServiceName}}:
    image: dgraph/dgraph:{{if .Version}}{{.Version}}{{else}}latest{{end}}
    container_name: alpha-{{.ServiceName}}
    ports:
      - "{{.Port}}:8080"
      - "9080:9080"
    command: dgraph alpha --my=dgraph-alpha-{{.ServiceName}}:7080 --zero=dgraph-zero-{{.ServiceName}}:5080
    environment:
      - DGRAPH_ALPHA_SECURITY=whitelist=0.0.0.0/0
    networks:
      - corgi-network
    volumes:
      - alpha-volume:/dgraph

  dgraph-ratel:
    image: dgraph/ratel:latest
    ports:
      - "8000:8000"
    networks:
      - corgi-network

networks:
  corgi-network:
    driver: bridge

volumes:
  zero-volume:
  alpha-volume:
`

var MakefileDgraph = `up:
	docker-compose up -d
down:
	docker-compose down --volumes
stop:
	docker stop dgraph-alpha-{{.ServiceName}} dgraph-zero-{{.ServiceName}}
id:
	docker ps -aqf "name=dgraph-alpha-{{.ServiceName}}" | awk '{print $1}'
logs:
	docker logs dgraph-alpha-{{.ServiceName}}
logs-zero:
	docker logs dgraph-zero-{{.ServiceName}}
help:
	make -qpRr | egrep -e '^[a-z].*:$$' | sed -e 's~:~~g' | sort

.PHONY: up down stop id logs logs-zero help
`
