package utils

import (
	"fmt"
	"strings"
)

// awsEnvKey converts an AWS resource name like "db/password" or "/app/log_level"
// into an env var fragment: "DB_PASSWORD" or "APP_LOG_LEVEL".
func awsEnvKey(name string) string {
	s := strings.ToUpper(name)
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, "-", "_")
	s = strings.ReplaceAll(s, ".", "_")
	s = strings.Trim(s, "_")
	return s
}

// autoInjectLocalstackServices ensures the SERVICES list passed to the LocalStack
// container contains every AWS service implied by the configured resources.
// E.g. listing topics: implies sns; listing streams: implies kinesis.
func autoInjectLocalstackServices(services []string, db DatabaseService) []string {
	want := map[string]bool{}
	for _, s := range services {
		want[s] = true
	}

	add := func(name string) {
		if !want[name] {
			services = append(services, name)
			want[name] = true
		}
	}

	if len(db.Queues) > 0 {
		add("sqs")
	}
	if len(db.Buckets) > 0 {
		add("s3")
	}
	if len(db.Topics) > 0 || len(db.Subscriptions) > 0 {
		add("sns")
	}
	if len(db.Subscriptions) > 0 {
		add("sqs")
	}
	if len(db.Secrets) > 0 {
		add("secretsmanager")
	}
	if len(db.Parameters) > 0 {
		add("ssm")
	}
	if len(db.Streams) > 0 {
		add("kinesis")
	}

	return services
}

func validateSubscriptions(name string, subs []SnsSubscription, topics, queues map[string]bool) error {
	for i, sub := range subs {
		if sub.Topic == "" || sub.Queue == "" {
			return fmt.Errorf(
				"db_services.%s.subscriptions[%d]: topic and queue are required",
				name, i,
			)
		}
		if !topics[sub.Topic] {
			return fmt.Errorf(
				"db_services.%s.subscriptions[%d]: topic %q not declared in topics:",
				name, i, sub.Topic,
			)
		}
		if !queues[sub.Queue] {
			return fmt.Errorf(
				"db_services.%s.subscriptions[%d]: queue %q not declared in queues:",
				name, i, sub.Queue,
			)
		}
	}
	return nil
}

func validateLocalstackParameters(name string, params []SsmParameter) error {
	for i, p := range params {
		if p.Name == "" {
			return fmt.Errorf("db_services.%s.parameters[%d]: name required", name, i)
		}
		switch p.Type {
		case "", "String", "StringList", "SecureString":
		default:
			return fmt.Errorf(
				"db_services.%s.parameters[%d]: type %q must be String, StringList, or SecureString",
				name, i, p.Type,
			)
		}
	}
	return nil
}

func validateLocalstackConfig(name string, db DatabaseService) error {
	topics := map[string]bool{}
	for _, t := range db.Topics {
		topics[t] = true
	}
	queues := map[string]bool{}
	for _, q := range db.Queues {
		queues[q] = true
	}

	if err := validateSubscriptions(name, db.Subscriptions, topics, queues); err != nil {
		return err
	}

	for i, s := range db.Secrets {
		if s.Name == "" {
			return fmt.Errorf("db_services.%s.secrets[%d]: name required", name, i)
		}
	}

	return validateLocalstackParameters(name, db.Parameters)
}
