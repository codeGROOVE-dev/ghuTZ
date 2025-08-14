package ghutz

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

func (d *Detector) fetchPullRequests(ctx context.Context, username string) ([]PullRequest, error) {
	apiURL := fmt.Sprintf("https://api.github.com/search/issues?q=author:%s+type:pr&sort=created&order=desc&per_page=100", username)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	// SECURITY: Validate and sanitize GitHub token before use
	if d.githubToken != "" {
		token := d.githubToken
		// Validate token format to prevent injection attacks
		if d.isValidGitHubToken(token) {
			req.Header.Set("Authorization", "token "+token)
		}
	}

	resp, err := d.cachedHTTPDo(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("fetching pull requests: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			d.logger.Debug("failed to close response body", "error", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("GitHub API returned status %d (failed to read response)", resp.StatusCode)
		}
		return nil, fmt.Errorf("GitHub API returned status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		TotalCount int `json:"total_count"`
		Items      []struct {
			Title     string    `json:"title"`
			Body      string    `json:"body"`
			CreatedAt time.Time `json:"created_at"`
		} `json:"items"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	d.logger.Debug("GitHub PR search results", "username", username, "total_count", result.TotalCount, "returned_items", len(result.Items))

	var prs []PullRequest
	for _, item := range result.Items {
		prs = append(prs, PullRequest{
			Title:     item.Title,
			Body:      item.Body,
			CreatedAt: item.CreatedAt,
		})
	}

	return prs, nil
}

func (d *Detector) fetchIssues(ctx context.Context, username string) ([]Issue, error) {
	apiURL := fmt.Sprintf("https://api.github.com/search/issues?q=author:%s+type:issue&sort=created&order=desc&per_page=100", username)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	// SECURITY: Validate and sanitize GitHub token before use
	if d.githubToken != "" {
		token := d.githubToken
		// Validate token format to prevent injection attacks
		if d.isValidGitHubToken(token) {
			req.Header.Set("Authorization", "token "+token)
		}
	}

	resp, err := d.cachedHTTPDo(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("fetching issues: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			d.logger.Debug("failed to close response body", "error", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("GitHub API returned status %d (failed to read response)", resp.StatusCode)
		}
		return nil, fmt.Errorf("GitHub API returned status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		TotalCount int `json:"total_count"`
		Items      []struct {
			Title     string    `json:"title"`
			Body      string    `json:"body"`
			CreatedAt time.Time `json:"created_at"`
			HTMLURL   string    `json:"html_url"`
		} `json:"items"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	d.logger.Debug("GitHub issue search results", "username", username, "total_count", result.TotalCount, "returned_items", len(result.Items))

	var issues []Issue
	for _, item := range result.Items {
		issues = append(issues, Issue{
			Title:     item.Title,
			Body:      item.Body,
			CreatedAt: item.CreatedAt,
			HTMLURL:   item.HTMLURL,
		})
	}

	return issues, nil
}

func (d *Detector) fetchUserComments(ctx context.Context, username string) ([]Comment, error) {
	if d.githubToken == "" {
		d.logger.Debug("GitHub token required for GraphQL API", "username", username)
		return nil, fmt.Errorf("GitHub token required for GraphQL API")
	}

	query := fmt.Sprintf(`{
		user(login: "%s") {
			issueComments(first: 100, orderBy: {field: UPDATED_AT, direction: DESC}) {
				nodes {
					createdAt
				}
			}
			commitComments(first: 100) {
				nodes {
					createdAt
				}
			}
		}
	}`, username)

	reqBody := map[string]string{
		"query": query,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling GraphQL query: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.github.com/graphql", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "bearer "+d.githubToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.cachedHTTPDo(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("fetching comments: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			d.logger.Debug("failed to close response body", "error", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("GitHub GraphQL API returned status %d (failed to read response)", resp.StatusCode)
		}
		return nil, fmt.Errorf("GitHub GraphQL API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse the response
	var result struct {
		Data struct {
			User struct {
				IssueComments struct {
					Nodes []struct {
						CreatedAt time.Time `json:"createdAt"`
					} `json:"nodes"`
				} `json:"issueComments"`
				CommitComments struct {
					Nodes []struct {
						CreatedAt time.Time `json:"createdAt"`
					} `json:"nodes"`
				} `json:"commitComments"`
			} `json:"user"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	if len(result.Errors) > 0 {
		d.logger.Debug("GitHub GraphQL errors", "username", username, "errors", result.Errors)
		return nil, fmt.Errorf("GraphQL errors: %v", result.Errors[0].Message)
	}

	var comments []Comment

	// Add issue comments
	for _, node := range result.Data.User.IssueComments.Nodes {
		comments = append(comments, Comment{
			CreatedAt: node.CreatedAt,
			Type:      "issue",
		})
	}

	// Add commit comments
	for _, node := range result.Data.User.CommitComments.Nodes {
		comments = append(comments, Comment{
			CreatedAt: node.CreatedAt,
			Type:      "commit",
		})
	}

	d.logger.Debug("fetched user comments", "username", username,
		"issue_comments", len(result.Data.User.IssueComments.Nodes),
		"commit_comments", len(result.Data.User.CommitComments.Nodes),
		"total", len(comments))

	return comments, nil
}

func (d *Detector) fetchOrganizations(ctx context.Context, username string) ([]Organization, error) {
	apiURL := fmt.Sprintf("https://api.github.com/users/%s/orgs", username)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	// SECURITY: Validate and sanitize GitHub token before use
	if d.githubToken != "" {
		token := d.githubToken
		// Validate token format to prevent injection attacks
		if d.isValidGitHubToken(token) {
			req.Header.Set("Authorization", "token "+token)
		}
	}

	resp, err := d.cachedHTTPDo(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("fetching organizations: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			d.logger.Debug("failed to close response body", "error", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("GitHub API returned status %d (failed to read response)", resp.StatusCode)
		}
		return nil, fmt.Errorf("GitHub API returned status %d: %s", resp.StatusCode, string(body))
	}

	var orgs []Organization
	if err := json.NewDecoder(resp.Body).Decode(&orgs); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return orgs, nil
}

func (d *Detector) fetchUser(ctx context.Context, username string) *GitHubUser {
	url := fmt.Sprintf("https://api.github.com/users/%s", username)

	req, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
	if err != nil {
		return nil
	}

	// SECURITY: Validate and sanitize GitHub token before use
	if d.githubToken != "" {
		token := d.githubToken
		// Validate token format to prevent injection attacks
		if d.isValidGitHubToken(token) {
			req.Header.Set("Authorization", "token "+token)
		}
	}

	resp, err := d.cachedHTTPDo(ctx, req)
	if err != nil {
		return nil
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			d.logger.Debug("failed to close response body", "error", err)
		}
	}()

	var user GitHubUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil
	}

	return &user
}