# ghutz - GitHub User Timezone Detector

A Go library and CLI tool that derives timezone and GPS coordinates for GitHub users, enabling better pairing for code reviews across time zones.

## Features

- **Multiple Detection Methods**: Uses various approaches to determine user timezone
  - GitHub profile scraping for local time
  - Location field geocoding via Google Maps API
  - Pull request activity pattern analysis
  - Contextual analysis using Gemini AI
- **Flexible Authentication**: Supports GitHub tokens from environment or `gh` CLI
- **Web Interface**: Built-in web server with user-friendly form
- **Cloud Ready**: Configured for deployment to Google Cloud Run with ko

## Installation

```bash
go install github.com/ghutz/ghutz/cmd/ghutz@latest
```

Or build from source:

```bash
make build
```

## Usage

### CLI Mode

```bash
# Detect timezone for a GitHub user
ghutz tstromberg

# Output as JSON
ghutz --json tstromberg

# With verbose logging
ghutz --verbose tstromberg
```

### Web Server Mode

```bash
# Start web server on port 8080
ghutz --serve

# Custom port
ghutz --serve --port 3000
```

Then visit http://localhost:8080 to use the web interface.

## Configuration

### Authentication

The tool uses the following authentication methods in order:

1. `--github-token` flag
2. `GITHUB_TOKEN` environment variable
3. `gh auth token` command (if gh CLI is installed)

### API Keys

Set these environment variables or use flags:

- `GEMINI_API_KEY` - For Gemini AI contextual analysis
- `GOOGLE_MAPS_API_KEY` - For geocoding and timezone API
- `GOOGLE_CLOUD_PROJECT` - For GCP services

## Deployment to Cloud Run

The project includes ko configuration for easy deployment:

```bash
# Deploy to Cloud Run
make deploy

# Or manually with ko
KO_DOCKER_REPO=gcr.io/your-project ko build ./cmd/ghutz
```

## Development

```bash
# Run tests
make test

# Run linter
make lint

# Build and run locally
make run ARGS="username"

# Start development server
make serve
```

## How It Works

1. **Profile Timezone**: Attempts to extract timezone from GitHub profile page
2. **Location Geocoding**: If location field exists, geocodes it to coordinates
3. **Activity Analysis**: Analyzes PR creation times to infer work hours
4. **AI Context**: Uses Gemini to analyze all available context clues

The tool prioritizes timezone accuracy over location accuracy, as timezone is more critical for code review pairing.

## License

MIT