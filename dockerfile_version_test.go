package main

import (
	"bufio"
	"os"
	"regexp"
	"strings"
	"testing"
)

func getVersionFromFile(filename string, searchPhrase string) (string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	re := regexp.MustCompile(searchPhrase) // Regex to capture Go version
	for scanner.Scan() {
		line := scanner.Text()
		matches := re.FindStringSubmatch(line)
		if len(matches) > 1 {
			return matches[1], nil
		}
	}

	return "", scanner.Err()
}

// Test function to check if Go version in go.mod matches Dockerfile
func TestGoVersionMatching(t *testing.T) {
	goModVersionSearchPhrase := `^go\s+(\d+\.\d+)`
	goModVersion, err := getVersionFromFile("go.mod", goModVersionSearchPhrase)
	if err != nil {
		t.Fatalf("Error getting Go version from go.mod: %v", err)
	}

	dockerFileVersionSearchPhrase := "GO_VERSION=(.*)"
	dockerfileVersion, err := getVersionFromFile("Dockerfile", dockerFileVersionSearchPhrase)
	if err != nil {
		t.Fatalf("Error getting Go version from Dockerfile: %v", err)
	}

	if !strings.HasPrefix(dockerfileVersion, goModVersion) {
		t.Errorf("Go version mismatch: go.mod (%s), Dockerfile (%s)", goModVersion, dockerfileVersion)
	}
}

// Test function to check if Go version in go.mod matches Dockerfile_boringcrypto
func TestGoVersionMatchingBoringCrypto(t *testing.T) {
	goModVersionSearchPhrase := `^go\s+(\d+\.\d+)`
	goModVersion, err := getVersionFromFile("go.mod", goModVersionSearchPhrase)
	if err != nil {
		t.Fatalf("Error getting Go version from go.mod: %v", err)
	}

	dockerFileVersionSearchPhrase := "GO_VERSION=(.*)"
	dockerfileVersion, err := getVersionFromFile("Dockerfile_boringcrypto", dockerFileVersionSearchPhrase)
	if err != nil {
		t.Fatalf("Error getting Go version from Dockerfile_boringcrypto: %v", err)
	}

	if !strings.HasPrefix(dockerfileVersion, goModVersion) {
		t.Errorf("Go version mismatch: go.mod (%s), Dockerfile (%s)", goModVersion, dockerfileVersion)
	}
}
