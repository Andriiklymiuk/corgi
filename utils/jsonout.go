package utils

import (
	"encoding/json"
	"io"
	"os"
)

// JSONOutput is true when the global --json flag is set.
var JSONOutput bool

func PrintJSONTo(w io.Writer, v any) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func PrintJSON(v any) { PrintJSONTo(os.Stdout, v) }

type jsonErr struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func WriteJSONError(w io.Writer, code, message string) {
	var e jsonErr
	e.Error.Code = code
	e.Error.Message = message
	PrintJSONTo(w, e)
}

func JSONError(code, message string) { WriteJSONError(os.Stdout, code, message) }
