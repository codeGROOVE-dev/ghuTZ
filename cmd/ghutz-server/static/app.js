let currentUsername = '';
let currentMap = null; // Track current map instance

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
    document.getElementById('peakRow').style.display = 'none';
    document.getElementById('quietRow').style.display = 'none';
    document.getElementById('orgsRow').style.display = 'none';
    document.getElementById('histogramRow').style.display = 'none';
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

    // Get UTC offset for timezone conversion
    const utcOffset = getUTCOffsetFromTimezone(data.timezone, data.activity_timezone);
    
    if (data.active_hours_local && (data.active_hours_local.start || data.active_hours_local.end)) {
        // Note: Despite the field name "local", these are actually UTC values that need conversion
        const localStart = (data.active_hours_local.start + utcOffset + 24) % 24;
        const localEnd = (data.active_hours_local.end + utcOffset + 24) % 24;
        const activeHoursText = formatActiveHours(localStart, localEnd);
        document.getElementById('activeHours').textContent = activeHoursText;
        document.getElementById('hoursRow').style.display = 'block';
    }

    if (data.lunch_hours_local && data.lunch_hours_local.confidence > 0) {
        // Note: Despite the field name "local", these are actually UTC values that need conversion
        const localLunchStart = (data.lunch_hours_local.start + utcOffset + 24) % 24;
        const localLunchEnd = (data.lunch_hours_local.end + utcOffset + 24) % 24;
        const lunchText = formatLunchHours(localLunchStart, localLunchEnd);
        const confidenceText = Math.round(data.lunch_hours_local.confidence * 100) + '% confidence';
        document.getElementById('lunchHours').textContent = lunchText + ' (' + confidenceText + ')';
        document.getElementById('lunchRow').style.display = 'block';
    }

    if (data.peak_productivity && data.peak_productivity.count > 0) {
        // Peak productivity is also in UTC and needs conversion
        const localPeakStart = (data.peak_productivity.start + utcOffset + 24) % 24;
        const localPeakEnd = (data.peak_productivity.end + utcOffset + 24) % 24;
        const peakText = formatHour(localPeakStart) + '-' + formatHour(localPeakEnd);
        document.getElementById('peakHours').textContent = peakText;
        document.getElementById('peakRow').style.display = 'block';
    }

    if (data.quiet_hours_utc && data.quiet_hours_utc.length > 0) {
        // Convert UTC quiet hours to local based on timezone (utcOffset already calculated above)
        const quietStart = (data.quiet_hours_utc[0] + utcOffset + 24) % 24;
        const quietEnd = ((data.quiet_hours_utc[data.quiet_hours_utc.length - 1] + 1 + utcOffset) + 24) % 24;
        document.getElementById('quietHours').textContent = formatHour(quietStart) + '-' + formatHour(quietEnd);
        document.getElementById('quietRow').style.display = 'block';
    }

    if (data.top_organizations && data.top_organizations.length > 0) {
        // Create organization list with color-coded counts
        const orgsContainer = document.getElementById('organizations');
        orgsContainer.innerHTML = ''; // Clear existing content
        
        // Colors for top 3 only
        const colors = [
            '#4285F4', // Google Blue for 1st
            '#FBBC04', // Google Yellow for 2nd
            '#EA4335', // Google Red for 3rd
        ];
        const greyColor = '#999999'; // Grey for all others
        
        // Show all organizations with color-coded counts
        data.top_organizations.forEach((org, i) => {
            if (i > 0) {
                const separator = document.createTextNode(', ');
                orgsContainer.appendChild(separator);
            }
            
            const orgSpan = document.createElement('span');
            orgSpan.textContent = org.name + ' ';
            
            // Create colored count in parentheses
            const countSpan = document.createElement('span');
            countSpan.style.color = i < 3 ? colors[i] : greyColor;
            countSpan.style.fontWeight = 'bold';
            countSpan.textContent = `(${org.count})`;
            
            orgSpan.appendChild(countSpan);
            orgsContainer.appendChild(orgSpan);
        });
        
        document.getElementById('orgsRow').style.display = 'block';
    }

    // Draw histogram if activity data is available
    if (data.hourly_activity_utc) {
        drawHistogram(data);
        document.getElementById('histogramRow').style.display = 'block';
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
    const methodElement = document.getElementById('method');
    
    // Clear any existing content
    methodElement.innerHTML = '';
    
    if (data.gemini_reasoning && data.gemini_reasoning.trim()) {
        // Create tooltip container for method with reasoning
        const tooltipContainer = document.createElement('span');
        tooltipContainer.className = 'tooltip-container';
        
        const methodSpan = document.createElement('span');
        methodSpan.className = 'method-with-reasoning';
        methodSpan.textContent = methodName;
        
        const tooltip = document.createElement('span');
        tooltip.className = 'tooltip';
        tooltip.textContent = data.gemini_reasoning;
        
        tooltipContainer.appendChild(methodSpan);
        tooltipContainer.appendChild(tooltip);
        methodElement.appendChild(tooltipContainer);
    } else {
        // No reasoning available, just show method name
        methodElement.textContent = methodName;
    }

    // Show results first so map container has proper dimensions
    document.getElementById('result').classList.add('show');

    if (data.location) {
        const mapRow = document.getElementById('mapRow');
        if (mapRow) {
            mapRow.style.display = 'block';
        }
        // Initialize map after ensuring all DOM updates are complete and Leaflet is ready
        initMapWhenReady(data.location.latitude, data.location.longitude, data.username);
    }
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

function getUTCOffsetFromTimezone(timezone, activityTimezone) {
    // Try to extract UTC offset from timezone string
    if (activityTimezone && activityTimezone.startsWith('UTC')) {
        const offsetStr = activityTimezone.replace('UTC', '').replace('GMT', '');
        const offset = parseInt(offsetStr) || 0;
        return offset;
    }
    
    // Try standard timezone
    if (timezone) {
        try {
            const now = new Date();
            const tzDate = new Date(now.toLocaleString("en-US", {timeZone: timezone}));
            const utcDate = new Date(now.toLocaleString("en-US", {timeZone: "UTC"}));
            return Math.round((tzDate - utcDate) / (1000 * 60 * 60));
        } catch (e) {
            // Fallback for UTC+X format
            if (timezone.startsWith('UTC')) {
                const offsetStr = timezone.replace('UTC', '').replace('GMT', '');
                return parseInt(offsetStr) || 0;
            }
        }
    }
    
    return 0;
}

let activityChart = null; // Global chart instance

function drawHistogram(data) {
    const canvas = document.getElementById('activityChart');
    if (!canvas) {
        console.warn('Canvas element not found');
        return;
    }
    
    // Destroy existing chart if it exists
    if (activityChart) {
        activityChart.destroy();
    }
    
    const hourlyData = data.hourly_activity_utc || {};
    const hourlyOrgData = data.hourly_organization_activity || {};
    const utcOffset = getUTCOffsetFromTimezone(data.timezone, data.activity_timezone);
    const topOrgs = data.top_organizations || [];
    
    // Define organization colors - only top 3 get colors
    const orgColors = [
        '#4285F4', // Google Blue for 1st
        '#FBBC04', // Google Yellow for 2nd
        '#EA4335', // Google Red for 3rd
    ];
    const otherColor = '#999999'; // Grey for all others
    
    // Create datasets for stacked bar chart
    const datasets = [];
    
    // Create a dataset for each top organization (colors for top 3, grey for rest)
    for (let i = 0; i < topOrgs.length; i++) {
        const orgName = topOrgs[i].name;
        const orgData = [];
        
        for (let localHour = 0; localHour < 24; localHour++) {
            const utcHour = (localHour - utcOffset + 24) % 24;
            const hourOrgs = hourlyOrgData[utcHour] || {};
            orgData.push(hourOrgs[orgName] || 0);
        }
        
        datasets.push({
            label: orgName,
            data: orgData,
            backgroundColor: i < 3 ? orgColors[i] : otherColor,
            borderColor: i < 3 ? orgColors[i] : otherColor,
            borderWidth: 1
        });
    }
    
    // We don't need a separate "Other" dataset since all orgs beyond top 3 are already grey
    // But we do need to handle any unattributed activity
    if (topOrgs.length > 0) {
        const otherData = [];
        const topOrgNames = topOrgs.map(org => org.name);
        
        for (let localHour = 0; localHour < 24; localHour++) {
            const utcHour = (localHour - utcOffset + 24) % 24;
            const hourOrgs = hourlyOrgData[utcHour] || {};
            let otherCount = 0;
            
            // Only count unattributed activity (not already in any org)
            const totalHourCount = hourlyData[utcHour] || 0;
            const attributedCount = Object.values(hourOrgs).reduce((sum, count) => sum + count, 0);
            const unattributedCount = Math.max(0, totalHourCount - attributedCount);
            
            otherData.push(unattributedCount);
        }
        
        // Only add "Unattributed" dataset if there's data
        if (otherData.some(count => count > 0)) {
            datasets.push({
                label: 'Unattributed',
                data: otherData,
                backgroundColor: otherColor,
                borderColor: otherColor,
                borderWidth: 1
            });
        }
    }
    
    // If no organization data, fall back to simple display
    if (datasets.length === 0) {
        const chartData = [];
        for (let localHour = 0; localHour < 24; localHour++) {
            const utcHour = (localHour - utcOffset + 24) % 24;
            chartData.push(hourlyData[utcHour] || 0);
        }
        
        datasets.push({
            label: 'Activity',
            data: chartData,
            backgroundColor: '#999999',
            borderColor: '#999999',
            borderWidth: 1
        });
    }
    
    // Create chart labels
    const chartLabels = [];
    for (let localHour = 0; localHour < 24; localHour++) {
        const hourLabel = String(localHour).padStart(2, '0') + ':00';
        chartLabels.push(hourLabel);
    }
    
    // Create Chart.js bar chart
    const ctx = canvas.getContext('2d');
    activityChart = new Chart(ctx, {
        type: 'bar',
        data: {
            labels: chartLabels,
            datasets: datasets
        },
        options: {
            responsive: true,
            maintainAspectRatio: false,
            plugins: {
                legend: {
                    display: false // Legend is shown in the organizations row
                },
                title: {
                    display: true,
                    text: `Daily Activity Pattern (${data.timezone})`,
                    font: {
                        size: 14
                    }
                }
            },
            scales: {
                y: {
                    beginAtZero: true,
                    stacked: true,
                    title: {
                        display: true,
                        text: 'Activity Count'
                    },
                    ticks: {
                        precision: 0 // Show whole numbers only
                    }
                },
                x: {
                    stacked: true,
                    title: {
                        display: true,
                        text: 'Hour (Local Time)'
                    },
                    ticks: {
                        maxRotation: 45,
                        minRotation: 0
                    }
                }
            },
            animation: {
                duration: 500 // Smooth animation
            },
            interaction: {
                intersect: false,
                mode: 'index'
            },
            onHover: (event, elements) => {
                canvas.style.cursor = elements.length > 0 ? 'pointer' : 'default';
            }
        }
    });
    
}


function initMapWhenReady(lat, lng, username) {
    // Ensure DOM updates are complete and Leaflet is ready
    function attemptMapInit() {
        const mapDiv = document.getElementById('map');
        if (!mapDiv) {
            // Map container not ready, try again
            setTimeout(attemptMapInit, 50);
            return;
        }
        
        // Check if map container is actually visible and has dimensions
        if (mapDiv.offsetHeight === 0 || mapDiv.offsetWidth === 0) {
            // Container not properly sized yet, try again
            setTimeout(attemptMapInit, 50);
            return;
        }
        
        // Check if Leaflet is loaded
        if (typeof L === 'undefined') {
            // Leaflet not loaded yet, try again (but don't wait forever)
            const maxRetries = 20; // 1 second max wait
            if (!attemptMapInit.retries) attemptMapInit.retries = 0;
            if (attemptMapInit.retries < maxRetries) {
                attemptMapInit.retries++;
                setTimeout(attemptMapInit, 50);
                return;
            }
            // Fallback if Leaflet never loads
            createMapFallback(lat, lng, username, mapDiv);
            return;
        }
        
        // All conditions met, initialize the map
        initMap(lat, lng, username);
    }
    
    // Start with a small delay to ensure DOM updates are complete
    setTimeout(attemptMapInit, 100);
}

function createMapFallback(lat, lng, username, mapDiv) {
    const mapLink = document.createElement('a');
    mapLink.href = `https://www.openstreetmap.org/?mlat=${lat}&mlon=${lng}&zoom=6#map=6/${lat}/${lng}`;
    mapLink.target = '_blank';
    mapLink.textContent = `View ${username} on OpenStreetMap (${lat.toFixed(2)}, ${lng.toFixed(2)})`;
    mapLink.style.cssText = 'color: #000; text-decoration: underline;';
    mapDiv.innerHTML = '';
    mapDiv.appendChild(mapLink);
}

function initMap(lat, lng, username) {
    const mapDiv = document.getElementById('map');
    if (!mapDiv) return;
    
    // Remove any existing map instance
    if (currentMap) {
        currentMap.remove();
        currentMap = null;
    }
    
    // Clear any existing content
    mapDiv.innerHTML = '';
    
    try {
        // Create the map with explicit options
        const map = L.map(mapDiv, {
            center: [lat, lng],
            zoom: 6,  // Zoomed out to show state/country context
            scrollWheelZoom: false,
            attributionControl: true
        });
        
        // Store the map instance globally for cleanup
        currentMap = map;
        
        // Add OpenStreetMap tiles with proper attribution
        L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
            attribution: '© <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors',
            maxZoom: 18,
            subdomains: ['a', 'b', 'c']
        }).addTo(map);
        
        // Add a marker for the user location
        L.marker([lat, lng])
            .addTo(map)
            .bindPopup(`<strong>${username}</strong><br/>${lat.toFixed(2)}, ${lng.toFixed(2)}`)
            .openPopup();
        
        // Force the map to recalculate its size after creation
        // This is crucial for proper tile loading when container was recently made visible
        setTimeout(() => {
            map.invalidateSize();
        }, 50);
        
    } catch (error) {
        console.error('Failed to initialize map:', error);
        // Fallback to link if map creation fails
        createMapFallback(lat, lng, username, mapDiv);
    }
}

document.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' && e.ctrlKey) {
        document.getElementById('detectForm').dispatchEvent(new Event('submit'));
    }
});