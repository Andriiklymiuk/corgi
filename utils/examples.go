package utils

import (
	"fmt"

	"github.com/spf13/cobra"
)

type CorgiExample struct {
	Title      string
	Link       string
	PublicLink string
	Path       string
	Files      []string
	ShouldSeed bool
}

var ExampleProjects = []CorgiExample{
	{
		Title:      "2 postgres databases with echo logs",
		Link:       "https://github.com/Andriiklymiuk/corgi_examples/blob/main/echoExample.corgi-compose.yml",
		PublicLink: "https://github.com/Andriiklymiuk/corgi_examples/blob/main/echoExample.corgi-compose.yml",
		Path:       "echo_example_with_postgres_databases",
	},
	{
		Title:      "Postgres with data + go + react native",
		Link:       "https://github.com/Andriiklymiuk/corgi_examples/blob/main/postgres/postgres-seeded-go-reactnative.corgi-compose.yml",
		PublicLink: "https://github.com/Andriiklymiuk/corgi/tree/main/examples/postgres",
		Path:       "postgres_with_data_go_reactnative_example",
		Files:      []string{"https://github.com/Andriiklymiuk/corgi_examples/blob/main/postgres/users_dump.sql"},
		ShouldSeed: true,
	},
	{
		Title:      "Rabbitmq + go + nestjs servers",
		Link:       "https://github.com/Andriiklymiuk/corgi_examples/blob/main/rabbitmq/rabbitmq-go-nestjs.corgi-compose.yml",
		PublicLink: "https://github.com/Andriiklymiuk/corgi_examples/blob/main/rabbitmq/rabbitmq-go-nestjs.corgi-compose.yml",
		Path:       "rabbitmq_go_nestjs_queue_example",
	},
	{
		Title:      "AWS SQS + postgres + go + deno servers",
		Link:       "https://github.com/Andriiklymiuk/corgi_examples/blob/main/aws_sqs/aws_sqs_postgres_go_deno.corgi-compose.yml",
		PublicLink: "https://github.com/Andriiklymiuk/corgi_examples/blob/main/aws_sqs/aws_sqs_postgres_go_deno.corgi-compose.yml",
		Path:       "aws_sqs_postgres_go_deno_queue_example",
	},
	{
		Title:      "MongoDb + go server",
		Link:       "https://github.com/Andriiklymiuk/corgi_examples/blob/main/mongodb/mongodb-go.corgi-compose.yml",
		PublicLink: "https://github.com/Andriiklymiuk/corgi_examples/blob/main/mongodb/mongodb-go.corgi-compose.yml",
		Path:       "mongodb_go_example",
	},
	{
		Title:      "Redis + bun server + expo app",
		Link:       "https://github.com/Andriiklymiuk/corgi_examples/blob/main/redis/redis-bun-expo.corgi-compose.yml",
		PublicLink: "https://github.com/Andriiklymiuk/corgi_examples/blob/main/redis/redis-bun-expo.corgi-compose.yml",
		Path:       "redis_bun_expo_example",
	},
	{
		Title:      "Hono server, websocket + expo app",
		Link:       "https://github.com/Andriiklymiuk/corgi_examples/blob/main/honoExpoTodo/hono-bun-expo.corgi-compose.yml",
		PublicLink: "https://github.com/Andriiklymiuk/corgi_examples/blob/main/honoExpoTodo/hono-bun-expo.corgi-compose.yml",
		Path:       "hono_expo_example",
	},
}

func ExtractExamplePaths(examples []CorgiExample) []string {
	var paths []string
	for _, example := range examples {
		if example.Path != "" {
			paths = append(paths, example.Path)
		}
	}
	return paths
}

func FindExampleByPath(examples []CorgiExample, targetPath string) *CorgiExample {
	for _, item := range examples {
		if item.Path == targetPath {
			return &item
		}
	}
	return nil
}

func DownloadExample(
	cobraCmd *cobra.Command,
	path string,
	filenameFlag string,
) (string, error) {
	fmt.Printf("Selected path: %s\n", path)
	selectedExample := FindExampleByPath(ExampleProjects, path)
	if selectedExample == nil {
		return "", fmt.Errorf("selected path not found in examples")
	}
	downloadedFile, err := DownloadFileFromURL(selectedExample.Link, filenameFlag, "")
	if err != nil {
		return "", fmt.Errorf("error downloading template: %v", err)
	}

	for _, file := range selectedExample.Files {
		_, err := DownloadFileFromURL(file, "", "")
		if err != nil {
			fmt.Println("error downloading file: ", err)
		}
	}
	if selectedExample.ShouldSeed {
		err := cobraCmd.Flags().Set("seed", "true")
		if err != nil {
			fmt.Println("error setting seed flag: ", err)
		}
	}
	return downloadedFile, nil
}
