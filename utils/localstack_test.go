package utils

import (
	"strings"
	"testing"
)

func TestAwsEnvKey(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"db/password", "DB_PASSWORD"},
		{"/app/log_level", "APP_LOG_LEVEL"},
		{"my-secret", "MY_SECRET"},
		{"v1.2.3", "V1_2_3"},
		{"_padded_", "PADDED"},
		{"already_upper", "ALREADY_UPPER"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := awsEnvKey(tt.in); got != tt.want {
			t.Errorf("awsEnvKey(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestAutoInjectLocalstackServices(t *testing.T) {
	t.Run("queues imply sqs", func(t *testing.T) {
		got := autoInjectLocalstackServices(nil, DatabaseService{Queues: []string{"q1"}})
		if !contains(got, "sqs") {
			t.Errorf("want sqs, got %v", got)
		}
	})

	t.Run("subscriptions imply sns + sqs", func(t *testing.T) {
		got := autoInjectLocalstackServices(nil, DatabaseService{
			Subscriptions: []SnsSubscription{{Topic: "t", Queue: "q"}},
		})
		if !contains(got, "sns") || !contains(got, "sqs") {
			t.Errorf("want sns+sqs, got %v", got)
		}
	})

	t.Run("no duplicates", func(t *testing.T) {
		got := autoInjectLocalstackServices([]string{"sqs"}, DatabaseService{
			Queues: []string{"q1"},
		})
		if countOf(got, "sqs") != 1 {
			t.Errorf("duplicate sqs: %v", got)
		}
	})

	t.Run("each resource type adds its service", func(t *testing.T) {
		got := autoInjectLocalstackServices(nil, DatabaseService{
			Buckets:    []string{"b"},
			Topics:     []string{"t"},
			Secrets:    []AwsSecret{{Name: "s"}},
			Parameters: []SsmParameter{{Name: "p"}},
			Streams:    []string{"k"},
		})
		for _, want := range []string{"s3", "sns", "secretsmanager", "ssm", "kinesis"} {
			if !contains(got, want) {
				t.Errorf("missing %q in %v", want, got)
			}
		}
	})
}

func TestValidateLocalstackConfig(t *testing.T) {
	t.Run("subscription requires declared topic + queue", func(t *testing.T) {
		err := validateLocalstackConfig("foo", DatabaseService{
			Topics: []string{"t1"},
			Queues: []string{"q1"},
			Subscriptions: []SnsSubscription{
				{Topic: "t1", Queue: "q1"},
			},
		})
		if err != nil {
			t.Errorf("want nil err, got %v", err)
		}
	})

	t.Run("subscription with undeclared topic errors", func(t *testing.T) {
		err := validateLocalstackConfig("foo", DatabaseService{
			Queues: []string{"q1"},
			Subscriptions: []SnsSubscription{
				{Topic: "missing", Queue: "q1"},
			},
		})
		if err == nil || !strings.Contains(err.Error(), "missing") {
			t.Errorf("want missing-topic error, got %v", err)
		}
	})

	t.Run("subscription missing fields errors", func(t *testing.T) {
		err := validateLocalstackConfig("foo", DatabaseService{
			Subscriptions: []SnsSubscription{{Topic: ""}},
		})
		if err == nil {
			t.Error("want err for empty topic/queue")
		}
	})

	t.Run("secret without name errors", func(t *testing.T) {
		err := validateLocalstackConfig("svc", DatabaseService{
			Secrets: []AwsSecret{{Value: "x"}},
		})
		if err == nil || !strings.Contains(err.Error(), "name required") {
			t.Errorf("want name-required err, got %v", err)
		}
	})

	t.Run("parameter type valid types accepted", func(t *testing.T) {
		for _, typ := range []string{"", "String", "StringList", "SecureString"} {
			err := validateLocalstackConfig("s", DatabaseService{
				Parameters: []SsmParameter{{Name: "p", Type: typ}},
			})
			if err != nil {
				t.Errorf("type %q rejected: %v", typ, err)
			}
		}
	})

	t.Run("parameter type invalid rejected", func(t *testing.T) {
		err := validateLocalstackConfig("s", DatabaseService{
			Parameters: []SsmParameter{{Name: "p", Type: "Bogus"}},
		})
		if err == nil || !strings.Contains(err.Error(), "Bogus") {
			t.Errorf("want type err, got %v", err)
		}
	})
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func countOf(s []string, v string) int {
	n := 0
	for _, x := range s {
		if x == v {
			n++
		}
	}
	return n
}
