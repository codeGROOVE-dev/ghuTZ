package main

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
)

// getGitHubToken returns a GitHub token from environment or gh CLI
func getGitHubToken() string {
	// First check environment variable
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return token
	}
	
	// Try to get token from gh CLI
	cmd := exec.Command("gh", "auth", "token")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = nil // Ignore errors
	
	if err := cmd.Run(); err == nil {
		if token := strings.TrimSpace(out.String()); token != "" {
			return token
		}
	}
	
	return ""
}