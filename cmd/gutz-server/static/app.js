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
    
    // Update the page title
    document.title = username ? `guTZ: ${username}` : 'guTZ';
}

async function detectUser(username) {
    const submitBtn = document.getElementById('submitBtn');
    const errorDiv = document.getElementById('error');
    const resultDiv = document.getElementById('result');

    if (!username) return;

    submitBtn.disabled = true;
    submitBtn.innerHTML = 'TRACKING...';
    const loadingEl = document.getElementById('loading');
    if (loadingEl) {
        loadingEl.classList.add('show');
        startRotatingMessages(loadingEl);
    }
    errorDiv.classList.remove('show');
    resultDiv.classList.remove('show');

    try {
        const response = await fetch('/api/v1/detect', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({username})
        });

        if (!response.ok) {
            let errorMessage = 'Detection failed';
            let errorDetails = '';
            
            try {
                const errorData = await response.json();
                errorMessage = errorData.error || errorMessage;
                errorDetails = errorData.details || '';
            } catch (e) {
                // Fallback if response isn't JSON
                const errorText = await response.text();
                if (errorText) {
                    errorMessage = errorText;
                }
            }
            
            const error = new Error(errorMessage);
            error.details = errorDetails;
            error.status = response.status;
            throw error;
        }

        const data = await response.json();
        updateURL(username);
        displayResults(data);

    } catch (error) {
        // Format error message based on the error type
        let errorHTML = '';
        
        if (error.status === 404 || error.message.toLowerCase().includes('not found')) {
            errorHTML = '<strong>SUSPECT NOT FOUND:</strong> "' + username + '" - They\'ve gone off the grid!';
        } else if (error.status === 429 || error.message.toLowerCase().includes('rate limit')) {
            errorHTML = '<strong>SURVEILLANCE OVERLOAD:</strong> ' + error.message;
        } else if (error.status === 504 || error.message.toLowerCase().includes('timeout')) {
            errorHTML = '<strong>TRAIL TOO LONG:</strong> ' + error.message;
        } else {
            errorHTML = '<strong>TRAIL WENT COLD:</strong> ' + error.message;
        }
        
        // Add details if available
        if (error.details) {
            errorHTML += '<br><span style="font-size: 0.9em; color: #666; margin-top: 8px; display: inline-block;">' + error.details + '</span>';
        }
        
        errorDiv.innerHTML = errorHTML;
        errorDiv.classList.add('show');
    } finally {
        submitBtn.disabled = false;
        submitBtn.innerHTML = 'TRACK DEVELOPER';
        const loadingEl = document.getElementById('loading');
        if (loadingEl) {
            loadingEl.classList.remove('show');
            stopRotatingMessages();
        }
    }
}

document.getElementById('detectForm').addEventListener('submit', async (e) => {
    e.preventDefault();
    const username = document.getElementById('username').value.trim();
    await detectUser(username);
});

function extractUTCOffset(timezone) {
    // Extract numeric offset from strings like "UTC-4" or "America/New_York"
    if (timezone.startsWith('UTC')) {
        const match = timezone.match(/UTC([+-]?\d+(?:\.\d+)?)/);
        return match ? parseFloat(match[1]) : 0;
    }
    // For named timezones, we'd need a full lookup, so return null
    return null;
}

