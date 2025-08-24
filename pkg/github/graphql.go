package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/codeGROOVE-dev/retry"
)

// GraphQLClient handles GitHub GraphQL API requests.
type GraphQLClient struct {
	cachedHTTPDo func(context.Context, *http.Request) (*http.Response, error)
	logger       *slog.Logger
	token        string
}

// NewGraphQLClient creates a new GraphQL client.
func NewGraphQLClient(token string, cachedHTTPDo func(context.Context, *http.Request) (*http.Response, error), logger *slog.Logger) *GraphQLClient {
	return &GraphQLClient{
		token:        token,
		cachedHTTPDo: cachedHTTPDo,
		logger:       logger,
	}
}

// GraphQLResponse represents the response from a GraphQL query.
type GraphQLResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []GraphQLError  `json:"errors,omitempty"`
}

// GraphQLError represents an error in a GraphQL response.
type GraphQLError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

// NOTE: CommitActivityResponse was removed because GitHub's GraphQL search API
// doesn't support COMMIT type. Use REST API for commit search instead.

// UserProfileResponse for non-paginated user data.
type UserProfileResponse struct {
	User struct { //nolint:govet // complex nested struct with many fields
		Repositories struct {
			Nodes []struct {
				CreatedAt       time.Time `json:"createdAt"`
				UpdatedAt       time.Time `json:"updatedAt"`
				PushedAt        time.Time `json:"pushedAt"`
				PrimaryLanguage struct {
					Name string `json:"name"`
				} `json:"primaryLanguage"`
				Name           string `json:"name"`
				NameWithOwner  string `json:"nameWithOwner"`
				Description    string `json:"description"`
				URL            string `json:"url"`
				StargazerCount int    `json:"stargazerCount"`
				IsFork         bool   `json:"isFork"`
			} `json:"nodes"`
			TotalCount int `json:"totalCount"`
		} `json:"repositories"`
		// Pull requests
		PullRequests struct {
			Nodes []struct {
				Title      string    `json:"title"`
				Body       string    `json:"body"`
				CreatedAt  time.Time `json:"createdAt"`
				UpdatedAt  time.Time `json:"updatedAt"`
				URL        string    `json:"url"`
				State      string    `json:"state"`
				Repository struct {
					NameWithOwner string `json:"nameWithOwner"`
				} `json:"repository"`
			} `json:"nodes"`
			TotalCount int `json:"totalCount"`
		} `json:"pullRequests"`
		// Issues
		Issues struct {
			Nodes []struct {
				Title      string    `json:"title"`
				Body       string    `json:"body"`
				CreatedAt  time.Time `json:"createdAt"`
				UpdatedAt  time.Time `json:"updatedAt"`
				URL        string    `json:"url"`
				State      string    `json:"state"`
				Repository struct {
					NameWithOwner string `json:"nameWithOwner"`
				} `json:"repository"`
			} `json:"nodes"`
			TotalCount int `json:"totalCount"`
		} `json:"issues"`
		ContributionsCollection struct {
			StartedAt                     time.Time `json:"startedAt"`
			EndedAt                       time.Time `json:"endedAt"`
			TotalCommitContributions      int       `json:"totalCommitContributions"`
			TotalPullRequestContributions int       `json:"totalPullRequestContributions"`
			TotalIssueContributions       int       `json:"totalIssueContributions"`
		} `json:"contributionsCollection"`

		// Starred repositories
		StarredRepositories struct {
			Nodes []struct {
				PrimaryLanguage struct {
					Name string `json:"name"`
				} `json:"primaryLanguage"`
				Name           string `json:"name"`
				NameWithOwner  string `json:"nameWithOwner"`
				Description    string `json:"description"`
				URL            string `json:"url"`
				StargazerCount int    `json:"stargazerCount"`
			} `json:"nodes"`
			TotalCount int `json:"totalCount"`
		} `json:"starredRepositories"`

		// Gists
		Gists struct {
			Nodes []struct {
				CreatedAt   time.Time `json:"createdAt"`
				UpdatedAt   time.Time `json:"updatedAt"`
				Description string    `json:"description"`
				URL         string    `json:"url"`
				IsPublic    bool      `json:"isPublic"`
			} `json:"nodes"`
			TotalCount int `json:"totalCount"`
		} `json:"gists"`

		// Social accounts
		SocialAccounts struct {
			Nodes []struct {
				Provider    string `json:"provider"`
				URL         string `json:"url"`
				DisplayName string `json:"displayName"`
			} `json:"nodes"`
		} `json:"socialAccounts"`

		// Organizations
		Organizations struct {
			Nodes []struct {
				Name        string `json:"name"`
				Login       string `json:"login"`
				Location    string `json:"location"`
				Description string `json:"description"`
			} `json:"nodes"`
		} `json:"organizations"`

		// Statistics
		Followers struct {
			TotalCount int `json:"totalCount"`
		} `json:"followers"`
		Following struct {
			TotalCount int `json:"totalCount"`
		} `json:"following"`

		CreatedAt       time.Time `json:"createdAt"`
		UpdatedAt       time.Time `json:"updatedAt"`
		Name            string    `json:"name"`
		Login           string    `json:"login"`
		Email           string    `json:"email"`
		Location        string    `json:"location"`
		Bio             string    `json:"bio"`
		Company         string    `json:"company"`
		Blog            string    `json:"websiteUrl"`
		TwitterUsername string    `json:"twitterUsername"`
	} `json:"user"`
}

