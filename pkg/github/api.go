package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// FetchPublicEvents fetches public events (limited to last 30 days by GitHub API).
func (c *Client) FetchPublicEvents(ctx context.Context, username string) ([]PublicEvent, error) {
	const maxPages = 3 // 100 events per page * 3 = 300 (GitHub's max)
	const perPage = 100

	var allEvents []PublicEvent

	for page := 1; page <= maxPages; page++ {
		apiURL := fmt.Sprintf("https://api.github.com/users/%s/events/public?per_page=%d&page=%d", url.PathEscape(username), perPage, page)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, http.NoBody)
		if err != nil {
			return allEvents, fmt.Errorf("creating request: %w", err)
		}

		// Add GitHub token if available
		if c.githubToken != "" && c.isValidGitHubToken(c.githubToken) {
			req.Header.Set("Authorization", "token "+c.githubToken)
		}

		resp, err := c.cachedHTTPDo(ctx, req)
		if err != nil {
			c.logger.Debug("failed to fetch events page", "page", page, "error", err)
			break // Return what we have so far
		}

		processEvents := func() bool {
			defer func() {
				if err := resp.Body.Close(); err != nil {
					c.logger.Debug("failed to close response body", "error", err)
				}
			}()

			if resp.StatusCode != http.StatusOK {
				c.logger.Debug("GitHub API returned non-200 status", "status", resp.StatusCode, "page", page)
				return false // Stop processing
			}

			var events []PublicEvent
			if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
				c.logger.Debug("failed to decode events", "page", page, "error", err)
				return false // Stop processing
			}

			if len(events) == 0 {
				return false // No more events
			}

			// Add all events (GitHub API already limits to 30 days)
			allEvents = append(allEvents, events...)

			// If we got fewer events than requested, we've reached the end
			return len(events) >= perPage
		}

		if !processEvents() {
			break
		}
	}

	c.logger.Debug("fetched public events", "username", username, "count", len(allEvents))
	return allEvents, nil
}

// FetchUserGistsDetails fetches full gist objects with descriptions for a user.
func (c *Client) FetchUserGistsDetails(ctx context.Context, username string) ([]Gist, error) {
	apiURL := fmt.Sprintf("https://api.github.com/users/%s/gists?per_page=100", url.PathEscape(username))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	if c.githubToken != "" && c.isValidGitHubToken(c.githubToken) {
		req.Header.Set("Authorization", "token "+c.githubToken)
	}

	resp, err := c.cachedHTTPDo(ctx, req)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			c.logger.Debug("failed to close response body", "error", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		c.logger.Warn("🚩 GitHub API Error", "status", resp.StatusCode, "url", apiURL)
		return nil, fmt.Errorf("github API returned status %d", resp.StatusCode)
	}

	var gists []Gist
	if err := json.NewDecoder(resp.Body).Decode(&gists); err != nil {
		return nil, fmt.Errorf("decoding gists response: %w", err)
	}

	c.logger.Debug("fetched gist details", "username", username, "count", len(gists))
	return gists, nil
}