function displayResults(data) {
    // Reset all optional fields first
    document.getElementById('dataSourcesRow').style.display = 'none';
    document.getElementById('hoursRow').style.display = 'none';
    document.getElementById('lunchRow').style.display = 'none';
    document.getElementById('peakRow').style.display = 'none';
    document.getElementById('sleepRow').style.display = 'none';
    document.getElementById('orgsRow').style.display = 'none';
    document.getElementById('activitySummaryRow').style.display = 'none';
    document.getElementById('histogramRow').style.display = 'none';
    document.getElementById('locationRow').style.display = 'none';
    document.getElementById('mapRow').style.display = 'none';
    
    // Removed scary warnings - timezone discrepancies are now shown elegantly in the timezone display
    const warningDiv = document.getElementById('suspiciousWarning');
    if (warningDiv) {
        warningDiv.style.display = 'none';
    }
    
    // Set user field with combined username and full name
    const userElement = document.getElementById('displayUser');
    let userDisplay = '';
    
    if (data.name) {
        // Apple-style: "Full Name (username)" with GitHub link on username
        userDisplay = `${data.name} (<a href="https://github.com/${data.username}" target="_blank" style="color: inherit; text-decoration: none;">${data.username}</a>)`;
    } else {
        // Just username with GitHub link if no name available
        userDisplay = `<a href="https://github.com/${data.username}" target="_blank" style="color: inherit; text-decoration: none;">${data.username}</a>`;
    }
    userElement.innerHTML = userDisplay;
    
    // Show timezone with current local time and UTC offset
    const currentTime = getCurrentTimeInTimezone(data.timezone);
    const utcOffsetStr = getUTCOffsetString(data.timezone);
    let timezoneHTML = `<span title="Our best guess based on all available signals">${data.timezone} (${currentTime}, ${utcOffsetStr})</span>`;
    
    // Show additional timezone information as sub-items
    let subItems = [];
    
    // Show GitHub profile timezone if it exists
    if (data.verification && data.verification.profile_timezone) {
        let profileTzHTML = `<span title="Timezone set in the user's GitHub profile settings">GitHub Profile: ${data.verification.profile_timezone}</span>`;
        
        // Only show discrepancy warning if 4+ hours off
        if (data.verification.timezone_offset_diff >= 4) {
            const hourText = data.verification.timezone_offset_diff === 1 ? 'hour' : 'hours';
            if (data.verification.timezone_mismatch === 'major') {
                profileTzHTML += ` <span style="color: #dc2626; font-weight: 500;">⚠️ ${data.verification.timezone_offset_diff} ${hourText} off</span>`;
            } else {
                profileTzHTML += ` <span style="color: #ea580c;">(${data.verification.timezone_offset_diff} ${hourText} off)</span>`;
            }
        }
        subItems.push(profileTzHTML);
    }
    
    // Show GitHub location-derived timezone if different from profile timezone
    if (data.verification && data.verification.profile_location_timezone && 
        data.verification.profile_location_timezone !== data.verification.profile_timezone) {
        let locTzHTML = `<span title="Timezone derived from the location in the user's GitHub profile">GitHub Location: ${data.verification.profile_location_timezone}</span>`;
        
        // Calculate offset from detected timezone
        const detectedOffset = extractUTCOffset(data.timezone) || extractUTCOffset(utcOffsetStr);
        const locationOffset = extractUTCOffset(data.verification.profile_location_timezone);
        
        if (detectedOffset !== null && locationOffset !== null) {
            const diff = Math.abs(locationOffset - detectedOffset);
            // Show warning if 4+ hours off from detected
            if (diff >= 4) {
                const hourText = diff === 1 ? 'hour' : 'hours';
                // Use red warning for extreme differences (10+ hours)
                if (diff >= 10) {
                    locTzHTML += ` <span style="color: #dc2626; font-weight: 500;">⚠️ ${diff} ${hourText} off</span>`;
                } else {
                    locTzHTML += ` <span style="color: #ea580c;">⚠️ ${diff} ${hourText} off</span>`;
                }
            }
        }
        subItems.push(locTzHTML);
    }
    
    // Show activity-based timezone if it exists and is different
    if (data.activity_timezone && data.activity_timezone !== data.timezone) {
        let activityTzHTML = `<span title="Timezone detected from comprehensive GitHub activity patterns including commits, PRs, issues, releases, and other events">GitHub Activity: ${data.activity_timezone}</span>`;
        
        // Calculate offset difference for activity timezone
        const detectedOffset = extractUTCOffset(data.timezone) || extractUTCOffset(utcOffsetStr);
        const activityOffset = extractUTCOffset(data.activity_timezone);
        
        if (detectedOffset !== null && activityOffset !== null) {
            const diff = Math.abs(activityOffset - detectedOffset);
            // Only show warning if 4+ hours off
            if (diff >= 4) {
                const hourText = diff === 1 ? 'hour' : 'hours';
                activityTzHTML += ` <span style="color: #ea580c;">(${diff} ${hourText} off)</span>`;
            }
        }
        subItems.push(activityTzHTML);
    }
    
    // Add sub-items with proper formatting
    if (subItems.length > 0) {
        for (const item of subItems) {
            timezoneHTML += `<br><span style="margin-left: 1.5em; color: #6b7280;">└─ ${item}</span>`;
        }
    }
    
    document.getElementById('timezone').innerHTML = timezoneHTML;

    // Name is now combined with username, no separate field needed



    // Get UTC offset for timezone conversion
    const utcOffset = getUTCOffsetFromTimezone(data.timezone, data.activity_timezone);
    
    if (data.active_hours_local && (data.active_hours_local.start || data.active_hours_local.end)) {
        // These are now actual local values, no conversion needed
        const localStart = data.active_hours_local.start;
        const localEnd = data.active_hours_local.end;
        
        // Get relative time deltas using the converted local times
        const startDelta = getRelativeTimeDelta(localStart, data.timezone);
        const endDelta = getRelativeTimeDelta(localEnd, data.timezone);
        
        // Format active hours with relative time deltas
        const startTime = formatHour(localStart);
        const endTime = formatHour(localEnd);
        const activeHoursText = `${startTime} <span style="color: #666; font-size: 0.9em;">(${startDelta})</span> - ${endTime} <span style="color: #666; font-size: 0.9em;">(${endDelta})</span>`;
        
        document.getElementById('activeHours').innerHTML = activeHoursText;
        document.getElementById('hoursRow').style.display = 'table-row';
    }

    if (data.lunch_hours_local && data.lunch_hours_local.confidence > 0) {
        // These are now actual local values, no conversion needed
        const lunchText = formatLunchHours(data.lunch_hours_local.start, data.lunch_hours_local.end);
        const confidenceText = Math.round(data.lunch_hours_local.confidence * 100) + '% confidence';
        document.getElementById('lunchHours').textContent = lunchText + ' (' + confidenceText + ')';
        document.getElementById('lunchRow').style.display = 'table-row';
    }

    if (data.peak_productivity_local && data.peak_productivity_local.count > 0) {
        // Using the local version which is already converted
        const peakText = formatHour(data.peak_productivity_local.start) + '-' + formatHour(data.peak_productivity_local.end);
        document.getElementById('peakHours').textContent = peakText;
        document.getElementById('peakRow').style.display = 'table-row';
    }

    // Use pre-calculated rest ranges from the server (30-minute precision!)
    if (data.sleep_ranges_local && data.sleep_ranges_local.length > 0) {
        const restText = data.sleep_ranges_local.map(range => 
            formatHour(range.start) + '-' + formatHour(range.end)
        ).join(', ');
        document.getElementById('sleepHours').textContent = restText;
        document.getElementById('sleepRow').style.display = 'table-row';
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
        
        // Show all organizations with color-coded counts and links
        data.top_organizations.forEach((org, i) => {
            if (i > 0) {
                const separator = document.createTextNode(', ');
                orgsContainer.appendChild(separator);
            }
            
            // Create a link to the GitHub organization
            const orgLink = document.createElement('a');
            orgLink.href = `https://github.com/${org.name}`;
            orgLink.target = '_blank';
            orgLink.style.color = 'inherit';
            orgLink.style.textDecoration = 'none';
            orgLink.textContent = org.name;
            
            // Create wrapper span to hold link and count
            const orgSpan = document.createElement('span');
            orgSpan.appendChild(orgLink);
            orgSpan.appendChild(document.createTextNode(' '));
            
            // Create colored count in parentheses
            const countSpan = document.createElement('span');
            countSpan.style.color = i < 3 ? colors[i] : greyColor;
            countSpan.style.fontWeight = 'bold';
            countSpan.textContent = `(${org.count})`;
            
            orgSpan.appendChild(countSpan);
            orgsContainer.appendChild(orgSpan);
        });
        
        document.getElementById('orgsRow').style.display = 'table-row';
    }

    // Show activity data summary if available
    if (data.activity_date_range && data.activity_date_range.oldest_activity && data.activity_date_range.newest_activity) {
        // Validate that dates are not zero/invalid (like "0001-01-01T00:00:00Z")
        const oldestDateObj = new Date(data.activity_date_range.oldest_activity);
        const newestDateObj = new Date(data.activity_date_range.newest_activity);
        
        // Check if dates are valid and not from year 1 (which indicates zero time values)
        if (oldestDateObj.getFullYear() > 1900 && newestDateObj.getFullYear() > 1900) {
            // Calculate total events from hourly activity
            let totalEvents = 0;
            if (data.hourly_activity_utc) {
                Object.values(data.hourly_activity_utc).forEach(count => {
                    totalEvents += count;
                });
            }

            // Format dates
            const oldestDate = oldestDateObj.toISOString().split('T')[0];
            const newestDate = newestDateObj.toISOString().split('T')[0];
        
        let summaryHTML = '';
        if (totalEvents > 0) {
            summaryHTML = `${totalEvents} events from ${oldestDate} to ${newestDate}`;
        } else {
            summaryHTML = `${oldestDate} to ${newestDate}`;
        }

        if (data.activity_date_range.total_days > 0) {
            summaryHTML += ` (${data.activity_date_range.total_days} days)`;
        }
        
        // Add warnings for insufficient data or new accounts
        let warnings = [];
        if (totalEvents < 100) {
            warnings.push('⚠️ INSUFFICIENT DATA: Less than 100 events may reduce accuracy');
        }
        
        // Check if account is less than 120 days old
        if (data.created_at) {
            const createdDate = new Date(data.created_at);
            const daysSinceCreation = Math.floor((Date.now() - createdDate.getTime()) / (1000 * 60 * 60 * 24));
            if (daysSinceCreation < 120) {
                warnings.push('⚠️ NEW ACCOUNT: Account less than 120 days old may have limited data');
            }
        }
        
        if (warnings.length > 0) {
            // Format warnings as a list with proper spacing
            summaryHTML += '<div style="margin-top: 10px;">';
            warnings.forEach(warning => {
                summaryHTML += `<div style="margin: 5px 0; color: #b45309;">${warning}</div>`;
            });
            summaryHTML += '</div>';
        }

            document.getElementById('activitySummary').innerHTML = summaryHTML;
            document.getElementById('activitySummaryRow').style.display = 'table-row';
        }
    }

    // Draw histogram if activity data is available
    if (data.hourly_activity_utc) {
        drawHistogram(data);
        document.getElementById('histogramRow').style.display = 'block';
    }

    let locationText = '';
    let locationTooltip = '';
    if (data.gemini_suggested_location) {
        locationText = data.gemini_suggested_location;
        locationTooltip = 'Location detected using AI analysis of activity patterns and profile data';
    } else if (data.location_name) {
        locationText = data.location_name;
        locationTooltip = 'Location detected from activity patterns and timezone analysis';
    } else if (data.location) {
        locationText = data.location.latitude.toFixed(2) + ', ' + data.location.longitude.toFixed(2);
        locationTooltip = 'Approximate coordinates based on timezone detection';
    }
    
    if (locationText) {
        let locationHTML = locationTooltip ? 
            `<span title="${locationTooltip}">${locationText}</span>` : 
            locationText;
        let locationSubItems = [];
        
        // Check for profile location discrepancy
        if (data.verification && data.verification.profile_location) {
            const distanceKm = Math.round(data.verification.location_distance_km);
            
            // Always show profile location if it exists
            let profileLocHTML = `<span title="Location listed in the user's GitHub profile">GitHub Profile: ${data.verification.profile_location}</span>`;
            
            // Only show distance warning if significant (800+ km)
            if (distanceKm > 0 && distanceKm >= 800) {
                const distanceText = `${distanceKm.toLocaleString()} km`;
                
                if (data.verification.location_mismatch === 'major') {
                    profileLocHTML += ` <span style="color: #dc2626; font-weight: 500;">⚠️ ${distanceText} away</span>`;
                } else {
                    profileLocHTML += ` <span style="color: #ea580c;">(${distanceText} away)</span>`;
                }
            } else if (data.verification.location_distance_km === -1) {
                // Geocoding failed
                profileLocHTML += ` <span style="color: #6b7280;">(unrecognized)</span>`;
            }
            
            locationSubItems.push(profileLocHTML);
        }
        
        // Add sub-items with proper formatting
        if (locationSubItems.length > 0) {
            for (const item of locationSubItems) {
                locationHTML += `<br><span style="margin-left: 1.5em; color: #6b7280;">└─ ${item}</span>`;
            }
        }
        
        document.getElementById('location').innerHTML = locationHTML;
        document.getElementById('locationRow').style.display = 'table-row';
    } else if (data.verification && data.verification.profile_location) {
        // No detected location but user claims a location
        let locationHTML = 'Unknown';
        locationHTML += `<br><span style="margin-left: 1.5em; color: #6b7280;">└─ GitHub Profile: ${data.verification.profile_location}</span>`;
        document.getElementById('location').innerHTML = locationHTML;
        document.getElementById('locationRow').style.display = 'table-row';
    }

    const methodName = formatMethodName(data.method);
    const methodElement = document.getElementById('method');
    
    // Clear any existing content
    methodElement.innerHTML = '';
    
    // Handle reasoning tooltip if available
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
    
    // Handle data sources separately (should be sorted on the server side)
    if (data.data_sources && data.data_sources.length > 0) {
        const sourcesElement = document.getElementById('dataSources');
        sourcesElement.textContent = data.data_sources.join(', ');
        document.getElementById('dataSourcesRow').style.display = 'table-row';
    }

    // Show results first so map container has proper dimensions
    document.getElementById('result').classList.add('show');

    // Use the location (which is now always the best detected location from Gemini)
    if (data.location) {
        const mapRow = document.getElementById('mapRow');
        if (mapRow) {
            mapRow.style.display = 'block';
        }
        // Determine location name to display
        let displayLocation = locationText || 'Unknown Location';
        // Initialize map after ensuring all DOM updates are complete and Leaflet is ready
        initMapWhenReady(data.location.latitude, data.location.longitude, data.username, displayLocation);
    }
}

