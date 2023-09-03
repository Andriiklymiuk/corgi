package templates

var DockerComposeRedis = `version: "3.9"
      
services:
  redis-{{.ServiceName}}:
    image: redis/redis-stack:latest
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
	docker compose down    
stop:
	docker stop redis-{{.ServiceName}}
id:
	docker ps -aqf "name=redis-{{.ServiceName}}" | awk '{print $1}'
remove:
	docker rm redis-{{.ServiceName}}
help:
	make -qpRr | egrep -e '^[a-z].*:$$' | sed -e 's~:~~g' | sort

.PHONY: up down stop id remove help
`
var RedisConfiguration = `aclfile /etc/redis/users.acl`

var RedisAccessControlList = `user default off -@all
user {{.User}} on +@all +@pubsub ~* &* >{{.Password}}`
