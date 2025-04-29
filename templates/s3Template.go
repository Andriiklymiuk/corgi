package templates

var S3Region = "eu-central-1"

var DockerComposeS3 = `services:
  s3-{{.ServiceName}}:
    image: localstack/localstack:{{if .Version}}{{.Version}}{{else}}latest{{end}}
    container_name: s3-{{.ServiceName}}
    hostname: s3
    environment:
      - SERVICES=s3
    ports:
      - '{{.Port}}:4566'
    volumes:
      - ./bootstrap:/etc/localstack/init/ready.d/
    networks:
      - corgi-network

networks:
  corgi-network:
    driver: bridge
`

var MakefileS3 = `up:
	chmod +x bootstrap/bootstrap.sh && docker compose up -d
down:
	docker compose down --volumes
stop:
	docker stop s3-{{.ServiceName}}
id:
	docker ps -aqf "name=s3-{{.ServiceName}}" | awk '{print $1}'
remove:
	docker rm --volumes s3-{{.ServiceName}}
logs:
	docker logs s3-{{.ServiceName}}
help:
	make -qpRr | egrep -e '^[a-z].*:$$' | sed -e 's~:~~g' | sort
.PHONY: up down stop id remove logs help
`

var BootstrapS3 = `#!/usr/bin/env bash
set -euo pipefail
echo "configuring s3"
echo "==================="
awslocal \
  --endpoint-url=http://localhost:4566 \
  s3 mb \
  s3://{{.DatabaseName}} \
  --region eu-central-1
`
