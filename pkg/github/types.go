package github

import (
	"encoding/json"
	"time"
)

// GitHubUser represents a GitHub user profile.
type GitHubUser struct {
	Login          string          `json:"login"`
	Name           string          `json:"name"`
	Location       string          `json:"location"`
	Bio            string          `json:"bio"`
	Blog           string          `json:"blog"`
	Company        string          `json:"company"`
	Email          string          `json:"email"`
	TwitterHandle  string          `json:"twitter_username"`
	Followers      int             `json:"followers"`
	Following      int             `json:"following"`
	PublicRepos    int             `json:"public_repos"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
	ProfileHTML    string          // HTML content of profile page
	SocialAccounts []SocialAccount `json:"socialAccounts,omitempty"`
}

// SocialAccount represents a social media account linked to GitHub profile
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

// Gist represents a GitHub gist
type Gist struct {
	ID          string    `json:"id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Description string    `json:"description"`
	Public      bool      `json:"public"`
	HTMLURL     string    `json:"html_url"`
}

// Repository represents a GitHub repository
type Repository struct {
	Name        string    `json:"name"`
	FullName    string    `json:"full_name"`
	Description string    `json:"description"`
	Fork        bool      `json:"fork"`
	Language    string    `json:"language"`
	StarCount   int       `json:"stargazers_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	PushedAt    time.Time `json:"pushed_at"`
	Topics      []string  `json:"topics"`
	HTMLURL     string    `json:"html_url"`
}

// Organization represents a GitHub organization.
type Organization struct {
	Login       string `json:"login"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Location    string `json:"location"`
	Blog        string `json:"blog"`
}

// Issue represents a GitHub issue
type Issue struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	State     string    `json:"state"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	HTMLURL   string    `json:"html_url"`
	RepoName  string    // Added to track which repo this issue belongs to
}

// PullRequest represents a GitHub pull request
type PullRequest struct {
	Number    int        `json:"number"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	State     string     `json:"state"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	MergedAt  *time.Time `json:"merged_at"`
	HTMLURL   string     `json:"html_url"`
	RepoName  string     // Added to track which repo this PR belongs to
}

// PRSearchItem represents a GitHub pull request search result
type PRSearchItem struct {
	Number     int       `json:"number"`
	Title      string    `json:"title"`
	Body       string    `json:"body"`
	State      string    `json:"state"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	HTMLURL    string    `json:"html_url"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

// IssueSearchItem represents a GitHub issue search result
type IssueSearchItem struct {
	Number     int       `json:"number"`
	Title      string    `json:"title"`
	Body       string    `json:"body"`
	State      string    `json:"state"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	HTMLURL    string    `json:"html_url"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

// Comment represents a GitHub comment (issue or commit)
type Comment struct {
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	HTMLURL   string    `json:"html_url"`
}
