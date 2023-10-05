package templates

var DockerComposeMSSQL = `version: "3.9"

services:
  mssql-{{.ServiceName}}:
    image: mcr.microsoft.com/mssql/server:{{if .Version}}{{.Version}}{{else}}2022{{end}}-latest
    container_name: mssql-{{.ServiceName}}
    environment:
      SA_PASSWORD: "{{.Password}}"
      ACCEPT_EULA: "Y"
      MSSQL_SA_PASSWORD: "{{.Password}}"
      MSSQL_PID: "Express"
    ports:
      - "{{.Port}}:1433"
    networks:
      - corgi-network
    volumes:
      - mssql-data:/var/opt/mssql
      - ./bootstrap:/var/opt/mssql-tools/startup
      - .:/var/opt/mssql/backup
    cap_add:
      - SYS_PTRACE

networks:
  corgi-network:
    driver: bridge

volumes:
  mssql-data:
`

var MakefileMSSQL = `up:
	chmod +x bootstrap/bootstrap.sh && docker-compose up -d && docker exec mssql-{{.ServiceName}} /var/opt/mssql-tools/startup/bootstrap.sh
down:
	docker-compose down    
stop:
	docker stop mssql-{{.ServiceName}}
id:
	docker ps -aqf "name=mssql-{{.ServiceName}}" | awk '{print $1}'
seed:
	cat dump.bak | docker exec -i $$(docker ps -aqf "name=mssql-{{.ServiceName}}") /opt/mssql-tools/bin/sqlcmd -U {{.User}} -P {{.Password}} -Q "RESTORE DATABASE [{{.DatabaseName}}] FROM DISK = '/var/opt/mssql/backup/dump.bak' WITH REPLACE"
getSelfDump:
	docker exec -i $$(docker ps -aqf "name=mssql-{{.ServiceName}}") /opt/mssql-tools/bin/sqlcmd -U {{.User}} -P {{.Password}} -Q "BACKUP DATABASE [{{.DatabaseName}}] TO DISK = '/var/opt/mssql/backup/dump.bak'"
remove:
	docker rm mssql-{{.ServiceName}}
help:
	make -qpRr | egrep -e '^[a-z].*:$$' | sed -e 's~:~~g' | sort

.PHONY: up down stop id seed getSelfDump remove help
`

var BootstrapMSSQL = `#!/bin/bash

set -euo pipefail

echo "waiting for mssql to be ready"
for i in {1..90}; do
  if /opt/mssql-tools/bin/sqlcmd -S localhost -U sa -P "{{.Password}}" -Q "SELECT 1" > /dev/null 2>&1; then
    echo "mssql is ready"
    break
  fi
  echo "waiting for mssql..."
  sleep 1
done

echo "configuring mssql"
echo "==================="

# Creating the specified database.
/opt/mssql-tools/bin/sqlcmd -S localhost -U sa -P "{{.Password}}" -Q "CREATE DATABASE [{{.DatabaseName}}]"

# Creating the user.
/opt/mssql-tools/bin/sqlcmd -S localhost -U sa -P "{{.Password}}" -Q "CREATE LOGIN {{.User}} WITH PASSWORD = '{{.Password}}'; USE [{{.DatabaseName}}]; CREATE USER {{.User}} FOR LOGIN {{.User}}"

# Add user sysadmin permissions
/opt/mssql-tools/bin/sqlcmd -S localhost -U sa -P "{{.Password}}" -Q "ALTER SERVER ROLE sysadmin ADD MEMBER {{.User}}"

# Add user to dbcreator server role (not needed, because of sysadmin, but i will leave it here just in case)
/opt/mssql-tools/bin/sqlcmd -S localhost -U sa -P "{{.Password}}" -Q "ALTER SERVER ROLE dbcreator ADD MEMBER {{.User}}"

# Granting permissions to the user for the specific database (not needed, because of sysadmin, but i will leave it here just in case)
/opt/mssql-tools/bin/sqlcmd -S localhost -U sa -P "{{.Password}}" -Q "USE [{{.DatabaseName}}]; EXEC sp_addrolemember 'db_owner', '{{.User}}'"
`
