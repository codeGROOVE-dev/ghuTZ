<div align="center">
  <img src="media/octocat-small.png" alt="guTZ Detective" width="120">

  # guTZ 🕵️

  [![Experimental](https://img.shields.io/badge/status-experimental-orange.svg)](https://github.com/codeGROOVE-dev/guTZ)
  [![Go Report Card](https://goreportcard.com/badge/github.com/codeGROOVE-dev/guTZ)](https://goreportcard.com/report/github.com/codeGROOVE-dev/guTZ)
  [![Go Code](https://img.shields.io/badge/go%20code-pretty%20good-brightgreen.svg)](go.mod)

  **Stalks GitHub users to figure out where they live** 🌍
  *(for perfectly legitimate timezone coordination purposes)*
</div>

---

## What Is This Madness?

Ever wondered when your favorite open source maintainer is actually awake? Tired of pinging someone at 3am their time? **guTZ** is here to help by being *slightly creepy* in the name of better collaboration.

We analyze GitHub activity patterns with the determination of a caffeinated detective to figure out where on Earth someone codes from. It's like GeoGuessr, but for developers!

<div align="center">
  <img src="media/screenshot.png" alt="The Detective UI in Action" width="500">
  <br><sub><i>Our detective UI tracking down timezones since 2024</i></sub>
</div>

## Live Demo

https://tz.github.robot-army.dev/

## Quick Start

```bash
# Install it
go install github.com/codeGROOVE-dev/guTZ/cmd/gutz@latest

# Stalk someone (respectfully)
gutz torvalds

# Start the web detective agency
gutz-server
# Visit http://localhost:8080 for the full experience
```

## How It Works

Our digital detective employs **15+ sophisticated methods** to triangulate a user's timezone:

🔍 **The Sherlock Suite:**
- **Profile HTML scraping** - extracts timezone data from GitHub's rendered pages
- **Location geocoding** - turns "San Francisco, CA" into precise UTC offsets
- **Activity pattern analysis** - detects sleep cycles, work hours, and peak productivity  
- **Lunch break detection** - finds those sacred 30-90min breaks between 11am-2:30pm
- **Social media stalking** - follows Mastodon, Twitter, Bluesky links for location hints
- **Website excavation** - crawls personal websites for timezone breadcrumbs
- **Repository archaeology** - analyzes contributed repos for regional patterns (.ca domains, anyone?)
- **Organization infiltration** - checks company locations of user's orgs
- **Comment linguistics** - scans issue/PR comments for timezone mentions
- **Country TLD detection** - spots .uk, .ca, .de domains in all URLs
- **DST transition analysis** - detects daylight saving time patterns in activity
- **Evening activity prioritization** - 7-11pm local time reveals true location
- **Sleep pattern analysis** - identifies 6-8 hour quiet periods
- **Multi-source confidence scoring** - weighs all signals for final verdict
- **AI detective interrogation** - Gemini LLM analyzes all evidence with detective persona

> **Pro Tip:** Evening activity (7-11pm) + lunch breaks are our secret weapons. That's when the real coding happens – no meetings, no Slack, just pure commits revealing your true timezone.

## For the Paranoid

Need API keys for maximum stalking efficiency:

```bash
export GITHUB_TOKEN="ghp_..." # More API calls + GraphQL access = deeper intel
export GOOGLE_MAPS_API_KEY="..." # Geocode locations like "San Francisco, CA"
export GEMINI_API_KEY="..." # Our AI detective analyzes all the evidence  
export GCP_PROJECT="your-project" # For Gemini API access (optional)
```

Don't have them? No worries, we'll still deliver results with public data, social scraping, and pure algorithmic detective work.

## Library Usage

```go
import "github.com/codeGROOVE-dev/guTZ/pkg/gutz"

detector := gutz.New(ctx)
result, _ := detector.Detect(ctx, "octocat")

fmt.Printf("%s probably lives in %s (confidence: %.0f%%)\n",
    result.Username, result.Timezone, result.Confidence*100)
// Output: octocat probably lives in America/Los_Angeles (confidence: 85%)
```

## The Fine Print

- **Accuracy:** Frighteningly good! Our ML-powered multi-source approach nails it 85%+ of the time
- **Privacy:** We only use public GitHub data + social media links (but yeah, still kinda creepy)
- **Caching:** Results cached for 20 days with HTTP caching for API calls (people don't move *that* often)
- **Speed:** 2-5 seconds per detection (detective work takes time, but we're getting faster)
- **Data Sources:** GitHub API, GraphQL, Google Maps, Gemini AI, social platforms, and good old web scraping
- **Confidence Scoring:** Every detection includes confidence metrics based on signal strength

## Contributing

Found a bug? Want to add a detection method? PRs welcome!

---

<div align="center">
  <sub>Made with 🪿 by <a href="https://codegroove.dev">codeGROOVE</a></sub>
</div>