// FetchUserGists fetches gist timestamps for a user.
func (c *Client) FetchUserGists(ctx context.Context, username string) ([]time.Time, error) {
	apiURL := fmt.Sprintf("https://api.github.com/users/%s/gists?per_page=100", url.PathEscape(username))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	if c.githubToken != "" && c.isValidGitHubToken(c.githubToken) {
		req.Header.Set("Authorization", "token "+c.githubToken)
	}

	resp, err := c.cachedHTTPDo(ctx, req)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			c.logger.Debug("failed to close response body", "error", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var gists []struct {
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&gists); err != nil {
		return nil, err
	}

	// Collect both created and updated timestamps
	timestamps := make([]time.Time, 0, len(gists)*2)
	for _, gist := range gists {
		timestamps = append(timestamps, gist.CreatedAt, gist.UpdatedAt)
	}

	c.logger.Debug("fetched gist timestamps", "username", username, "count", len(timestamps))
	return timestamps, nil
}

// pageResult contains the result of fetching a single page.
type pageResult struct {
	totalCount int
	hasMore    bool
}

// FetchPullRequests fetches pull requests for a user with default page limit.
func (c *Client) FetchPullRequests(ctx context.Context, username string) ([]PullRequest, error) {
	return c.FetchPullRequestsWithLimit(ctx, username, 2)
}

// fetchPRPage fetches a single page of pull requests.
func (c *Client) fetchPRPage(ctx context.Context, username string, page, perPage int) ([]PullRequest, pageResult, error) {
	apiURL := fmt.Sprintf("https://api.github.com/search/issues?q=author:%s+type:pr&sort=created&order=desc&per_page=%d&page=%d",
		url.QueryEscape(username), perPage, page)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, http.NoBody)
	if err != nil {
		return nil, pageResult{}, fmt.Errorf("creating request: %w", err)
	}

	// SECURITY: Validate and sanitize GitHub token before use
	if c.githubToken != "" {
		token := c.githubToken
		// Validate token format to prevent injection attacks
		if c.isValidGitHubToken(token) {
			req.Header.Set("Authorization", "token "+token)
		}
	}

	resp, err := c.cachedHTTPDo(ctx, req)
	if err != nil {
		c.logger.Debug("failed to fetch PR page", "page", page, "error", err)
		return nil, pageResult{}, nil // Return what we have so far
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			c.logger.Debug("failed to close response body", "error", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			c.logger.Warn("🚩 GitHub API Error", "status", resp.StatusCode, "page", page)
			return nil, pageResult{}, nil
		}
		c.logger.Warn("🚩 GitHub API Error", "status", resp.StatusCode, "page", page, "body", string(body))
		return nil, pageResult{}, nil
	}

	var result struct {
		Items []struct {
			Title         string    `json:"title"`
			Body          string    `json:"body"`
			CreatedAt     time.Time `json:"created_at"`
			HTMLURL       string    `json:"html_url"`
			RepositoryURL string    `json:"repository_url"`
		} `json:"items"`
		TotalCount int `json:"total_count"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		c.logger.Debug("failed to decode PR response", "page", page, "error", err)
		return nil, pageResult{}, nil
	}

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
			Title:     item.Title,
			Body:      item.Body,
			CreatedAt: item.CreatedAt,
			HTMLURL:   item.HTMLURL,
			RepoName:  repo,
		})
	}

	hasMore := len(result.Items) == perPage
	return prs, pageResult{totalCount: result.TotalCount, hasMore: hasMore}, nil
}

// FetchPullRequestsWithLimit fetches pull requests for a user with custom page limit.
func (c *Client) FetchPullRequestsWithLimit(ctx context.Context, username string, maxPages int) ([]PullRequest, error) {
	var allPRs []PullRequest
	const perPage = 100

	for page := 1; page <= maxPages; page++ {
		prs, result, err := c.fetchPRPage(ctx, username, page, perPage)
		if err != nil {
			return allPRs, err
		}

		if page == 1 && result.totalCount > 0 {
			c.logger.Debug("GitHub PR search results", "username", username, "total_count", result.totalCount)
		}

		allPRs = append(allPRs, prs...)

		if !result.hasMore {
			break
		}
	}

	c.logger.Debug("fetched pull requests", "username", username, "count", len(allPRs))
	return allPRs, nil
}

// fetchIssuePage fetches a single page of issues.
func (c *Client) fetchIssuePage(ctx context.Context, username string, page, perPage int) ([]Issue, pageResult, error) {
	apiURL := fmt.Sprintf("https://api.github.com/search/issues?q=author:%s+type:issue&sort=created&order=desc&per_page=%d&page=%d",
		url.QueryEscape(username), perPage, page)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, http.NoBody)
	if err != nil {
		return nil, pageResult{}, fmt.Errorf("creating request: %w", err)
	}

	// SECURITY: Validate and sanitize GitHub token before use
	if c.githubToken != "" {
		token := c.githubToken
		// Validate token format to prevent injection attacks
		if c.isValidGitHubToken(token) {
			req.Header.Set("Authorization", "token "+token)
		}
	}

	resp, err := c.cachedHTTPDo(ctx, req)
	if err != nil {
		c.logger.Debug("failed to fetch issue page", "page", page, "error", err)
		return nil, pageResult{}, nil // Return what we have so far
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			c.logger.Debug("failed to close response body", "error", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			c.logger.Warn("🚩 GitHub API Error", "status", resp.StatusCode, "page", page)
			return nil, pageResult{}, nil
		}
		c.logger.Warn("🚩 GitHub API Error", "status", resp.StatusCode, "page", page, "body", string(body))
		return nil, pageResult{}, nil
	}

	var result struct {
		Items []struct {
			Title     string    `json:"title"`
			Body      string    `json:"body"`
			CreatedAt time.Time `json:"created_at"`
			HTMLURL   string    `json:"html_url"`
		} `json:"items"`
		TotalCount int `json:"total_count"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		c.logger.Debug("failed to decode issue response", "page", page, "error", err)
		return nil, pageResult{}, nil
	}

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
			Title:     item.Title,
			Body:      item.Body,
			CreatedAt: item.CreatedAt,
			HTMLURL:   item.HTMLURL,
			RepoName:  repo,
		})
	}

	hasMore := len(result.Items) == perPage
	return issues, pageResult{totalCount: result.TotalCount, hasMore: hasMore}, nil
}

