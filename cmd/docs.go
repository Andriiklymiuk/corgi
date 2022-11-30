/*
Copyright © 2022 ANDRII KLYMIUK
*/
package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

// docsCmd represents the docs command
var docsCmd = &cobra.Command{
	Use:   "docs",
	Short: "Do stuff with docs",
	Long:  `Helper set of commands to make your life easier with docs and corgi `,
	Run:   runDocs,
}

func init() {
	rootCmd.AddCommand(docsCmd)
	docsCmd.PersistentFlags().BoolP("generate", "g", false, "Generate cobra docs. Useful for development only, because it updates corgi docs.")
}

type CorgiComposeItems struct {
	item        string
	example     string
	itemType    string
	description string
}

var serviceItems = []CorgiComposeItems{
	{
		item:        "cloneFrom",
		example:     "git@github.com:Andriiklymiuk/corgi.git",
		itemType:    "string",
		description: "Git url to target repo. By default nothing is cloned.",
	},
	{
		item:        "environment",
		example:     "- YOUR_ENV=dev\n\t- YOUR__ANTOHER_ENV=abcdef",
		itemType:    "[]string",
		description: "List of environment variables to copy and put into your env file.\n\t\t\tBy default no environments are added.",
	},
	{
		item:        "envPath",
		example:     "./path/to/.env",
		itemType:    "string",
		description: "Path to .env file in target repo. By default .env file is used",
	},
	{
		item:        "ignoreEnv",
		example:     "false",
		itemType:    "boolean",
		description: "Should service ignore env and don't change env file or not. By default is false (env is not ignored)",
	},
	{
		item:        "path",
		example:     "./path/to/target/repo",
		itemType:    "string",
		description: "Path to the actual project repo.\n\t\t\tBy default the path to the folder in which corgi-compose.yml is used",
	},
	{
		item:        "copyEnvFromFilePath",
		example:     "./path/to/.env-file-to-copy-from",
		itemType:    "string",
		description: "The path to the .env, which content will be copied to service repo .env file",
	},
	{
		item:        "port",
		example:     "5432",
		itemType:    "number",
		description: "Service port, that will be added to .env file.",
	},
	{
		item:        "manualRun",
		example:     "true",
		itemType:    "boolean",
		description: "Determines if the service will be run with run cmd.\n\t\t\tIf it is true, that to run you add `--services manual_to_run_service` to run cmd.\n\t\t\tBy default it is false.",
	},
	{
		item:        "depends_on_db",
		example:     "- name: db_name_from_db_services\n\t- envAlias: NAME_BEFORE_DB_IN_ENV",
		itemType:    "[]DependsOnDb",
		description: "Adds db credentials (DB_HOST,etc) from db_services will be copied to .env.\n\t\t\tenvAlias adds string before db credentials, like NAME_BEFORE_DB_IN_ENV_DB_HOST",
	},
	{
		item:        "depends_on_services",
		example:     "- name: service_name\n\t- envAlias: NAME_TO_USE_IN_ENV\n\t- suffix: /special/suffix",
		itemType:    "[]DependsOnService",
		description: "Adds service credentials to .env.\n\t\t\tsuffix is added at the end of added value\n\t\t\tNAME_TO_USE_IN_ENV=localhost:port/special/suffix will be added to .env\n\t\t\tIf you add just name, than it is SERVICE_NAME=localhost:port_in_env",
	},
	{
		item:        "beforeStart",
		example:     "- install dependencies\n\t- do some builds",
		itemType:    "\t[]string",
		description: "List of commands to run consequently, before start commands are run.",
	},
	{
		item:        "start",
		example:     "- run your service\n\t- run some other stuff",
		itemType:    "[]string",
		description: "List of commands to run in parallel for the service needs.",
	},
	{
		item:        "afterStart",
		example:     "- do some needed cleanups",
		itemType:    "[]string",
		description: "List of commands to run consequently, when the cli is exited.",
	},
}

var dbServiceItems = []CorgiComposeItems{
	{
		item:        "driver",
		example:     "postgres",
		itemType:    "string",
		description: "This is database driver for this service.\n\t\t\tBy default postgres is used.",
	},
	{
		item:        "host",
		example:     "localhost",
		itemType:    "string",
		description: "This is database host for this service, that will be used in `DB_HOST.\n\t\t\tBy default localhost is used",
	},
	{
		item:        "databaseName",
		example:     "corgi-database",
		itemType:    "string",
		description: "This is database name for this service, that will be used in DB_NAME",
	},
	{
		item:        "user",
		example:     "corgi",
		itemType:    "string",
		description: "This is database user for this service, that will be used in DB_USER",
	},
	{
		item:        "password",
		example:     "corgiSecurePassword",
		itemType:    "string",
		description: "This is database password for this service, that will be used in DB_PASSWORD",
	},
	{
		item:        "port",
		example:     "5432",
		itemType:    "number",
		description: "This is database port for this service, that will be used in DB_PORT",
	},
	{
		item:        "seedFromFilePath",
		example:     "./path/to/dump.sql",
		itemType:    "string",
		description: "Path to dump.sql file from which data is seeded.\n\t\t\tUse either seedFromFilePath or seedFromDb/seedFromDbEnvPath",
	},
	{
		item:        "seedFromDbEnvPath",
		example:     "./path/to/db/info/.env",
		itemType:    "string",
		description: "Path to .env file with db credentials for db, from which data is seeded.\n\t\t\tUse either seedFromFilePath or seedFromDb/seedFromDbEnvPath",
	},
	{
		item:        "seedFromDb",
		example:     "- host: seed_db_host\n\t- databaseName: seed_db_name\n\t- user: seed_db_user\n\t- password: seed_db_password\n\t- port: seed_db_port",
		itemType:    "\tSeedFromDb",
		description: "Db credentials to seed from.\n\t\t\t\tUse either seedFromFilePath or seedFromDb/seedFromDbEnvPath",
	},
}

func runDocs(cmd *cobra.Command, args []string) {
	generateCobraDocs(cmd)

	writer := tabwriter.NewWriter(os.Stdout, 0, 8, 1, '\t', tabwriter.AlignRight)
	fmt.Fprintln(writer, "item\texample\titemType\tdescription")
	fmt.Fprintln(writer, "service items:\t\t\t")
	for _, item := range serviceItems {
		s := fmt.Sprintf("%s\t%s\t%s\t%s", item.item, item.example, item.itemType, item.description)
		fmt.Fprintln(writer, s)
	}
	fmt.Fprintln(writer, "\t\t\t")
	fmt.Fprintln(writer, "\t\t\t")
	fmt.Fprintln(writer, "db_service items:\t\t\t")
	for _, item := range dbServiceItems {
		s := fmt.Sprintf("%s\t%s\t%s\t%s", item.item, item.example, item.itemType, item.description)
		fmt.Fprintln(writer, s)
	}
	writer.Flush()
}

func generateCobraDocs(cmd *cobra.Command) {
	shouldGenerateCobraDocs, err := cmd.Flags().GetBool("generate")
	if err != nil {
		fmt.Println("Couldn't read flag:", err)
	}

	if !shouldGenerateCobraDocs {
		return
	}
	err = doc.GenMarkdownTree(cmd.Root(), "./resources/readme")
	if err != nil {
		fmt.Println("Cobra docs are not regenerated: ", err)
	} else {
		fmt.Println("Cobra docs are generated, exiting ..")
	}
	os.Exit(1)

}
