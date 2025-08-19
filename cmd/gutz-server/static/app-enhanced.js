let currentUsername = '';
let currentMap = null;

// Loading state manager
class LoadingStateManager {
    constructor() {
        this.stages = [
            { id: 'profile', name: 'Fetching GitHub profile', status: 'pending', icon: '👤' },
            { id: 'activity', name: 'Analyzing activity patterns', status: 'pending', icon: '📊' },
            { id: 'social', name: 'Checking social links', status: 'pending', icon: '🔗' },
            { id: 'location', name: 'Detecting location', status: 'pending', icon: '📍' },
            { id: 'ai', name: 'AI context analysis', status: 'pending', icon: '🤖' },
            { id: 'timezone', name: 'Finalizing timezone', status: 'pending', icon: '🌍' }
        ];
        this.startTime = null;
        this.progressInterval = null;
    }

    start() {
        this.startTime = Date.now();
        this.stages.forEach(s => s.status = 'pending');
        this.render();
        this.startProgressAnimation();
    }

    updateStage(stageId, status, message = null) {
        const stage = this.stages.find(s => s.id === stageId);
        if (stage) {
            stage.status = status;
            if (message) stage.message = message;
            this.render();
        }
    }

    startProgressAnimation() {
        let dots = 0;
        this.progressInterval = setInterval(() => {
            const activeStage = this.stages.find(s => s.status === 'in-progress');
            if (activeStage) {
                const dotsStr = '.'.repeat((dots % 3) + 1);
                document.getElementById('loading-dots').textContent = dotsStr;
                dots++;
            }
        }, 500);
    }

    stop() {
        if (this.progressInterval) {
            clearInterval(this.progressInterval);
        }
    }

    render() {
        const loadingEl = document.getElementById('enhanced-loading');
        if (!loadingEl) return;

        const elapsed = this.startTime ? Math.floor((Date.now() - this.startTime) / 1000) : 0;
        
        let html = `
            <div class="loading-stages">
                <div class="elapsed-time">Searching for ${elapsed}s<span id="loading-dots">...</span></div>
                <div class="stages-list">
        `;

        this.stages.forEach(stage => {
            let statusIcon = '';
            let statusClass = '';
            
            if (stage.status === 'completed') {
                statusIcon = '✓';
                statusClass = 'completed';
            } else if (stage.status === 'in-progress') {
                statusIcon = '⏳';
                statusClass = 'in-progress';
            } else if (stage.status === 'failed') {
                statusIcon = '⚠';
                statusClass = 'failed';
            } else {
                statusIcon = '○';
                statusClass = 'pending';
            }

            html += `
                <div class="stage-item ${statusClass}">
                    <span class="stage-icon">${stage.icon}</span>
                    <span class="stage-name">${stage.name}</span>
                    <span class="stage-status">${statusIcon}</span>
                    ${stage.message ? `<div class="stage-message">${stage.message}</div>` : ''}
                </div>
            `;
        });

        html += `
                </div>
            </div>
        `;

        loadingEl.innerHTML = html;
        loadingEl.classList.add('show');
    }
}

// WebSocket for real-time updates
class DetectionWebSocket {
    constructor(username, onUpdate, onComplete, onError) {
        this.username = username;
        this.onUpdate = onUpdate;
        this.onComplete = onComplete;
        this.onError = onError;
        this.ws = null;
    }

    connect() {
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const wsUrl = `${protocol}//${window.location.host}/ws/detect`;
        
        this.ws = new WebSocket(wsUrl);
        
        this.ws.onopen = () => {
            this.ws.send(JSON.stringify({ username: this.username }));
        };

        this.ws.onmessage = (event) => {
            const data = JSON.parse(event.data);
            
            if (data.type === 'progress') {
                this.onUpdate(data);
            } else if (data.type === 'complete') {
                this.onComplete(data.result);
                this.close();
            } else if (data.type === 'error') {
                this.onError(data.error);
                this.close();
            }
        };

        this.ws.onerror = (error) => {
            console.error('WebSocket error:', error);
            this.onError('Connection error');
        };

        this.ws.onclose = () => {
            // Fallback to polling if WebSocket fails
            if (!this.closed) {
                this.fallbackToPolling();
            }
        };
    }

