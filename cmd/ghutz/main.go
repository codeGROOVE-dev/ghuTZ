package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ghutz/ghutz/pkg/ghutz"
)

var (
	serve        = flag.Bool("serve", false, "Run as web server")
	port         = flag.String("port", "8080", "Port for web server")
	githubToken  = flag.String("github-token", "", "GitHub API token (defaults to gh auth token)")
	geminiAPIKey = flag.String("gemini-api-key", os.Getenv("GEMINI_API_KEY"), "Gemini API key")
	geminiModel  = flag.String("gemini-model", "gemini-2.5-flash", "Gemini model to use (e.g., gemini-1.5-flash, gemini-1.5-pro)")
	mapsAPIKey   = flag.String("maps-api-key", os.Getenv("GOOGLE_MAPS_API_KEY"), "Google Maps API key")
	gcpProject   = flag.String("gcp-project", os.Getenv("GOOGLE_CLOUD_PROJECT"), "Google Cloud project ID")
	verbose      = flag.Bool("verbose", false, "Enable verbose logging")
	jsonOutput   = flag.Bool("json", false, "Output as JSON")
)

func main() {
	flag.Parse()

	var logger *slog.Logger
	if *verbose {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		}))
	} else {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	token := *githubToken
	if token == "" {
		token = getGitHubToken()
	}

	detector := ghutz.NewWithLogger(logger,
		ghutz.WithGitHubToken(token),
		ghutz.WithMapsAPIKey(*mapsAPIKey),
		ghutz.WithGeminiAPIKey(*geminiAPIKey),
		ghutz.WithGeminiModel(*geminiModel),
		ghutz.WithGCPProject(*gcpProject),
	)

	if *serve {
		runServer(detector)
		return
	}

	if flag.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] <github-username>\n", os.Args[0])
		flag.PrintDefaults()
		os.Exit(1)
	}

	username := flag.Arg(0)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := detector.Detect(ctx, username)
	if err != nil {
		cancel()
		log.Fatalf("Error: %v", err)
	}

	if *jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			log.Fatalf("Error encoding JSON: %v", err)
		}
		return
	}

	fmt.Println()
	fmt.Printf("╭─────────────────────────────────────────────────────╮\n")
	fmt.Printf("│  GitHub User Timezone Detection                    │\n")
	fmt.Printf("╰─────────────────────────────────────────────────────╯\n")
	fmt.Println()

	fmt.Printf("  👤 User:       %s\n", result.Username)

	tzConfidence := result.TimezoneConfidence
	if tzConfidence == 0 {
		tzConfidence = result.Confidence
	}
	// Get current local time in detected timezone
	localTimeStr := ""
	if loc, err := time.LoadLocation(result.Timezone); err == nil {
		now := time.Now().In(loc)
		localTimeStr = fmt.Sprintf(" (Local time: %s)", now.Format("3:04 PM MST"))
	}
	
	fmt.Printf("  🕐 Timezone:   %s%s ", result.Timezone, localTimeStr)
	printConfidenceBadge(tzConfidence)
	fmt.Println()

	var displayedLocation string
	if result.GeminiSuggestedLocation != "" {
		displayedLocation = result.GeminiSuggestedLocation
		fmt.Printf("  📍 Location:   %s (AI-suggested)\n", displayedLocation)
	} else if result.LocationName != "" {
		displayedLocation = result.LocationName
		fmt.Printf("  📍 Location:   %s\n", displayedLocation)
	} else if result.Location != nil {
		displayedLocation = getLocationNameForTimezone(result.Timezone)
		fmt.Printf("  📍 Location:   %s (approximate)\n", displayedLocation)
	} else {
		displayedLocation = getIntelligentLocationGuess(result.Timezone, result.Method)
		if displayedLocation != "" {
			fmt.Printf("  📍 Location:   %s\n", displayedLocation)
		}
	}

	if result.Location != nil {
		locConfidence := result.LocationConfidence
		if locConfidence == 0 {
			locConfidence = result.Confidence * 0.8
		}
		fmt.Printf("  🗺  Coordinates: %.5f, %.5f ", result.Location.Latitude, result.Location.Longitude)
		printConfidenceBadge(locConfidence)
		fmt.Println()
		fmt.Printf("  🔗 Map:        https://maps.google.com/?q=%.5f,%.5f\n",
			result.Location.Latitude, result.Location.Longitude)
	} else if displayedLocation != "" {
		fmt.Printf("  🔗 Map:        https://maps.google.com/?q=%s\n",
			strings.ReplaceAll(displayedLocation, " ", "+"))
	}

	methodName := formatMethodName(result.Method)
	fmt.Printf("  ⚙️  Method:     %s\n", methodName)
	fmt.Println()
}

