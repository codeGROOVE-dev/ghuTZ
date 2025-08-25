package github

import (
	"context"
	"errors"
	"strings"

	"github.com/codeGROOVE-dev/guTZ/pkg/constants"
)

// Common errors.
var (
	ErrNoGitHubToken = errors.New("no GitHub token available")
	ErrUserNotFound  = errors.New("user not found")
)

// FetchUserEnhancedGraphQL fetches comprehensive user data using our enhanced GraphQL implementation
// This replaces multiple REST API calls with a single GraphQL query.
func (c *Client) FetchUserEnhancedGraphQL( //nolint:revive // Multiple return values for batch data fetch
	ctx context.Context, username string,
) (*User, []Repository, []PullRequest, []Issue, error) {
	if c.githubToken == "" {
		// Can't use GraphQL without a token
		c.logger.Debug("No GitHub token available for GraphQL", "username", username)
		return nil, nil, nil, nil, ErrNoGitHubToken
	}

	graphql := NewGraphQLClient(c.githubToken, c.cachedHTTPDo, c.logger)

	profile, err := graphql.FetchUserProfile(ctx, username)
	if err != nil {
		c.logger.Warn("🚩 GraphQL User Profile API Error", "username", username, "error", err)
		return nil, nil, nil, nil, err
	}

	if profile.User.Login == "" {
		return nil, nil, nil, nil, ErrUserNotFound
	}

	// Convert GraphQL response to User struct
	user := &User{
		Login:         profile.User.Login,
		Name:          profile.User.Name,
		Email:         profile.User.Email,
		Location:      profile.User.Location,
		Bio:           profile.User.Bio,
		Company:       profile.User.Company,
		Blog:          profile.User.Blog,
		TwitterHandle: profile.User.TwitterUsername,
		CreatedAt:     profile.User.CreatedAt,
		UpdatedAt:     profile.User.UpdatedAt,
		Followers:     profile.User.Followers.TotalCount,
		Following:     profile.User.Following.TotalCount,
		PublicRepos:   profile.User.Repositories.TotalCount,
	}

	// Extract social accounts
	for _, social := range profile.User.SocialAccounts.Nodes {
		c.logger.Debug("found social account",
			"provider", social.Provider,
			"url", social.URL,
			"display", social.DisplayName)

		// Add to user's social accounts
		user.SocialAccounts = append(user.SocialAccounts, SocialAccount{
			Provider:    social.Provider,
			URL:         social.URL,
			DisplayName: social.DisplayName,
		})
	}

	// Convert repositories from GraphQL response
	var repositories []Repository
	for i := range profile.User.Repositories.Nodes {
		repo := &profile.User.Repositories.Nodes[i]
		language := ""
		if repo.PrimaryLanguage.Name != "" {
			language = repo.PrimaryLanguage.Name
		}

		repositories = append(repositories, Repository{
			Name:        repo.Name,
			FullName:    repo.NameWithOwner,
			Description: repo.Description,
			Language:    language,
			HTMLURL:     repo.URL,
			StarCount:   repo.StargazerCount,
			Fork:        repo.IsFork,
			CreatedAt:   repo.CreatedAt,
			UpdatedAt:   repo.UpdatedAt,
			PushedAt:    repo.PushedAt,
		})
	}

	c.logger.Debug("extracted repositories from GraphQL", "username", username, "count", len(repositories))

	// Convert pull requests from GraphQL response
	var pullRequests []PullRequest
	for i := range profile.User.PullRequests.Nodes {
		pr := &profile.User.PullRequests.Nodes[i]
		pullRequests = append(pullRequests, PullRequest{
			Title:     pr.Title,
			Body:      pr.Body,
			CreatedAt: pr.CreatedAt,
			UpdatedAt: pr.UpdatedAt,
			HTMLURL:   pr.URL,
			State:     pr.State,
			RepoName:  pr.Repository.NameWithOwner,
		})
	}

	// Convert issues from GraphQL response
	var issues []Issue
	for i := range profile.User.Issues.Nodes {
		issue := &profile.User.Issues.Nodes[i]
		issues = append(issues, Issue{
			Title:     issue.Title,
			Body:      issue.Body,
			CreatedAt: issue.CreatedAt,
			UpdatedAt: issue.UpdatedAt,
			HTMLURL:   issue.URL,
			State:     issue.State,
			RepoName:  issue.Repository.NameWithOwner,
		})
	}

	c.logger.Debug("extracted PRs and issues from GraphQL", "username", username,
		"pr_count", len(pullRequests), "issue_count", len(issues))

	// Check if we need to fetch more data for better timezone detection
	totalDataPoints := len(pullRequests) + len(issues)
	minDesiredDataPoints := constants.TargetDataPoints

	// If we need more data, fetch additional pages using the paginated API
	if totalDataPoints < minDesiredDataPoints {
		// Log prominently when we need to fetch more data
		c.logger.Info("📊 INSUFFICIENT DATA - FETCHING MORE",
			"username", username,
			"initial_data_points", totalDataPoints,
			"target", minDesiredDataPoints,
			"reason", "Need more data for accurate timezone detection")

		// Fetch additional pages using the paginated activity API
		additionalPRs, additionalIssues, err := c.FetchActivityWithGraphQL(ctx, username)
		if err != nil {
			c.logger.Warn("failed to fetch additional activity data", "error", err)
			// Continue with what we have rather than failing entirely
		} else {
			// Merge the additional data (avoiding duplicates based on URL)
			existingPRURLs := make(map[string]bool)
			for _, pr := range pullRequests {
				existingPRURLs[pr.HTMLURL] = true
			}
			for _, pr := range additionalPRs {
				if !existingPRURLs[pr.HTMLURL] {
					pullRequests = append(pullRequests, pr)
				}
			}

			existingIssueURLs := make(map[string]bool)
			for _, issue := range issues {
				existingIssueURLs[issue.HTMLURL] = true
			}
			for _, issue := range additionalIssues {
				if !existingIssueURLs[issue.HTMLURL] {
					issues = append(issues, issue)
				}
			}

			c.logger.Info("📈 FETCHED ADDITIONAL DATA",
				"username", username,
				"total_prs_now", len(pullRequests),
				"total_issues_now", len(issues),
				"total_data_points_now", len(pullRequests)+len(issues))
		}
	}

	return user, repositories, pullRequests, issues, nil
}

