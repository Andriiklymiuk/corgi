package templates

var DockerComposeArangoDB = `version: "3.9"

services:
  arangodb-{{.ServiceName}}:
    image: arangodb:{{if .Version}}{{.Version}}{{else}}latest{{end}}
    container_name: arangodb-{{.ServiceName}}
    environment:
      - ARANGO_ROOT_PASSWORD={{.Password}}
    ports:
      - "{{.Port}}:8529"
    networks:
      - corgi-network
    volumes:
      - ./bootstrap:/opt

networks:
  corgi-network:
    driver: bridge
`

var MakefileArangoDB = `up:
	chmod +x bootstrap/bootstrap.sh && docker-compose up -d && docker exec arangodb-{{.ServiceName}} /opt/bootstrap.sh
down:
	docker compose down    
stop:
	docker stop arangodb-{{.ServiceName}}
id:
	docker ps -aqf "name=arangodb-{{.ServiceName}}" | awk '{print $1}'
getSelfDump:
	# TODO, you can use arangosh for it
seed:
	# TODO, you can use arangosh for it
remove:
	docker rm arangodb-{{.ServiceName}}
help:
	make -qpRr | egrep -e '^[a-z].*:$$' | sed -e 's~:~~g' | sort

.PHONY: up down stop id seed getSelfDump remove help
`

// TODO: fix not connecting error
// bootstrap/bootstrap.sh
var BootstrapArangodb = `#!/bin/sh

sleep 10

set -euo pipefail

echo "Configuring ArangoDB"
echo "==================="

retry_command() {
  local max_attempts="$1"
  shift
  local cmd="$@"
  local attempt_num=1

  until $cmd
  do
    if [ $attempt_num -eq $max_attempts ]
    then
      echo "Attempt $attempt_num failed! Exiting..."
      return 1
    fi

    echo "Attempt $attempt_num failed! Trying again in 5 seconds..."
    sleep 5
    attempt_num=$((attempt_num+1))
  done
}

# Test connection to ArangoDB using arangosh with a retry mechanism.
retry_command 5 arangosh --server.endpoint http://localhost:8529 --server.username root --server.password {{.Password}} --javascript.execute-string 'print("Arangodb_connected");'

# Check if the specified database already exists using arangosh.
if ! arangosh --server.endpoint http://localhost:8529 --server.username root --server.password {{.Password}} --javascript.execute-string 'if (!db._databases().includes("{{.DatabaseName}}")) { print("not exists"); } else { print("exists"); }' | grep "not exists" > /dev/null; then
    echo "Database {{.DatabaseName}} already exists. Skipping creation."
else
    # Create the specified database using arangosh with a retry mechanism.
    echo "Creating database {{.DatabaseName}}"
    retry_command 5 arangosh --server.endpoint http://localhost:8529 --server.username root --server.password {{.Password}} --javascript.execute-string 'db._createDatabase("{{.DatabaseName}}");'
fi

# Check if the specified user already exists using arangosh.
echo 'const users = require("@arangodb/users"); if (!users.exists("{{.User}}")) { print("not exists"); } else { print("exists"); }' > /tmp/arangosh_check_user_script.js
if ! arangosh --server.endpoint http://localhost:8529 --server.username root --server.password {{.Password}} --javascript.execute /tmp/arangosh_check_user_script.js | grep "not exists" > /dev/null; then
    echo "User {{.User}} already exists. Skipping creation."
else
    # Create the specified user using arangosh with a retry mechanism.
    echo "Creating user {{.User}}"
    echo "const users = require('@arangodb/users'); users.save('{{.User}}','{{.Password}}');" > /tmp/arangosh_script.js
    retry_command 5 arangosh --server.endpoint http://localhost:8529 --server.username root --server.password "{{.Password}}" --javascript.execute /tmp/arangosh_script.js
    rm /tmp/arangosh_script.js
fi
rm /tmp/arangosh_check_user_script.js

# Grant the specified user access to the specified database using arangosh with a retry mechanism.
echo "Granting user {{.User}} access to database {{.DatabaseName}}"
echo 'require("@arangodb/users").grantDatabase("{{.User}}", "{{.DatabaseName}}", "rw");' > /tmp/arangosh_grant_script.js
retry_command 5 arangosh --server.endpoint http://localhost:8529 --server.username root --server.password "{{.Password}}" --javascript.execute /tmp/arangosh_grant_script.js
rm /tmp/arangosh_grant_script.js
`
