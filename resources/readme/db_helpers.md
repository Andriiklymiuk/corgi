## How to run db helpers

These are helpers command for your db_services.

```bash 
  # this will show you help message
  corgi

  # before running db commands you need to create corgi-compose.yml file, add services config there and run corgi init, so that there is db_services folder, that is created

  # example to run db service
  corgi db

  # example to show help commands
  corgi db -h
  corgi -h
```


You can run cli with flags, without specifying service, to do smth with all databases, for example:

```bash 

  # run db service and start all databases
  corgi db -u
  # similar to
  corgi db -upAll

  # stop, remove and start all databases 
  corgi db  -r -s -u
  # similar to
  corgi db  -rsu
  # similar to
  corgi db  -removeAll -stopAll -upAll
```

You can run each service individually, e.g. `corgi db`. It will show you interactive menu to choose one of the service databases, that are located in `corgi_services/db_services` folder.

```bash 
Use the arrow keys to navigate: ↓ ↑ → ← 
? Select service: 
  ▸ 🛑  close program
    analytics
    backend
    backoffice
```

This menu helps to choose target service and its commands, that are located in Makefile of targeted service (we choose backoffice service for example)

```bash 
Connection info to backoffice:

PORT 5432
USER corgi
PASSWORD corgiPassword
DB corgi-adm

backoffice ist running 🔴
Use the arrow keys to navigate: ↓ ↑ → ← 
? Select command: 
  ▸ ⬅️  go back
    down
    help
    id
    listDocker
    seed
↓   up
```

</br>

## Database seeding

If you want to do seeding to do database seeding (population with data), you need to:

0. [Create database dump](./resources/readme/database_dump.md), name it `dump.sql` and place it in targeted service, e.g. place it in `corgi_services/db_services/backoffice` folder
1. Run `corgi db` from root folder
2. Choose service
3. Choose seed. It will populate db.

**Important**: seeding is best to do on newly created db.

[Main docs](../../Readme.md)