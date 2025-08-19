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
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
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

// FetchPullRequests fetches pull requests for a user with default page limit.
func (c *Client) FetchPullRequests(ctx context.Context, username string) ([]PullRequest, error) {
	return c.FetchPullRequestsWithLimit(ctx, username, 2)
}

// FetchPullRequestsWithLimit fetches pull requests for a user with custom page limit.
func (c *Client) FetchPullRequestsWithLimit(ctx context.Context, username string, maxPages int) ([]PullRequest, error) {
	var allPRs []PullRequest
	const perPage = 100

	for page := 1; page <= maxPages; page++ {
		apiURL := fmt.Sprintf("https://api.github.com/search/issues?q=author:%s+type:pr&sort=created&order=desc&per_page=%d&page=%d",
			url.QueryEscape(username), perPage, page)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, http.NoBody)
		if err != nil {
			return allPRs, fmt.Errorf("creating request: %w", err)
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
			break // Return what we have so far
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				c.logger.Debug("failed to close response body", "error", err)
			}
		}()

		if resp.StatusCode != http.StatusOK {
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				c.logger.Debug("GitHub API error", "status", resp.StatusCode, "page", page)
				break
			}
			c.logger.Debug("GitHub API error", "status", resp.StatusCode, "page", page, "body", string(body))
			break
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
			break
		}

		if page == 1 {
			c.logger.Debug("GitHub PR search results", "username", username, "total_count", result.TotalCount)
		}

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
			allPRs = append(allPRs, PullRequest{
				Title:     item.Title,
				Body:      item.Body,
				CreatedAt: item.CreatedAt,
				HTMLURL:   item.HTMLURL,
				RepoName:  repo,
			})
		}

		// If we got fewer items than requested, we've reached the end
		if len(result.Items) < perPage {
			break
		}
	}

	c.logger.Debug("fetched pull requests", "username", username, "count", len(allPRs))
	return allPRs, nil
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
		apiURL := fmt.Sprintf("https://api.github.com/search/issues?q=author:%s+type:issue&sort=created&order=desc&per_page=%d&page=%d",
			url.QueryEscape(username), perPage, page)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, http.NoBody)
		if err != nil {
			return allIssues, fmt.Errorf("creating request: %w", err)
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
			break // Return what we have so far
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				c.logger.Debug("failed to close response body", "error", err)
			}
		}()

		if resp.StatusCode != http.StatusOK {
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				c.logger.Debug("GitHub API error", "status", resp.StatusCode, "page", page)
				break
			}
			c.logger.Debug("GitHub API error", "status", resp.StatusCode, "page", page, "body", string(body))
			break
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
			break
		}

		if page == 1 {
			c.logger.Debug("GitHub issue search results", "username", username, "total_count", result.TotalCount)
		}

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
			allIssues = append(allIssues, Issue{
				Title:     item.Title,
				Body:      item.Body,
				CreatedAt: item.CreatedAt,
				HTMLURL:   item.HTMLURL,
				RepoName:  repo,
			})
		}

		// If we got fewer items than requested, we've reached the end
		if len(result.Items) < perPage {
			break
		}
	}

	c.logger.Debug("fetched issues", "username", username, "count", len(allIssues))
	return allIssues, nil
}

