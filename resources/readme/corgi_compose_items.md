## Corgi compose items

Examples of these items usage can be found in [examples folder](../../examples/).

You can add service in services part of yml file.

Corgi compose `service` can contain the following items (properties):

| Item        | Example           | itemType  |  Description
| ------------| :-------------    | -         | --
| cloneFrom             | `git@github.com:Andriiklymiuk/corgi.git` | `string` | Git url to target repo. By default nothing is cloned.
| branch                | `some/feature/branch` | `string` | Branch to use for git checkout. By default default branch for repo is used.
| environment           | - YOUR_ENV=dev<br>- YOUR__ANTOHER_ENV=abcdef  | `[]string` | List of environment variables to copy and put into your env file.<br>By default no environments are added.
| envPath               | ./path/to/.env | `string` | Path to .env file in target repo. By default .env file is used
| ignoreEnv             | false | `string` | Should service ignore env and don't change env file or not. By default is false (env is not ignored)
| path                  | ./path/to/target/repo | `string` | Path to the actual project repo.<br>By default the path to the folder in which corgi-compose.yml is used
| copyEnvFromFilePath   | ./path/to/.env-file-to-copy-from  | `string` | The path to the .env, which content will be copied to service repo .env file
| port                  | 5432 | `number` | Service port, that will be added to .env file.
| manualRun             | true | `boolean` | Determines if the service will be run with run cmd.<br>If it is true, that to run you add `--services manual_to_run_service` to run cmd.<br>By default it is false.
| depends_on_db         | - name: db_name_from_db_services<br>- envAlias: NAME_BEFORE_DB_IN_ENV | `[]DependsOnDb` | Adds db credentials (`DB_HOST`,etc) from db_services will be copied to .env.<br>envAlias adds string before db credentials, like NAME_BEFORE_DB_IN_ENV_DB_HOST
| depends_on_services   | - name: service_name<br>- envAlias: NAME_TO_USE_IN_ENV<br>- suffix: /special/suffix | `[]DependsOnService` | Adds service credentials to .env.<br>suffix is added at the end of added value<br>NAME_TO_USE_IN_ENV=localhost:port/special/suffix will be added to .env<br>If you add just name, than it is SERVICE_NAME=localhost:port_in_env
| beforeStart           | - install dependencies<br>- do some builds | `[]string` | List of commands to run consequently, before start commands are run.
| start                 | - run your service<br>- run some other stuff | `[]string` | List of commands to run in parallel for the service needs.
| afterStart            | - do some needed cleanups | `[]string` | List of commands to run consequently, when the cli is exited.
 
 <br>
 
Also, you can add service in db_services part.
Corgi compose `db_service` can contain the following items (properties):

| Item        | Example           | itemType  |  Description
| ------------| :-------------    | -         | --
| driver                | postgres | `string` | This is database driver for this service. By default postgres is used. For now it supports: postgres, mongodb, mysql, rabbitmq, sqs and redis
| host                  | localhost | `string` | This is database host for this service, that will be used in `DB_HOST`. By default localhost is used
| databaseName          | corgi-database | `string` | This is database name for this service, that will be used in `DB_NAME`
| user                  | corgi | `string` | This is database user for this service, that will be used in `DB_USER`
| password              | corgiSecurePassword | `string` | This is database password for this service, that will be used in `DB_PASSWORD`
| port                  | 5432 | `number` | This is database port for this service, that will be used in `DB_PORT`
| seedFromFilePath      | ./path/to/dump.sql | `string` | Path to dump.sql file from which data is seeded.<br>Use either seedFromFilePath or seedFromDb/seedFromDbEnvPath
| seedFromDbEnvPath     | ./path/to/db/info/.env | `string` | Path to .env file with db credentials for db, from which data is seeded.<br>Use either seedFromFilePath or seedFromDb/seedFromDbEnvPath
| seedFromDb            | - host: seed_db_host<br>- databaseName: seed_db_name<br>- user: seed_db_user<br>- password: seed_db_password<br>- port: seed_db_port | `SeedFromDb` | Db credentials to seed from.<br>Use either seedFromFilePath or seedFromDb/seedFromDbEnvPath

Also, you can add required items in required part.
Corgi compose `required` can contain the following items (properties):

| Item        | Example           | itemType  |  Description
| ------------| :-------------    | -         | --
| why                   | - pass butter<br>- help with service X | `[]string` | The reasons to use/install this required command.
| install               | - install cmd 1<br>- install cmd 2 | `[]string` | Installation steps to run, if cmd not found.
| optional              | true | `boolean` | Show or not the prompt, before this cmd installation.<br>By default false.
| checkCmd              | this_command -v | `string` | Command to run to check, if it is installed.<br>By default cmd name is used.

[Main docs](../../README.md)