func runServer(detector *ghutz.Detector) {
	http.HandleFunc("/", handleHome)
	http.HandleFunc("/detect", handleDetect(detector))
	http.HandleFunc("/api/detect", handleAPIDetect(detector))

	addr := ":" + *port
	log.Printf("Starting server on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

func handleHome(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tmpl := `<!DOCTYPE html>
<html>
<head>
    <title>GitHub User Timezone Detector</title>
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Helvetica, Arial, sans-serif;
            max-width: 800px;
            margin: 50px auto;
            padding: 20px;
            background: #f6f8fa;
        }
        .container {
            background: white;
            padding: 30px;
            border-radius: 6px;
            box-shadow: 0 1px 3px rgba(0,0,0,0.12);
        }
        h1 {
            color: #24292e;
            margin-bottom: 20px;
        }
        input[type="text"] {
            width: 100%;
            padding: 10px;
            font-size: 16px;
            border: 1px solid #d1d5da;
            border-radius: 6px;
            margin-bottom: 15px;
        }
        button {
            background: #2ea44f;
            color: white;
            padding: 10px 20px;
            font-size: 16px;
            border: none;
            border-radius: 6px;
            cursor: pointer;
        }
        button:hover {
            background: #2c974b;
        }
        button:disabled {
            background: #94d3a2;
            cursor: not-allowed;
        }
        #result {
            margin-top: 30px;
            padding: 20px;
            background: #f1f8ff;
            border-radius: 6px;
            display: none;
        }
        #result.show {
            display: block;
        }
        #error {
            color: #cb2431;
            margin-top: 10px;
        }
        #map {
            height: 400px;
            margin-top: 20px;
            border-radius: 6px;
        }
        .confidence {
            display: inline-block;
            padding: 2px 6px;
            border-radius: 3px;
            font-size: 12px;
            font-weight: 600;
        }
        .confidence.high { background: #2ea44f; color: white; }
        .confidence.medium { background: #fb8500; color: white; }
        .confidence.low { background: #cb2431; color: white; }
    </style>
</head>
<body>
    <div class="container">
        <h1>ghuTZ - GitHub User Timezone Detector</h1>
        <p>Enter a GitHub username to detect their timezone and approximate location.</p>

        <form id="detectForm">
            <input type="text" id="username" placeholder="Enter GitHub username" required>
            <button type="submit" id="submitBtn">Detect Timezone</button>
        </form>

        <div id="error"></div>

        <div id="result">
            <h2>Results for <span id="resultUsername"></span></h2>
            <p><strong>Timezone:</strong> <span id="timezone"></span></p>
            <p><strong>Confidence:</strong> <span id="confidence"></span></p>
            <p><strong>Method:</strong> <span id="method"></span></p>
            <div id="map"></div>
        </div>
    </div>

    <script>
        document.getElementById('detectForm').addEventListener('submit', async (e) => {
            e.preventDefault();

            const username = document.getElementById('username').value;
            const submitBtn = document.getElementById('submitBtn');
            const errorDiv = document.getElementById('error');
            const resultDiv = document.getElementById('result');

            submitBtn.disabled = true;
            submitBtn.textContent = 'Detecting...';
            errorDiv.textContent = '';
            resultDiv.classList.remove('show');

            try {
                const response = await fetch('/api/detect', {
                    method: 'POST',
                    headers: {'Content-Type': 'application/json'},
                    body: JSON.stringify({username})
                });

                if (!response.ok) {
                    throw new Error('Detection failed');
                }

                const data = await response.json();

                document.getElementById('resultUsername').textContent = data.username;
                document.getElementById('timezone').textContent = data.timezone;

                const confidenceSpan = document.getElementById('confidence');
                const confidencePct = Math.round(data.confidence * 100);
                confidenceSpan.textContent = confidencePct + '%';
                confidenceSpan.className = 'confidence';
                if (confidencePct >= 80) confidenceSpan.classList.add('high');
                else if (confidencePct >= 50) confidenceSpan.classList.add('medium');
                else confidenceSpan.classList.add('low');

                document.getElementById('method').textContent = data.method.replace('_', ' ');

                resultDiv.classList.add('show');

                if (data.location) {
                    initMap(data.location.latitude, data.location.longitude, data.username);
                }

            } catch (error) {
                errorDiv.textContent = 'Error: ' + error.message;
            } finally {
                submitBtn.disabled = false;
                submitBtn.textContent = 'Detect Timezone';
            }
        });

        function initMap(lat, lng, username) {
            const mapDiv = document.getElementById('map');
            mapDiv.innerHTML = '<iframe width="100%" height="100%" frameborder="0" style="border:0" ' +
                'src="https://www.openstreetmap.org/export/embed.html?bbox=' +
                (lng - 0.1) + ',' + (lat - 0.1) + ',' + (lng + 0.1) + ',' + (lat + 0.1) +
                '&layer=mapnik&marker=' + lat + ',' + lng + '" allowfullscreen></iframe>';
        }
    </script>
</body>
</html>`

	w.Header().Set("Content-Type", "text/html")
	_, _ = fmt.Fprint(w, tmpl)
}

func handleDetect(detector *ghutz.Detector) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		username := r.FormValue("username")
		if username == "" {
			http.Error(w, "Username required", http.StatusBadRequest)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		result, err := detector.Detect(ctx, username)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		tmpl := template.Must(template.New("result").Parse(`
			<h2>Results for {{.Username}}</h2>
			<p>Timezone: {{.Timezone}}</p>
			{{if .Location}}
			<p>Location: {{.Location.Latitude}}, {{.Location.Longitude}}</p>
			{{end}}
			<p>Confidence: {{.Confidence}}</p>
			<p>Method: {{.Method}}</p>
		`))

		w.Header().Set("Content-Type", "text/html")
		if err := tmpl.Execute(w, result); err != nil {
			log.Printf("template execution failed: %v", err)
		}
	}
}

func handleAPIDetect(detector *ghutz.Detector) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Username string `json:"username"`
		}

		r.Body = http.MaxBytesReader(w, r.Body, 1024*1024)
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			log.Printf("invalid request: %v from %s", err, r.RemoteAddr)
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		req.Username = strings.TrimSpace(req.Username)
		if req.Username == "" || len(req.Username) > 100 {
			log.Printf("invalid username %q from %s", req.Username, r.RemoteAddr)
			http.Error(w, "Invalid username", http.StatusBadRequest)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		result, err := detector.Detect(ctx, req.Username)
		if err != nil {
			log.Printf("detection failed for %s: %v", req.Username, err)
			http.Error(w, "Detection failed", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	}
}

// Helper functions for enhanced output

func printConfidenceBadge(confidence float64) {
	percent := confidence * 100
	var badge string
	var color string

	switch {
	case percent >= 90:
		badge = "●●●"
		color = "\033[32m" // Green
	case percent >= 70:
		badge = "●●○"
		color = "\033[33m" // Yellow
	case percent >= 50:
		badge = "●○○"
		color = "\033[33m" // Yellow
	default:
		badge = "○○○"
		color = "\033[31m" // Red
	}

	// Print colored badge with percentage
	fmt.Printf("%s%s\033[0m (%.0f%%)", color, badge, percent)
}

func formatMethodName(method string) string {
	methodNames := map[string]string{
		"github_profile":          "GitHub Profile Timezone",
		"location_geocoding":      "Location Field Geocoding",
		"activity_patterns":       "Activity Pattern Analysis",
		"company_heuristic":       "Company-based Inference",
		"email_heuristic":         "Email Domain Analysis",
		"blog_heuristic":          "Blog Domain Analysis",
		"website_gemini_analysis": "Website AI Analysis",
		"website_pattern_match":   "Website Pattern Matching",
		"gemini_analysis":         "AI Context Analysis",
		"gemini_inferred":         "AI Location Inference",
		"known_location":          "Known Location Database",
		"default_fallback":        "Default Fallback",
	}

	if name, ok := methodNames[method]; ok {
		return name
	}
	return method
}

func getLocationNameForTimezone(timezone string) string {
	// Handle Etc/GMT timezones specially
	if strings.HasPrefix(timezone, "Etc/GMT") {
		// Etc/GMT+N means UTC-N (confusingly inverted)
		if timezone == "Etc/GMT+1" {
			return "Atlantic (UTC-1)"
		} else if timezone == "Etc/GMT-1" {
			return "Central Europe (UTC+1)"
		}
		return strings.Replace(timezone, "Etc/", "", 1)
	}
	
	// Map timezones to their primary cities/regions
	timezoneLocations := map[string]string{
		"America/Los_Angeles":  "San Francisco Bay Area, CA", // More representative for tech workers
		"America/Vancouver":    "Vancouver, BC",
		"America/Denver":       "Denver, CO",
		"America/Phoenix":      "Phoenix, AZ",
		"America/Chicago":      "Chicago, IL",
		"America/New_York":     "New York, NY",
		"America/Toronto":      "Toronto, ON",
		"America/Mexico_City":  "Mexico City, Mexico",
		"America/Sao_Paulo":    "São Paulo, Brazil",
		"America/Buenos_Aires": "Buenos Aires, Argentina",
		"America/Halifax":      "Halifax, NS",
		"America/St_Johns":     "St. John's, NL",
		"Europe/London":        "London, UK",
		"Europe/Paris":         "Paris, France",
		"Europe/Berlin":        "Berlin, Germany",
		"Europe/Madrid":        "Madrid, Spain",
		"Europe/Rome":          "Rome, Italy",
		"Europe/Amsterdam":     "Amsterdam, Netherlands",
		"Europe/Brussels":      "Brussels, Belgium",
		"Europe/Warsaw":        "Warsaw, Poland",
		"Europe/Stockholm":     "Stockholm, Sweden",
		"Europe/Oslo":          "Oslo, Norway",
		"Europe/Copenhagen":    "Copenhagen, Denmark",
		"Europe/Helsinki":      "Helsinki, Finland",
		"Europe/Athens":        "Athens, Greece",
		"Europe/Istanbul":      "Istanbul, Turkey",
		"Europe/Moscow":        "Moscow, Russia",
		"Europe/Kiev":          "Kyiv, Ukraine",
		"Asia/Tokyo":           "Tokyo, Japan",
		"Asia/Shanghai":        "Shanghai, China",
		"Asia/Hong_Kong":       "Hong Kong",
		"Asia/Singapore":       "Singapore",
		"Asia/Seoul":           "Seoul, South Korea",
		"Asia/Taipei":          "Taipei, Taiwan",
		"Asia/Bangkok":         "Bangkok, Thailand",
		"Asia/Jakarta":         "Jakarta, Indonesia",
		"Asia/Manila":          "Manila, Philippines",
		"Asia/Kolkata":         "Kolkata, India",
		"Asia/Mumbai":          "Mumbai, India",
		"Asia/Dubai":           "Dubai, UAE",
		"Asia/Jerusalem":       "Jerusalem, Israel",
		"Africa/Cairo":         "Cairo, Egypt",
		"Africa/Lagos":         "Lagos, Nigeria",
		"Africa/Johannesburg":  "Johannesburg, South Africa",
		"Australia/Sydney":     "Sydney, Australia",
		"Australia/Melbourne":  "Melbourne, Australia",
		"Australia/Brisbane":   "Brisbane, Australia",
		"Australia/Perth":      "Perth, Australia",
		"Pacific/Auckland":     "Auckland, New Zealand",
		"Pacific/Honolulu":     "Honolulu, HI",
		"Pacific/Anchorage":    "Anchorage, AK",
		"UTC":                  "UTC (No specific location)",
	}

	if location, ok := timezoneLocations[timezone]; ok {
		return location
	}

	// Try to extract a reasonable name from the timezone string
	parts := strings.Split(timezone, "/")
	if len(parts) >= 2 {
		city := strings.ReplaceAll(parts[len(parts)-1], "_", " ")
		return city
	}

	return timezone
}

// getIntelligentLocationGuess provides educated location guess based on timezone and detection method
func getIntelligentLocationGuess(timezone, method string) string {
	// For activity pattern detection with improved timezones, provide intelligent guess
	if method == "activity_patterns" {
		switch timezone {
		case "Europe/Warsaw":
			return "Poland (inferred from name and activity patterns)"
		case "Europe/Berlin":
			return "Germany (inferred from activity patterns)"
		case "Europe/Paris":
			return "France (inferred from activity patterns)"
		case "America/Denver":
			return "Colorado, USA (inferred from activity patterns)"
		case "America/Los_Angeles":
			return "California, USA (inferred from activity patterns)"
		case "America/New_York":
			return "Eastern USA (inferred from activity patterns)"
		}
	}

	// For other methods, use the generic mapping
	if location := getLocationNameForTimezone(timezone); location != timezone {
		return fmt.Sprintf("%s (approximate)", location)
	}

	return ""
}
