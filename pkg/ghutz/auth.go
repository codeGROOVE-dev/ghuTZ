package ghutz

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

// getGeminiAPIKey returns the Gemini API key from environment
func getGeminiAPIKey() string {
	return os.Getenv("GEMINI_API_KEY")
}

// getGoogleCloudProject returns the Google Cloud project from environment or metadata
func getGoogleCloudProject() string {
	if project := os.Getenv("GOOGLE_CLOUD_PROJECT"); project != "" {
		return project
	}
	if project := os.Getenv("GCP_PROJECT"); project != "" {
		return project
	}
	return ""
}