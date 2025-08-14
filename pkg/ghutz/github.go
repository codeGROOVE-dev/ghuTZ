package ghutz

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// PublicEvent represents a GitHub public event
type PublicEvent struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	CreatedAt time.Time `json:"created_at"`
	Repo      struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	} `json:"repo"`
	Payload json.RawMessage `json:"payload"`
}

// fetchPublicEvents fetches public events (limited to last 30 days by GitHub API)
func (d *Detector) fetchPublicEvents(ctx context.Context, username string) ([]PublicEvent, error) {
	const maxPages = 3 // 100 events per page * 3 = 300 (GitHub's max)
	const perPage = 100
	
	var allEvents []PublicEvent
	
	for page := 1; page <= maxPages; page++ {
		apiURL := fmt.Sprintf("https://api.github.com/users/%s/events/public?per_page=%d&page=%d", username, perPage, page)
		
		req, err := http.NewRequestWithContext(ctx, "GET", apiURL, http.NoBody)
		if err != nil {
			return allEvents, fmt.Errorf("creating request: %w", err)
		}
		
		// Add GitHub token if available
		if d.githubToken != "" && d.isValidGitHubToken(d.githubToken) {
			req.Header.Set("Authorization", "token "+d.githubToken)
		}
		
		resp, err := d.cachedHTTPDo(ctx, req)
		if err != nil {
			d.logger.Debug("failed to fetch events page", "page", page, "error", err)
			break // Return what we have so far
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				d.logger.Debug("failed to close response body", "error", err)
			}
		}()
		
		if resp.StatusCode != http.StatusOK {
			d.logger.Debug("GitHub API returned non-200 status", "status", resp.StatusCode, "page", page)
			break // Return what we have so far
		}
		
		var events []PublicEvent
		if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
			d.logger.Debug("failed to decode events", "page", page, "error", err)
			break // Return what we have so far
		}
		
		if len(events) == 0 {
			break // No more events
		}
		
		// Add all events (GitHub API already limits to 30 days)
		allEvents = append(allEvents, events...)
		
		// If we got fewer events than requested, we've reached the end
		if len(events) < perPage {
			break
		}
	}
	
	d.logger.Debug("fetched public events", "username", username, "count", len(allEvents))
	return allEvents, nil
}

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
			Title         string    `json:"title"`
			Body          string    `json:"body"`
			CreatedAt     time.Time `json:"created_at"`
			HTMLURL       string    `json:"html_url"`
			RepositoryURL string    `json:"repository_url"` // This is just a URL string
		} `json:"items"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	d.logger.Debug("GitHub PR search results", "username", username, "total_count", result.TotalCount, "returned_items", len(result.Items))

	var prs []PullRequest
	for _, item := range result.Items {
		// Extract repository from HTML URL (format: https://github.com/owner/repo/pull/123)
		repo := ""
		if item.HTMLURL != "" {
			if strings.HasPrefix(item.HTMLURL, "https://github.com/") {
				parts := strings.Split(strings.TrimPrefix(item.HTMLURL, "https://github.com/"), "/")
				if len(parts) >= 2 {
					repo = parts[0] + "/" + parts[1]
				}
			}
		}
		prs = append(prs, PullRequest{
			Title:      item.Title,
			Body:       item.Body,
			CreatedAt:  item.CreatedAt,
			HTMLURL:    item.HTMLURL,
			Repository: repo,
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
		// Extract repository from HTML URL (format: https://github.com/owner/repo/issues/123)
		repo := ""
		if item.HTMLURL != "" {
			if strings.HasPrefix(item.HTMLURL, "https://github.com/") {
				parts := strings.Split(strings.TrimPrefix(item.HTMLURL, "https://github.com/"), "/")
				if len(parts) >= 2 {
					repo = parts[0] + "/" + parts[1]
				}
			}
		}
		issues = append(issues, Issue{
			Title:      item.Title,
			Body:       item.Body,
			CreatedAt:  item.CreatedAt,
			HTMLURL:    item.HTMLURL,
			Repository: repo,
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