package github

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Client provides methods for interacting with the GitHub API
type Client struct {
	logger      *slog.Logger
	httpClient  *http.Client
	githubToken string
	cachedHTTPDo func(context.Context, *http.Request) (*http.Response, error)
}

// NewClient creates a new GitHub API client
func NewClient(logger *slog.Logger, httpClient *http.Client, githubToken string, cachedHTTPDo func(context.Context, *http.Request) (*http.Response, error)) *Client {
	return &Client{
		logger:      logger,
		httpClient:  httpClient,
		githubToken: githubToken,
		cachedHTTPDo: cachedHTTPDo,
	}
}

// isValidGitHubToken checks if a token looks valid (basic check)
func (c *Client) isValidGitHubToken(token string) bool {
	// GitHub tokens have specific prefixes
	// Classic: 40 chars hex
	// Fine-grained: github_pat_ prefix
	// OAuth: gho_ prefix
	// App: ghs_ prefix
	if token == "" {
		return false
	}
	
	if strings.HasPrefix(token, "github_pat_") ||
		strings.HasPrefix(token, "gho_") ||
		strings.HasPrefix(token, "ghs_") ||
		strings.HasPrefix(token, "ghp_") {
		return true
	}
	
	// Classic tokens are 40 hex chars
	if len(token) == 40 {
		for _, c := range token {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return false
			}
		}
		return true
	}
	
	return false
}

// defaultHTTPClient returns a default HTTP client with timeout
func defaultHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
	}
}