// FetchActivityWithGraphQL fetches PRs and Issues using GraphQL
// This is MUCH more efficient than the search API:
// - Single query vs multiple search requests
// - 5000 points/hour vs 30 requests/minute
// - Can fetch 100 PRs + 100 issues in one request.
func (c *Client) FetchActivityWithGraphQL(ctx context.Context, username string) ([]PullRequest, []Issue, error) {
	if c.githubToken == "" {
		// Without a token, we can't use GraphQL or the search API effectively
		// Return empty arrays rather than hitting rate limits
		c.logger.Info("No GitHub token available - activity data limited", "username", username)
		return []PullRequest{}, []Issue{}, nil
	}

	graphql := NewGraphQLClient(c.githubToken, c.cachedHTTPDo, c.logger)

	// Fetch first page of activity data (PRs and Issues together)
	activityData, err := graphql.FetchActivityData(ctx, username, "", "")
	if err != nil {
		c.logger.Warn("🚩 GraphQL Activity API Error", "username", username, "error", err)
		// Don't fall back to REST - GraphQL is our primary method
		return []PullRequest{}, []Issue{}, err
	}

	c.logger.Info("GraphQL activity fetch successful",
		"username", username,
		"prs_fetched", len(activityData.User.PullRequests.Nodes),
		"issues_fetched", len(activityData.User.Issues.Nodes),
		"total_prs", activityData.User.PullRequests.TotalCount,
		"total_issues", activityData.User.Issues.TotalCount,
		"more_prs", activityData.User.PullRequests.PageInfo.HasNextPage,
		"more_issues", activityData.User.Issues.PageInfo.HasNextPage)

	// Convert to our structs
	var prs []PullRequest
	for i := range activityData.User.PullRequests.Nodes {
		pr := &activityData.User.PullRequests.Nodes[i]
		prs = append(prs, PullRequest{
			Title:     pr.Title,
			Number:    pr.Number,
			State:     pr.State,
			CreatedAt: pr.CreatedAt,
			UpdatedAt: pr.UpdatedAt,
			HTMLURL:   pr.URL,
			RepoName:  pr.Repository.Owner.Login + "/" + pr.Repository.Name,
		})
	}

	var issues []Issue
	for i := range activityData.User.Issues.Nodes {
		issue := &activityData.User.Issues.Nodes[i]
		issues = append(issues, Issue{
			Title:     issue.Title,
			Number:    issue.Number,
			State:     issue.State,
			CreatedAt: issue.CreatedAt,
			UpdatedAt: issue.UpdatedAt,
			HTMLURL:   issue.URL,
			RepoName:  issue.Repository.Owner.Login + "/" + issue.Repository.Name,
		})
	}

	// If we need more data and it's available, fetch additional pages
	// Calculate how many pages we need based on current data density
	totalDataPoints := len(prs) + len(issues)
	minDesiredDataPoints := constants.TargetDataPoints // Use the shared constant for consistency

	// Calculate max pages based on how sparse the data is
	// If we have very few data points, fetch more pages
	maxAdditionalPages := 3 // Default to fetching more pages
	if totalDataPoints < 20 {
		maxAdditionalPages = 8 // Fetch up to 8 additional pages for very sparse data
	} else if totalDataPoints < 50 {
		maxAdditionalPages = 6 // Fetch up to 6 additional pages for sparse data
	} else if totalDataPoints < 100 {
		maxAdditionalPages = 4 // Fetch up to 4 additional pages for moderate data
	}

	pagesFetched := 1

	prCursor := activityData.User.PullRequests.PageInfo.EndCursor
	issueCursor := activityData.User.Issues.PageInfo.EndCursor

	// Log prominently when we need to fetch more data
	if totalDataPoints < minDesiredDataPoints {
		c.logger.Info("📊 INSUFFICIENT DATA - FETCHING MORE",
			"username", username,
			"initial_data_points", totalDataPoints,
			"target", minDesiredDataPoints,
			"max_additional_pages", maxAdditionalPages,
			"reason", "Need more data for accurate timezone detection")
	}

	c.logger.Debug("determining additional pages to fetch",
		"initial_data_points", totalDataPoints,
		"max_additional_pages", maxAdditionalPages,
		"min_desired", minDesiredDataPoints)

	for pagesFetched < maxAdditionalPages && totalDataPoints < minDesiredDataPoints {
		if !activityData.User.PullRequests.PageInfo.HasNextPage &&
			!activityData.User.Issues.PageInfo.HasNextPage {
			break // No more data available
		}

		// Fetch next page
		nextData, err := graphql.FetchActivityData(ctx, username, prCursor, issueCursor)
		if err != nil {
			c.logger.Warn("failed to fetch additional page", "error", err, "page", pagesFetched+1)
			break
		}

		// Append new data
		for i := range nextData.User.PullRequests.Nodes {
			pr := &nextData.User.PullRequests.Nodes[i]
			prs = append(prs, PullRequest{
				Title:     pr.Title,
				Number:    pr.Number,
				State:     pr.State,
				CreatedAt: pr.CreatedAt,
				UpdatedAt: pr.UpdatedAt,
				HTMLURL:   pr.URL,
				RepoName:  pr.Repository.Owner.Login + "/" + pr.Repository.Name,
			})
		}

		for i := range nextData.User.Issues.Nodes {
			issue := &nextData.User.Issues.Nodes[i]
			issues = append(issues, Issue{
				Title:     issue.Title,
				Number:    issue.Number,
				State:     issue.State,
				CreatedAt: issue.CreatedAt,
				UpdatedAt: issue.UpdatedAt,
				HTMLURL:   issue.URL,
				RepoName:  issue.Repository.Owner.Login + "/" + issue.Repository.Name,
			})
		}

		// Update cursors for next iteration
		if nextData.User.PullRequests.PageInfo.HasNextPage {
			prCursor = nextData.User.PullRequests.PageInfo.EndCursor
		}
		if nextData.User.Issues.PageInfo.HasNextPage {
			issueCursor = nextData.User.Issues.PageInfo.EndCursor
		}

		pagesFetched++

		// Update total data points count
		totalDataPoints = len(prs) + len(issues)

		// Log progress prominently
		c.logger.Info("📈 FETCHED ADDITIONAL DATA PAGE",
			"page", pagesFetched,
			"new_prs", len(nextData.User.PullRequests.Nodes),
			"new_issues", len(nextData.User.Issues.Nodes),
			"total_data_points_now", totalDataPoints,
			"target", minDesiredDataPoints)
	}

	c.logger.Info("GraphQL activity fetch complete",
		"username", username,
		"total_prs_fetched", len(prs),
		"total_issues_fetched", len(issues),
		"pages_fetched", pagesFetched)

	return prs, issues, nil
}

