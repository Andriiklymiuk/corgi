package templates

var DockerComposeElasticsearch = `services:
  elasticsearch-{{.ServiceName}}:
    image: docker.elastic.co/elasticsearch/elasticsearch:{{if .Version}}{{.Version}}{{else}}8.10.2{{end}}
    container_name: elasticsearch-{{.ServiceName}}
    environment:
      - "discovery.type=single-node"
      - "ES_JAVA_OPTS=-Xms512m -Xmx512m"
      - "ELASTIC_PASSWORD={{.Password}}"
      - "xpack.security.enabled=false"
    ports:
      - "{{.Port}}:9200"
    volumes:
      - ./bootstrap:/usr/local/bootstrap
      - esdata:/usr/share/elasticsearch/data
    networks:
      - corgi-network

  kibana:
    image: docker.elastic.co/kibana/kibana:{{if .Version}}{{.Version}}{{else}}8.10.2{{end}}
    container_name: kibana-{{.ServiceName}}
    depends_on:
      - elasticsearch-{{.ServiceName}}
    environment:
      - "ELASTICSEARCH_HOSTS=http://elasticsearch-{{.ServiceName}}:9200"
      - "ELASTICSEARCH_USERNAME={{.User}}"
      - "ELASTICSEARCH_PASSWORD={{.Password}}"
    ports:
      - "5601:5601"
    networks:
      - corgi-network

volumes:
  esdata:

networks:
  corgi-network:
    driver: bridge
`

var MakefileElasticsearch = `up:
	chmod +x bootstrap/bootstrap.sh && docker compose up -d && docker exec elasticsearch-{{.ServiceName}} /usr/local/bootstrap/bootstrap.sh
down:
	docker compose down --volumes
stop:
	docker stop elasticsearch-{{.ServiceName}}
	docker stop kibana-{{.ServiceName}}
id:
	docker ps -aqf "name=elasticsearch-{{.ServiceName}}" | awk '{print $1}'
logs:
	docker logs elasticsearch-{{.ServiceName}}
	docker logs kibana-{{.ServiceName}}
remove:
	docker rm --volumes elasticsearch-{{.ServiceName}}
	docker rm --volumes kibana-{{.ServiceName}}
	docker volume rm {{.ServiceName}}_esdata
help:
	make -qpRr | egrep -e '^[a-z].*:$$' | sed -e 's~:~~g' | sort

.PHONY: up down stop id logs remove help
`

var BootstrapElasticsearch = `#!/bin/bash

set -euo pipefail

echo "waiting for elasticsearch to be ready"
for i in {1..90}; do
  if curl -s -u elastic:{{.Password}} "http://elasticsearch-{{.ServiceName}}:9200/" | grep -q "number" ; then
    echo "elasticsearch is ready"
    break
  fi
  echo "waiting for elasticsearch..."
  sleep 1
done

echo "configuring elasticsearch"
echo "=========================="

# Check if user exists
user_exists=$(curl -s -u elastic:{{.Password}} -o /dev/null -w "%{http_code}" "http://elasticsearch-{{.ServiceName}}:9200/_security/user/{{.User}}")

# If the user doesn't exist, create the user with superuser and kibana_admin roles.
if [ "$user_exists" == "404" ]; then
    # TODO: This won't work, because xpack.security.enabled=false , but if i change to true, than kibana doesn't throws security exception (not enough privileges on indexes, though i have superuser role)
    echo "Creating user {{.User}}"
    curl -s -X POST -u elastic:{{.Password}} "http://elasticsearch-{{.ServiceName}}:9200/_security/user/{{.User}}" -H 'Content-Type: application/json' -d'
    {
      "password": "{{.Password}}",
      "roles": ["superuser", "kibana_admin"],
      "full_name": "Admin User",
      "email": "{{.User}}@example.com"
    }'
else
    echo "User {{.User}} already exists"
fi

# Check if the index exists.
index_exists=$(curl -s -u {{.User}}:{{.Password}} -o /dev/null -w "%{http_code}" "http://elasticsearch-{{.ServiceName}}:9200/{{.DatabaseName}}")

# If the index doesn't exist (404 Not Found), then create the index.
if [ "$index_exists" == "404" ]; then
    echo "Creating the index {{.DatabaseName}}"
    curl -s -X PUT -u elastic:{{.Password}} "http://elasticsearch-{{.ServiceName}}:9200/{{.DatabaseName}}" -H 'Content-Type: application/json' -d'
    {
      "settings" : {
        "number_of_shards" : 1,
        "number_of_replicas" : 0
      }
    }'
else
    echo "Index {{.DatabaseName}} already exists"
fi
`