    fallbackToPolling() {
        // Fallback to traditional HTTP polling
        detectUserWithPolling(this.username);
    }

    close() {
        this.closed = true;
        if (this.ws) {
            this.ws.close();
        }
    }
}

// Enhanced detection with progress updates
async function detectUserEnhanced(username) {
    const submitBtn = document.getElementById('submitBtn');
    const errorDiv = document.getElementById('error');
    const resultDiv = document.getElementById('result');
    const loadingManager = new LoadingStateManager();

    if (!username) return;

    submitBtn.disabled = true;
    submitBtn.innerHTML = 'INITIALIZING SEARCH...';
    errorDiv.classList.remove('show');
    resultDiv.classList.remove('show');

    loadingManager.start();

    // Try WebSocket first for real-time updates
    const ws = new DetectionWebSocket(
        username,
        (update) => {
            // Handle progress updates
            if (update.stage) {
                loadingManager.updateStage(update.stage, update.status, update.message);
            }
            if (update.buttonText) {
                submitBtn.innerHTML = update.buttonText;
            }
        },
        (result) => {
            // Handle completion
            loadingManager.stop();
            updateURL(username);
            displayResults(result);
            submitBtn.disabled = false;
            submitBtn.innerHTML = 'TRACK DEVELOPER';
            document.getElementById('enhanced-loading').classList.remove('show');
        },
        (error) => {
            // Handle error
            loadingManager.stop();
            errorDiv.innerHTML = 'TRAIL WENT COLD: ' + error;
            errorDiv.classList.add('show');
            submitBtn.disabled = false;
            submitBtn.innerHTML = 'TRACK DEVELOPER';
            document.getElementById('enhanced-loading').classList.remove('show');
        }
    );

    ws.connect();
}

// Fallback polling detection
async function detectUserWithPolling(username) {
    const submitBtn = document.getElementById('submitBtn');
    const errorDiv = document.getElementById('error');
    const resultDiv = document.getElementById('result');

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
        if (error.message.includes('404') || error.message.includes('not found')) {
            errorDiv.innerHTML = 'SUSPECT NOT FOUND: "' + username + '" - They\'ve gone off the grid!';
        } else {
            errorDiv.innerHTML = 'TRAIL WENT COLD: ' + error.message;
        }
        errorDiv.classList.add('show');
    } finally {
        submitBtn.disabled = false;
        submitBtn.innerHTML = 'TRACK DEVELOPER';
        const loadingEl = document.getElementById('enhanced-loading');
        if (loadingEl) loadingEl.classList.remove('show');
    }
}

// Keep existing functions
function updateURL(username) {
    currentUsername = username;
    const newURL = username ? '/' + username : '/';
    window.history.replaceState({}, '', newURL);
}

window.addEventListener('load', function() {
    const inputElement = document.getElementById('username');
    const existingUsername = inputElement.value.trim();
    
    if (existingUsername) {
        setTimeout(() => {
            // Use enhanced detection if available
            if (typeof detectUserEnhanced !== 'undefined') {
                detectUserEnhanced(existingUsername);
            } else {
                detectUser(existingUsername);
            }
        }, 100);
    }
});

document.getElementById('detectForm').addEventListener('submit', async (e) => {
    e.preventDefault();
    const username = document.getElementById('username').value.trim();
    
    // Use enhanced detection if available
    if (typeof detectUserEnhanced !== 'undefined') {
        await detectUserEnhanced(username);
    } else {
        await detectUser(username);
    }
});

// Export existing display functions
window.displayResults = displayResults;
window.formatMethodName = formatMethodName;
window.formatActiveHours = formatActiveHours;
window.formatLunchHours = formatLunchHours;
window.formatHour = formatHour;
window.getUTCOffsetFromTimezone = getUTCOffsetFromTimezone;
window.drawHistogram = drawHistogram;
window.initMapWhenReady = initMapWhenReady;
window.createMapFallback = createMapFallback;
window.initMap = initMap;