// ActivityDataResponse for paginated activity data.
type ActivityDataResponse struct {
	User struct {
		PullRequests struct {
			Nodes []struct {
				CreatedAt  time.Time `json:"createdAt"`
				UpdatedAt  time.Time `json:"updatedAt"`
				Repository struct {
					Name  string `json:"name"`
					Owner struct {
						Login string `json:"login"`
					} `json:"owner"`
				} `json:"repository"`
				Title  string `json:"title"`
				URL    string `json:"url"`
				State  string `json:"state"`
				Number int    `json:"number"`
			} `json:"nodes"`
			PageInfo struct {
				EndCursor   string `json:"endCursor"`
				HasNextPage bool   `json:"hasNextPage"`
			} `json:"pageInfo"`
			TotalCount int `json:"totalCount"`
		} `json:"pullRequests"`

		Issues struct {
			Nodes []struct {
				CreatedAt  time.Time `json:"createdAt"`
				UpdatedAt  time.Time `json:"updatedAt"`
				Repository struct {
					Name  string `json:"name"`
					Owner struct {
						Login string `json:"login"`
					} `json:"owner"`
				} `json:"repository"`
				Title  string `json:"title"`
				URL    string `json:"url"`
				State  string `json:"state"`
				Number int    `json:"number"`
			} `json:"nodes"`
			PageInfo struct {
				EndCursor   string `json:"endCursor"`
				HasNextPage bool   `json:"hasNextPage"`
			} `json:"pageInfo"`
			TotalCount int `json:"totalCount"`
		} `json:"issues"`
	} `json:"user"`
}

// CommentDataResponse for paginated comments.
type CommentDataResponse struct {
	User struct {
		// Issue comments (includes comments on both issues and PRs)
		IssueComments struct {
			Nodes []struct {
				CreatedAt time.Time `json:"createdAt"`
				Body      string    `json:"body"`
				URL       string    `json:"url"`
			} `json:"nodes"`
			PageInfo struct {
				EndCursor   string `json:"endCursor"`
				HasNextPage bool   `json:"hasNextPage"`
			} `json:"pageInfo"`
		} `json:"issueComments"`
		// Commit comments
		CommitComments struct {
			Nodes []struct {
				CreatedAt time.Time `json:"createdAt"`
				Commit    struct {
					URL string `json:"url"`
				} `json:"commit"`
				Body string `json:"body"`
			} `json:"nodes"`
			PageInfo struct {
				EndCursor   string `json:"endCursor"`
				HasNextPage bool   `json:"hasNextPage"`
			} `json:"pageInfo"`
		} `json:"commitComments"`
	} `json:"user"`
}

