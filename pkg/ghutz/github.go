package ghutz

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// PublicEvent represents a GitHub public event.
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

// fetchPublicEvents fetches public events (limited to last 30 days by GitHub API).
func (d *Detector) fetchPublicEvents(ctx context.Context, username string) ([]PublicEvent, error) {
	const maxPages = 3 // 100 events per page * 3 = 300 (GitHub's max)
	const perPage = 100

	var allEvents []PublicEvent

	for page := 1; page <= maxPages; page++ {
		apiURL := fmt.Sprintf("https://api.github.com/users/%s/events/public?per_page=%d&page=%d", username, perPage, page)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, http.NoBody)
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


// fetchUserGists fetches gist timestamps for a user.
func (d *Detector) fetchUserGists(ctx context.Context, username string) ([]time.Time, error) {
	apiURL := fmt.Sprintf("https://api.github.com/users/%s/gists?per_page=100", username)
	
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	
	if d.githubToken != "" && d.isValidGitHubToken(d.githubToken) {
		req.Header.Set("Authorization", "token "+d.githubToken)
	}
	
	resp, err := d.cachedHTTPDo(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
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
		timestamps = append(timestamps, gist.CreatedAt)
		timestamps = append(timestamps, gist.UpdatedAt)
	}
	
	d.logger.Debug("fetched gist timestamps", "username", username, "count", len(timestamps))
	return timestamps, nil
}

func (d *Detector) fetchPullRequests(ctx context.Context, username string) ([]PullRequest, error) {
	var allPRs []PullRequest
	const perPage = 100
	const maxPages = 2 // Fetch up to 200 PRs
	
	for page := 1; page <= maxPages; page++ {
		apiURL := fmt.Sprintf("https://api.github.com/search/issues?q=author:%s+type:pr&sort=created&order=desc&per_page=%d&page=%d", 
			username, perPage, page)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, http.NoBody)
		if err != nil {
			return allPRs, fmt.Errorf("creating request: %w", err)
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
			d.logger.Debug("failed to fetch PR page", "page", page, "error", err)
			break // Return what we have so far
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				d.logger.Debug("failed to close response body", "error", err)
			}
		}()

		if resp.StatusCode != http.StatusOK {
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				d.logger.Debug("GitHub API error", "status", resp.StatusCode, "page", page)
				break
			}
			d.logger.Debug("GitHub API error", "status", resp.StatusCode, "page", page, "body", string(body))
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
			d.logger.Debug("failed to decode PR response", "page", page, "error", err)
			break
		}

		if page == 1 {
			d.logger.Debug("GitHub PR search results", "username", username, "total_count", result.TotalCount)
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
				Title:      item.Title,
				Body:       item.Body,
				CreatedAt:  item.CreatedAt,
				HTMLURL:    item.HTMLURL,
				Repository: repo,
			})
		}
		
		// If we got fewer items than requested, we've reached the end
		if len(result.Items) < perPage {
			break
		}
	}

	d.logger.Debug("fetched pull requests", "username", username, "count", len(allPRs))
	return allPRs, nil
}

func (d *Detector) fetchIssues(ctx context.Context, username string) ([]Issue, error) {
	var allIssues []Issue
	const perPage = 100
	const maxPages = 2 // Fetch up to 200 issues
	
	for page := 1; page <= maxPages; page++ {
		apiURL := fmt.Sprintf("https://api.github.com/search/issues?q=author:%s+type:issue&sort=created&order=desc&per_page=%d&page=%d", 
			username, perPage, page)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, http.NoBody)
		if err != nil {
			return allIssues, fmt.Errorf("creating request: %w", err)
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
			d.logger.Debug("failed to fetch issue page", "page", page, "error", err)
			break // Return what we have so far
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				d.logger.Debug("failed to close response body", "error", err)
			}
		}()

		if resp.StatusCode != http.StatusOK {
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				d.logger.Debug("GitHub API error", "status", resp.StatusCode, "page", page)
				break
			}
			d.logger.Debug("GitHub API error", "status", resp.StatusCode, "page", page, "body", string(body))
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
			d.logger.Debug("failed to decode issue response", "page", page, "error", err)
			break
		}

		if page == 1 {
			d.logger.Debug("GitHub issue search results", "username", username, "total_count", result.TotalCount)
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
				Title:      item.Title,
				Body:       item.Body,
				CreatedAt:  item.CreatedAt,
				HTMLURL:    item.HTMLURL,
				Repository: repo,
			})
		}
		
		// If we got fewer items than requested, we've reached the end
		if len(result.Items) < perPage {
			break
		}
	}

	d.logger.Debug("fetched issues", "username", username, "count", len(allIssues))
	return allIssues, nil
}

