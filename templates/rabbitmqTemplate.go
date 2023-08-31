package templates

var DockerComposeRabbitMQ = `version: "3.9"

services:
  rabbitmq-{{.ServiceName}}:
    image: rabbitmq:3-management
    container_name: rabbitmq-{{.ServiceName}}
    environment:
      - RABBITMQ_DEFAULT_USER={{.User}}
      - RABBITMQ_DEFAULT_PASS={{.Password}}
    volumes:
      - /var/lib/rabbitmq
    ports:
      - "{{.Port}}:5672"
      - "15672:15672"
    networks:
      - corgi-network

networks:
  corgi-network:
    driver: bridge
`

var MakefileRabbitMQ = `up:
	docker compose up -d
down:
	docker compose down    
stop:
	docker stop rabbitmq-{{.ServiceName}}
id:
	docker ps -aqf "name=rabbitmq-{{.ServiceName}}" | awk '{print $1}'
remove:
	docker rm rabbitmq-{{.ServiceName}}
help:
	make -qpRr | egrep -e '^[a-z].*:$$' | sed -e 's~:~~g' | sort

.PHONY: up down stop id remove help
`
