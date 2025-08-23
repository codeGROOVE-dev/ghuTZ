package gutz

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/codeGROOVE-dev/guTZ/pkg/github"
)

// CommitMessageSample represents a sample commit message for analysis.
type CommitMessageSample struct {
	Message    string
	Author     string
	Repository string // Repository name (owner/repo)
}

// cleanCommitMessage removes Signed-off-by lines and other metadata from commit messages.
func cleanCommitMessage(msg string) string {
	lines := strings.Split(msg, "\n")
	var cleanedLines []string
	
	for _, line := range lines {
		// Skip Signed-off-by lines and similar metadata
		if strings.HasPrefix(line, "Signed-off-by:") ||
			strings.HasPrefix(line, "Co-authored-by:") ||
			strings.HasPrefix(line, "Reviewed-by:") ||
			strings.HasPrefix(line, "Acked-by:") ||
			strings.HasPrefix(line, "Tested-by:") {
			continue
		}
		cleanedLines = append(cleanedLines, line)
	}
	
	// Join lines and trim whitespace
	result := strings.TrimSpace(strings.Join(cleanedLines, "\n"))
	
	// Also trim any trailing whitespace from multi-line messages
	result = strings.TrimSpace(result)
	
	return result
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
			if commit.Message == "" {
				continue
			}

			// Clean the commit message: remove Signed-off-by lines and trim
			cleanedMsg := cleanCommitMessage(commit.Message)
			if cleanedMsg == "" || seen[cleanedMsg] {
				continue
			}

			// Skip automated commits
			msgLower := strings.ToLower(cleanedMsg)
			if strings.Contains(msgLower, "merge pull request") ||
				strings.Contains(msgLower, "merge branch") ||
				strings.Contains(msgLower, "auto-generated") ||
				strings.Contains(msgLower, "dependabot") {
				continue
			}

			seen[cleanedMsg] = true
			samples = append(samples, CommitMessageSample{
				Message:    cleanedMsg,
				Author:     commit.Author.Name,
				Repository: event.Repo.Name,
			})

			if len(samples) >= maxSamples {
				return samples
			}
		}
	}

	return samples
}

// textSampleWithTime holds a text sample with its timestamp for sorting.
type textSampleWithTime struct {
	sample    string
	timestamp time.Time
}

// collectTextSamples collects the most recent text samples from PRs, issues, and comments.
func collectTextSamples(prs []github.PullRequest, issues []github.Issue, comments []github.Comment, maxSamples int) []string {
	var allSamples []textSampleWithTime
	seen := make(map[string]bool)

	// Collect from PR titles (only titles, not bodies for brevity)
	for i := range prs {
		if prs[i].Title != "" && !seen[prs[i].Title] {
			seen[prs[i].Title] = true
			repoInfo := ""
			if prs[i].RepoName != "" {
				repoInfo = " - " + prs[i].RepoName + " PR"
			}
			allSamples = append(allSamples, textSampleWithTime{
				sample:    fmt.Sprintf("* \"%s\"%s", prs[i].Title, repoInfo),
				timestamp: prs[i].UpdatedAt,
			})
		}
	}

	// Collect from issue titles (only titles, not bodies for brevity)
	for i := range issues {
		if issues[i].Title != "" && !seen[issues[i].Title] {
			seen[issues[i].Title] = true
			repoInfo := ""
			if issues[i].RepoName != "" {
				repoInfo = " - " + issues[i].RepoName + " issue"
			}
			allSamples = append(allSamples, textSampleWithTime{
				sample:    fmt.Sprintf("* \"%s\"%s", issues[i].Title, repoInfo),
				timestamp: issues[i].UpdatedAt,
			})
		}
	}

	// Collect from comments
	for _, comment := range comments {
		if comment.Body != "" && !seen[comment.Body] {
			// Take first 150 chars if it's long
			body := comment.Body
			if len(body) > 150 {
				body = body[:150] + "..."
			}
			// Skip template comments
			if strings.Contains(body, "<!--") || strings.Contains(body, "## Checklist") {
				continue
			}
			seen[comment.Body] = true
			repoInfo := ""
			if comment.Repository != "" {
				repoInfo = " - " + comment.Repository + " comment"
			}
			allSamples = append(allSamples, textSampleWithTime{
				sample:    fmt.Sprintf("* \"%s\"%s", body, repoInfo),
				timestamp: comment.UpdatedAt,
			})
		}
	}

	// Sort by timestamp (most recent first)
	sort.Slice(allSamples, func(i, j int) bool {
		return allSamples[i].timestamp.After(allSamples[j].timestamp)
	})

	// Take the most recent samples up to maxSamples
	var result []string
	for i, s := range allSamples {
		if i >= maxSamples {
			break
		}
		result = append(result, s.sample)
	}

	return result
}
