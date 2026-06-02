package templates

var DockerComposeMailpit = `services:
  mailpit-{{.ServiceName}}:
    image: axllent/mailpit:{{if .Version}}{{.Version}}{{else}}latest{{end}}
    container_name: mailpit-{{.ServiceName}}
    ports:
      - "{{.Port}}:1025"
      - "{{if .Port2}}{{.Port2}}{{else}}8025{{end}}:8025"
    networks:
      - corgi-network

networks:
  corgi-network:
    driver: bridge
`

var MakefileMailpit = `up:
	docker compose up -d
down:
	docker compose down --volumes
stop:
	docker stop mailpit-{{.ServiceName}}
id:
	docker ps -aqf "name=mailpit-{{.ServiceName}}" | awk '{print $1}'
remove:
	docker rm --volumes mailpit-{{.ServiceName}}
logs:
	docker logs mailpit-{{.ServiceName}}
help:
	make -qpRr | egrep -e '^[a-z].*:$$' | sed -e 's~:~~g' | sort

.PHONY: up down stop id remove logs help
`
