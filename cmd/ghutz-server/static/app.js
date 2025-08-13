let currentUsername = '';

window.addEventListener('load', function() {
    // Check if username is already filled (from server-side template)
    const inputElement = document.getElementById('username');
    const existingUsername = inputElement.value.trim();
    
    if (existingUsername) {
        // Auto-detect if username is pre-filled from URL
        // Small delay to ensure page is fully loaded
        setTimeout(() => {
            detectUser(existingUsername);
        }, 100);
    }
});

function updateURL(username) {
    currentUsername = username;
    // Use clean URL structure: /username
    // Handle empty username (go to homepage)
    const newURL = username ? '/' + username : '/';
    window.history.replaceState({}, '', newURL);
}

async function detectUser(username) {
    const submitBtn = document.getElementById('submitBtn');
    const errorDiv = document.getElementById('error');
    const resultDiv = document.getElementById('result');

    if (!username) return;

    submitBtn.disabled = true;
    submitBtn.innerHTML = 'TRACKING...';
    const loadingEl = document.getElementById('loading');
    if (loadingEl) loadingEl.classList.add('show');
    errorDiv.classList.remove('show');
    resultDiv.classList.remove('show');

    try {
        const response = await fetch('/api/v1/detect', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({username})
        });

        if (!response.ok) {
            const errorText = await response.text();
            throw new Error(errorText || 'Detection failed');
        }

        const data = await response.json();
        updateURL(username);
        displayResults(data);

    } catch (error) {
        // Check if it's a 404 (user not found)
        if (error.message.includes('404') || error.message.includes('not found')) {
            errorDiv.innerHTML = 'SUSPECT NOT FOUND: "' + username + '" - They\'ve gone off the grid!';
        } else {
            errorDiv.innerHTML = 'TRAIL WENT COLD: ' + error.message;
        }
        errorDiv.classList.add('show');
    } finally {
        submitBtn.disabled = false;
        submitBtn.innerHTML = 'TRACK DEVELOPER';
        const loadingEl = document.getElementById('loading');
        if (loadingEl) loadingEl.classList.remove('show');
    }
}

document.getElementById('detectForm').addEventListener('submit', async (e) => {
    e.preventDefault();
    const username = document.getElementById('username').value.trim();
    await detectUser(username);
});

function displayResults(data) {
    // Reset all optional fields first
    document.getElementById('nameRow').style.display = 'none';
    document.getElementById('activityRow').style.display = 'none';
    document.getElementById('hoursRow').style.display = 'none';
    document.getElementById('lunchRow').style.display = 'none';
    document.getElementById('locationRow').style.display = 'none';
    document.getElementById('mapRow').style.display = 'none';
    
    // Set required fields
    document.getElementById('displayUsername').textContent = data.username;
    document.getElementById('timezone').textContent = data.timezone;

    // Display full name if available
    if (data.name) {
        document.getElementById('displayName').textContent = data.name;
        document.getElementById('nameRow').style.display = 'block';
    }

    // Update confidence meter
    const confidencePct = Math.round((data.timezone_confidence || data.confidence || 0) * 100);
    const confidenceBar = document.getElementById('confidenceBar');
    const confidenceText = document.getElementById('confidenceText');
    
    if (confidenceBar) {
        confidenceBar.style.width = confidencePct + '%';
    }
    if (confidenceText) {
        confidenceText.textContent = confidencePct + '%';
    }

    if (data.activity_timezone) {
        document.getElementById('activityTz').textContent = data.activity_timezone;
        document.getElementById('activityRow').style.display = 'block';
    }

    if (data.active_hours_local && (data.active_hours_local.start || data.active_hours_local.end)) {
        const activeHoursText = formatActiveHours(data.active_hours_local.start, data.active_hours_local.end);
        document.getElementById('activeHours').textContent = activeHoursText;
        document.getElementById('hoursRow').style.display = 'block';
    }

    if (data.lunch_hours_local && data.lunch_hours_local.confidence > 0) {
        const lunchText = formatLunchHours(data.lunch_hours_local.start, data.lunch_hours_local.end);
        document.getElementById('lunchHours').textContent = lunchText + ' (' + Math.round(data.lunch_hours_local.confidence * 100) + '%)';
        document.getElementById('lunchRow').style.display = 'block';
    }

    let locationText = '';
    if (data.gemini_suggested_location) {
        locationText = data.gemini_suggested_location;
    } else if (data.location_name) {
        locationText = data.location_name;
    } else if (data.location) {
        locationText = data.location.latitude.toFixed(2) + ', ' + data.location.longitude.toFixed(2);
    }
    
    if (locationText) {
        document.getElementById('location').textContent = locationText;
        document.getElementById('locationRow').style.display = 'block';
    }

    const methodName = formatMethodName(data.method);
    document.getElementById('method').textContent = methodName;

    if (data.location) {
        const mapRow = document.getElementById('mapRow');
        if (mapRow) {
            mapRow.style.display = 'block';
        }
        initMap(data.location.latitude, data.location.longitude, data.username);
    }

    document.getElementById('result').classList.add('show');
}

function formatMethodName(method) {
    const methodNames = {
        'github_profile': 'Profile Scraping',
        'location_geocoding': 'Location Geocoding', 
        'activity_patterns': 'Activity Analysis',
        'gemini_refined_activity': 'AI + Activity',
        'company_heuristic': 'Company Intel',
        'email_heuristic': 'Email Domain',
        'blog_heuristic': 'Blog Analysis',
        'website_gemini_analysis': 'Website + AI',
        'gemini_analysis': 'AI Analysis'
    };
    return methodNames[method] || method;
}

function formatActiveHours(start, end) {
    return formatHour(start) + '-' + formatHour(end);
}

function formatLunchHours(start, end) {
    return formatHour(start) + '-' + formatHour(end);
}

function formatHour(decimalHour) {
    const hour = Math.floor(decimalHour);
    const minutes = Math.round((decimalHour - hour) * 60);
    const period = hour < 12 ? 'am' : 'pm';
    const displayHour = hour === 0 ? 12 : hour > 12 ? hour - 12 : hour;
    const minuteStr = minutes === 0 ? '00' : minutes.toString().padStart(2, '0');
    return displayHour + ':' + minuteStr + period;
}

function initMap(lat, lng, username) {
    const mapDiv = document.getElementById('map');
    if (!mapDiv) return;
    
    // Clear any existing content
    mapDiv.innerHTML = '';
    
    // Check if Leaflet is loaded
    if (typeof L === 'undefined') {
        // Fallback to simple link if Leaflet fails to load
        const mapLink = document.createElement('a');
        mapLink.href = `https://www.openstreetmap.org/?mlat=${lat}&mlon=${lng}&zoom=10#map=10/${lat}/${lng}`;
        mapLink.target = '_blank';
        mapLink.textContent = `View ${username} on OpenStreetMap (${lat.toFixed(2)}, ${lng.toFixed(2)})`;
        mapLink.style.cssText = 'color: #000; text-decoration: underline;';
        mapDiv.appendChild(mapLink);
        return;
    }
    
    // Create the map
    const map = L.map(mapDiv).setView([lat, lng], 10);
    
    // Add OpenStreetMap tiles
    L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
        attribution: '© OpenStreetMap contributors',
        maxZoom: 18,
    }).addTo(map);
    
    // Add a marker for the user location
    L.marker([lat, lng])
        .addTo(map)
        .bindPopup(`${username}<br/>${lat.toFixed(2)}, ${lng.toFixed(2)}`)
        .openPopup();
}

document.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' && e.ctrlKey) {
        document.getElementById('detectForm').dispatchEvent(new Event('submit'));
    }
});