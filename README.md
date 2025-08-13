<div align="center">
  <img src="media/octocat.png" alt="ghuTZ Detective Logo" width="200">
  
  # ghuTZ - GitHub Timezone Detective 🔍
  
  [![Go Report Card](https://goreportcard.com/badge/github.com/codeGROOVE-dev/ghuTZ)](https://goreportcard.com/report/github.com/codeGROOVE-dev/ghuTZ)
  [![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
  [![Go Version](https://img.shields.io/badge/go-%3E%3D1.21-blue)](go.mod)
  
  **A Go library and CLI tool that derives timezone and GPS coordinates for GitHub users**  
  *Enabling better pairing for code reviews across time zones*
</div>

---

## 🎯 Features

- **🕵️ Multiple Detection Methods**: Uses 8+ approaches to determine user timezone
  - GitHub profile scraping for local time
  - Location field geocoding via Google Maps API  
  - Pull request activity pattern analysis
  - Lunch break detection from activity gaps
  - Contextual analysis using Gemini AI with private investigator persona
  - Organization membership analysis
  - Website/blog content extraction
  - Username pattern recognition

- **🔐 Flexible Authentication**: Supports GitHub tokens from environment or `gh` CLI

- **🌐 Web Interface**: Beautiful detective-themed web UI with confidence indicators

- **☁️ Cloud Ready**: Configured for deployment to Google Cloud Run with ko

- **⚡ Fast & Cached**: Results cached for 20 days, sub-2 second detection

## 📦 Installation

### Using Go Install

```bash
go install github.com/codeGROOVE-dev/ghuTZ/cmd/ghutz@latest
```

### Build from Source

```bash
git clone https://github.com/codeGROOVE-dev/ghuTZ.git
cd ghuTZ
make build
# Binaries will be in out/ directory
```

## 🚀 Usage

### CLI Mode

```bash
# Detect timezone for a GitHub user
./out/ghutz tstromberg

# Output as JSON
./out/ghutz --json tstromberg

# With verbose logging (see the detective at work!)
./out/ghutz --verbose tstromberg

# Force activity pattern analysis
./out/ghutz --activity tstromberg
```

### Web Server Mode

```bash
# Start web server on port 8080
./out/ghutz-server

# Custom port
./out/ghutz-server --port 3000

# Then visit http://localhost:8080
```

<div align="center">
  <img src="media/screenshot.png" alt="Web Interface Screenshot" width="600">
</div>

### As a Library

```go
import "github.com/codeGROOVE-dev/ghuTZ/pkg/ghutz"

detector := ghutz.New(
    ghutz.WithGitHubToken(token),
    ghutz.WithGoogleMapsAPIKey(mapsKey),
    ghutz.WithGeminiAPIKey(geminiKey),
)
defer detector.Close()

result, err := detector.Detect(context.Background(), "octocat")
if err != nil {
    log.Fatal(err)
}

fmt.Printf("%s is in %s (%.0f%% confidence)\n", 
    result.Username, 
    result.Timezone,
    result.Confidence*100)
```

## 🔑 Configuration

### Environment Variables

```bash
# Required for full functionality
export GITHUB_TOKEN="ghp_..." # GitHub personal access token
export GOOGLE_MAPS_API_KEY="..." # For geocoding locations
export GEMINI_API_KEY="..." # For AI-powered detection

# Optional
export CACHE_DIR="/custom/cache/path" # Default: ~/.cache/ghutz
export PORT="8080" # For web server
```

### Using GitHub CLI Token

If you have `gh` CLI installed and authenticated, ghuTZ will automatically use its token:

```bash
gh auth login
./out/ghutz username # Will use gh's token
```

## 🏗️ Architecture

```
pkg/ghutz/
├── detector.go       # Main detection orchestration
├── activity.go       # PR/Issue activity pattern analysis  
├── cache.go         # Otter-based caching (20-day TTL)
├── gemini_prompt.go # AI detective persona
└── types.go         # Core data structures

cmd/
├── ghutz/          # CLI application
└── ghutz-server/   # Web server with detective UI
    ├── templates/  # HTML templates
    └── static/     # JavaScript & detective logo
```

## 🔬 Detection Methods

1. **Profile Scraping**: Checks GitHub profile HTML for timezone data
2. **Location Geocoding**: Converts location field to coordinates → timezone
3. **Activity Analysis**: Analyzes PR/issue/comment timestamps for patterns
4. **Lunch Detection**: Identifies midday activity gaps (12-1pm typical)
5. **Sleep Pattern**: Finds consistent quiet hours (midnight-6am typical)
6. **AI Context**: Gemini analyzes all available data with detective reasoning
7. **Organization Hints**: Checks org locations and descriptions
8. **Website Scanning**: Extracts location clues from personal sites

## 🐳 Docker Deployment

### Using ko (recommended for Cloud Run)

```bash
# Install ko
go install github.com/google/ko@latest

# Deploy to Cloud Run
export KO_DOCKER_REPO=gcr.io/your-project
make deploy
```

### Traditional Docker

```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o ghutz-server ./cmd/ghutz-server

FROM alpine:latest
RUN apk --no-cache add ca-certificates
COPY --from=builder /app/ghutz-server /ghutz-server
COPY --from=builder /app/cmd/ghutz-server/templates /templates
COPY --from=builder /app/cmd/ghutz-server/static /static
CMD ["/ghutz-server"]
```

## 🧪 Development

```bash
# Run tests
make test

# Lint code
make lint

# Fix linting issues
make fix

# Clean build artifacts
make clean

# Full build pipeline
make all
```

## 📊 API Endpoints

### REST API

```bash
# Detect timezone (GET or POST)
curl http://localhost:8080/api/v1/detect?username=octocat

# Returns:
{
  "username": "octocat",
  "timezone": "America/Los_Angeles",
  "confidence": 0.85,
  "method": "activity_patterns",
  "location": {
    "latitude": 37.7749,
    "longitude": -122.4194
  }
}
```

## 🤝 Contributing

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing`)
5. Open a Pull Request

## 📜 License

MIT License - see [LICENSE](LICENSE) file for details

## 🙏 Acknowledgments

- Detective Octocat logo - because every timezone detective needs a magnifying glass
- GitHub API for making this possible
- Google Maps & Gemini APIs for enhanced detection
- The Go community for excellent libraries

## 🔗 Links

- [GitHub Repository](https://github.com/codeGROOVE-dev/ghuTZ)
- [Issue Tracker](https://github.com/codeGROOVE-dev/ghuTZ/issues)
- [codeGROOVE](https://codegroove.dev)

---

<div align="center">
  <sub>Built with 🔍 detective skills and ☕ by <a href="https://codegroove.dev">codeGROOVE</a></sub>
</div>