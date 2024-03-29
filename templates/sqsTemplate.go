package templates

var SqsRegion = "eu-central-1"

var DockerComposeSqs = `version: "3.9"
      
services:
  sqs-{{.ServiceName}}:
    image: localstack/localstack:{{if .Version}}{{.Version}}{{else}}latest{{end}}
    container_name: sqs-{{.ServiceName}}
    hostname: sqs
    environment:
      - SERVICES=sqs
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

var MakefileSqs = `up:
	chmod +x bootstrap/bootstrap.sh && docker compose up -d
down:
	docker compose down --volumes
stop:
	docker stop sqs-{{.ServiceName}}
id:
	docker ps -aqf "name=sqs-{{.ServiceName}}" | awk '{print $1}'
remove:
	docker rm --volumes sqs-{{.ServiceName}}
logs:
	docker logs sqs-{{.ServiceName}}
help:
	make -qpRr | egrep -e '^[a-z].*:$$' | sed -e 's~:~~g' | sort

.PHONY: up down stop id remove logs help
`

var BootstrapSqs = `#!/usr/bin/env bash

set -euo pipefail

echo "configuring sqs"
echo "==================="

awslocal \
	--endpoint-url=http://localhost:4566 \
	sqs create-queue \
	--queue-name {{.DatabaseName}} \
  --region eu-central-1 \
	--attributes VisibilityTimeout=30
`
