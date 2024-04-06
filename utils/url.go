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

func DownloadFileFromURL(
	url string,
	fileName string,
	privateToken string,
) (string, error) {
	// Convert the URL to a raw content URL if it's a GitHub or GitLab URL
	rawURL := convertToRawURL(url)

	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %v", err)
	}

	if privateToken != "" {
		if strings.Contains(rawURL, "github.com") || strings.Contains(rawURL, "githubusercontent.com") {
			req.Header.Add("Authorization", "token "+privateToken)
		} else if strings.Contains(rawURL, "gitlab.com") {
			// For GitLab, use the PRIVATE-TOKEN header, but for gitlab repos with SSO it won't work properly
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

	// Extract the filename from the URL
	if fileName == "" {
		fileName = path.Base(rawURL)
	}

	// Specify a directory to save the file. For simplicity, using the current directory.
	downloadDir := "." // Change as needed.
	filePath := filepath.Join(downloadDir, fileName)

	// Create the file
	file, err := os.Create(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to create file %s: %v", filePath, err)
	}
	defer file.Close()

	// Write the response body to the file
	_, err = io.Copy(file, resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to write to file %s: %v", filePath, err)
	}

	return filePath, nil
}

func convertToRawURL(url string) string {
	rawURL := url

	// For GitHub URLs
	if strings.Contains(url, "github.com") {
		rawURL = strings.Replace(rawURL, "github.com", "raw.githubusercontent.com", 1)
		rawURL = strings.Replace(rawURL, "/blob/", "/", 1)
	}

	// For GitLab URLs
	if strings.Contains(url, "gitlab.com") {
		rawURL = strings.Replace(rawURL, "/-/blob/", "/-/raw/", 1)
	}

	return rawURL
}
