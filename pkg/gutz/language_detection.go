package gutz

import (
	"encoding/json"
	"strings"

	"github.com/codeGROOVE-dev/guTZ/pkg/github"
)

// CommitMessageSample represents a sample commit message for analysis.
type CommitMessageSample struct {
	Message string
	Author  string
}

// collectCommitMessageSamples collects sample commit messages from events.
func collectCommitMessageSamples(events []github.PublicEvent, maxSamples int) []CommitMessageSample {
	var samples []CommitMessageSample
	seen := make(map[string]bool)

	for _, event := range events {
		if event.Type != "PushEvent" {
			continue
		}

		// Parse the payload to get commit messages
		var payload struct {
			Commits []struct {
				Message string `json:"message"`
				Author  struct {
					Name  string `json:"name"`
					Email string `json:"email"`
				} `json:"author"`
			} `json:"commits"`
		}

		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			continue
		}

		for _, commit := range payload.Commits {
			if commit.Message == "" || seen[commit.Message] {
				continue
			}

			// Skip automated commits
			msgLower := strings.ToLower(commit.Message)
			if strings.Contains(msgLower, "merge pull request") ||
				strings.Contains(msgLower, "merge branch") ||
				strings.Contains(msgLower, "auto-generated") ||
				strings.Contains(msgLower, "dependabot") {
				continue
			}

			seen[commit.Message] = true
			samples = append(samples, CommitMessageSample{
				Message: commit.Message,
				Author:  commit.Author.Name,
			})

			if len(samples) >= maxSamples {
				return samples
			}
		}
	}

	return samples
}

// collectTextSamples collects sample text from PRs, issues, and comments.
func collectTextSamples(prs []github.PullRequest, issues []github.Issue, comments []github.Comment, maxSamples int) []string {
	var samples []string
	seen := make(map[string]bool)

	// Collect from PR titles and bodies
	for i := range prs {
		if prs[i].Title != "" && !seen[prs[i].Title] && len(samples) < maxSamples {
			seen[prs[i].Title] = true
			samples = append(samples, "PR Title: "+prs[i].Title)
		}
		if prs[i].Body != "" && !seen[prs[i].Body] && len(samples) < maxSamples {
			// Take first 200 chars of body if it's long
			body := prs[i].Body
			if len(body) > 200 {
				body = body[:200] + "..."
			}
			if !strings.Contains(body, "<!--") && !strings.Contains(body, "## Checklist") {
				seen[prs[i].Body] = true
				samples = append(samples, "PR Body: "+body)
			}
		}
	}

	// Collect from issue titles and bodies
	for i := range issues {
		if len(samples) >= maxSamples {
			break
		}
		if issues[i].Title != "" && !seen[issues[i].Title] {
			seen[issues[i].Title] = true
			samples = append(samples, "Issue Title: "+issues[i].Title)
		}
		if issues[i].Body != "" && !seen[issues[i].Body] && len(samples) < maxSamples {
			// Take first 200 chars of body if it's long
			body := issues[i].Body
			if len(body) > 200 {
				body = body[:200] + "..."
			}
			if !strings.Contains(body, "<!--") && !strings.Contains(body, "## Checklist") {
				seen[issues[i].Body] = true
				samples = append(samples, "Issue Body: "+body)
			}
		}
	}

	// Collect from comments
	for _, comment := range comments {
		if len(samples) >= maxSamples {
			break
		}
		if comment.Body != "" && !seen[comment.Body] {
			// Take first 200 chars if it's long
			body := comment.Body
			if len(body) > 200 {
				body = body[:200] + "..."
			}
			seen[comment.Body] = true
			samples = append(samples, "Comment: "+body)
		}
	}

	return samples
}
