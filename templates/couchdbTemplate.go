package templates

var DockerComposeCouchDB = `version: "3.9"

services:
  couchdb-{{.ServiceName}}:
    image: couchdb:{{if .Version}}{{.Version}}{{else}}latest{{end}}
    container_name: couchdb-{{.ServiceName}}
    environment:
      - COUCHDB_USER={{.User}}
      - COUCHDB_PASSWORD={{.Password}}
    ports:
      - "{{.Port}}:5984"
    volumes:
      - ./bootstrap:/var/opt/couchdb/startup
    networks:
      - corgi-network

networks:
  corgi-network:
    driver: bridge
`

var MakefileCouchDB = `up:
	chmod +x bootstrap/bootstrap.sh && docker compose up -d && docker exec couchdb-{{.ServiceName}} /var/opt/couchdb/startup/bootstrap.sh
down:
	docker compose down --volumes
stop:
	docker stop couchdb-{{.ServiceName}}
id:
	docker ps -aqf "name=couchdb-{{.ServiceName}}" | awk '{print $1}'
seed:
	cat dump.json | docker exec -i $(c) curl -X POST -H "Content-Type: application/json" -u {{.User}}:{{.Password}} http://localhost:5984/{{.DatabaseName}}/_bulk_docs -d @-
{{if .SeedFromDb.Host}}getDump:
	curl -X GET -H "Content-Type: application/json" -u {{.SeedFromDb.User}}:{{.SeedFromDb.Password}} http://{{.SeedFromDb.Host}}:{{.SeedFromDb.Port}}/{{.SeedFromDb.DatabaseName}}/_all_docs?include_docs=true > dump.json
{{end}}getSelfDump:
	curl -X GET -H "Content-Type: application/json" -u {{.User}}:{{.Password}} http://{{.Host}}:{{.Port}}/{{.DatabaseName}}/_all_docs?include_docs=true > dump.json
remove:
	docker rm --volumes couchdb-{{.ServiceName}}
logs:
	docker logs couchdb-{{.ServiceName}}
help:
	make -qpRr | egrep -e '^[a-z].*:$$' | sed -e 's~:~~g' | sort

.PHONY: up down stop id seed {{if .SeedFromDb.Host}}getDump {{end}}getSelfDump remove logs help
`

var BootstrapCouchDB = `#!/bin/bash

set -euo pipefail

echo "Waiting for CouchDB to be ready"
for i in {1..90}; do
  if curl -s -X GET "http://localhost:5984/" -u {{.User}}:{{.Password}} | grep 'Welcome'; then
    echo "CouchDB is ready"
    break
  fi
  echo "waiting for CouchDB..."
  sleep 1
done

echo "Configuring CouchDB"
echo "==========================="

# Creating the specified database.
curl -X PUT "http://localhost:5984/{{.DatabaseName}}" -u {{.User}}:{{.Password}}
`
