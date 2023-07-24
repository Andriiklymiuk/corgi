package templates

var DockerComposeRedis = `version: "3.9"
      
services:
  redis-{{.ServiceName}}:
    build: .
    restart: always
    ports:
      - '{{.Port}}:6379'
    container_name: redis-{{.ServiceName}}
    networks:
      - corgi-network

networks:
  corgi-network:
    driver: bridge
`

var DockerfileRedis = `FROM redis:latest
COPY redis.conf /usr/local/etc/redis/redis.conf
COPY users.acl /etc/redis/users.acl
CMD [ "redis-server", "/usr/local/etc/redis/redis.conf" ]
`

var MakefileRedis = `up:
	docker compose up -d --build
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
var RedisConfiguration = `requirepass {{.Password}}
aclfile /etc/redis/users.acl
`

var RedisAccessControlList = `user {{.User}} on +@all +@pubsub ~* &* >{{.Password}}`