// FetchUserWithGraphQL fetches user data including social accounts via GraphQL.
func (c *Client) FetchUserWithGraphQL(ctx context.Context, username string) *GitHubUser {
	c.logger.Debug("attempting GraphQL user fetch", "username", username)
	query := fmt.Sprintf(`{
		user(login: "%s") {
			login
			name
			location
			company
			bio
			websiteUrl
			twitterUsername
			createdAt
			socialAccounts(first: 10) {
				nodes {
					displayName
					url
					provider
				}
			}
		}
	}`, username)

	reqBody := map[string]string{
		"query": query,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		c.logger.Debug("failed to marshal GraphQL query", "error", err)
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.github.com/graphql", bytes.NewBuffer(jsonBody))
	if err != nil {
		c.logger.Debug("failed to create GraphQL request", "error", err)
		return nil
	}

	req.Header.Set("Authorization", "Bearer "+c.githubToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.cachedHTTPDo(ctx, req)
	if err != nil {
		c.logger.Debug("failed to execute GraphQL request", "error", err)
		return nil
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			c.logger.Debug("failed to close response body", "error", err)
		}
	}()

	// Check if response was from cache
	fromCache := resp.Header.Get("X-From-Cache") == "true"

	var result struct {
		Data struct {
			User struct {
				Login           string `json:"login"`
				Name            string `json:"name"`
				Location        string `json:"location"`
				Company         string `json:"company"`
				Bio             string `json:"bio"`
				Email           string `json:"email"`
				WebsiteURL      string `json:"websiteUrl"`
				TwitterUsername string `json:"twitterUsername"`
				CreatedAt       string `json:"createdAt"`
				SocialAccounts  struct {
					Nodes []struct {
						DisplayName string `json:"displayName"`
						URL         string `json:"url"`
						Provider    string `json:"provider"`
					} `json:"nodes"`
				} `json:"socialAccounts"`
			} `json:"user"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		c.logger.Debug("failed to decode GraphQL response", "error", err)
		return nil
	}

	if len(result.Errors) > 0 {
		c.logger.Debug("GraphQL user profile query failed", 
			"username", username,
			"error", result.Errors[0].Message,
			"query_type", "user_profile_with_social_accounts")
		return nil
	}

	user := &GitHubUser{
		Login:         result.Data.User.Login,
		Name:          result.Data.User.Name,
		Location:      result.Data.User.Location,
		Company:       result.Data.User.Company,
		Bio:           result.Data.User.Bio,
		Email:         result.Data.User.Email,
		Blog:          result.Data.User.WebsiteURL,
		TwitterHandle: result.Data.User.TwitterUsername,
	}

	// Process social accounts
	c.logger.Debug("GraphQL social accounts raw", "username", username,
		"accounts_count", len(result.Data.User.SocialAccounts.Nodes))

	for _, account := range result.Data.User.SocialAccounts.Nodes {
		c.logger.Debug("processing social account", "provider", account.Provider,
			"url", account.URL, "display_name", account.DisplayName)

		// Add social links to bio for extraction
		if user.Bio != "" {
			user.Bio += " | "
		}

		// Include provider information in bio for better context
		if account.Provider != "" && account.Provider != "GENERIC" {
			user.Bio += fmt.Sprintf("[%s] ", account.Provider)
		}
		user.Bio += account.URL

		// Use first website-like URL as blog if not set
		if user.Blog == "" && account.Provider == "GENERIC" {
			user.Blog = account.URL
		}
	}

	c.logger.Debug("fetched user via GraphQL", "username", username, "name", user.Name,
		"blog", user.Blog, "social_accounts", len(result.Data.User.SocialAccounts.Nodes), "cache", fromCache)

	return user
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
		return nil, fmt.Errorf("GitHub GraphQL API returned status %d: %s", resp.StatusCode, string(body))
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
		c.logger.Debug("GitHub GraphQL errors", "username", username, "errors", result.Errors)
		return nil, fmt.Errorf("GraphQL errors: %v", result.Errors[0].Message)
	}

	var comments []Comment

	// Add issue comments
	for _, node := range result.Data.User.IssueComments.Nodes {
		comments = append(comments, Comment{
			CreatedAt: node.CreatedAt,
			Body:      node.Body,
			HTMLURL:   fmt.Sprintf("https://github.com/%s", node.Repository.NameWithOwner),
		})
	}

	// Add commit comments
	for _, node := range result.Data.User.CommitComments.Nodes {
		comments = append(comments, Comment{
			CreatedAt: node.CreatedAt,
			Body:      node.Body,
			HTMLURL:   fmt.Sprintf("https://github.com/%s/commit/%s", node.Commit.Repository.NameWithOwner, node.Commit.AbbreviatedOid),
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
		return nil, fmt.Errorf("GitHub API returned status %d: %s", resp.StatusCode, string(body))
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

// FetchUser fetches comprehensive user data, trying GraphQL first then REST API.
func (c *Client) FetchUser(ctx context.Context, username string) *GitHubUser {
	// First try to get enhanced data via GraphQL if we have a token
	if c.githubToken != "" && c.isValidGitHubToken(c.githubToken) {
		if user := c.FetchUserWithGraphQL(ctx, username); user != nil {
			// If GraphQL didn't find social accounts, try HTML scraping
			if !strings.Contains(user.Bio, "@") && !strings.Contains(user.Bio, "http") {
				c.logger.Debug("GraphQL found no social accounts, trying HTML scraping", "username", username)
				if htmlSocials := c.FetchSocialFromHTML(ctx, username); len(htmlSocials) > 0 {
					for _, social := range htmlSocials {
						if user.Bio != "" {
							user.Bio += " | "
						}
						user.Bio += social
					}
					c.logger.Debug("HTML scraping found social accounts", "username", username, "count", len(htmlSocials))
				}
			}
			return user
		}
	}

	// Fallback to REST API
	apiURL := fmt.Sprintf("https://api.github.com/users/%s", url.PathEscape(username))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, http.NoBody)
	if err != nil {
		return nil
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
		c.logger.Debug("failed to fetch user", "username", username, "error", err)
		return nil
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			c.logger.Debug("failed to close response body", "error", err)
		}
	}()

	var user GitHubUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		c.logger.Debug("failed to decode user", "username", username, "error", err)
		return nil
	}

	c.logger.Debug("fetched user full name", "username", username, "name", user.Name)

	return &user
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
		return nil, fmt.Errorf("GitHub GraphQL API returned status %d: %s", resp.StatusCode, string(body))
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
		c.logger.Debug("GitHub GraphQL errors", "username", username, "errors", result.Errors)
		return nil, fmt.Errorf("GraphQL errors: %v", result.Errors[0].Message)
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
		return nil, fmt.Errorf("GitHub API returned status %d: %s", resp.StatusCode, string(body))
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
		return nil, nil, fmt.Errorf("GitHub API returned status %d: %s", resp.StatusCode, string(body))
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

// fetchUserCommits fetches commit timestamps for a user's recent commits across their repositories.
func (c *Client) FetchUserCommits(ctx context.Context, username string) ([]time.Time, error) {
	return c.FetchUserCommitsWithLimit(ctx, username, 2) // Default to 2 pages (200 commits)
}

// fetchUserCommitsWithLimit fetches commit timestamps with a configurable page limit.
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
		c.logger.Debug("GitHub API error", "status", resp.StatusCode, "page", page, "body", string(body))
		return timestamps, fmt.Errorf("GitHub API error: %d", resp.StatusCode)
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