// fetchUserWithGraphQL fetches user data including social accounts via GraphQL
func (d *Detector) fetchUserWithGraphQL(ctx context.Context, username string) *GitHubUser {
	d.logger.Debug("attempting GraphQL user fetch", "username", username)
	query := fmt.Sprintf(`{
		user(login: "%s") {
			login
			name
			location
			company
			bio
			email
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
		d.logger.Debug("failed to marshal GraphQL query", "error", err)
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.github.com/graphql", bytes.NewBuffer(jsonBody))
	if err != nil {
		d.logger.Debug("failed to create GraphQL request", "error", err)
		return nil
	}

	req.Header.Set("Authorization", "Bearer "+d.githubToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.cachedHTTPDo(ctx, req)
	if err != nil {
		d.logger.Debug("failed to execute GraphQL request", "error", err)
		return nil
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			d.logger.Debug("failed to close response body", "error", err)
		}
	}()

	var result struct {
		Data struct {
			User struct {
				Login           string `json:"login"`
				Name            string `json:"name"`
				Location        string `json:"location"`
				Company         string `json:"company"`
				Bio             string `json:"bio"`
				Email           string `json:"email"`
				WebsiteUrl      string `json:"websiteUrl"`
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
		d.logger.Debug("failed to decode GraphQL response", "error", err)
		return nil
	}

	if len(result.Errors) > 0 {
		d.logger.Debug("GraphQL query returned errors", "error", result.Errors[0].Message)
		return nil
	}

	user := &GitHubUser{
		Login:           result.Data.User.Login,
		Name:            result.Data.User.Name,
		Location:        result.Data.User.Location,
		Company:         result.Data.User.Company,
		Bio:             result.Data.User.Bio,
		Email:           result.Data.User.Email,
		Blog:            result.Data.User.WebsiteUrl,
		TwitterUsername: result.Data.User.TwitterUsername,
		CreatedAt:       result.Data.User.CreatedAt,
	}

	// Process social accounts
	d.logger.Debug("GraphQL social accounts raw", "username", username, 
		"accounts_count", len(result.Data.User.SocialAccounts.Nodes))
	
	for _, account := range result.Data.User.SocialAccounts.Nodes {
		d.logger.Debug("processing social account", "provider", account.Provider, 
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

	d.logger.Debug("fetched user via GraphQL", "username", username, "name", user.Name, 
		"blog", user.Blog, "social_accounts", len(result.Data.User.SocialAccounts.Nodes))

	return user
}

func (d *Detector) fetchUserComments(ctx context.Context, username string) ([]Comment, error) {
	if d.githubToken == "" {
		d.logger.Debug("GitHub token required for GraphQL API", "username", username)
		return nil, errors.New("GitHub token required for GraphQL API")
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
						CreatedAt  time.Time `json:"createdAt"`
						Body       string    `json:"body"`
						Repository struct {
							NameWithOwner string `json:"nameWithOwner"`
						} `json:"repository"`
						Issue *struct {
							Number int `json:"number"`
						} `json:"issue"`
						PullRequest *struct {
							Number int `json:"number"`
						} `json:"pullRequest"`
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
		d.logger.Debug("GitHub GraphQL errors", "username", username, "errors", result.Errors)
		return nil, fmt.Errorf("GraphQL errors: %v", result.Errors[0].Message)
	}

	var comments []Comment

	// Add issue comments
	for _, node := range result.Data.User.IssueComments.Nodes {
		comments = append(comments, Comment{
			CreatedAt:  node.CreatedAt,
			Type:       "issue",
			Body:       node.Body,
			Repository: node.Repository.NameWithOwner,
		})
	}

	// Add commit comments
	for _, node := range result.Data.User.CommitComments.Nodes {
		comments = append(comments, Comment{
			CreatedAt:  node.CreatedAt,
			Type:       "commit",
			Body:       node.Body,
			Repository: node.Commit.Repository.NameWithOwner,
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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, http.NoBody)
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
	// First try to get enhanced data via GraphQL if we have a token
	if d.githubToken != "" && d.isValidGitHubToken(d.githubToken) {
		if user := d.fetchUserWithGraphQL(ctx, username); user != nil {
			// If GraphQL didn't find social accounts, try HTML scraping
			if !strings.Contains(user.Bio, "@") && !strings.Contains(user.Bio, "http") {
				d.logger.Debug("GraphQL found no social accounts, trying HTML scraping", "username", username)
				if htmlSocials := d.fetchSocialFromHTML(ctx, username); len(htmlSocials) > 0 {
					for _, social := range htmlSocials {
						if user.Bio != "" {
							user.Bio += " | "
						}
						user.Bio += social
					}
					d.logger.Debug("HTML scraping found social accounts", "username", username, "count", len(htmlSocials))
				}
			}
			return user
		}
	}
	
	// Fallback to REST API
	url := fmt.Sprintf("https://api.github.com/users/%s", username)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
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
		d.logger.Debug("failed to fetch user", "username", username, "error", err)
		return nil
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			d.logger.Debug("failed to close response body", "error", err)
		}
	}()

	var user GitHubUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		d.logger.Debug("failed to decode user", "username", username, "error", err)
		return nil
	}
	
	d.logger.Debug("fetched user full name", "username", username, "name", user.Name)

	return &user
}

func (d *Detector) fetchUserRepositories(ctx context.Context, username string) ([]Repository, error) {
	// First try to get pinned repositories using GraphQL
	pinnedRepos, err := d.fetchPinnedRepositories(ctx, username)
	if err != nil {
		d.logger.Debug("failed to fetch pinned repositories, falling back to popular repos", "username", username, "error", err)
	}

	// If we have pinned repos, use those
	if len(pinnedRepos) > 0 {
		d.logger.Debug("using pinned repositories", "username", username, "count", len(pinnedRepos))
		return pinnedRepos, nil
	}

	// Fall back to most starred repositories
	popularRepos, err := d.fetchPopularRepositories(ctx, username)
	if err != nil {
		d.logger.Debug("failed to fetch popular repositories", "username", username, "error", err)
		return []Repository{}, err
	}

	d.logger.Debug("using popular repositories", "username", username, "count", len(popularRepos))
	return popularRepos, nil
}

func (d *Detector) fetchPinnedRepositories(ctx context.Context, username string) ([]Repository, error) {
	if d.githubToken == "" {
		d.logger.Debug("GitHub token required for GraphQL API", "username", username)
		return nil, errors.New("GitHub token required for GraphQL API")
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

	req.Header.Set("Authorization", "bearer "+d.githubToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.cachedHTTPDo(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("fetching pinned repositories: %w", err)
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
		d.logger.Debug("GitHub GraphQL errors", "username", username, "errors", result.Errors)
		return nil, fmt.Errorf("GraphQL errors: %v", result.Errors[0].Message)
	}

	var repositories []Repository
	for _, node := range result.Data.User.PinnedItems.Nodes {
		repo := Repository{
			Name:            node.Name,
			FullName:        node.NameWithOwner,
			Description:     node.Description,
			Language:        node.PrimaryLanguage.Name,
			StargazersCount: node.StargazerCount,
			IsPinned:        true,
			IsFork:          node.IsFork,
			HTMLURL:         node.URL,
		}
		repositories = append(repositories, repo)
	}

	d.logger.Debug("fetched pinned repositories", "username", username, "count", len(repositories))
	return repositories, nil
}

func (d *Detector) fetchPopularRepositories(ctx context.Context, username string) ([]Repository, error) {
	// Fetch all repos (up to 100) to ensure we don't miss important ones
	apiURL := fmt.Sprintf("https://api.github.com/users/%s/repos?sort=updated&per_page=100", username)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, http.NoBody)
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
		return nil, fmt.Errorf("fetching popular repositories: %w", err)
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
			Name:            apiRepo.Name,
			FullName:        apiRepo.FullName,
			Description:     apiRepo.Description,
			Language:        apiRepo.Language,
			StargazersCount: apiRepo.StargazersCount,
			IsPinned:        false,
			IsFork:          apiRepo.Fork,
			HTMLURL:         apiRepo.HTMLURL,
		}
		repositories = append(repositories, repo)
	}

	d.logger.Debug("fetched popular repositories", "username", username, "count", len(repositories))
	return repositories, nil
}

// fetchSocialFromHTML scrapes GitHub profile HTML for social media links
func (d *Detector) fetchSocialFromHTML(ctx context.Context, username string) []string {
	url := fmt.Sprintf("https://github.com/%s", username)
	
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		d.logger.Debug("failed to create HTML request", "username", username, "error", err)
		return nil
	}
	
	req.Header.Set("User-Agent", "GitHub-Timezone-Detector/1.0")
	
	resp, err := d.cachedHTTPDo(ctx, req)
	if err != nil {
		d.logger.Debug("failed to fetch profile HTML", "username", username, "error", err)
		return nil
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			d.logger.Debug("failed to close response body", "error", err)
		}
	}()
	
	if resp.StatusCode != http.StatusOK {
		d.logger.Debug("profile HTML returned non-200", "username", username, "status", resp.StatusCode)
		return nil
	}
	
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024)) // 2MB limit
	if err != nil {
		d.logger.Debug("failed to read profile HTML", "username", username, "error", err)
		return nil
	}
	
	html := string(body)
	d.logger.Debug("scraped profile HTML", "username", username, "html_length", len(html))
	
	// Use the existing extraction function
	return extractSocialMediaFromHTML(html)
}

// fetchStarredRepositories fetches repositories the user has starred for additional timestamp data and repository details.
func (d *Detector) fetchStarredRepositories(ctx context.Context, username string) ([]time.Time, []Repository, error) {
	apiURL := fmt.Sprintf("https://api.github.com/users/%s/starred?per_page=100", username)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, http.NoBody)
	if err != nil {
		return nil, nil, fmt.Errorf("creating request: %w", err)
	}

	// SECURITY: Validate and sanitize GitHub token before use
	if d.githubToken != "" {
		token := d.githubToken
		if d.isValidGitHubToken(token) {
			req.Header.Set("Authorization", "token "+token)
		}
	}

	// Request timestamps in response headers
	req.Header.Set("Accept", "application/vnd.github.v3.star+json")

	resp, err := d.cachedHTTPDo(ctx, req)
	if err != nil {
		return nil, nil, fmt.Errorf("fetching starred repositories: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			d.logger.Debug("failed to close response body", "error", err)
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

	d.logger.Debug("fetched starred repositories", "username", username, "count", len(timestamps))
	return timestamps, repos, nil
}

// fetchUserCommits fetches commit timestamps for a user's recent commits across their repositories.
func (d *Detector) fetchUserCommits(ctx context.Context, username string) ([]time.Time, error) {
	var allTimestamps []time.Time
	const perPage = 100
	const maxPages = 2 // Fetch up to 200 commits
	
	for page := 1; page <= maxPages; page++ {
		// Use GitHub Search API to find commits by this user
		// Note: This requires authentication for better rate limits
		searchURL := fmt.Sprintf("https://api.github.com/search/commits?q=author:%s&sort=author-date&order=desc&per_page=%d&page=%d", 
			username, perPage, page)
		
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, http.NoBody)
		if err != nil {
			return allTimestamps, fmt.Errorf("creating request: %w", err)
		}
		
		// The commit search API requires this Accept header
		req.Header.Set("Accept", "application/vnd.github.cloak-preview+json")
		
		// SECURITY: Validate and sanitize GitHub token before use
		if d.githubToken != "" {
			token := d.githubToken
			if d.isValidGitHubToken(token) {
				req.Header.Set("Authorization", "token "+token)
			}
		}
		
		resp, err := d.cachedHTTPDo(ctx, req)
		if err != nil {
			d.logger.Debug("failed to fetch commit page", "page", page, "error", err)
			break // Return what we have so far
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				d.logger.Debug("failed to close response body", "error", err)
			}
		}()
		
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			d.logger.Debug("GitHub API error", "status", resp.StatusCode, "page", page, "body", string(body))
			break
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
			d.logger.Debug("failed to decode commit response", "page", page, "error", err)
			break
		}
		
		if page == 1 {
			d.logger.Debug("GitHub commit search results", "username", username, "total_count", searchResult.TotalCount)
		}
		
		for _, item := range searchResult.Items {
			// Use author date (when the commit was originally authored)
			if !item.Commit.Author.Date.IsZero() {
				allTimestamps = append(allTimestamps, item.Commit.Author.Date)
			}
		}
		
		// If we got fewer items than requested, we've reached the end
		if len(searchResult.Items) < perPage {
			break
		}
	}
	
	d.logger.Debug("fetched user commits", "username", username, "count", len(allTimestamps))
	return allTimestamps, nil
}
