package ghutz

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
)

func getGitHubToken() string {
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return token
	}
	
	cmd := exec.Command("gh", "auth", "token")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = nil
	
	if err := cmd.Run(); err == nil {
		if token := strings.TrimSpace(out.String()); token != "" {
			return token
		}
	}
	
	return ""
}

func getGeminiAPIKey() string {
	return os.Getenv("GEMINI_API_KEY")
}

func getGoogleCloudProject() string {
	if project := os.Getenv("GOOGLE_CLOUD_PROJECT"); project != "" {
		return project
	}
	if project := os.Getenv("GCP_PROJECT"); project != "" {
		return project
	}
	return ""
}