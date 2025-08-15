package templates

var DockerComposeRabbitMQ = `services:
  rabbitmq-{{.ServiceName}}:
    image: rabbitmq:{{if .Version}}{{.Version}}-{{end}}management
    container_name: rabbitmq-{{.ServiceName}}
    environment:
      - RABBITMQ_DEFAULT_USER={{.User}}
      - RABBITMQ_DEFAULT_PASS={{.Password}}{{if and .Additional .Additional.DefinitionPath}}
      - RABBITMQ_SERVER_ADDITIONAL_ERL_ARGS=-rabbitmq_management load_definitions "/etc/rabbitmq/definitions.json"{{end}}
    volumes:
      - /var/lib/rabbitmq{{if and .Additional .Additional.DefinitionPath}}
      - {{.Additional.DefinitionPath}}:/etc/rabbitmq/definitions.json{{end}}
    ports:
      - "{{.Port}}:5672"
      - "{{if .Port2}}{{.Port2}}{{else}}15672{{end}}:15672"
    networks:
      - corgi-network

networks:
  corgi-network:
    driver: bridge
`

var MakefileRabbitMQ = `up:
	docker compose up -d
down:
	docker compose down --volumes
stop:
	docker stop rabbitmq-{{.ServiceName}}
id:
	docker ps -aqf "name=rabbitmq-{{.ServiceName}}" | awk '{print $1}'
remove:
	docker rm --volumes rabbitmq-{{.ServiceName}}
logs:
	docker logs rabbitmq-{{.ServiceName}}
help:
	make -qpRr | egrep -e '^[a-z].*:$$' | sed -e 's~:~~g' | sort

.PHONY: up down stop id remove logs help
`
