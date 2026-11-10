package utils

import "net/url"

func PlainHTTPURL(hostPort, path string) string {
	return (&url.URL{Scheme: "http", Host: hostPort, Path: path}).String()
}
