package templates

// LocalstackRegion is the default AWS region for the localstack driver.
var LocalstackRegion = "eu-central-1"

var DockerComposeLocalstack = `services:
  localstack-{{.ServiceName}}:
    image: localstack/localstack:{{if .Version}}{{.Version}}{{else}}latest{{end}}
    container_name: localstack-{{.ServiceName}}
    hostname: localstack
    environment:
      - SERVICES={{if .Services}}{{range $i, $s := .Services}}{{if $i}},{{end}}{{$s}}{{end}}{{else}}sqs,s3{{end}}
      - AWS_DEFAULT_REGION=eu-central-1
      - DEBUG=0
    ports:
      - '{{.Port}}:4566'
    volumes:
      - ./bootstrap:/etc/localstack/init/ready.d/
      - localstack-{{.ServiceName}}-data:/var/lib/localstack
    networks:
      - corgi-network

volumes:
  localstack-{{.ServiceName}}-data:
networks:
  corgi-network:
    driver: bridge
`

var MakefileLocalstack = `up:
	chmod +x bootstrap/bootstrap.sh && docker compose up -d
down:
	docker compose down --volumes
stop:
	docker stop localstack-{{.ServiceName}}
id:
	docker ps -aqf "name=localstack-{{.ServiceName}}" | awk '{print $1}'
remove:
	docker rm --volumes localstack-{{.ServiceName}}
logs:
	docker logs localstack-{{.ServiceName}}
help:
	make -qpRr | egrep -e '^[a-z].*:$$' | sed -e 's~:~~g' | sort
.PHONY: up down stop id remove logs help
`

var BootstrapLocalstack = `#!/usr/bin/env bash
set -euo pipefail

echo "configuring localstack (services={{if .Services}}{{range $i, $s := .Services}}{{if $i}},{{end}}{{$s}}{{end}}{{else}}sqs,s3{{end}})"
echo "==================="

{{range .Queues}}
echo "  create SQS queue: {{.}}"
awslocal \
  --endpoint-url=http://localhost:4566 \
  sqs create-queue \
  --queue-name {{.}} \
  --region eu-central-1 \
  --attributes VisibilityTimeout=30
{{end}}

{{range .Buckets}}
echo "  create S3 bucket: {{.}}"
awslocal \
  --endpoint-url=http://localhost:4566 \
  s3 mb \
  s3://{{.}} \
  --region eu-central-1
{{end}}
`
