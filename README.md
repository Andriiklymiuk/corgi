<div align="center">
  <img width="300" height="300" src="./resources/corgi.png">
  
  # 🐶 CORGI 🐶
  [![Reliability Rating](https://sonarcloud.io/api/project_badges/measure?project=Andriiklymiuk_corgi&metric=reliability_rating)](https://sonarcloud.io/summary/new_code?id=Andriiklymiuk_corgi)
  [![Bugs](https://sonarcloud.io/api/project_badges/measure?project=Andriiklymiuk_corgi&metric=bugs)](https://sonarcloud.io/summary/new_code?id=Andriiklymiuk_corgi)
  [![Code Smells](https://sonarcloud.io/api/project_badges/measure?project=Andriiklymiuk_corgi&metric=code_smells)](https://sonarcloud.io/summary/new_code?id=Andriiklymiuk_corgi)

  [![Security Rating](https://sonarcloud.io/api/project_badges/measure?project=Andriiklymiuk_corgi&metric=security_rating)](https://sonarcloud.io/summary/new_code?id=Andriiklymiuk_corgi)
  [![Vulnerabilities](https://sonarcloud.io/api/project_badges/measure?project=Andriiklymiuk_corgi&metric=vulnerabilities)](https://sonarcloud.io/summary/new_code?id=Andriiklymiuk_corgi)

  [![Maintainability Rating](https://sonarcloud.io/api/project_badges/measure?project=Andriiklymiuk_corgi&metric=sqale_rating)](https://sonarcloud.io/summary/new_code?id=Andriiklymiuk_corgi)
  [![Lines of Code](https://sonarcloud.io/api/project_badges/measure?project=Andriiklymiuk_corgi&metric=ncloc)](https://sonarcloud.io/summary/new_code?id=Andriiklymiuk_corgi)
  [![Technical Debt](https://sonarcloud.io/api/project_badges/measure?project=Andriiklymiuk_corgi&metric=sqale_index)](https://sonarcloud.io/summary/new_code?id=Andriiklymiuk_corgi)

  [![Quality Gate Status](https://sonarcloud.io/api/project_badges/measure?project=Andriiklymiuk_corgi&metric=alert_status)](https://sonarcloud.io/summary/new_code?id=Andriiklymiuk_corgi)
</div>

Send someone your project yml file, init and run it in minutes.

No more long meetings, explanations of how to run new project with multiple microservices and configs. Just send corgi-compose.yml file to your team and corgi will do the rest.

Auto git cloning, db seeding, concurrent running and much more.

While in services you can create whatever you want, but in db services **for now it supports**: 
- postgres, [example](https://github.com/Andriiklymiuk/corgi_examples/tree/main/postgres)
- mongodb, [example](https://github.com/Andriiklymiuk/corgi_examples/blob/main/mongodb/mongodb-go.corgi-compose.yml)
- rabbitmq, [example](https://github.com/Andriiklymiuk/corgi_examples/blob/main/rabbitmq/rabbitmq-go-nestjs.corgi-compose.yml)
- aws sqs, [example](https://github.com/Andriiklymiuk/corgi_examples/blob/main/aws_sqs/aws_sqs_postgres_go_deno.corgi-compose.yml)
- redis, [example](https://github.com/Andriiklymiuk/corgi_examples/blob/main/redis/redis-bun-expo.corgi-compose.yml)
- redis-server
- mysql
- [mariadb](https://mariadb.org)
- [dynamodb](https://aws.amazon.com/dynamodb/)
- [kafka](https://kafka.apache.org)
- [mssql](https://www.microsoft.com/en-us/sql-server/sql-server-downloads)
- [cassandra](https://cassandra.apache.org/_/index.html)
- [cockroachDb](https://www.cockroachlabs.com)
- [clickhouse](https://clickhouse.com)
- [scylla](https://www.scylladb.com)
- [keydb](https://docs.keydb.dev)
- [influxdb](https://www.influxdata.com)
- [surrealdb](https://surrealdb.com)
- [neo4j](https://neo4j.com)
- [arangodb](https://arangodb.com)
- [elasticsearch](https://www.elastic.co/elasticsearch#)
- [timescaledb](https://www.timescale.com)
- [couchdb](https://couchdb.apache.org)
- [dgraph](https://dgraph.io)
- [meilisearch](https://www.meilisearch.com)
- [faunadb](https://fauna.com)
- [yugabytedb](https://www.yugabyte.com)
- [skytable](https://skytable.io)
- [dragonfly](https://www.dragonflydb.io)
- [redict](https://redict.io)


## Documentation

You can check documentation on https://andriiklymiuk.github.io/corgi/

## VSCODE users

You can install [corgi extension](https://marketplace.visualstudio.com/items?itemName=corgi.corgi) to get syntax highlights and much more


## Quick install with [Homebrew](https://brew.sh)

```bash
brew install andriiklymiuk/homebrew-tools/corgi

# ask for help to check if it works
corgi -h
```

It will install it globally.

With it you can run `corgi` in any folder on your local.



Credits:

- <a href="https://www.freepik.com/free-vector/cute-corgi-dog-astronaut-floating-space-cartoon-vector-icon-illustration-animal-science-icon-concept-isolated-premium-vector-flat-cartoon-style_22271104.htm#query=corgi%20icon&position=7&from_view=keyword">Corgi image by catalyststuff</a> on Freepik
