package templates

var DockerComposeRedis = `version: "3.9"
      
services:
  redis-{{.ServiceName}}:
    image: redis/redis-stack:{{if .Version}}{{.Version}}{{else}}latest{{end}}
    ports:
      - '{{.Port}}:6379'
    container_name: redis-{{.ServiceName}}
    volumes:
      - ./data:/data
      - ./redis.conf:/usr/local/etc/redis/redis.conf
      - ./users.acl:/etc/redis/users.acl
    command:
      [
        'redis-server',
        '/usr/local/etc/redis/redis.conf',
        '--loadmodule',
        '/opt/redis-stack/lib/rejson.so',
        '--loadmodule',
        '/opt/redis-stack/lib/redisearch.so',
      ]
    networks:
      - corgi-network

networks:
  corgi-network:
    driver: bridge
`

var MakefileRedis = `up:
	docker compose up -d
down:
	docker compose down --volumes
stop:
	docker stop redis-{{.ServiceName}}
id:
	docker ps -aqf "name=redis-{{.ServiceName}}" | awk '{print $1}'
seed:
  @echo "Copying dump.rdb into local Docker container..."
  docker cp ./dump.rdb redis-{{.ServiceName}}:/data/
  @echo "Restarting Redis service in Docker container..."
  docker restart redis-{{.ServiceName}}
getDump:
  @echo "Creating Redis dump..."
  docker exec redis-{{.ServiceName}} redis-cli SAVE
  @echo "Copying dump.rdb to current directory..."
  docker cp redis-{{.ServiceName}}:/data/dump.rdb ./dump.rdb
remove:
	docker rm --volumes redis-{{.ServiceName}}
logs:
	docker logs redis-{{.ServiceName}}
help:
	make -qpRr | egrep -e '^[a-z].*:$$' | sed -e 's~:~~g' | sort

.PHONY: up down stop id remove getDump seed logs help
`
var RedisConfiguration = `aclfile /etc/redis/users.acl`

var RedisAccessControlList = `user default off -@all
user {{.User}} on +@all +@pubsub ~* &* >{{.Password}}`
