package utils

import _ "embed"

//go:embed corgi-compose.schema.json
var composeJSONSchema string

func ComposeJSONSchema() string { return composeJSONSchema }
