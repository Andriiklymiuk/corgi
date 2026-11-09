package utils

import (
	"encoding/json"
	"io"
	"os"
)

var JSONOutput bool

func PrintJSONTo(w io.Writer, v any) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func PrintJSON(v any) { PrintJSONTo(withMirror(os.Stdout), v) }

// The error envelope. fix is the next step for the code (from ErrorCatalog, or
// a specific one such as a did-you-mean), and lookup the command that explains
// the code, so an agent can act without a person reading the message.
type jsonErr struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Fix     string `json:"fix,omitempty"`
		Lookup  string `json:"lookup,omitempty"`
	} `json:"error"`
}

func WriteJSONError(w io.Writer, code, message string) { WriteJSONErrorFix(w, code, message, "") }

func WriteJSONErrorFix(w io.Writer, code, message, fix string) {
	var e jsonErr
	e.Error.Code = code
	e.Error.Message = message
	e.Error.Fix = fix
	if e.Error.Fix == "" {
		e.Error.Fix = FixFor(code)
	}
	if _, ok := LookupCode(code); ok {
		e.Error.Lookup = "corgi vocab " + code
	}
	PrintJSONTo(w, e)
}

func JSONError(code, message string) { WriteJSONError(withMirror(os.Stdout), code, message) }

func JSONErrorFix(code, message, fix string) {
	WriteJSONErrorFix(withMirror(os.Stdout), code, message, fix)
}
