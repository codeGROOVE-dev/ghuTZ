package github

import (
	"encoding/json"
	"time"
)

//nolint:revive // The name GitHubUser is intentional for clarity in API context

// GitHubUser represents a GitHub user profile.
type GitHubUser struct {
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	Email          string    `json:"email"`
	Bio            string    `json:"bio"`
	Blog           string    `json:"blog"`
	Company        string    `json:"company"`
	Login          string    `json:"login"`
	TwitterHandle  string    `json:"twitter_username"`
	Location       string    `json:"location"`
	Name           string    `json:"name"`
	ProfileHTML    string
	SocialAccounts []SocialAccount `json:"socialAccounts,omitempty"`
	Followers      int             `json:"followers"`
	Following      int             `json:"following"`
	PublicRepos    int             `json:"public_repos"`
}

// SocialAccount represents a social media account linked to GitHub profile.
type SocialAccount struct {
	Provider    string `json:"provider"`
	URL         string `json:"url"`
	DisplayName string `json:"displayName"`
}

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

// Gist represents a GitHub gist.
type Gist struct {
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	ID          string    `json:"id"`
	Description string    `json:"description"`
	HTMLURL     string    `json:"html_url"`
	Public      bool      `json:"public"`
}

// Repository represents a GitHub repository.
type Repository struct {
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	PushedAt    time.Time `json:"pushed_at"`
	Name        string    `json:"name"`
	FullName    string    `json:"full_name"`
	Description string    `json:"description"`
	Language    string    `json:"language"`
	HTMLURL     string    `json:"html_url"`
	Topics      []string  `json:"topics"`
	StarCount   int       `json:"stargazers_count"`
	Fork        bool      `json:"fork"`
}

// Organization represents a GitHub organization.
type Organization struct {
	Login       string `json:"login"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Location    string `json:"location"`
	Blog        string `json:"blog"`
}

// Issue represents a GitHub issue.
type Issue struct {
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	State     string    `json:"state"`
	HTMLURL   string    `json:"html_url"`
	RepoName  string
	Number    int `json:"number"`
}

// PullRequest represents a GitHub pull request.
type PullRequest struct {
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	MergedAt  *time.Time `json:"merged_at"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	State     string     `json:"state"`
	HTMLURL   string     `json:"html_url"`
	RepoName  string
	Number    int `json:"number"`
}

// PRSearchItem represents a GitHub pull request search result.
type PRSearchItem struct {
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	Title      string    `json:"title"`
	Body       string    `json:"body"`
	State      string    `json:"state"`
	HTMLURL    string    `json:"html_url"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	Number int `json:"number"`
}

// IssueSearchItem represents a GitHub issue search result.
type IssueSearchItem struct {
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	Title      string    `json:"title"`
	Body       string    `json:"body"`
	State      string    `json:"state"`
	HTMLURL    string    `json:"html_url"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	Number int `json:"number"`
}

// Comment represents a GitHub comment (issue or commit).
type Comment struct {
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	HTMLURL   string    `json:"html_url"`
}