// FetchUserProfile fetches all non-paginated user data in a single query.
func (c *GraphQLClient) FetchUserProfile(ctx context.Context, username string) (*UserProfileResponse, error) {
	query := `
	query($login: String!) {
		user(login: $login) {
			name
			login
			email
			location
			bio
			company
			websiteUrl
			twitterUsername
			createdAt
			updatedAt
			
			socialAccounts(first: 10) {
				nodes {
					provider
					url
					displayName
				}
			}
			
			organizations(first: 20) {
				nodes {
					name
					login
					location
					description
				}
			}
			
			followers {
				totalCount
			}
			following {
				totalCount
			}
			repositories(first: 100, ownerAffiliations: OWNER, orderBy: {field: CREATED_AT, direction: DESC}) {
				nodes {
					name
					nameWithOwner
					description
					createdAt
					updatedAt
					pushedAt
					primaryLanguage {
						name
					}
					stargazerCount
					url
					isFork
				}
				totalCount
			}
			
			pullRequests(first: 100, orderBy: {field: CREATED_AT, direction: DESC}) {
				nodes {
					title
					body
					createdAt
					updatedAt
					url
					state
					repository {
						nameWithOwner
					}
				}
				totalCount
			}
			
			issues(first: 100, orderBy: {field: CREATED_AT, direction: DESC}) {
				nodes {
					title
					body
					createdAt
					updatedAt
					url
					state
					repository {
						nameWithOwner
					}
				}
				totalCount
			}
			
			contributionsCollection {
				startedAt
				endedAt
				totalCommitContributions
				totalPullRequestContributions
				totalIssueContributions
			}
			
			starredRepositories(first: 50, orderBy: {field: STARRED_AT, direction: DESC}) {
				nodes {
					name
					nameWithOwner
					description
					primaryLanguage {
						name
					}
					stargazerCount
					url
				}
				totalCount
			}
			
			gists(first: 50, orderBy: {field: CREATED_AT, direction: DESC}) {
				nodes {
					createdAt
					updatedAt
					description
					url
					isPublic
				}
				totalCount
			}
		}
	}`

	variables := map[string]any{
		"login": username,
	}

	resp, err := c.executeQuery(ctx, query, variables)
	if err != nil {
		c.logger.Error("🚩 GraphQL User Profile Query Failed",
			"query_type", "user_profile_with_starred_repos_and_gists",
			"username", username, "error", err)
		return nil, err
	}

	var result UserProfileResponse
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("unmarshaling user profile: %w", err)
	}

	return &result, nil
}

// FetchActivityData fetches PRs and Issues in a single paginated query.
func (c *GraphQLClient) FetchActivityData(ctx context.Context, username string, prCursor, issueCursor string) (*ActivityDataResponse, error) {
	query := `
	query($login: String!, $prCursor: String, $issueCursor: String) {
		user(login: $login) {
			pullRequests(first: 100, orderBy: {field: CREATED_AT, direction: DESC}, after: $prCursor) {
				nodes {
					title
					number
					createdAt
					updatedAt
					url
					state
					repository {
						name
						owner {
							login
						}
					}
				}
				pageInfo {
					hasNextPage
					endCursor
				}
				totalCount
			}
			
			issues(first: 100, orderBy: {field: CREATED_AT, direction: DESC}, after: $issueCursor) {
				nodes {
					title
					number
					createdAt
					updatedAt
					url
					state
					repository {
						name
						owner {
							login
						}
					}
				}
				pageInfo {
					hasNextPage
					endCursor
				}
				totalCount
			}
		}
	}`

	variables := map[string]any{
		"login": username,
	}

	if prCursor != "" {
		variables["prCursor"] = prCursor
	}
	if issueCursor != "" {
		variables["issueCursor"] = issueCursor
	}

	resp, err := c.executeQuery(ctx, query, variables)
	if err != nil {
		c.logger.Error("🚩 GraphQL Activity Data Query Failed", "query_type", "user_activity_prs_and_issues", "username", username, "error", err)
		return nil, err
	}

	var result ActivityDataResponse
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("unmarshaling activity data: %w", err)
	}

	return &result, nil
}

