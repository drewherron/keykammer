// Keykammer Website JavaScript

document.addEventListener('DOMContentLoaded', function() {
    
    // Add smooth scrolling for anchor links
    document.querySelectorAll('a[href^="#"]').forEach(anchor => {
        anchor.addEventListener('click', function (e) {
            e.preventDefault();
            const target = document.querySelector(this.getAttribute('href'));
            if (target) {
                target.scrollIntoView({
                    behavior: 'smooth',
                    block: 'start'
                });
            }
        });
    });

    // Check discovery server status
    checkDiscoveryServerStatus();
    
    // Update download links with actual releases
    updateDownloadLinks();
    
    // Add copy functionality to code blocks
    addCodeCopyButtons();
    
    // Check server status every 30 seconds
    setInterval(checkDiscoveryServerStatus, 30000);
});

function updateDownloadLinks() {
    // Download links now point directly to binaries in dist/ folder
    // No JavaScript intervention needed - browsers handle downloads automatically
}

function addCodeCopyButtons() {
    const codeBlocks = document.querySelectorAll('.code-block');
    
    codeBlocks.forEach(block => {
        const copyButton = document.createElement('button');
        copyButton.textContent = 'Copy';
        copyButton.className = 'copy-btn';
        copyButton.style.cssText = `
            position: absolute;
            top: 10px;
            right: 10px;
            background: #3498db;
            color: white;
            border: none;
            padding: 5px 10px;
            border-radius: 4px;
            cursor: pointer;
            font-size: 0.8rem;
            opacity: 0;
            transition: opacity 0.3s ease;
        `;
        
        block.style.position = 'relative';
        block.appendChild(copyButton);
        
        // Show copy button on hover
        block.addEventListener('mouseenter', () => {
            copyButton.style.opacity = '1';
        });
        
        block.addEventListener('mouseleave', () => {
            copyButton.style.opacity = '0';
        });
        
        // Copy functionality
        copyButton.addEventListener('click', () => {
            const code = block.querySelector('code');
            const text = code.textContent.replace(/<br>/g, '\n');
            
            navigator.clipboard.writeText(text).then(() => {
                copyButton.textContent = 'Copied!';
                setTimeout(() => {
                    copyButton.textContent = 'Copy';
                }, 2000);
            }).catch(err => {
                console.error('Failed to copy text: ', err);
            });
        });
    });
}

// Add some visual enhancements
function addVisualEnhancements() {
    // Add fade-in animation for features
    const features = document.querySelectorAll('.feature');
    
    const observer = new IntersectionObserver((entries) => {
        entries.forEach(entry => {
            if (entry.isIntersecting) {
                entry.target.style.opacity = '1';
                entry.target.style.transform = 'translateY(0)';
            }
        });
    });
    
    features.forEach(feature => {
        feature.style.opacity = '0';
        feature.style.transform = 'translateY(20px)';
        feature.style.transition = 'opacity 0.6s ease, transform 0.6s ease';
        observer.observe(feature);
    });
}

// Initialize visual enhancements
addVisualEnhancements();

// Discovery Server Status Checking
function checkDiscoveryServerStatus() {
    const statusLight = document.getElementById('status-light');
    const statusText = document.getElementById('status-text');
    const statusLightMobile = document.getElementById('status-light-mobile');
    const statusTextMobile = document.getElementById('status-text-mobile');
    const discoveryServerUrl = 'http://discovery.keykammer.com:53952';
    
    // Set checking state for both desktop and mobile
    if (statusLight) {
        statusLight.className = 'status-light checking';
        statusText.textContent = 'checking...';
    }
    if (statusLightMobile) {
        statusLightMobile.className = 'status-light checking';
        statusTextMobile.textContent = 'checking...';
    }
    
    // Create a promise that will timeout after 5 seconds
    const timeoutPromise = new Promise((_, reject) => {
        setTimeout(() => reject(new Error('timeout')), 5000);
    });
    
    // Fetch the health endpoint
    const fetchPromise = fetch(`${discoveryServerUrl}/health`, {
        method: 'GET',
        mode: 'cors',
        cache: 'no-cache',
        headers: {
            'Accept': 'application/json',
        },
    });
    
    // Race between fetch and timeout
    Promise.race([fetchPromise, timeoutPromise])
        .then(response => {
            if (response.ok) {
                // Server is online - update both desktop and mobile
                if (statusLight) {
                    statusLight.className = 'status-light online';
                    statusText.textContent = 'online';
                }
                if (statusLightMobile) {
                    statusLightMobile.className = 'status-light online';
                    statusTextMobile.textContent = 'online';
                }
            } else {
                // Server responded but with error
                if (statusLight) {
                    statusLight.className = 'status-light offline';
                    statusText.textContent = 'offline';
                }
                if (statusLightMobile) {
                    statusLightMobile.className = 'status-light offline';
                    statusTextMobile.textContent = 'offline';
                }
            }
        })
        .catch(error => {
            // Network error or timeout
            if (statusLight) {
                statusLight.className = 'status-light offline';
                statusText.textContent = 'offline';
            }
            if (statusLightMobile) {
                statusLightMobile.className = 'status-light offline';
                statusTextMobile.textContent = 'offline';
            }
            console.log('Discovery server check failed:', error.message);
        });
}

// Function to manually trigger status check (for debugging)
window.checkServerStatus = checkDiscoveryServerStatus;