package templates

var DockerComposeCassandra = `version: "3.9"

services:
  cassandra-{{.ServiceName}}:
    image: cassandra:{{if .Version}}{{.Version}}{{else}}latest{{end}}
    container_name: cassandra-{{.ServiceName}}
    environment:
      - CASSANDRA_USERNAME={{.User}}
      - CASSANDRA_PASSWORD={{.Password}}
      - CASSANDRA_CLUSTER_NAME={{.DatabaseName}}
    ports:
      - "{{.Port}}:9042"
    volumes:
      - ./data:/var/lib/cassandra
    networks:
      - corgi-network

networks:
  corgi-network:
    driver: bridge
`

var MakefileCassandra = `up:
	docker compose up -d
down:
	docker compose down
stop:
	docker stop cassandra-{{.ServiceName}}
id:
	docker ps -aqf "name=cassandra-{{.ServiceName}}" | awk '{print $1}'
seed:
	cat dump.cql | docker exec -i $(id) cqlsh -u {{.User}} -p {{.Password}}
getSelfDump:
	echo "USE {{.DatabaseName}};" > dump.cql && \
	docker exec $(id) cqlsh -u {{.User}} -p {{.Password}} -e "DESCRIBE KEYSPACE {{.DatabaseName}}" >> dump.cql && \
	for table in $$(docker exec $(id) cqlsh -u {{.User}} -p {{.Password}} -e "DESCRIBE TABLES FROM {{.DatabaseName}}"); do \
	    echo "COPY {{.DatabaseName}}.$$table TO STDOUT;" | docker exec -i $(id) cqlsh -u {{.User}} -p {{.Password}} >> dump.cql; \
	done
remove:
	docker rm cassandra-{{.ServiceName}}
help:
	make -qpRr | egrep -e '^[a-z].*:$$' | sed -e 's~:~~g' | sort

.PHONY: up down stop id remove getSelfDump seed help
`
