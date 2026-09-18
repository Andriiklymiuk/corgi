package utils

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const githubHost = "github.com"

func DownloadFileFromURL(
	url, fileName, privateToken string,
) (string, error) {
	rawURL := convertToRawURL(url)

	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %v", err)
	}

	if privateToken != "" {
		if strings.Contains(rawURL, githubHost) || strings.Contains(rawURL, "githubusercontent.com") {
			req.Header.Add("Authorization", "token "+privateToken)
		} else if strings.Contains(rawURL, "gitlab.com") {
			req.Header.Add("PRIVATE-TOKEN", privateToken)
		}
	}
	fmt.Println("Downloading file from", rawURL)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to download file: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf(
			"failed to download file, server returned status code %d",
			resp.StatusCode,
		)
	}

	if fileName == "" {
		fileName = path.Base(rawURL)
	}

	downloadDir := "."
	filePath := filepath.Join(downloadDir, fileName)

	file, err := os.Create(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to create file %s: %v", filePath, err)
	}
	defer file.Close()

	_, err = io.Copy(file, resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to write to file %s: %v", filePath, err)
	}

	return filePath, nil
}

func convertToRawURL(url string) string {
	rawURL := url

	if strings.Contains(url, githubHost) {
		rawURL = strings.Replace(rawURL, githubHost, "raw.githubusercontent.com", 1)
		rawURL = strings.Replace(rawURL, "/blob/", "/", 1)
	}

	if strings.Contains(url, "gitlab.com") {
		rawURL = strings.Replace(rawURL, "/-/blob/", "/-/raw/", 1)
	}

	return rawURL
}