// extractRepoFromURL extracts the repository name from a GitHub URL.
// Example: https://github.com/owner/repo/issues/123#issuecomment-456 -> "owner/repo".
func extractRepoFromURL(url string) string {
	if url == "" {
		return ""
	}

	// Remove the GitHub prefix
	const prefix = "https://github.com/"
	if !strings.HasPrefix(url, prefix) {
		return ""
	}

	path := strings.TrimPrefix(url, prefix)

	// Split by "/" and take the first two parts (owner/repo)
	parts := strings.Split(path, "/")
	if len(parts) >= 2 {
		return parts[0] + "/" + parts[1]
	}

	return ""
}

// FetchCommentsWithGraphQL fetches both issue comments (includes PR comments) and commit comments using GraphQL.
func (c *Client) FetchCommentsWithGraphQL(ctx context.Context, username string) ([]Comment, error) {
	if c.githubToken == "" {
		return nil, nil // Can't fetch comments without auth
	}

	graphql := NewGraphQLClient(c.githubToken, c.cachedHTTPDo, c.logger)

	commentData, err := graphql.FetchComments(ctx, username, "", "")
	if err != nil {
		return nil, err
	}

	var comments []Comment

	// Process issue comments (includes PR comments)
	for _, comment := range commentData.User.IssueComments.Nodes {
		repo := extractRepoFromURL(comment.URL)
		comments = append(comments, Comment{
			Body:       comment.Body,
			CreatedAt:  comment.CreatedAt,
			UpdatedAt:  comment.CreatedAt, // Use CreatedAt as UpdatedAt for histogram data
			HTMLURL:    comment.URL,
			Repository: repo,
		})
	}

	// Process commit comments
	for _, comment := range commentData.User.CommitComments.Nodes {
		url := ""
		if comment.Commit.URL != "" {
			url = comment.Commit.URL
		}
		repo := extractRepoFromURL(url)
		comments = append(comments, Comment{
			Body:       comment.Body,
			CreatedAt:  comment.CreatedAt,
			UpdatedAt:  comment.CreatedAt, // Use CreatedAt as UpdatedAt for histogram data
			HTMLURL:    url,
			Repository: repo,
		})
	}

	// Fetch additional pages if available and we don't have enough data
	issueCursor := ""
	commitCursor := ""
	hasMoreIssueComments := commentData.User.IssueComments.PageInfo.HasNextPage
	hasMoreCommitComments := commentData.User.CommitComments.PageInfo.HasNextPage

	if hasMoreIssueComments {
		issueCursor = commentData.User.IssueComments.PageInfo.EndCursor
	}
	if hasMoreCommitComments {
		commitCursor = commentData.User.CommitComments.PageInfo.EndCursor
	}

	// Fetch multiple pages if needed for better data coverage
	minDesiredComments := 200
	maxCommentPages := 3
	commentPagesFetched := 1

	// Log if we need more comment data
	if len(comments) < minDesiredComments && (hasMoreIssueComments || hasMoreCommitComments) {
		c.logger.Info("💬 FETCHING ADDITIONAL COMMENT DATA",
			"username", username,
			"initial_comments", len(comments),
			"target", minDesiredComments,
			"max_pages", maxCommentPages)
	}

	for commentPagesFetched < maxCommentPages && len(comments) < minDesiredComments {
		if !hasMoreIssueComments && !hasMoreCommitComments {
			break // No more data available
		}

		nextData, err := graphql.FetchComments(ctx, username, issueCursor, commitCursor)
		if err != nil {
			c.logger.Warn("failed to fetch additional comment page", "error", err, "page", commentPagesFetched+1)
			break
		}

		// Process additional issue comments
		for _, comment := range nextData.User.IssueComments.Nodes {
			repo := extractRepoFromURL(comment.URL)
			comments = append(comments, Comment{
				Body:       comment.Body,
				CreatedAt:  comment.CreatedAt,
				UpdatedAt:  comment.CreatedAt, // Use CreatedAt as UpdatedAt for histogram data
				HTMLURL:    comment.URL,
				Repository: repo,
			})
		}

		// Process additional commit comments
		for _, comment := range nextData.User.CommitComments.Nodes {
			url := ""
			if comment.Commit.URL != "" {
				url = comment.Commit.URL
			}
			repo := extractRepoFromURL(url)
			comments = append(comments, Comment{
				Body:       comment.Body,
				CreatedAt:  comment.CreatedAt,
				UpdatedAt:  comment.CreatedAt, // Use CreatedAt as UpdatedAt for histogram data
				HTMLURL:    url,
				Repository: repo,
			})
		}

		// Update cursors and flags for next iteration
		hasMoreIssueComments = nextData.User.IssueComments.PageInfo.HasNextPage
		hasMoreCommitComments = nextData.User.CommitComments.PageInfo.HasNextPage

		if hasMoreIssueComments {
			issueCursor = nextData.User.IssueComments.PageInfo.EndCursor
		}
		if hasMoreCommitComments {
			commitCursor = nextData.User.CommitComments.PageInfo.EndCursor
		}

		commentPagesFetched++

		c.logger.Info("💬 FETCHED COMMENT PAGE",
			"page", commentPagesFetched,
			"total_comments_now", len(comments),
			"new_issue_comments", len(nextData.User.IssueComments.Nodes),
			"new_commit_comments", len(nextData.User.CommitComments.Nodes),
			"target", minDesiredComments)
	}

	c.logger.Info("GraphQL comment fetch complete",
		"username", username,
		"total_comments", len(comments))

	return comments, nil
}

// CompareAPIEfficiency shows the efficiency gains from using GraphQL.
func CompareAPIEfficiency() map[string]any {
	return map[string]any{
		"rest_search_api": map[string]any{
			"rate_limit":       "30 requests/minute",
			"requests_needed":  4, // User + PRs + Issues + Comments
			"data_per_request": "30-100 items",
			"secondary_limits": "Very restrictive",
			"total_api_calls":  "4-8 (with pagination)",
		},
		"graphql_api": map[string]any{
			"rate_limit":       "5000 points/hour",
			"requests_needed":  2, // 1 for profile+orgs, 1 for PRs+Issues
			"data_per_request": "100 PRs + 100 Issues + profile + orgs",
			"secondary_limits": "None",
			"total_api_calls":  "2-3 (with pagination)",
			"point_cost":       "~10-20 points per query",
		},
		"efficiency_gain": map[string]any{
			"api_calls_reduced": "50-75%",
			"rate_limit_usage":  "95% less",
			"response_time":     "2-3x faster",
			"data_completeness": "Includes social accounts",
		},
	}
}
