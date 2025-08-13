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
    submitBtn.innerHTML = 'DETECTING...';
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
        errorDiv.textContent = '❌ ' + error.message;
        errorDiv.classList.add('show');
    } finally {
        submitBtn.disabled = false;
        submitBtn.innerHTML = 'DETECT';
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
    document.getElementById('activityLabel').style.display = 'none';
    document.getElementById('activityTz').style.display = 'none';
    document.getElementById('hoursLabel').style.display = 'none';
    document.getElementById('activeHours').style.display = 'none';
    document.getElementById('lunchLabel').style.display = 'none';
    document.getElementById('lunchHours').style.display = 'none';
    document.getElementById('locationLabel').style.display = 'none';
    document.getElementById('location').style.display = 'none';
    document.getElementById('map').style.display = 'none';
    
    // Set required fields
    document.getElementById('displayUsername').textContent = data.username;
    document.getElementById('timezone').textContent = data.timezone;

    // Display full name if available
    if (data.name) {
        document.getElementById('displayName').textContent = data.name;
        document.getElementById('nameRow').style.display = 'contents';
    }

    const confidenceSpan = document.getElementById('confidence');
    const confidencePct = Math.round((data.timezone_confidence || data.confidence || 0) * 100);
    let badgeClass;
    
    if (confidencePct >= 80) {
        badgeClass = 'confidence-high';
    } else if (confidencePct >= 50) {
        badgeClass = 'confidence-medium';
    } else {
        badgeClass = 'confidence-low';
    }

    confidenceSpan.textContent = confidencePct + '%';
    confidenceSpan.className = 'confidence-badge ' + badgeClass;

    if (data.activity_timezone) {
        document.getElementById('activityTz').textContent = data.activity_timezone;
        document.getElementById('activityLabel').style.display = 'flex';
        document.getElementById('activityTz').style.display = 'block';
    }

    if (data.active_hours_local && (data.active_hours_local.start || data.active_hours_local.end)) {
        const activeHoursText = formatActiveHours(data.active_hours_local.start, data.active_hours_local.end);
        document.getElementById('activeHours').textContent = activeHoursText;
        document.getElementById('hoursLabel').style.display = 'flex';
        document.getElementById('activeHours').style.display = 'block';
    }

    if (data.lunch_hours_local && data.lunch_hours_local.confidence > 0) {
        const lunchText = formatLunchHours(data.lunch_hours_local.start, data.lunch_hours_local.end);
        document.getElementById('lunchHours').textContent = lunchText + ' (' + Math.round(data.lunch_hours_local.confidence * 100) + '%)';
        document.getElementById('lunchLabel').style.display = 'flex';
        document.getElementById('lunchHours').style.display = 'block';
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
        document.getElementById('locationLabel').style.display = 'flex';
        document.getElementById('location').style.display = 'block';
    }

    const methodName = formatMethodName(data.method);
    document.getElementById('method').textContent = methodName;

    if (data.location) {
        initMap(data.location.latitude, data.location.longitude, data.username);
    }

    document.getElementById('result').classList.add('show');
}

function formatMethodName(method) {
    const methodNames = {
        'github_profile': 'Profile',
        'location_geocoding': 'Geocoding', 
        'activity_patterns': 'Activity',
        'gemini_refined_activity': 'AI+Activity',
        'company_heuristic': 'Company',
        'email_heuristic': 'Email',
        'blog_heuristic': 'Blog',
        'website_gemini_analysis': 'Website+AI',
        'gemini_analysis': 'AI'
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
    mapDiv.style.display = 'block';
    mapDiv.innerHTML = '<iframe width="100%" height="100%" frameborder="0" style="border:0" ' +
        'src="https://www.openstreetmap.org/export/embed.html?bbox=' +
        (lng - 0.1) + ',' + (lat - 0.1) + ',' + (lng + 0.1) + ',' + (lat + 0.1) +
        '&layer=mapnik&marker=' + lat + ',' + lng + '" allowfullscreen></iframe>';
}

document.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' && e.ctrlKey) {
        document.getElementById('detectForm').dispatchEvent(new Event('submit'));
    }
});