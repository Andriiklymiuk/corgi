package utils

import (
	"bufio"
	"fmt"
	"os"
)

func GetTargetService() (string, error) {
	files, err := GetFoldersListInDirectory()
	if err != nil {
		return "", fmt.Errorf("error getting db folders: %s", err)
	}
	backString := "🛑  close program"
	chosenItem, err := PickItemFromListPrompt(
		"Select service",
		files,
		backString,
	)

	if err != nil {
		if err.Error() == backString {
			os.Exit(1)
			return "", fmt.Errorf(backString)
		}

		return "", fmt.Errorf("failed to choose %s", err)
	}

	return chosenItem, nil
}

func GetFoldersListInDirectory() ([]string, error) {
	filesInDirectory, err := os.ReadDir(
		fmt.Sprintf("./%s/", RootDbServicesFolder),
	)
	if err != nil {
		return nil, err
	}

	var files []string

	for _, file := range filesInDirectory {
		if file.Type().IsDir() && file.Name() != ".git" {
			files = append(files, file.Name())
		}
	}

	return files, nil
}

func CheckIfFileExistsInDirectory(pathToDirectory string, fileName string) (bool, error) {
	filesInDirectory, err := os.ReadDir(pathToDirectory)
	if err != nil {
		return false, err
	}
	var makeFileExists bool
	for _, file := range filesInDirectory {
		if file.Name() == fileName {
			makeFileExists = true
		}
	}
	return makeFileExists, nil
}

func GetFileContent(fileName string) []string {
	f, err := os.Open(fileName)
	if err != nil {
		fmt.Println(err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	result := []string{}

	for scanner.Scan() {
		line := scanner.Text()
		result = append(result, line)
	}
	return result
}
