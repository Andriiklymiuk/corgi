package templates

var DockerComposeKafka = `version: "3.9"

services:
  zookeeper-{{.ServiceName}}:
    image: confluentinc/cp-zookeeper:latest
    container_name: zookeeper-{{.ServiceName}}
    environment:
      ZOOKEEPER_CLIENT_PORT: 2181
      ZOOKEEPER_TICK_TIME: 2000
    networks:
      - kafka-network

  kafka-{{.ServiceName}}:
    image: confluentinc/cp-kafka:latest
    container_name: kafka-{{.ServiceName}}
    depends_on:
      - zookeeper-{{.ServiceName}}
    environment:
      KAFKA_BROKER_ID: 1
      KAFKA_ZOOKEEPER_CONNECT: zookeeper-{{.ServiceName}}:2181
      KAFKA_ADVERTISED_LISTENERS: SASL_PLAINTEXT://kafka-{{.ServiceName}}:9092
      KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR: 1
      KAFKA_SASL_MECHANISM_INTER_BROKER_PROTOCOL: PLAIN
      KAFKA_SASL_ENABLED_MECHANISMS: PLAIN
      KAFKA_SASL_JAAS_CONFIG: |
        org.apache.kafka.common.security.plain.PlainLoginModule required \
        username="{{.User}}" \
        password="{{.Password}}" \
        user_{{.User}}="{{.Password}}";
    ports:
      - "{{.Port}}:9092"
    networks:
      - kafka-network
    volumes:
      - ./bootstrap:/etc/kafka-init

networks:
  kafka-network:
    driver: bridge
`

var MakefileKafka = `up:
	chmod +x bootstrap/bootstrap.sh && docker-compose up -d && sleep 10 && docker exec -it kafka-{{.ServiceName}} /etc/kafka-init/bootstrap.sh
down:
	docker-compose down    
stop:
	docker stop kafka-{{.ServiceName}} zookeeper-{{.ServiceName}}
logs:
	docker logs kafka-{{.ServiceName}}
remove:
	docker rm kafka-{{.ServiceName}} zookeeper-{{.ServiceName}}
help:
	make -qpRr | egrep -e '^[a-z].*:$$' | sed -e 's~:~~g' | sort

.PHONY: up down stop logs remove help
`

var BootstrapKafka = `#!/usr/bin/env bash

set -euo pipefail

echo "configuring kafka"
echo "==================="

kafka-topics --create --topic {{.DatabaseName}} --bootstrap-server localhost:9092 --partitions 1 --replication-factor 1
`
