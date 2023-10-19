package templates

var DockerComposeNeo4j = `version: "3.9"

services:
  neo4j-{{.ServiceName}}:
    image: neo4j:{{if .Version}}{{.Version}}{{else}}latest{{end}}
    container_name: neo4j-{{.ServiceName}}
    environment:
      - NEO4J_AUTH=neo4j/{{.Password}}
      - NEO4J_dbms_security_procedures_unrestricted=apoc.*
    ports:
      - "{{.Port}}:7687"
      - "7474:7474"
    volumes:
      - $PWD/plugins:/plugins
      - $PWD/data:/data
      - ./bootstrap:/docker-entrypoint-initdb.d
    networks:
      - corgi-network

networks:
  corgi-network:
    driver: bridge
`

var MakefileNeo4j = `up:
	chmod +x bootstrap/bootstrap.sh && docker compose up -d && docker exec neo4j-{{.ServiceName}} /docker-entrypoint-initdb.d/bootstrap.sh
down:
	docker compose down --volumes    
stop:
	docker stop neo4j-{{.ServiceName}}
id:
	docker ps -aqf "name=neo4j-{{.ServiceName}}" | awk '{print $1}'
seed:
	cat dump.cypher | docker exec -i neo4j-{{.ServiceName}} bin/cypher-shell -u {{.User}} -p {{.Password}} --database={{.DatabaseName}}
getSelfDump:
	docker exec neo4j-{{.ServiceName}} bin/cypher-shell -u {{.User}} -p {{.Password}} --database={{.DatabaseName}} "CALL apoc.export.cypher.all('stdout:', {format: 'cypher-shell', separateFiles: false, cypherFormat: 'create'});" > dump.cypher
remove:
	docker rm --volumes neo4j-{{.ServiceName}}
logs:
	docker logs neo4j-{{.ServiceName}}
help:
	make -qpRr | egrep -e '^[a-z].*:$$' | sed -e 's~:~~g' | sort

.PHONY: up down stop id seed getSelfDump remove logs help
`

var BootstrapNeo4j = `#!/bin/bash

set -euo pipefail

echo "Waiting for Neo4j to be ready"

# Define a function to test if Neo4j is ready.
is_neo4j_ready() {
  if cypher-shell -u neo4j -p {{.Password}} "RETURN 1;" > /dev/null 2>&1; then
    return 0
  else
    echo "Failed to connect. Let's check the error:"
    cypher-shell -u neo4j -p {{.Password}} "RETURN 1;"
    return 1
  fi
}

# Wait for Neo4j to be ready.
for i in {1..90}; do
  if is_neo4j_ready; then
    echo "Neo4j is ready"
    break
  fi
  echo "waiting for Neo4j..."
  sleep 1
done

# Attempt to create a new user and ignore the error if the user already exists.
cypher-shell -u neo4j -p {{.Password}} "CREATE USER {{.User}} SET PASSWORD '{{.Password}}' CHANGE NOT REQUIRED;" || \
(echo "User might already exist. Continuing...")

# Fix format of database name
database_name=$(echo "{{.DatabaseName}}" | tr '-' '_')

# Create a new database (graph), but this is only available in enterprise edition, so commenting it out
# cypher-shell -u neo4j -p {{.Password}} "CREATE DATABASE $database_name;"

# Give the new user access to the new database, also commenting out because we are using community edition:
# cypher-shell -u neo4j -p {{.Password}} "GRANT ALL PRIVILEGES ON DATABASE $database_name TO {{.User}};"

echo "Neo4j initialization completed"
echo "==================="
`
