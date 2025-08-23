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

function displayResults(data) {
    // Reset all optional fields first
    document.getElementById('nameRow').style.display = 'none';
    document.getElementById('dataSourcesRow').style.display = 'none';
    document.getElementById('activityRow').style.display = 'none';
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
    
    // Set required fields with GitHub profile link
    const usernameElement = document.getElementById('displayUsername');
    usernameElement.innerHTML = `<a href="https://github.com/${data.username}" target="_blank" style="color: inherit; text-decoration: none;">${data.username}</a>`;
    
    // Show timezone with current local time and UTC offset
    const currentTime = getCurrentTimeInTimezone(data.timezone);
    const utcOffsetStr = getUTCOffsetString(data.timezone);
    let timezoneHTML = `${data.timezone} (${currentTime}, ${utcOffsetStr})`;
    
    // Check for verification discrepancy - only show if timezone offset differs
    if (data.verification && data.verification.claimed_timezone && data.verification.timezone_offset_diff > 0) {
        let claimHTML = ` — user claims `;
        
        // Check if we have a Gemini mismatch reason to show as tooltip
        let hasTooltip = data.gemini_suspicious_mismatch && data.gemini_mismatch_reason;
        
        if (hasTooltip) {
            // Create tooltip container for claimed timezone
            claimHTML += `<span class="tooltip-container">`;
            claimHTML += `<span style="text-decoration: underline; text-decoration-style: dotted; cursor: help;">`;
            claimHTML += data.verification.claimed_timezone;
            claimHTML += `</span>`;
            claimHTML += `<span class="tooltip">${data.gemini_mismatch_reason}</span>`;
            claimHTML += `</span>`;
        } else {
            claimHTML += data.verification.claimed_timezone;
        }
        
        if (data.verification.timezone_offset_diff > 0) {
            claimHTML += ` (${data.verification.timezone_offset_diff} hours off)`;
        }
        
        // Apply color based on mismatch level
        if (data.verification.timezone_mismatch === 'major' || data.gemini_suspicious_mismatch) {
            // Red for >3 timezone difference or suspicious mismatch
            claimHTML = `<span style="color: #dc2626;">${claimHTML}</span>`;
        } else if (data.verification.timezone_mismatch === 'minor') {
            // Black (normal) for >1 timezone difference
            claimHTML = `<span>${claimHTML}</span>`;
        }
        timezoneHTML += claimHTML;
    }
    
    document.getElementById('timezone').innerHTML = timezoneHTML;

    // Display full name if available
    if (data.name) {
        document.getElementById('displayName').textContent = data.name;
        document.getElementById('nameRow').style.display = 'table-row';
    }


    if (data.activity_timezone) {
        document.getElementById('activityTz').textContent = data.activity_timezone;
        document.getElementById('activityRow').style.display = 'table-row';
    }

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
    if (data.gemini_suggested_location) {
        locationText = data.gemini_suggested_location;
    } else if (data.location_name) {
        locationText = data.location_name;
    } else if (data.location) {
        locationText = data.location.latitude.toFixed(2) + ', ' + data.location.longitude.toFixed(2);
    }
    
    if (locationText) {
        let locationHTML = locationText;
        
        // Check for verification discrepancy
        // Show claimed location if:
        // 1. Distance > 50 miles OR
        // 2. Distance is -1 (geocoding failed) OR  
        // 3. No detected location but claimed location exists
        if (data.verification && data.verification.claimed_location) {
            const showClaimed = 
                data.verification.location_distance_miles > 50 || 
                data.verification.location_distance_miles === -1 ||
                !locationText;
                
            if (showClaimed) {
                let claimHTML = ` — user claims `;
                
                // Check if we have a Gemini mismatch reason to show as tooltip
                let hasTooltip = data.gemini_suspicious_mismatch && data.gemini_mismatch_reason;
                
                if (hasTooltip) {
                    // Create tooltip container for claimed location
                    claimHTML += `<span class="tooltip-container">`;
                    claimHTML += `<span style="text-decoration: underline; text-decoration-style: dotted; cursor: help;">`;
                    claimHTML += data.verification.claimed_location;
                    claimHTML += `</span>`;
                    claimHTML += `<span class="tooltip">${data.gemini_mismatch_reason}</span>`;
                    claimHTML += `</span>`;
                } else {
                    claimHTML += data.verification.claimed_location;
                }
                
                // Only show distance if it's a valid positive number
                if (data.verification.location_distance_miles > 0) {
                    claimHTML += ` (${Math.round(data.verification.location_distance_miles)} mi away)`;
                } else if (data.verification.location_distance_miles === -1) {
                    // Geocoding failed - can't determine distance
                    claimHTML += ` (location unrecognized)`;
                }
                
                // Apply color based on mismatch level
                if (data.verification.location_mismatch === 'major' || data.gemini_suspicious_mismatch) {
                    // Red for >1000 miles or suspicious mismatch
                    claimHTML = `<span style="color: #dc2626;">${claimHTML}</span>`;
                } else if (data.verification.location_mismatch === 'minor') {
                    // Black (normal) for >250 miles
                    claimHTML = `<span>${claimHTML}</span>`;
                } else if (data.verification.location_distance_miles === -1) {
                    // Gray for unrecognized locations
                    claimHTML = `<span style="color: #6b7280;">${claimHTML}</span>`;
                }
                locationHTML += claimHTML;
            }
        }
        
        document.getElementById('location').innerHTML = locationHTML;
        document.getElementById('locationRow').style.display = 'table-row';
    } else if (data.verification && data.verification.claimed_location) {
        // No detected location but user claims a location
        let locationHTML = 'Unknown — user claims ';
        locationHTML += `<span style="color: #6b7280;">${data.verification.claimed_location}</span>`;
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
    // Try to extract UTC offset from timezone string
    if (activityTimezone && activityTimezone.startsWith('UTC')) {
        const offsetStr = activityTimezone.replace('UTC', '').replace('GMT', '');
        const offset = parseInt(offsetStr) || 0;
        return offset;
    }
    
    // Try standard timezone
    if (timezone) {
        try {
            // Better way to get timezone offset - use Intl.DateTimeFormat
            const now = new Date();
            const formatter = new Intl.DateTimeFormat('en-US', {
                timeZone: timezone,
                timeZoneName: 'short'
            });
            
            // Parse the timezone offset from the formatted date
            const parts = formatter.formatToParts(now);
            const timeZoneName = parts.find(part => part.type === 'timeZoneName')?.value || '';
            
            // Try to extract offset from timezone abbreviation (e.g., "PST", "EDT", "GMT+12")
            const offsetMatch = timeZoneName.match(/GMT([+-]\d+)/);
            if (offsetMatch) {
                return parseInt(offsetMatch[1]);
            }
            
            // Alternative method: compare UTC and local times
            const utcTime = Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate(), 
                                     now.getUTCHours(), now.getUTCMinutes(), now.getUTCSeconds());
            const tzTime = new Date(now.toLocaleString("en-US", {timeZone: timezone})).getTime();
            const offset = Math.round((tzTime - utcTime) / (1000 * 60 * 60));
            
            // Sanity check - offset should be between -12 and +14
            if (offset >= -12 && offset <= 14) {
                return offset;
            }
        } catch (e) {
            console.error('Error calculating timezone offset for', timezone, ':', e);
            // Fallback for UTC+X format
            if (timezone.startsWith('UTC')) {
                const offsetStr = timezone.replace('UTC', '').replace('GMT', '');
                return parseInt(offsetStr) || 0;
            }
        }
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
    
    // Add sleep annotation
    if (data.sleep_ranges_local && data.sleep_ranges_local.length > 0) {
        data.sleep_ranges_local.forEach((range, i) => {
            const startIndex = Math.floor(range.start / increment);
            const endIndex = Math.floor(range.end / increment);
            annotations[`sleep${i}`] = {
                type: 'box',
                xMin: startIndex - 0.5,
                xMax: endIndex - 0.5,
                backgroundColor: 'rgba(66, 133, 244, 0.1)', // Light blue for sleep
                borderColor: 'rgba(66, 133, 244, 0.3)',
                borderWidth: 1,
                label: {
                    content: '💤 Sleep',
                    enabled: true,
                    position: 'start'
                }
            };
        });
    }
    
    // Add lunch annotation
    if (data.lunch_hours_local && data.lunch_hours_local.start) {
        const lunchStartIndex = Math.floor(data.lunch_hours_local.start / increment);
        const lunchEndIndex = Math.floor(data.lunch_hours_local.end / increment);
        annotations.lunch = {
            type: 'box',
            xMin: lunchStartIndex - 0.5,
            xMax: lunchEndIndex - 0.5,
            backgroundColor: 'rgba(52, 168, 83, 0.1)', // Light green for lunch
            borderColor: 'rgba(52, 168, 83, 0.3)',
            borderWidth: 1,
            label: {
                content: '🍽️ Lunch',
                enabled: true,
                position: 'center'
            }
        };
    }
    
    // Add peak productivity annotation
    if (data.peak_productivity_local && data.peak_productivity_local.start) {
        const peakStartIndex = Math.floor(data.peak_productivity_local.start / increment);
        const peakEndIndex = Math.floor(data.peak_productivity_local.end / increment);
        annotations.peak = {
            type: 'box',
            xMin: peakStartIndex - 0.5,
            xMax: peakEndIndex - 0.5,
            backgroundColor: 'rgba(251, 188, 4, 0.1)', // Light yellow for peak
            borderColor: 'rgba(251, 188, 4, 0.3)',
            borderWidth: 1,
            label: {
                content: '⚡ Peak',
                enabled: true,
                position: 'end'
            }
        };
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
                    text: `Daily Activity Pattern (${data.timezone}) - 30-minute Resolution`,
                    font: {
                        size: 14
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
    const currentMessage = funkyMessages[messageIndex % funkyMessages.length];
    loadingEl.innerHTML = `${currentMessage} (${elapsed}s)`;
    messageIndex++;
}

function stopRotatingMessages() {
    if (rotatingInterval) {
        clearInterval(rotatingInterval);
        rotatingInterval = null;
    }
}