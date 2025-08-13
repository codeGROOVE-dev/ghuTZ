package main

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