function formatMethodName(method) {
    const methodNames = {
        'github_profile': 'Profile Scraping',
        'location_geocoding': 'Location Geocoding', 
        'location_field': 'Location Field Analysis',
        'activity_patterns': 'Activity Analysis',
        'gemini_refined_activity': 'AI + Activity',
        'company_heuristic': 'Company Intel',
        'email_heuristic': 'Email Domain',
        'blog_heuristic': 'Blog Analysis',
        'website_gemini_analysis': 'Website + AI',
        'gemini_analysis': 'Activity + AI Context Analysis',
        'gemini_enhanced': 'Activity + AI Enhanced',
        'gemini_corrected': 'AI-Corrected Location'
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
    // Prefer the real timezone if we have it (e.g., America/New_York)
    // Only fall back to activity timezone if no real timezone is available
    
    // Try standard timezone first (this is the authoritative one)
    if (timezone && !timezone.startsWith('UTC')) {
        try {
            // Simple approach: Create dates in UTC and target timezone, compare them
            const now = new Date();
            
            // Get UTC hours
            const utcHours = now.getUTCHours();
            
            // Get local hours in the target timezone
            const localString = now.toLocaleString('en-US', {
                timeZone: timezone,
                hour: 'numeric',
                hour12: false
            });
            const localHours = parseInt(localString);
            
            // Calculate offset
            // If it's 20:00 UTC and 16:00 local, offset = 16 - 20 = -4
            let offset = localHours - utcHours;
            
            // Handle day boundary (e.g., 23:00 UTC is 01:00 local next day in UTC+2)
            if (offset > 12) {
                offset -= 24;
            } else if (offset < -12) {
                offset += 24;
            }
            
            console.log('Timezone offset calculation:', {
                timezone, utcHours, localHours, offset
            });
            
            return offset;
        } catch (e) {
            console.error('Error calculating timezone offset for', timezone, ':', e);
        }
    }
    
    // Fallback to UTC offset formats (either from timezone or activityTimezone)
    const utcTimezone = timezone?.startsWith('UTC') ? timezone : activityTimezone;
    if (utcTimezone && utcTimezone.startsWith('UTC')) {
        const offsetStr = utcTimezone.replace('UTC', '').replace('GMT', '');
        const offset = parseInt(offsetStr) || 0;
        return offset;
    }
    
    return 0;
}

function getCurrentTimeInTimezone(timezone) {
    try {
        // Handle both IANA timezone names (America/Denver) and UTC offset formats (UTC-6)
        let timezoneName = timezone;
        
        // Convert UTC offset format to a valid timezone identifier for Intl.DateTimeFormat
        if (timezone.startsWith('UTC')) {
            // For UTC offset formats, we'll manually calculate the time
            const offsetStr = timezone.replace('UTC', '');
            const offset = parseInt(offsetStr) || 0;
            
            const now = new Date();
            const utcTime = now.getTime() + (now.getTimezoneOffset() * 60000);
            const localTime = new Date(utcTime + (offset * 3600000));
            
            return localTime.toLocaleTimeString('en-US', { 
                hour12: true, 
                hour: 'numeric', 
                minute: '2-digit'
            });
        } else {
            // For IANA timezone names, use Intl.DateTimeFormat
            const now = new Date();
            return now.toLocaleTimeString('en-US', {
                timeZone: timezoneName,
                hour12: true,
                hour: 'numeric',
                minute: '2-digit'
            });
        }
    } catch (error) {
        // Fallback: just show UTC time if timezone parsing fails
        return new Date().toLocaleTimeString('en-US', { 
            timeZone: 'UTC',
            hour12: true, 
            hour: 'numeric', 
            minute: '2-digit'
        }) + ' UTC';
    }
}

function getUTCOffsetString(timezone) {
    try {
        if (timezone.startsWith('UTC')) {
            // Already in UTC format
            return timezone;
        } else {
            // For IANA timezone names, calculate the current UTC offset
            const now = new Date();
            const utcTime = new Date(now.toLocaleString("en-US", {timeZone: "UTC"}));
            const localTime = new Date(now.toLocaleString("en-US", {timeZone: timezone}));
            const offsetMs = localTime.getTime() - utcTime.getTime();
            const offsetHours = Math.round(offsetMs / (1000 * 60 * 60));
            
            if (offsetHours >= 0) {
                return `UTC+${offsetHours}`;
            } else {
                return `UTC${offsetHours}`;
            }
        }
    } catch (error) {
        return 'UTC+0';
    }
}

function getRelativeTimeDelta(targetHour, timezone) {
    try {
        // Get current time in the target timezone
        const now = new Date();
        let currentLocalTime;
        
        if (timezone.startsWith('UTC')) {
            const offsetStr = timezone.replace('UTC', '');
            const offset = parseInt(offsetStr) || 0;
            const utcTime = now.getTime() + (now.getTimezoneOffset() * 60000);
            currentLocalTime = new Date(utcTime + (offset * 3600000));
        } else {
            currentLocalTime = new Date(now.toLocaleString("en-US", {timeZone: timezone}));
        }
        
        const currentHour = currentLocalTime.getHours() + (currentLocalTime.getMinutes() / 60);
        
        // Calculate the simple difference
        let delta = targetHour - currentHour;
        
        // Intuitive logic for "ago" vs "from now":
        // - If the time already happened today (negative delta), always show "ago"
        // - If the time is coming up later today (positive delta), show "from now"
        // - Special case: if it's evening and showing morning time, that's tomorrow
        
        let isTomorrow = false;
        
        if (delta < 0) {
            // Target is before current time
            // This is always "ago" for today
            // No adjustment needed
        } else if (delta > 0) {
            // Target is after current time
            // Check if this is actually tomorrow (e.g., it's 8pm and target is 7am)
            if (targetHour < 12 && currentHour > 18) {
                // Morning target, evening now = tomorrow
                isTomorrow = true;
                // Don't show as "from now" if it's more than 12 hours away
                // Instead show as negative (ago) from this morning
                delta = targetHour - (currentHour - 24);
            }
            // Otherwise it's later today, keep positive delta
        }
        
        // Calculate absolute delta for determining units
        const absDelta = Math.abs(delta);
        
        // Format the delta with smart units
        if (absDelta < 0.1) {
            return 'now';
        } else if (absDelta < 1) {
            // Less than 1 hour - show minutes
            const minutes = Math.round(absDelta * 60);
            if (delta > 0) {
                return isTomorrow ? `${minutes}m ago` : (minutes === 1 ? '1m from now' : `${minutes}m from now`);
            } else {
                return minutes === 1 ? '1m ago' : `${minutes}m ago`;
            }
        } else if (absDelta < 24) {
            // Less than 24 hours - show hours
            const hours = Math.round(absDelta);
            if (delta > 0 && !isTomorrow) {
                return hours === 1 ? '1h from now' : `${hours}h from now`;
            } else {
                return hours === 1 ? '1h ago' : `${hours}h ago`;
            }
        } else {
            // 24+ hours - show days
            const days = Math.round(absDelta / 24);
            if (delta > 0) {
                return days === 1 ? '1d from now' : `${days}d from now`;
            } else {
                return days === 1 ? '1d ago' : `${days}d ago`;
            }
        }
    } catch (error) {
        return '';
    }
}

let activityChart = null; // Global chart instance

function drawHistogram(data) {
    const canvas = document.getElementById('activityChart');
    if (!canvas) {
        console.warn('Canvas element not found');
        return;
    }
    
    // Ensure canvas wrapper for proper sizing
    if (!canvas.parentElement || canvas.parentElement.id !== 'chartWrapper') {
        const wrapper = document.createElement('div');
        wrapper.id = 'chartWrapper';
        wrapper.style.cssText = 'position: relative; flex: 1; min-height: 280px;';
        canvas.parentElement.insertBefore(wrapper, canvas);
        wrapper.appendChild(canvas);
    }
    
    // Destroy existing chart if it exists
    if (activityChart) {
        activityChart.destroy();
    }
    
    // Use half-hourly data (30-minute resolution)
    // Convert string keys back to numbers if needed
    const halfHourlyDataRaw = data.half_hourly_activity_utc || {};
    const halfHourlyData = {};
    for (const key in halfHourlyDataRaw) {
        const numKey = parseFloat(key);
        halfHourlyData[numKey] = halfHourlyDataRaw[key];
    }
    
    const hourlyData = data.hourly_activity_utc || {};
    const hourlyOrgData = data.hourly_organization_activity || {};
    const utcOffset = getUTCOffsetFromTimezone(data.timezone, data.activity_timezone);
    console.log('Timezone:', data.timezone, 'Activity timezone:', data.activity_timezone, 'Calculated offset:', utcOffset);
    const topOrgs = data.top_organizations || [];
    
    // Define organization colors - only top 3 get colors
    const orgColors = [
        '#4285F4', // Google Blue for 1st
        '#FBBC04', // Google Yellow for 2nd
        '#EA4335', // Google Red for 3rd
    ];
    const otherColor = '#999999'; // Grey for all others
    
    // Create datasets for stacked bar chart - 48 bars for 30-minute resolution
    const datasets = [];
    const numBars = 48;
    const increment = 0.5;
    
    // Create a dataset for each top organization (colors for top 3, grey for rest)
    for (let i = 0; i < topOrgs.length; i++) {
        const orgName = topOrgs[i].name;
        const orgData = [];
        
        for (let barIndex = 0; barIndex < numBars; barIndex++) {
            const localTime = barIndex * increment;
            const utcTime = (localTime - utcOffset + 24) % 24;
            
            // For half-hourly, we don't have org breakdown, so distribute proportionally
            const halfHourCount = halfHourlyData[utcTime] || 0;
            const utcHour = Math.floor(utcTime);
            const hourOrgs = hourlyOrgData[utcHour] || {};
            const hourTotal = hourlyData[utcHour] || 1;
            const orgHourCount = hourOrgs[orgName] || 0;
            // Distribute the org's hourly count proportionally to half-hour slots
            orgData.push(Math.round((halfHourCount * orgHourCount) / hourTotal));
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
        
        for (let barIndex = 0; barIndex < numBars; barIndex++) {
            const localTime = barIndex * increment;
            const utcTime = (localTime - utcOffset + 24) % 24;
            
            const halfHourCount = halfHourlyData[utcTime] || 0;
            const utcHour = Math.floor(utcTime);
            const hourOrgs = hourlyOrgData[utcHour] || {};
            const hourTotal = hourlyData[utcHour] || 0;
            const attributedCount = Object.values(hourOrgs).reduce((sum, count) => sum + count, 0);
            // Scale the unattributed count proportionally for half-hour slots
            const unattributedHourly = Math.max(0, hourTotal - attributedCount);
            const unattributedHalfHour = hourTotal > 0 ? Math.round((halfHourCount * unattributedHourly) / hourTotal) : halfHourCount;
            otherData.push(unattributedHalfHour);
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
        for (let barIndex = 0; barIndex < numBars; barIndex++) {
            const localTime = barIndex * increment;
            const utcTime = (localTime - utcOffset + 24) % 24;
            chartData.push(halfHourlyData[utcTime] || 0);
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
    for (let barIndex = 0; barIndex < numBars; barIndex++) {
        const localTime = barIndex * increment;
        const hour = Math.floor(localTime);
        const minutes = Math.round((localTime - hour) * 60);
        const label = String(hour).padStart(2, '0') + ':' + String(minutes).padStart(2, '0');
        chartLabels.push(label);
    }
    
    // Add annotations for sleep, lunch, and peak times if available
    const annotations = {};
    
    // Add sleep annotation with gradient and better visuals
    if (data.sleep_ranges_local && data.sleep_ranges_local.length > 0) {
        data.sleep_ranges_local.forEach((range, i) => {
            // Handle wraparound sleep (e.g., 22:00 to 06:00)
            if (range.end < range.start) {
                // Sleep wraps around midnight - create two boxes
                // First box: from start to midnight (24:00)
                const firstStartIndex = Math.floor(range.start / increment);
                const firstEndIndex = Math.floor(24 / increment); // End of day
                annotations[`sleep${i}_part1`] = {
                    type: 'box',
                    xMin: firstStartIndex - 0.5,
                    xMax: firstEndIndex - 0.5,
                    backgroundColor: 'rgba(37, 99, 235, 0.08)', // Soft blue gradient
                    borderColor: 'rgba(37, 99, 235, 0.4)',
                    borderWidth: 1,
                    borderDash: [5, 3],
                    label: {
                        content: '💤 Sleep',
                        enabled: true,
                        position: 'start',
                        font: {
                            size: 11,
                            weight: 'bold'
                        },
                        color: 'rgba(37, 99, 235, 0.8)',
                        yAdjust: -8
                    }
                };
                
                // Second box: from midnight to end
                const secondStartIndex = 0; // Start of day
                const secondEndIndex = Math.floor(range.end / increment);
                annotations[`sleep${i}_part2`] = {
                    type: 'box',
                    xMin: secondStartIndex - 0.5,
                    xMax: secondEndIndex - 0.5,
                    backgroundColor: 'rgba(37, 99, 235, 0.08)', // Soft blue gradient
                    borderColor: 'rgba(37, 99, 235, 0.4)',
                    borderWidth: 1,
                    borderDash: [5, 3],
                    label: {
                        content: '💤 Sleep',
                        enabled: true,
                        position: 'end',
                        font: {
                            size: 11,
                            weight: 'bold'
                        },
                        color: 'rgba(37, 99, 235, 0.8)',
                        yAdjust: -8
                    }
                };
            } else {
                // Normal sleep within the same day
                const startIndex = Math.floor(range.start / increment);
                const endIndex = Math.floor(range.end / increment);
                annotations[`sleep${i}`] = {
                    type: 'box',
                    xMin: startIndex - 0.5,
                    xMax: endIndex - 0.5,
                    backgroundColor: 'rgba(37, 99, 235, 0.08)', // Soft blue gradient
                    borderColor: 'rgba(37, 99, 235, 0.4)',
                    borderWidth: 1,
                    borderDash: [5, 3],
                    label: {
                        content: '💤 Sleep',
                        enabled: true,
                        position: 'start',
                        font: {
                            size: 11,
                            weight: 'bold'
                        },
                        color: 'rgba(37, 99, 235, 0.8)',
                        yAdjust: -8
                    }
                };
            }
        });
    }
    
    // Add lunch annotation with confidence indicator
    if (data.lunch_hours_local && data.lunch_hours_local.start) {
        const lunchStartIndex = Math.floor(data.lunch_hours_local.start / increment);
        const lunchEndIndex = Math.floor(data.lunch_hours_local.end / increment);
        const confidence = data.lunch_hours_local.confidence || 0;
        const opacity = Math.max(0.08, confidence * 0.15); // Scale opacity with confidence
        
        annotations.lunch = {
            type: 'box',
            xMin: lunchStartIndex - 0.5,
            xMax: lunchEndIndex - 0.5,
            backgroundColor: `rgba(34, 197, 94, ${opacity})`, // Green with confidence-based opacity
            borderColor: 'rgba(34, 197, 94, 0.5)',
            borderWidth: 1,
            borderDash: confidence > 0.5 ? [] : [3, 3], // Solid if confident, dashed if not
            label: {
                content: `🍽️ Lunch ${confidence > 0 ? '(' + Math.round(confidence * 100) + '%)' : ''}`,
                enabled: true,
                position: 'center',
                font: {
                    size: 11,
                    weight: 'bold'
                },
                color: 'rgba(34, 197, 94, 0.9)',
                yAdjust: -8
            }
        };
    }
    
    // Peak productivity annotation removed - the peak is visually obvious from the bars themselves
    // Work hours annotation removed - not useful for visualization
    
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
                    text: `Daily Activity Pattern (${data.timezone}) - 30-minute Resolution`,
                    font: {
                        size: 14
                    },
                    padding: {
                        bottom: 10
                    }
                },
                tooltip: {
                    callbacks: {
                        afterLabel: function(context) {
                            const index = context.dataIndex;
                            const localTime = index * increment;
                            const hour = Math.floor(localTime);
                            const minutes = Math.round((localTime - hour) * 60);
                            const timeStr = String(hour).padStart(2, '0') + ':' + String(minutes).padStart(2, '0');
                            
                            const labels = [];
                            
                            // Check if this time is in sleep period
                            if (data.sleep_ranges_local) {
                                for (const range of data.sleep_ranges_local) {
                                    if (localTime >= range.start && localTime < range.end) {
                                        labels.push('💤 Sleep period');
                                        break;
                                    }
                                }
                            }
                            
                            // Check if this is lunch time
                            if (data.lunch_hours_local && data.lunch_hours_local.start) {
                                if (localTime >= data.lunch_hours_local.start && localTime < data.lunch_hours_local.end) {
                                    labels.push('🍽️ Lunch break');
                                }
                            }
                            
                            
                            return labels;
                        }
                    }
                },
                annotation: {
                    annotations: annotations
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
                        text: 'Time (Local)'
                    },
                    ticks: {
                        maxRotation: 45,
                        minRotation: 0,
                        autoSkip: true,
                        maxTicksLimit: 24, // Show only hour labels for readability
                        callback: function(value, index) {
                            // Only show labels for whole hours (every other bar)
                            if (index % 2 !== 0) {
                                return '';
                            }
                            return this.getLabelForValue(value);
                        }
                    },
                    barPercentage: 0.95, // Thin bars for 48 bars
                    categoryPercentage: 1.0
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
    
    // Add a visual legend below the chart for sleep, lunch, and peak times
    createActivityLegend(data);
}

function createActivityLegend(data) {
    // Remove existing legend if it exists
    const existingLegend = document.getElementById('activityLegend');
    if (existingLegend) {
        existingLegend.remove();
    }
    
    const legendItems = [];
    
    // Add sleep periods
    if (data.sleep_ranges_local && data.sleep_ranges_local.length > 0) {
        const sleepPeriods = data.sleep_ranges_local.map(range => {
            const startHour = Math.floor(range.start);
            const startMin = Math.round((range.start - startHour) * 60);
            const endHour = Math.floor(range.end);
            const endMin = Math.round((range.end - endHour) * 60);
            return `${String(startHour).padStart(2, '0')}:${String(startMin).padStart(2, '0')}-${String(endHour).padStart(2, '0')}:${String(endMin).padStart(2, '0')}`;
        }).join(', ');
        legendItems.push({
            icon: '💤',
            label: 'Sleep',
            value: sleepPeriods,
            color: 'rgba(37, 99, 235, 0.8)'
        });
    }
    
    // Add lunch period
    if (data.lunch_hours_local && data.lunch_hours_local.start) {
        const lunchStart = data.lunch_hours_local.start;
        const lunchEnd = data.lunch_hours_local.end;
        const startHour = Math.floor(lunchStart);
        const startMin = Math.round((lunchStart - startHour) * 60);
        const endHour = Math.floor(lunchEnd);
        const endMin = Math.round((lunchEnd - endHour) * 60);
        const confidence = data.lunch_hours_local.confidence || 0;
        const lunchStr = `${String(startHour).padStart(2, '0')}:${String(startMin).padStart(2, '0')}-${String(endHour).padStart(2, '0')}:${String(endMin).padStart(2, '0')}`;
        legendItems.push({
            icon: '🍽️',
            label: 'Lunch',
            value: confidence > 0 ? `${lunchStr} (${Math.round(confidence * 100)}% conf)` : lunchStr,
            color: 'rgba(34, 197, 94, 0.9)'
        });
    }
    
    // Add peak productivity
    if (data.peak_productivity_local && data.peak_productivity_local.start) {
        const peakStart = data.peak_productivity_local.start;
        const peakEnd = data.peak_productivity_local.end;
        const startHour = Math.floor(peakStart);
        const startMin = Math.round((peakStart - startHour) * 60);
        const endHour = Math.floor(peakEnd);
        const endMin = Math.round((peakEnd - endHour) * 60);
        const peakCount = data.peak_productivity_local.count || 0;
        const peakStr = `${String(startHour).padStart(2, '0')}:${String(startMin).padStart(2, '0')}-${String(endHour).padStart(2, '0')}:${String(endMin).padStart(2, '0')}`;
        legendItems.push({
            icon: '⚡',
            label: 'Peak',
            value: peakCount > 0 ? `${peakStr} (${peakCount} events)` : peakStr,
            color: 'rgba(251, 146, 60, 0.9)'
        });
    }
    
    // Work hours removed from legend - already shown in the main results table
    
    if (legendItems.length === 0) return;
    
    // Create legend container
    const legendDiv = document.createElement('div');
    legendDiv.id = 'activityLegend';
    legendDiv.style.cssText = `
        display: flex;
        flex-wrap: wrap;
        gap: 12px;
        margin-top: 10px;
        padding: 10px;
        background: #f9fafb;
        border-radius: 6px;
        border: 1px solid #e5e7eb;
        font-size: 12px;
        align-items: center;
        justify-content: center;
    `;
    
    legendItems.forEach(item => {
        const itemDiv = document.createElement('div');
        itemDiv.style.cssText = `
            display: flex;
            align-items: center;
            gap: 4px;
            padding: 3px 8px;
            background: white;
            border-radius: 4px;
            border: 1px solid #e5e7eb;
        `;
        
        itemDiv.innerHTML = `
            <span style="font-size: 14px;">${item.icon}</span>
            <span style="color: ${item.color}; font-weight: 600; font-size: 11px;">${item.label}:</span>
            <span style="color: #4b5563; font-size: 11px;">${item.value}</span>
        `;
        
        legendDiv.appendChild(itemDiv);
    });
    
    // Insert after canvas
    const container = document.getElementById('histogramContainer');
    if (container) {
        container.appendChild(legendDiv);
    }
}


function initMapWhenReady(lat, lng, username, locationName) {
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
        initMap(lat, lng, username, locationName);
    }
    
    // Start with a small delay to ensure DOM updates are complete
    setTimeout(attemptMapInit, 100);
}

function createMapFallback(lat, lng, username, mapDiv) {
    const mapLink = document.createElement('a');
    mapLink.href = `https://www.openstreetmap.org/?mlat=${lat}&mlon=${lng}&zoom=2#map=2/${lat}/${lng}`;
    mapLink.target = '_blank';
    mapLink.textContent = `View ${username} on OpenStreetMap (${lat.toFixed(2)}, ${lng.toFixed(2)})`;
    mapLink.style.cssText = 'color: #000; text-decoration: underline;';
    mapDiv.innerHTML = '';
    mapDiv.appendChild(mapLink);
}

function initMap(lat, lng, username, locationName) {
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
            zoom: 2,  // Zoomed out to show hemisphere context
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
            .bindPopup(`<strong>${username}</strong><br/>${locationName || 'Unknown Location'}`)
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

// Rotating messages functionality
let rotatingInterval = null;
let messageIndex = 0;
let startTime = 0;

const funkyMessages = [
    '🔍 Stalking GitHub profiles...',
    '📊 Crunching commit timestamps...',
    '🔑 Examining SSH keys...',
    '📱 Cyberstalking social media...',
    '🏢 Interrogating organizations...',
    '⭐ Judging starred repos...',
    '📝 Reading personal gists...',
    '🔀 Analyzing pull request drama...',
    '🐛 Questioning bug reports...',
    '💬 Eavesdropping on comments...',
    '🌐 Geocoding secret hideouts...',
    '🤖 Bribing AI overlords...',
    '📧 Deciphering email patterns...',
    '🐦 Infiltrating Twitter/X...',
    '🦋 Hunting BlueSky butterflies...',
    '🐘 Interrogating Mastodon elephants...',
    '📄 Ransacking GitHub Pages...',
    '🎯 Building evidence dossiers...',
    '🧪 Brewing timezone potions...',
    '🌙 Tracking nocturnal coding...',
    '☕ Detecting caffeine patterns...',
    '🍕 Calculating lunch algorithms...',
    '⏰ Violating space-time...',
    '🔮 Consulting crystal balls...',
    '🎪 Performing timezone acrobatics...',
    '🚀 Launching spy satellites...',
    '🔬 Examining commit DNA...',
    '🏃 Chasing timestamp rabbits...',
    '🎨 Painting developer portraits...',
    '🎭 Decoding repo drama...',
    '🎲 Rolling temporal dice...',
    '🌊 Surfing data tsunamis...',
    '🔥 Igniting analysis engines...',
    '⚡ Electrifying neural networks...',
    '🎵 Composing code symphonies...',
    '🍯 Following honey trails...',
    '🔍 Enhancing... ENHANCE MORE!...',
    '🎪 Juggling timezone possibilities...',
    '🏗️ Building conspiracy theories...',
    '🧬 Sequencing temporal DNA...'
];

function startRotatingMessages(loadingEl) {
    startTime = Date.now();
    messageIndex = 0;
    
    // Show initial message
    updateMessage(loadingEl);
    
    // Rotate every 250ms
    rotatingInterval = setInterval(() => {
        updateMessage(loadingEl);
    }, 250);
}

function updateMessage(loadingEl) {
    const elapsed = Math.floor((Date.now() - startTime) / 1000);
    
    // After 10 seconds, show rate limit message
    if (elapsed >= 10) {
        loadingEl.innerHTML = `Sorry, we probably got GitHub rate limited ... (${elapsed}s)`;
    } else {
        const currentMessage = funkyMessages[messageIndex % funkyMessages.length];
        loadingEl.innerHTML = `${currentMessage} (${elapsed}s)`;
        messageIndex++;
    }
}

function stopRotatingMessages() {
    if (rotatingInterval) {
        clearInterval(rotatingInterval);
        rotatingInterval = null;
    }
}