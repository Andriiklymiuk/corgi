package utils

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type Quote struct {
	Content string `json:"content"`
	Author  string `json:"author"`
}

func GetRandomQuote(tag string) string {
	resp, err := http.Get(
		fmt.Sprintf("https://api.quotable.io/random?tags=%s", tag),
	)
	if err != nil {
		return ""
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}

	var result Quote
	if err := json.Unmarshal(body, &result); err != nil {
		return ""
	}

	return result.Content
}