// FetchComments fetches both issue comments (which include PR comments) and commit comments.
func (c *GraphQLClient) FetchComments(ctx context.Context, username string, issueCursor, commitCursor string) (*CommentDataResponse, error) {
	query := `
	query($login: String!, $issueCursor: String, $commitCursor: String) {
		user(login: $login) {
			issueComments(first: 100, orderBy: {field: CREATED_AT, direction: DESC}, after: $issueCursor) {
				nodes {
					body
					createdAt
					url
				}
				pageInfo {
					hasNextPage
					endCursor
				}
			}
			commitComments(first: 100, orderBy: {field: CREATED_AT, direction: DESC}, after: $commitCursor) {
				nodes {
					body
					createdAt
					commit {
						url
					}
				}
				pageInfo {
					hasNextPage
					endCursor
				}
			}
		}
	}`

	variables := map[string]any{
		"login": username,
	}

	if issueCursor != "" {
		variables["issueCursor"] = issueCursor
	}
	if commitCursor != "" {
		variables["commitCursor"] = commitCursor
	}

	resp, err := c.executeQuery(ctx, query, variables)
	if err != nil {
		c.logger.Error("🚩 GraphQL Comments Query Failed", "query_type", "user_issue_comments", "username", username, "error", err)
		return nil, err
	}

	var result CommentDataResponse
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("unmarshaling comment data: %w", err)
	}

	return &result, nil
}

// FetchCommitActivities fetches commit activities using GraphQL search
// This is more efficient than the REST search API (5000 points/hour vs 30 requests/minute)
// NOTE: FetchCommitActivities was removed because GitHub's GraphQL search API
// doesn't support COMMIT type. Use REST API for commit search instead.

// executeQuery executes a GraphQL query with retry logic for transient server errors and rate limits.
func (c *GraphQLClient) executeQuery(ctx context.Context, query string, variables map[string]any) (*GraphQLResponse, error) {
	var resp *GraphQLResponse
	var lastErr error

	// Create a context with 15-second timeout for interactive service
	retryCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	err := retry.Do(
		func() error {
			resp, lastErr = c.executeQueryOnce(retryCtx, query, variables)
			if lastErr != nil {
				// Check for secondary rate limit - give up immediately
				if strings.Contains(lastErr.Error(), "secondary rate limit") {
					c.logger.Warn("GraphQL secondary rate limit detected, not retrying",
						"error", lastErr)
					return retry.Unrecoverable(fmt.Errorf("GitHub secondary rate limit: %w", lastErr))
				}

				// Check if this is a non-transient error
				errStr := lastErr.Error()
				if !strings.Contains(errStr, "GraphQL server error (transient)") &&
					!strings.Contains(errStr, "rate limit") &&
					!strings.Contains(errStr, "HTTP 403") &&
					!strings.Contains(errStr, "HTTP 429") &&
					!strings.Contains(errStr, "HTTP 502") &&
					!strings.Contains(errStr, "HTTP 503") &&
					!strings.Contains(errStr, "HTTP 504") {
					// Non-transient error, don't retry
					return retry.Unrecoverable(lastErr)
				}

				return lastErr
			}
			return nil
		},
		retry.Context(retryCtx),
		retry.Attempts(10),                // Fewer attempts for 15-second window
		retry.Delay(100*time.Millisecond), // Start at 100ms
		retry.MaxDelay(3*time.Second),     // Cap individual delay at 3 seconds
		retry.DelayType(retry.CombineDelay(retry.BackOffDelay, retry.RandomDelay)), // Exponential backoff with jitter
		retry.MaxJitter(200*time.Millisecond),                                      // Add up to 200ms of jitter
		retry.OnRetry(func(n uint, err error) {
			c.logger.Info("Retrying GraphQL query",
				"attempt", n+1,
				"error", err.Error())
		}),
		retry.LastErrorOnly(true),
	)
	if err != nil {
		return nil, fmt.Errorf("GraphQL query failed: %w", err)
	}

	return resp, nil
}

