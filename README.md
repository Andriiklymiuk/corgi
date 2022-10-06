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


## Inside:
- [Quick install with homebrew](#quick-install-with-homebrewhttpsbrewsh-without-repo-cloning)
- [Prerequisites](#prerequisites)
- [Services creation](#services-creation)
- [Db helpers creation](./resources/readme/db_helpers.md)
- [Database seeding](./resources/readme/db_helpers.md#database-seeding)
- [How to run in dev mode](./resources/readme/how_to_develop.md)
- [If you want to run without cli](#without-cli)
- [What is my purpose and why go](./resources/readme/why_it_exists.md)
- [Autogenerated docs about cli](./resources/readme/corgi.md)

</br>

## Quick install with [Homebrew](https://brew.sh)

```bash
brew tap andriiklymiuk/homebrew-tools

brew install corgi

# ask for help to check if it works
corgi -h
```

It will install it globally.

With it you can run `corgi` in any folder on your local.

[Create service file](#services-creation), if you want to run corgi.

</br>

## Prerequisites
If you want to run db_services or your services require docker, then you need
- [Docker](https://www.docker.com)

## Services creation

You need to create corgi-compose.yml file in root of your target repo to create services and db services.
It should be created in the following way:
```yml
db_services:
  corgi:
    databaseName: corgi-database
    user: corgi
    password: corgiSecurePassword
    port: 5432
    seedFromDb:
      host: db_host_for_seed_seed_db
      databaseName: db_name_for_seed_db
      user: db_user_for_seed_db
      password: db_password_for_seed_db
      port: db_port_for_seed_db
  corgiTest:
    databaseName: corgi-database-test
    user: corgi
    password: corgiSecurePasswordTest
    port: 5433

services:
  corgiServer:
    environment:
      - PORT=8965
    depends_on_db:
      - corgi
    beforeStart:
      - install your dependencies or do other stuff
      - that needs to be run before start cmd
    start:
      - start corgiServer
  corgiApp:
    cloneFrom: url_to_use_in_git_clone_if_path_doesn't_exist
    path: /path/for/service
    environment:
      - SOME_ENV=corgi_is_best
      - SOME_ENV2=corgi_is_best_indeed
    depends_on_services:
      - corgiServer
    beforeStart:
      - install your dependencies or do other stuff
      - that needs to be run before start cmd
    start:
      - start corgiApp
    afterStart:
      - do some cleanup staff on service close
```
Then run, which will create db_services.
```bash 
  corgi init
```
Or, if you want to just run services and already created db_services:
```bash 
  corgi run
```

***Tip***: there can be as many services as you wish. 
But create it with different ports to be able to run in all at the same time, if you want.

## Without cli

The beauty of this cli is that it is versatile and can be run without even opening cli, if it broke or smth has happened to it.
All database services are in `corgi_services/db_services` folder, so you can go to interested service folder and just run `make up` to start the database.

It can be done so, because cli is dependent upon on `docker-compose.yml` and MAKEFILE for each service, and it can be run independently.

</br>

Credits:

- <a href="https://www.freepik.com/free-vector/cute-corgi-dog-astronaut-floating-space-cartoon-vector-icon-illustration-animal-science-icon-concept-isolated-premium-vector-flat-cartoon-style_22271104.htm#query=corgi%20icon&position=7&from_view=keyword">Corgi image by catalyststuff</a> on Freepik
- Random quote is from https://api.quotable.io
