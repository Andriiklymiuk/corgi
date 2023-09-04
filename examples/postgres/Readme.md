This example contains:
- postgres database
- go server
- react native app
- sql dump file to populate postgres database

## How to start it?

1. Download:
- postgres-seeded-go-reactnative.corgi-compose.yml
- users_dump.sql.

2. Run
```bash
# init repos
corgi init
# seed local postgres db, if you don't want to seed it, just run without --seed
corgi run --seed
```

You can also do it from corgi vscode extension.
1. Choose `run from workspace root` -> Initialize repos
2. Choose `Databases` -> seed all db
3, Corgi run


This will startup ios app and go server.
You need xcode to be installed and other stuff mentioned in required section of postgres-seeded-go-reactnative.corgi-compose.yml file