// FetchIssues fetches issues for a user with default page limit.
func (c *Client) FetchIssues(ctx context.Context, username string) ([]Issue, error) {
	return c.FetchIssuesWithLimit(ctx, username, 2)
}

// FetchIssuesWithLimit fetches issues for a user with custom page limit.
func (c *Client) FetchIssuesWithLimit(ctx context.Context, username string, maxPages int) ([]Issue, error) {
	var allIssues []Issue
	const perPage = 100

	for page := 1; page <= maxPages; page++ {
		issues, result, err := c.fetchIssuePage(ctx, username, page, perPage)
		if err != nil {
			return allIssues, err
		}

		if page == 1 && result.totalCount > 0 {
			c.logger.Debug("GitHub issue search results", "username", username, "total_count", result.totalCount)
		}

		allIssues = append(allIssues, issues...)

		if !result.hasMore {
			break
		}
	}

	c.logger.Debug("fetched issues", "username", username, "count", len(allIssues))
	return allIssues, nil
}

// FetchUserComments fetches recent comments made by a user via GraphQL.
func (c *Client) FetchUserComments(ctx context.Context, username string) ([]Comment, error) {
	if c.githubToken == "" {
		c.logger.Debug("GitHub token required for GraphQL API", "username", username)
		return nil, errors.New("github token required for GraphQL API")
	}

	query := fmt.Sprintf(`{
		user(login: "%s") {
			issueComments(first: 100, orderBy: {field: UPDATED_AT, direction: DESC}) {
				nodes {
					createdAt
					body
					repository {
						nameWithOwner
					}
					issue {
						number
					}
					pullRequest {
						number
					}
				}
			}
			commitComments(first: 100) {
				nodes {
					createdAt
					body
					commit {
						repository {
							nameWithOwner
						}
						abbreviatedOid
					}
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

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.github.com/graphql", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "bearer "+c.githubToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.cachedHTTPDo(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("fetching comments: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			c.logger.Debug("failed to close response body", "error", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("GitHub GraphQL API returned status %d (failed to read response)", resp.StatusCode)
		}
		return nil, fmt.Errorf("graphQL API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse the response
	var result struct {
		Data struct {
			User struct {
				IssueComments struct {
					Nodes []struct {
						CreatedAt time.Time `json:"createdAt"`
						Issue     *struct {
							Number int `json:"number"`
						} `json:"issue"`
						PullRequest *struct {
							Number int `json:"number"`
						} `json:"pullRequest"`
						Body       string `json:"body"`
						Repository struct {
							NameWithOwner string `json:"nameWithOwner"`
						} `json:"repository"`
					} `json:"nodes"`
				} `json:"issueComments"`
				CommitComments struct {
					Nodes []struct {
						CreatedAt time.Time `json:"createdAt"`
						Body      string    `json:"body"`
						Commit    struct {
							Repository struct {
								NameWithOwner string `json:"nameWithOwner"`
							} `json:"repository"`
							AbbreviatedOid string `json:"abbreviatedOid"`
						} `json:"commit"`
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
		c.logger.Error("🚩 GitHub GraphQL API Error", "username", username, "errors", result.Errors)
		return nil, fmt.Errorf("graphQL error: %s", result.Errors[0].Message)
	}

	var comments []Comment

	// Add issue comments
	for _, node := range result.Data.User.IssueComments.Nodes {
		comments = append(comments, Comment{
			CreatedAt:  node.CreatedAt,
			Body:       node.Body,
			HTMLURL:    fmt.Sprintf("https://github.com/%s", node.Repository.NameWithOwner),
			Repository: node.Repository.NameWithOwner,
		})
	}

	// Add commit comments
	for _, node := range result.Data.User.CommitComments.Nodes {
		comments = append(comments, Comment{
			CreatedAt:  node.CreatedAt,
			Body:       node.Body,
			HTMLURL:    fmt.Sprintf("https://github.com/%s/commit/%s", node.Commit.Repository.NameWithOwner, node.Commit.AbbreviatedOid),
			Repository: node.Commit.Repository.NameWithOwner,
		})
	}

	c.logger.Debug("fetched user comments", "username", username,
		"issue_comments", len(result.Data.User.IssueComments.Nodes),
		"commit_comments", len(result.Data.User.CommitComments.Nodes),
		"total", len(comments))

	return comments, nil
}

// FetchOrganizations fetches organizations that a user belongs to.
func (c *Client) FetchOrganizations(ctx context.Context, username string) ([]Organization, error) {
	apiURL := fmt.Sprintf("https://api.github.com/users/%s/orgs", url.PathEscape(username))
	c.logger.Debug("fetching organizations from API", "url", apiURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	// SECURITY: Validate and sanitize GitHub token before use
	if c.githubToken != "" {
		token := c.githubToken
		// Validate token format to prevent injection attacks
		if c.isValidGitHubToken(token) {
			req.Header.Set("Authorization", "token "+token)
		}
	}

	resp, err := c.cachedHTTPDo(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("fetching organizations: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			c.logger.Debug("failed to close response body", "error", err)
		}
	}()

	// Check if response was from cache
	fromCache := resp.Header.Get("X-From-Cache") == "true"
	c.logger.Debug("organizations API response", "username", username, "cache", fromCache, "status", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("GitHub API returned status %d (failed to read response)", resp.StatusCode)
		}
		return nil, fmt.Errorf("github API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Read the response body first for debugging
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	previewLen := 100
	if len(body) < previewLen {
		previewLen = len(body)
	}
	c.logger.Debug("organizations API raw response", "username", username, "body_len", len(body), "body_preview", string(body[:previewLen]))

	var orgs []Organization
	if err := json.Unmarshal(body, &orgs); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return orgs, nil
}

// FetchUserRepositories fetches public repositories owned by a user.
func (c *Client) FetchUserRepositories(ctx context.Context, username string) ([]Repository, error) {
	// First try to get pinned repositories using GraphQL
	pinnedRepos, err := c.FetchPinnedRepositories(ctx, username)
	if err != nil {
		c.logger.Debug("failed to fetch pinned repositories, falling back to popular repos", "username", username, "error", err)
	}

	// If we have pinned repos, use those
	if len(pinnedRepos) > 0 {
		c.logger.Debug("using pinned repositories", "username", username, "count", len(pinnedRepos))
		return pinnedRepos, nil
	}

	// Fall back to most starred repositories
	popularRepos, err := c.FetchPopularRepositories(ctx, username)
	if err != nil {
		c.logger.Debug("failed to fetch popular repositories", "username", username, "error", err)
		return []Repository{}, err
	}

	c.logger.Debug("using popular repositories", "username", username, "count", len(popularRepos))
	return popularRepos, nil
}

// FetchPinnedRepositories fetches repositories pinned by a user via GraphQL.
func (c *Client) FetchPinnedRepositories(ctx context.Context, username string) ([]Repository, error) {
	if c.githubToken == "" {
		c.logger.Debug("GitHub token required for GraphQL API", "username", username)
		return nil, errors.New("github token required for GraphQL API")
	}

	query := fmt.Sprintf(`{
		user(login: "%s") {
			pinnedItems(first: 6, types: [REPOSITORY]) {
				nodes {
					... on Repository {
						name
						nameWithOwner
						description
						primaryLanguage {
							name
						}
						stargazerCount
						url
						isFork
					}
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

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.github.com/graphql", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "bearer "+c.githubToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.cachedHTTPDo(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("fetching pinned repositories: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			c.logger.Debug("failed to close response body", "error", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("GitHub GraphQL API returned status %d (failed to read response)", resp.StatusCode)
		}
		return nil, fmt.Errorf("graphQL API returned status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data struct {
			User struct {
				PinnedItems struct {
					Nodes []struct {
						Name            string `json:"name"`
						NameWithOwner   string `json:"nameWithOwner"`
						Description     string `json:"description"`
						PrimaryLanguage struct {
							Name string `json:"name"`
						} `json:"primaryLanguage"`
						URL            string `json:"url"`
						StargazerCount int    `json:"stargazerCount"`
						IsFork         bool   `json:"isFork"`
					} `json:"nodes"`
				} `json:"pinnedItems"`
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
		c.logger.Error("🚩 GitHub GraphQL API Error", "username", username, "errors", result.Errors)
		return nil, fmt.Errorf("graphQL error: %s", result.Errors[0].Message)
	}

	var repositories []Repository
	for _, node := range result.Data.User.PinnedItems.Nodes {
		repo := Repository{
			Name:        node.Name,
			FullName:    node.NameWithOwner,
			Description: node.Description,
			Language:    node.PrimaryLanguage.Name,
			StarCount:   node.StargazerCount,
			Fork:        node.IsFork,
			HTMLURL:     node.URL,
		}
		repositories = append(repositories, repo)
	}

	c.logger.Debug("fetched pinned repositories", "username", username, "count", len(repositories))
	return repositories, nil
}

// FetchPopularRepositories fetches user's most popular repositories sorted by stars.
func (c *Client) FetchPopularRepositories(ctx context.Context, username string) ([]Repository, error) {
	// Fetch all repos (up to 100) to ensure we don't miss important ones
	apiURL := fmt.Sprintf("https://api.github.com/users/%s/repos?sort=updated&per_page=100", url.PathEscape(username))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	// SECURITY: Validate and sanitize GitHub token before use
	if c.githubToken != "" {
		token := c.githubToken
		// Validate token format to prevent injection attacks
		if c.isValidGitHubToken(token) {
			req.Header.Set("Authorization", "token "+token)
		}
	}

	resp, err := c.cachedHTTPDo(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("fetching popular repositories: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			c.logger.Debug("failed to close response body", "error", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("GitHub API returned status %d (failed to read response)", resp.StatusCode)
		}
		return nil, fmt.Errorf("github API returned status %d: %s", resp.StatusCode, string(body))
	}

	var apiRepos []struct {
		Name            string `json:"name"`
		FullName        string `json:"full_name"`
		Description     string `json:"description"`
		Language        string `json:"language"`
		HTMLURL         string `json:"html_url"`
		StargazersCount int    `json:"stargazers_count"`
		Fork            bool   `json:"fork"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&apiRepos); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	var repositories []Repository
	for _, apiRepo := range apiRepos {
		repo := Repository{
			Name:        apiRepo.Name,
			FullName:    apiRepo.FullName,
			Description: apiRepo.Description,
			Language:    apiRepo.Language,
			StarCount:   apiRepo.StargazersCount,
			Fork:        apiRepo.Fork,
			HTMLURL:     apiRepo.HTMLURL,
		}
		repositories = append(repositories, repo)
	}

	c.logger.Debug("fetched popular repositories", "username", username, "count", len(repositories))
	return repositories, nil
}

// FetchProfileHTML fetches the raw HTML of a GitHub profile page.
func (c *Client) FetchProfileHTML(ctx context.Context, username string) string {
	profileURL := fmt.Sprintf("https://github.com/%s", url.PathEscape(username))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, profileURL, http.NoBody)
	if err != nil {
		return ""
	}

	// SECURITY: Validate and sanitize GitHub token before use
	if c.githubToken != "" && c.isValidGitHubToken(c.githubToken) {
		req.Header.Set("Authorization", "token "+c.githubToken)
	}

	resp, err := c.cachedHTTPDo(ctx, req)
	if err != nil {
		return ""
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			c.logger.Debug("failed to close response body", "error", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}

	return string(body)
}

// FetchSocialFromHTML scrapes GitHub profile HTML for social media links.
func (c *Client) FetchSocialFromHTML(ctx context.Context, username string) []string {
	profileURL := fmt.Sprintf("https://github.com/%s", username)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, profileURL, http.NoBody)
	if err != nil {
		c.logger.Debug("failed to create HTML request", "username", username, "error", err)
		return nil
	}

	req.Header.Set("User-Agent", "GitHub-Timezone-Detector/1.0")

	resp, err := c.cachedHTTPDo(ctx, req)
	if err != nil {
		c.logger.Debug("failed to fetch profile HTML", "username", username, "error", err)
		return nil
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			c.logger.Debug("failed to close response body", "error", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		c.logger.Debug("profile HTML returned non-200", "username", username, "status", resp.StatusCode)
		return nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024)) // 2MB limit
	if err != nil {
		c.logger.Debug("failed to read profile HTML", "username", username, "error", err)
		return nil
	}

	html := string(body)
	c.logger.Debug("scraped profile HTML", "username", username, "html_length", len(html))

	// Use the existing extraction function
	return ExtractSocialMediaFromHTML(html)
}

// FetchStarredRepositories fetches repositories the user has starred for additional timestamp data and repository details.
func (c *Client) FetchStarredRepositories(ctx context.Context, username string) ([]time.Time, []Repository, error) {
	apiURL := fmt.Sprintf("https://api.github.com/users/%s/starred?per_page=100", url.PathEscape(username))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, http.NoBody)
	if err != nil {
		return nil, nil, fmt.Errorf("creating request: %w", err)
	}

	// SECURITY: Validate and sanitize GitHub token before use
	if c.githubToken != "" {
		token := c.githubToken
		if c.isValidGitHubToken(token) {
			req.Header.Set("Authorization", "token "+token)
		}
	}

	// Request timestamps in response headers
	req.Header.Set("Accept", "application/vnd.github.v3.star+json")

	resp, err := c.cachedHTTPDo(ctx, req)
	if err != nil {
		return nil, nil, fmt.Errorf("fetching starred repositories: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			c.logger.Debug("failed to close response body", "error", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, nil, fmt.Errorf("GitHub API returned status %d (failed to read response)", resp.StatusCode)
		}
		return nil, nil, fmt.Errorf("github API returned status %d: %s", resp.StatusCode, string(body))
	}

	var starData []struct {
		StarredAt time.Time `json:"starred_at"`
		Repo      struct {
			Name        string `json:"name"`
			FullName    string `json:"full_name"`
			Description string `json:"description"`
		} `json:"repo"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&starData); err != nil {
		return nil, nil, fmt.Errorf("decoding response: %w", err)
	}

	var timestamps []time.Time
	var repos []Repository

	// Get the most recent 25 starred repos for analysis
	for i, star := range starData {
		if !star.StarredAt.IsZero() {
			timestamps = append(timestamps, star.StarredAt)
		}

		// Only include first 25 repos for Gemini analysis (most recent)
		if i < 25 {
			repos = append(repos, Repository{
				Name:        star.Repo.Name,
				FullName:    star.Repo.FullName,
				Description: star.Repo.Description,
			})
		}
	}

	c.logger.Debug("fetched starred repositories", "username", username, "count", len(timestamps))
	return timestamps, repos, nil
}

// FetchUserCommits fetches commit timestamps for a user's recent commits across their repositories.
func (c *Client) FetchUserCommits(ctx context.Context, username string) ([]time.Time, error) {
	return c.FetchUserCommitsWithLimit(ctx, username, 2) // Default to 2 pages (200 commits)
}

// FetchUserCommitsWithLimit fetches commit timestamps with a configurable page limit.
func (c *Client) FetchUserCommitsWithLimit(ctx context.Context, username string, maxPages int) ([]time.Time, error) {
	var allTimestamps []time.Time
	const perPage = 100

	// Fetch pages in parallel if maxPages > 1
	if maxPages > 1 {
		var wg sync.WaitGroup
		var mu sync.Mutex
		results := make([][]time.Time, maxPages)

		for page := 1; page <= maxPages; page++ {
			wg.Add(1)
			go func(p int) {
				defer wg.Done()
				timestamps, err := c.fetchCommitPage(ctx, username, p, perPage)
				if err != nil {
					// Log but continue - partial data is acceptable
					return
				}
				mu.Lock()
				results[p-1] = timestamps
				mu.Unlock()
			}(page)
		}

		wg.Wait()

		// Combine results in order
		for _, timestamps := range results {
			allTimestamps = append(allTimestamps, timestamps...)
		}
	} else {
		// Single page, no need for parallelization
		var err error
		allTimestamps, err = c.fetchCommitPage(ctx, username, 1, perPage)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch commits for user %s: %w", username, err)
		}
	}

	c.logger.Debug("fetched user commits", "username", username, "count", len(allTimestamps), "pages", maxPages)
	return allTimestamps, nil
}

// fetchCommitPage fetches a single page of commits.
func (c *Client) fetchCommitPage(ctx context.Context, username string, page int, perPage int) ([]time.Time, error) {
	var timestamps []time.Time

	// Use GitHub Search API to find commits by this user
	// Note: This requires authentication for better rate limits
	searchURL := fmt.Sprintf("https://api.github.com/search/commits?q=author:%s&sort=author-date&order=desc&per_page=%d&page=%d",
		url.QueryEscape(username), perPage, page)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, http.NoBody)
	if err != nil {
		return timestamps, fmt.Errorf("creating request: %w", err)
	}

	// The commit search API requires this Accept header
	req.Header.Set("Accept", "application/vnd.github.cloak-preview+json")

	// SECURITY: Validate and sanitize GitHub token before use
	if c.githubToken != "" {
		token := c.githubToken
		if c.isValidGitHubToken(token) {
			req.Header.Set("Authorization", "token "+token)
		}
	}

	resp, err := c.cachedHTTPDo(ctx, req)
	if err != nil {
		c.logger.Debug("failed to fetch commit page", "page", page, "error", err)
		return timestamps, err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			c.logger.Debug("failed to close response body", "error", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read response body: %w", err)
		}
		c.logger.Warn("🚩 GitHub API Error", "status", resp.StatusCode, "page", page, "body", string(body))
		return timestamps, fmt.Errorf("github API returned status %d", resp.StatusCode)
	}

	var searchResult struct {
		Items []struct {
			Commit struct {
				Author struct {
					Date time.Time `json:"date"`
				} `json:"author"`
				Committer struct {
					Date time.Time `json:"date"`
				} `json:"committer"`
			} `json:"commit"`
		} `json:"items"`
		TotalCount int `json:"total_count"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&searchResult); err != nil {
		c.logger.Debug("failed to decode commit response", "page", page, "error", err)
		return timestamps, err
	}

	if page == 1 {
		c.logger.Debug("GitHub commit search results", "username", username, "total_count", searchResult.TotalCount)
	}

	for _, item := range searchResult.Items {
		// Use author date (when the commit was originally authored)
		if !item.Commit.Author.Date.IsZero() {
			timestamps = append(timestamps, item.Commit.Author.Date)
		}
	}

	return timestamps, nil
}

// FetchUserCommitActivities fetches commit activities with repository information using GraphQL.
func (c *Client) FetchUserCommitActivities(ctx context.Context, username string) ([]CommitActivity, error) {
	return c.FetchUserCommitActivitiesWithLimit(ctx, username, 1) // Default to 1 page (100 commits)
}

// FetchUserCommitActivitiesGraphQL fetches commit activities using REST API fallback.
// NOTE: GitHub's GraphQL search API doesn't support COMMIT type, so we use REST API.
func (c *Client) FetchUserCommitActivitiesGraphQL(ctx context.Context, username string, maxCommits int) ([]CommitActivity, error) {
	// GitHub's GraphQL search API doesn't support searching commits directly
	// Fall back to using the search API to find commits
	c.logger.Debug("using REST API for commit activities (GraphQL doesn't support commit search)", "username", username)

	if maxCommits <= 0 {
		maxCommits = 100
	}

	// Calculate pages needed (100 commits per page max from search API)
	maxPages := (maxCommits + 99) / 100 // Round up
	if maxPages > 10 {
		maxPages = 10 // Reasonable limit
	}

	var allActivities []CommitActivity

	// Use GitHub search API to find commits by the user
	for page := 1; page <= maxPages; page++ {
		apiURL := fmt.Sprintf("https://api.github.com/search/commits?q=author:%s&sort=committer-date&order=desc&per_page=100&page=%d",
			url.QueryEscape(username), page)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, http.NoBody)
		if err != nil {
			return allActivities, fmt.Errorf("creating commit search request: %w", err)
		}

		// SECURITY: Validate and sanitize GitHub token before use
		if c.githubToken != "" && c.isValidGitHubToken(c.githubToken) {
			req.Header.Set("Authorization", "token "+c.githubToken)
		}

		// Required for commit search API
		req.Header.Set("Accept", "application/vnd.github.cloak-preview")

		resp, err := c.cachedHTTPDo(ctx, req)
		if err != nil {
			c.logger.Debug("commit search API request failed", "page", page, "error", err)
			break
		}

		// Process response and close body immediately to avoid resource leak
		var searchResult struct {
			Items []struct {
				Repository struct {
					FullName string `json:"full_name"`
					ID       int    `json:"id"`
				} `json:"repository"`
				Commit struct {
					Author struct {
						Name  string    `json:"name"`
						Email string    `json:"email"`
						Date  time.Time `json:"date"`
					} `json:"author"`
					Committer struct {
						Name  string    `json:"name"`
						Email string    `json:"email"`
						Date  time.Time `json:"date"`
					} `json:"committer"`
				} `json:"commit"`
			} `json:"items"`
			TotalCount int `json:"total_count"`
		}

		if resp.StatusCode != http.StatusOK {
			body, readErr := io.ReadAll(resp.Body)
			_ = resp.Body.Close() //nolint:errcheck // best effort close
			if readErr != nil {
				c.logger.Warn("🚩 GitHub Commit Search API Error", "status", resp.StatusCode, "page", page, "read_error", readErr)
			} else {
				c.logger.Warn("🚩 GitHub Commit Search API Error", "status", resp.StatusCode, "page", page, "body", string(body))
			}
			break
		}

		err = json.NewDecoder(resp.Body).Decode(&searchResult)
		_ = resp.Body.Close() //nolint:errcheck // best effort close
		if err != nil {
			c.logger.Debug("failed to decode commit search response", "page", page, "error", err)
			break
		}

		// Convert to CommitActivity objects
		for i := range searchResult.Items {
			item := &searchResult.Items[i]
			if item.Repository.FullName != "" && !item.Commit.Committer.Date.IsZero() {
				allActivities = append(allActivities, CommitActivity{
					AuthorDate:     item.Commit.Committer.Date,
					Repository:     item.Repository.FullName,
					RepositoryID:   item.Repository.ID,
					AuthorName:     item.Commit.Author.Name,
					AuthorEmail:    item.Commit.Author.Email,
					CommitterName:  item.Commit.Committer.Name,
					CommitterEmail: item.Commit.Committer.Email,
				})
			}
		}

		// If this page had fewer than 100 results, we're done
		if len(searchResult.Items) < 100 {
			break
		}

		// Respect maxCommits limit
		if len(allActivities) >= maxCommits {
			allActivities = allActivities[:maxCommits]
			break
		}
	}

	c.logger.Debug("fetched commit activities via REST API", "username", username, "count", len(allActivities))
	return allActivities, nil
}

// FetchUserCommitActivitiesWithLimit fetches commit activities with repository info and configurable page limit.
func (c *Client) FetchUserCommitActivitiesWithLimit(ctx context.Context, username string, maxPages int) ([]CommitActivity, error) {
	var allCommits []CommitActivity
	const perPage = 100

	for page := 1; page <= maxPages; page++ {
		commits, err := c.fetchCommitActivitiesPage(ctx, username, page, perPage)
		if err != nil {
			c.logger.Debug("failed to fetch commit activities page", "page", page, "error", err)
			// Return partial data if we got some results from earlier pages
			if len(allCommits) > 0 {
				break
			}
			return nil, fmt.Errorf("failed to fetch commit activities for user %s: %w", username, err)
		}

		allCommits = append(allCommits, commits...)

		// If we got fewer results than perPage, we've reached the end
		if len(commits) < perPage {
			break
		}
	}

	c.logger.Debug("fetched user commit activities", "username", username, "count", len(allCommits), "pages", maxPages)
	return allCommits, nil
}

// fetchCommitActivitiesPage fetches a single page of commit activities with repository information.
func (c *Client) fetchCommitActivitiesPage(ctx context.Context, username string, page int, perPage int) ([]CommitActivity, error) {
	var activities []CommitActivity

	// Use GitHub Search API to find commits by this user
	searchURL := fmt.Sprintf("https://api.github.com/search/commits?q=author:%s&sort=author-date&order=desc&per_page=%d&page=%d",
		url.QueryEscape(username), perPage, page)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, http.NoBody)
	if err != nil {
		return activities, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github.cloak-preview+json")

	if c.githubToken != "" {
		token := c.githubToken
		if c.isValidGitHubToken(token) {
			req.Header.Set("Authorization", "token "+token)
		}
	}

	resp, err := c.cachedHTTPDo(ctx, req)
	if err != nil {
		return activities, err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			c.logger.Debug("failed to close response body", "error", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read response body: %w", err)
		}
		c.logger.Warn("🚩 GitHub API Error", "status", resp.StatusCode, "page", page, "body", string(body))
		return activities, fmt.Errorf("GitHub API error: %d", resp.StatusCode)
	}

	// Enhanced response structure that includes repository information and email data
	var searchResult struct {
		Items []struct {
			Repository struct {
				FullName string `json:"full_name"`
				ID       int    `json:"id"`
			} `json:"repository"`
			Commit struct {
				Author struct {
					Name  string    `json:"name"`
					Email string    `json:"email"`
					Date  time.Time `json:"date"`
				} `json:"author"`
				Committer struct {
					Name  string    `json:"name"`
					Email string    `json:"email"`
					Date  time.Time `json:"date"`
				} `json:"committer"`
			} `json:"commit"`
		} `json:"items"`
		TotalCount int `json:"total_count"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&searchResult); err != nil {
		return activities, err
	}

	if page == 1 {
		c.logger.Debug("GitHub commit activities search results", "username", username, "total_count", searchResult.TotalCount)
	}

	for i := range searchResult.Items {
		item := &searchResult.Items[i]
		if !item.Commit.Author.Date.IsZero() && item.Repository.FullName != "" {
			activities = append(activities, CommitActivity{
				AuthorDate:     item.Commit.Author.Date,
				Repository:     item.Repository.FullName,
				RepositoryID:   item.Repository.ID,
				AuthorName:     item.Commit.Author.Name,
				AuthorEmail:    item.Commit.Author.Email,
				CommitterName:  item.Commit.Committer.Name,
				CommitterEmail: item.Commit.Committer.Email,
			})
		}
	}

	return activities, nil
}

// FetchUserSSHKeys fetches public SSH keys for a user.
func (c *Client) FetchUserSSHKeys(ctx context.Context, username string) ([]SSHKey, error) {
	apiURL := fmt.Sprintf("https://api.github.com/users/%s/keys", url.PathEscape(username))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	// Add GitHub token if available for higher rate limits
	if c.githubToken != "" && c.isValidGitHubToken(c.githubToken) {
		req.Header.Set("Authorization", "token "+c.githubToken)
	}

	resp, err := c.cachedHTTPDo(ctx, req)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			c.logger.Debug("failed to close response body", "error", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read response body: %w", err)
		}
		c.logger.Warn("🚩 GitHub SSH Keys API Error", "username", username, "status", resp.StatusCode, "body", string(body))
		return []SSHKey{}, nil // Return empty slice rather than error for non-critical data
	}

	var keys []SSHKey
	if err := json.NewDecoder(resp.Body).Decode(&keys); err != nil {
		return nil, fmt.Errorf("decoding SSH keys response: %w", err)
	}

	c.logger.Debug("fetched SSH keys", "username", username, "count", len(keys))
	return keys, nil
}
