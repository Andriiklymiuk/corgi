package utils

import _ "embed" // for go:embed

//go:embed corgi-compose.schema.json
var composeJSONSchema string

func ComposeJSONSchema() string { return composeJSONSchema }
