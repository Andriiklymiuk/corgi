package templates

// LocalstackRegion is the default AWS region for the localstack driver.
var LocalstackRegion = "eu-central-1"

// Image default pinned to 3.8 — last tag that runs without a LOCALSTACK_AUTH_TOKEN
// (Hobby plan or higher required for newer tags since March 2026).

var DockerComposeLocalstack = `services:
  localstack-{{.ServiceName}}:
    image: localstack/localstack:{{if .Version}}{{.Version}}{{else}}3.8{{end}}
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
  --attributes VisibilityTimeout=30 || true
{{end}}

{{range .Buckets}}
echo "  create S3 bucket: {{.}}"
awslocal \
  --endpoint-url=http://localhost:4566 \
  s3 mb \
  s3://{{.}} \
  --region eu-central-1 || true
{{end}}

{{range .Topics}}
echo "  create SNS topic: {{.}}"
awslocal \
  --endpoint-url=http://localhost:4566 \
  sns create-topic \
  --name {{.}} \
  --region eu-central-1 || true
{{end}}

{{range .Subscriptions}}
echo "  subscribe SQS {{.Queue}} to SNS {{.Topic}}"
awslocal \
  --endpoint-url=http://localhost:4566 \
  sns subscribe \
  --topic-arn arn:aws:sns:eu-central-1:000000000000:{{.Topic}} \
  --protocol sqs \
  --notification-endpoint arn:aws:sqs:eu-central-1:000000000000:{{.Queue}} \
  --region eu-central-1 || true
{{end}}

{{range .Secrets}}
echo "  create Secrets Manager secret: {{.Name}}"
awslocal \
  --endpoint-url=http://localhost:4566 \
  secretsmanager create-secret \
  --name "{{.Name}}" \
  --secret-string "{{.Value}}" \
  --region eu-central-1 || true
{{end}}

{{range .Parameters}}
echo "  put SSM parameter: {{.Name}}"
awslocal \
  --endpoint-url=http://localhost:4566 \
  ssm put-parameter \
  --name "{{.Name}}" \
  --value "{{.Value}}" \
  --type "{{if .Type}}{{.Type}}{{else}}String{{end}}" \
  --overwrite \
  --region eu-central-1 || true
{{end}}

{{range .Streams}}
echo "  create Kinesis stream: {{.}}"
awslocal \
  --endpoint-url=http://localhost:4566 \
  kinesis create-stream \
  --stream-name {{.}} \
  --shard-count 1 \
  --region eu-central-1 || true
{{end}}
`
