package ghutz

import (
	"encoding/json"
	"strings"
)

// CommitMessageSample represents a sample commit message for analysis
type CommitMessageSample struct {
	Message string
	Author  string
}

// collectCommitMessageSamples collects sample commit messages from events
func collectCommitMessageSamples(events []PublicEvent, maxSamples int) []CommitMessageSample {
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

// collectTextSamples collects sample text from PRs, issues, and comments
func collectTextSamples(prs []PullRequest, issues []Issue, comments []Comment, maxSamples int) []string {
	var samples []string
	seen := make(map[string]bool)
	
	// Collect from PR titles and bodies
	for _, pr := range prs {
		if pr.Title != "" && !seen[pr.Title] && len(samples) < maxSamples {
			seen[pr.Title] = true
			samples = append(samples, "PR Title: " + pr.Title)
		}
		if pr.Body != "" && !seen[pr.Body] && len(samples) < maxSamples {
			// Take first 200 chars of body if it's long
			body := pr.Body
			if len(body) > 200 {
				body = body[:200] + "..."
			}
			if !strings.Contains(body, "<!--") && !strings.Contains(body, "## Checklist") {
				seen[pr.Body] = true
				samples = append(samples, "PR Body: " + body)
			}
		}
	}
	
	// Collect from issue titles and bodies
	for _, issue := range issues {
		if len(samples) >= maxSamples {
			break
		}
		if issue.Title != "" && !seen[issue.Title] {
			seen[issue.Title] = true
			samples = append(samples, "Issue Title: " + issue.Title)
		}
		if issue.Body != "" && !seen[issue.Body] && len(samples) < maxSamples {
			// Take first 200 chars of body if it's long
			body := issue.Body
			if len(body) > 200 {
				body = body[:200] + "..."
			}
			if !strings.Contains(body, "<!--") && !strings.Contains(body, "## Checklist") {
				seen[issue.Body] = true
				samples = append(samples, "Issue Body: " + body)
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
			samples = append(samples, "Comment: " + body)
		}
	}
	
	return samples
}