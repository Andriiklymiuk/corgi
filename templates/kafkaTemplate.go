package templates

// todo: kafka in this setup has o users, so add one later
var DockerComposeKafka = `services:
  zookeeper-{{.ServiceName}}:
    image: confluentinc/cp-zookeeper:{{if .Version}}{{.Version}}{{else}}latest{{end}}
    container_name: zookeeper-{{.ServiceName}}
    environment:
      ZOOKEEPER_CLIENT_PORT: 2181
    networks:
      - corgi-network

  kafka-{{.ServiceName}}:
    image: confluentinc/cp-kafka:{{if .Version}}{{.Version}}{{else}}latest{{end}}
    container_name: kafka-{{.ServiceName}}
    depends_on:
      - zookeeper-{{.ServiceName}}
    environment:
      KAFKA_BROKER_ID: 1
      KAFKA_ZOOKEEPER_CONNECT: zookeeper-{{.ServiceName}}:2181
      KAFKA_ADVERTISED_LISTENERS: PLAINTEXT://kafka-{{.ServiceName}}:9092
      KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR: 1
    ports:
      - "{{.Port}}:9092"
    networks:
      - corgi-network
    volumes:
      - ./bootstrap:/etc/kafka-init

  kafdrop:
    image: obsidiandynamics/kafdrop
    container_name: kafdrop-{{.ServiceName}}
    depends_on:
      - kafka-{{.ServiceName}}
    environment:
      KAFKA_BROKERCONNECT: kafka-{{.ServiceName}}:9092
      JVM_OPTS: "-Xms32M -Xmx64M"
    ports:
      - "9000:9000"
    networks:
      - corgi-network

networks:
  corgi-network:
    driver: bridge
`

var MakefileKafka = `up:
	chmod +x bootstrap/bootstrap.sh && docker compose up -d && sleep 10 && docker exec kafka-{{.ServiceName}} /etc/kafka-init/bootstrap.sh
down:
	docker compose down --volumes
stop:
	docker stop kafka-{{.ServiceName}} zookeeper-{{.ServiceName}}
logs:
	docker logs kafka-{{.ServiceName}}
remove:
	docker rm --volumes kafka-{{.ServiceName}} zookeeper-{{.ServiceName}}
help:
	make -qpRr | egrep -e '^[a-z].*:$$' | sed -e 's~:~~g' | sort

.PHONY: up down stop logs remove help
`

var BootstrapKafka = `#!/usr/bin/env bash

set -euo pipefail

echo "waiting for kafka to be ready"
for i in {1..90}; do
  if kafka-topics --list --bootstrap-server localhost:9092 > /dev/null 2>&1; then
    echo "kafka is ready"
    break
  fi
  echo "waiting for kafka..."
  sleep 1
done

echo "configuring kafka"
echo "==================="

# Check if topic exists
if ! kafka-topics --list --bootstrap-server localhost:9092 | grep -q "^{{.DatabaseName}}$"; then
    kafka-topics --create --topic {{.DatabaseName}} --bootstrap-server localhost:9092 --partitions 1 --replication-factor 1
else
    echo "Topic '{{.DatabaseName}}' already exists. Skipping creation."
fi`
