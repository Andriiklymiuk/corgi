package templates

var DockerComposeKeyDB = `version: "3.9"
      
services:
  keydb-{{.ServiceName}}:
    image: eqalpha/keydb:{{if .Version}}{{.Version}}{{else}}latest{{end}}
    ports:
      - '{{.Port}}:6379'
    container_name: keydb-{{.ServiceName}}
    volumes:
      - ./data:/data
      - ./keydb.conf:/etc/keydb/keydb.conf
      - ./users.acl:/etc/keydb/users.acl
    command:
      [
        'keydb-server',
        '/etc/keydb/keydb.conf',
      ]
    networks:
      - corgi-network

networks:
  corgi-network:
    driver: bridge
`

var MakefileKeyDB = `up:
	docker compose up -d
down:
	docker compose down
stop:
	docker stop keydb-{{.ServiceName}}
id:
	docker ps -aqf "name=keydb-{{.ServiceName}}" | awk '{print $1}'
seed:
	echo "Copying dump.rdb into local Docker container..."
	docker cp ./dump.rdb keydb-{{.ServiceName}}:/data/
	echo "Restarting KeyDB service in Docker container..."
	docker restart keydb-{{.ServiceName}}
getDump:
	echo "Creating KeyDB dump..."
	docker exec keydb-{{.ServiceName}} keydb-cli SAVE
	echo "Copying dump.rdb to current directory..."
	docker cp keydb-{{.ServiceName}}:/data/dump.rdb ./dump.rdb
remove:
	docker rm keydb-{{.ServiceName}}
logs:
	docker logs keydb-{{.ServiceName}}
help:
	make -qpRr | egrep -e '^[a-z].*:$$' | sed -e 's~:~~g' | sort

.PHONY: up down stop id remove getDump seed logs help
`

var KeyDBConfiguration = `aclfile /etc/keydb/users.acl`

var KeyDBAccessControlList = `user default off -@all
user {{.User}} on +@all +@pubsub ~* &* >{{.Password}}`
