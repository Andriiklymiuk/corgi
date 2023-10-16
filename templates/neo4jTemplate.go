package templates

var DockerComposeNeo4j = `version: "3.9"

services:
  neo4j-{{.ServiceName}}:
    image: neo4j:latest
    container_name: neo4j-{{.ServiceName}}
    logging:
      driver: none
    environment:
      - NEO4J_AUTH={{.User}}/{{.Password}}
      - NEO4J_dbms_security_procedures_unrestricted=apoc.*
    ports:
      - "{{.Port}}:7687"
      - "7474:7474"
    volumes:
      - $PWD/plugins:/plugins
      - $PWD/data:/data
    networks:
      - corgi-network

networks:
  corgi-network:
    driver: bridge
`

var MakefileNeo4j = `up:
	docker compose up -d
down:
	docker compose down    
stop:
	docker stop neo4j-{{.ServiceName}}
id:
	docker ps -aqf "name=neo4j-{{.ServiceName}}" | awk '{print $1}'
seed:
	cat dump.cypher | docker exec -i neo4j-{{.ServiceName}} bin/cypher-shell -u {{.User}} -p {{.Password}} --database={{.DatabaseName}}
getSelfDump:
	docker exec neo4j-{{.ServiceName}} bin/cypher-shell -u {{.User}} -p {{.Password}} --database={{.DatabaseName}} "CALL apoc.export.cypher.all('stdout:', {format: 'cypher-shell', separateFiles: false, cypherFormat: 'create'});" > dump.cypher
remove:
	docker rm neo4j-{{.ServiceName}}
help:
	make -qpRr | egrep -e '^[a-z].*:$$' | sed -e 's~:~~g' | sort

.PHONY: up down stop id seed getSelfDump remove help
`