// executeQueryOnce executes a single GraphQL query attempt.
func (c *GraphQLClient) executeQueryOnce(ctx context.Context, query string, variables map[string]any) (*GraphQLResponse, error) {
	payload := map[string]any{
		"query":     query,
		"variables": variables,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshaling query: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.github.com/graphql", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.cachedHTTPDo(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			// Log error if logger is available, otherwise ignore
			_ = err
		}
	}()

	// Check HTTP status code first
	if resp.StatusCode == http.StatusForbidden {
		// Check for secondary rate limit
		if strings.Contains(resp.Header.Get("X-Ratelimit-Remaining"), "0") ||
			strings.Contains(resp.Header.Get("Retry-After"), "") {
			c.logger.Warn("GitHub secondary rate limit detected on GraphQL endpoint",
				"status", resp.StatusCode,
				"retry_after", resp.Header.Get("Retry-After"))
			return nil, errors.New("HTTP 403: secondary rate limit")
		}
		return nil, errors.New("HTTP 403: forbidden")
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		c.logger.Warn("GitHub rate limit exceeded on GraphQL endpoint",
			"status", resp.StatusCode,
			"rate_limit_remaining", resp.Header.Get("X-Ratelimit-Remaining"),
			"rate_limit_reset", resp.Header.Get("X-Ratelimit-Reset"))
		return nil, errors.New("HTTP 429: rate limit exceeded")
	}

	if resp.StatusCode == http.StatusBadGateway ||
		resp.StatusCode == http.StatusServiceUnavailable ||
		resp.StatusCode == http.StatusGatewayTimeout {
		c.logger.Warn("GitHub server error on GraphQL endpoint",
			"status", resp.StatusCode)
		return nil, fmt.Errorf("HTTP %d: server error", resp.StatusCode)
	}

	if resp.StatusCode != http.StatusOK {
		c.logger.Warn("unexpected HTTP status on GraphQL endpoint",
			"status", resp.StatusCode)
		return nil, fmt.Errorf("HTTP %d: unexpected status", resp.StatusCode)
	}

	var graphqlResp GraphQLResponse
	if err := json.NewDecoder(resp.Body).Decode(&graphqlResp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	if len(graphqlResp.Errors) > 0 {
		// Check if this is a server error that should be retried
		errMsg := graphqlResp.Errors[0].Message
		if strings.Contains(errMsg, "Something went wrong") ||
			strings.Contains(errMsg, "server error") ||
			strings.Contains(errMsg, "Internal server error") ||
			strings.Contains(errMsg, "timeout") ||
			strings.Contains(errMsg, "Timeout") {
			// Log the error with request ID if present
			c.logger.Warn("GraphQL server error (may be transient)",
				"error", errMsg,
				"type", graphqlResp.Errors[0].Type)
			// Return an error that indicates this could be retried
			// The HTTP layer doesn't see this as an error (200 OK), so we need to handle it here
			return nil, fmt.Errorf("GraphQL server error (transient): %s", errMsg)
		}
		// Check for rate limit errors in GraphQL response
		if strings.Contains(errMsg, "rate limit") || strings.Contains(errMsg, "API rate limit exceeded") {
			c.logger.Warn("GraphQL rate limit error",
				"error", errMsg,
				"type", graphqlResp.Errors[0].Type)
			return nil, fmt.Errorf("GraphQL rate limit: %s", errMsg)
		}
		return nil, fmt.Errorf("GraphQL error: %s", errMsg)
	}

	return &graphqlResp, nil
}
