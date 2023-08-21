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

## VSCODE users

You can install [corgi extension](https://marketplace.visualstudio.com/items?itemName=corgi.corgi) to get syntax highlights and much more


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
- [Examples](./examples/)
- [Corgi compose items](./resources/readme/corgi_compose_items.md)

</br>

## Quick install with [Homebrew](https://brew.sh)

```bash
brew install andriiklymiuk/homebrew-tools/corgi

# ask for help to check if it works
corgi -h
```

It will install it globally.

With it you can run `corgi` in any folder on your local.

[Create service file](#services-creation), if you want to run corgi.

</br>

## Prerequisites
- [Docker](https://www.docker.com) - for running databases

- [psql](https://formulae.brew.sh/formula/libpq) - to use auto seeding for postgresql databases


## Services creation

Corgi has several concepts to understand:

- db_services - database configs to use when doing creation/seeding/etc
- services - project folders to use for corgi. Can be server, app, anything you can imagine
- required - programs needed for running your project successfully (node,yarn,go,whatever you want). They are checked on init

These items are added to corgi-compose.yml file to create services, db services and check for required software.

Examples of corgi-compose.yml files are in [examples folder](./examples/). You can also check what should be in corgi-compose.yml by running ```corgi docs```. It will print out all possible items in corgi .yml file or you can go to [corgi compose items doc](./resources/readme/corgi_compose_items.md).

After creating corgi-compose.yml file, you can run to create db folders, clone git repos, etc.
```bash 
  corgi init
```
If you want to just run services and already created db_services:
```bash 
  corgi run
```

***Tip***: there can be as many services as you wish. 
But create it with different ports to be able to run in all at the same time, if you want.

You can read of what exactly happens on [run](./resources/readme/why_it_exists.md#what-happens-on-run) or on [init](./resources/readme/why_it_exists.md#what-happens-on-run) to better understand corgi logic.


</br>

Credits:

- <a href="https://www.freepik.com/free-vector/cute-corgi-dog-astronaut-floating-space-cartoon-vector-icon-illustration-animal-science-icon-concept-isolated-premium-vector-flat-cartoon-style_22271104.htm#query=corgi%20icon&position=7&from_view=keyword">Corgi image by catalyststuff</a> on Freepik
