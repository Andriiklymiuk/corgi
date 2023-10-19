package templates

var DynamoDBRegion = "eu-central-1"

var DockerComposeDynamoDB = `version: "3.9"

services:
  dynamodb-{{.ServiceName}}:
    image: localstack/localstack:{{if .Version}}{{.Version}}{{else}}latest{{end}}
    container_name: dynamodb-{{.ServiceName}}
    environment:
      - SERVICES=dynamodb
    ports:
      - "{{.Port}}:4566"
    volumes:
      - ./bootstrap:/docker-entrypoint-initaws.d
    networks:
      - corgi-network

networks:
  corgi-network:
    driver: bridge
`

var MakefileDynamoDB = `up:
	chmod +x bootstrap/bootstrap.sh && docker compose up -d
down:
	docker compose down --volumes
stop:
	docker stop dynamodb-{{.ServiceName}}
id:
	docker ps -aqf "name=dynamodb-{{.ServiceName}}" | awk '{print $1}'
remove:
	docker rm --volumes dynamodb-{{.ServiceName}}
logs:
	docker logs dynamodb-{{.ServiceName}}
help:
	make -qpRr | egrep -e '^[a-z].*:$$' | sed -e 's~:~~g' | sort

.PHONY: up down stop id remove logs help
`

var BootstrapDynamoDB = `#!/usr/bin/env bash

set -euo pipefail

echo "configuring DynamoDB"
echo "==================="

awslocal dynamodb create-table \
    --table-name {{.DatabaseName}} \
    --attribute-definitions AttributeName=Id,AttributeType=S \
    --key-schema AttributeName=Id,KeyType=HASH \
    --provisioned-throughput ReadCapacityUnits=5,WriteCapacityUnits=5
`
