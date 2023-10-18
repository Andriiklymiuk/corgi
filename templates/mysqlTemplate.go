package templates

var DockerComposeMySQL = `version: "3.9"

services:
  mysql-{{.ServiceName}}:
    image: mysql:{{if .Version}}{{.Version}}{{else}}latest{{end}}
    container_name: mysql-{{.ServiceName}}
    environment:
      - MYSQL_ROOT_PASSWORD={{.Password}}
      - MYSQL_DATABASE={{.DatabaseName}}
      - MYSQL_USER={{.User}}
      - MYSQL_PASSWORD={{.Password}}
    ports:
      - "{{.Port}}:3306"
    networks:
      - corgi-network

networks:
  corgi-network:
    driver: bridge
`

var MakefileMySQL = `up:
	docker-compose up -d
down:
	docker-compose down
stop:
	docker stop mysql-{{.ServiceName}}
id:
	docker ps -aqf "name=mysql-{{.ServiceName}}" | awk '{print $1}'
seed:
	cat dump.sql | docker exec -i $(c) mysql -u{{.User}} -p{{.Password}} {{.DatabaseName}}
{{if .SeedFromDb.Host}}getDump:
	mysqldump --host={{.SeedFromDb.Host}} --port={{.SeedFromDb.Port}} --user={{.SeedFromDb.User}} --password=$(p) {{.SeedFromDb.DatabaseName}} > dump.sql
{{end}}getSelfDump:
	mysqldump --host={{.Host}} --port={{.Port}} --user={{.User}} --password=$(p) {{.DatabaseName}} > dump.sql
remove:
	docker rm mysql-{{.ServiceName}}
help:
	make -qpRr | egrep -e '^[a-z].*:$$' | sed -e 's~:~~g' | sort

.PHONY: up down stop id seed {{if .SeedFromDb.Host}}getDump {{end}}getSelfDump remove help
`
