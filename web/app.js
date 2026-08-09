// Global state
let currentSessionId = null;
let currentResources = [];
let currentResults = [];

// DOM Elements
const uploadArea = document.getElementById('upload-area');
const fileInput = document.getElementById('file-input');
const uploadStatus = document.getElementById('upload-status');
const resourcesSection = document.getElementById('resources-section');
const resourcesLoading = document.getElementById('resources-loading');
const resourcesList = document.getElementById('resources-list');
const extractSection = document.getElementById('extract-section');
const extractForm = document.getElementById('extract-form');
const resourceSelect = document.getElementById('resource-select');
const namespaceInput = document.getElementById('namespace-input');
const nameInput = document.getElementById('name-input');
const allNamespacesCheck = document.getElementById('all-namespaces-check');
const formatSelect = document.getElementById('format-select');
const downloadBtn = document.getElementById('download-btn');
const resultsSection = document.getElementById('results-section');
const resultsSummary = document.getElementById('results-summary');
const resultsContent = document.getElementById('results-content');

// Upload handlers
uploadArea.addEventListener('click', () => fileInput.click());

uploadArea.addEventListener('dragover', (e) => {
    e.preventDefault();
    uploadArea.classList.add('drag-over');
});

uploadArea.addEventListener('dragleave', () => {
    uploadArea.classList.remove('drag-over');
});

uploadArea.addEventListener('drop', (e) => {
    e.preventDefault();
    uploadArea.classList.remove('drag-over');

    const files = e.dataTransfer.files;
    if (files.length > 0) {
        handleFileUpload(files[0]);
    }
});

fileInput.addEventListener('change', (e) => {
    if (e.target.files.length > 0) {
        handleFileUpload(e.target.files[0]);
    }
});

// File upload function
async function handleFileUpload(file) {
    const formData = new FormData();
    formData.append('dbfile', file);

    showStatus('Uploading file...', 'info');

    try {
        const response = await fetch('/api/upload', {
            method: 'POST',
            body: formData
        });

        if (!response.ok) {
            const error = await response.text();
            throw new Error(error || 'Upload failed');
        }

        const data = await response.json();
        currentSessionId = data.sessionId;

        showStatus(`File uploaded successfully: ${data.filename}`, 'success');

        // Load resources
        await loadResources();

    } catch (error) {
        showStatus(`Error: ${error.message}`, 'error');
    }
}

// Load resources from uploaded database
async function loadResources() {
    resourcesSection.classList.remove('hidden');
    resourcesLoading.classList.remove('hidden');
    resourcesList.classList.add('hidden');

    try {
        const response = await fetch(`/api/resources?sessionId=${currentSessionId}`);

        if (!response.ok) {
            throw new Error('Failed to load resources');
        }

        const data = await response.json();
        currentResources = data.resources;

        displayResources(data.resources);

        resourcesLoading.classList.add('hidden');
        resourcesList.classList.remove('hidden');
        extractSection.classList.remove('hidden');

    } catch (error) {
        showStatus(`Error loading resources: ${error.message}`, 'error');
        resourcesLoading.textContent = 'Failed to load resources';
    }
}

// Display resources as cards
function displayResources(resources) {
    // Sort resources by name
    resources.sort((a, b) => a.name.localeCompare(b.name));

    const grid = document.createElement('div');
    grid.className = 'resources-grid';

    resources.forEach(resource => {
        const card = document.createElement('div');
        card.className = 'resource-card';

        const badgeClass = resource.namespaced ? 'badge-namespaced' : 'badge-cluster';

        card.innerHTML = `
            <h3>${resource.name}</h3>
            <div class="resource-meta">
                <span class="resource-badge ${badgeClass}">${resource.type}</span>
                <span>${resource.count} objects</span>
            </div>
        `;

        card.addEventListener('click', () => {
            // Remove previous selection
            document.querySelectorAll('.resource-card').forEach(c =>
                c.classList.remove('selected')
            );

            // Select this card
            card.classList.add('selected');

            // Update form
            resourceSelect.value = resource.name;

            // Enable/disable namespace input based on resource type
            if (resource.namespaced) {
                namespaceInput.disabled = false;
                allNamespacesCheck.disabled = false;
            } else {
                namespaceInput.disabled = true;
                namespaceInput.value = '';
                allNamespacesCheck.disabled = true;
                allNamespacesCheck.checked = false;
            }
        });

        grid.appendChild(card);
    });

    // Populate select dropdown
    resourceSelect.innerHTML = '<option value="">-- Select a resource --</option>';
    resources.forEach(resource => {
        const option = document.createElement('option');
        option.value = resource.name;
        option.textContent = `${resource.name} (${resource.count})`;
        resourceSelect.appendChild(option);
    });

    resourcesList.innerHTML = '';
    resourcesList.appendChild(grid);
}

// Resource select change handler
resourceSelect.addEventListener('change', (e) => {
    const selectedResource = currentResources.find(r => r.name === e.target.value);

    if (selectedResource) {
        if (selectedResource.namespaced) {
            namespaceInput.disabled = false;
            allNamespacesCheck.disabled = false;
        } else {
            namespaceInput.disabled = true;
            namespaceInput.value = '';
            allNamespacesCheck.disabled = true;
            allNamespacesCheck.checked = false;
        }
    }
});

// Extract form submission
extractForm.addEventListener('submit', async (e) => {
    e.preventDefault();

    const resource = resourceSelect.value;
    if (!resource) {
        alert('Please select a resource type');
        return;
    }

    const extractRequest = {
        sessionId: currentSessionId,
        resource: resource,
        namespace: namespaceInput.value,
        name: nameInput.value,
        allNamespaces: allNamespacesCheck.checked,
        format: formatSelect.value
    };

    try {
        const response = await fetch('/api/extract', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify(extractRequest)
        });

        if (!response.ok) {
            throw new Error('Extraction failed');
        }

        const data = await response.json();
        currentResults = data.objects;

        displayResults(data.objects, extractRequest.format);
        downloadBtn.classList.remove('hidden');

    } catch (error) {
        showStatus(`Error extracting objects: ${error.message}`, 'error');
    }
});

// Display extraction results
function displayResults(objects, format) {
    resultsSection.classList.remove('hidden');

    if (objects.length === 0) {
        resultsSummary.innerHTML = '<strong>No objects found matching the criteria</strong>';
        resultsContent.innerHTML = '';
        return;
    }

    resultsSummary.innerHTML = `<strong>Found ${objects.length} object(s)</strong>`;

    resultsContent.innerHTML = '';

    objects.forEach((obj, index) => {
        const item = document.createElement('div');
        item.className = 'result-item';

        let content = '';
        if (format === 'yaml') {
            // Format YAML content
            content = `# Key: ${obj.key}\n`;
            if (obj.namespace) {
                content += `# Namespace: ${obj.namespace}\n`;
            }
            content += `# Resource: ${obj.resource}\n`;
            content += `# Name: ${obj.name}\n`;
            content += `---\n`;
            content += formatYAML(obj.object);
        } else {
            content = JSON.stringify(obj.object, null, 2);
        }

        const title = obj.namespace
            ? `${obj.resource}/${obj.namespace}/${obj.name}`
            : `${obj.resource}/${obj.name}`;

        item.innerHTML = `
            <div class="result-header">
                <span class="result-title">${title}</span>
                <span class="result-meta">${format.toUpperCase()}</span>
            </div>
            <div class="result-content">
                <pre>${escapeHtml(content)}</pre>
            </div>
        `;

        resultsContent.appendChild(item);
    });

    // Scroll to results
    resultsSection.scrollIntoView({ behavior: 'smooth' });
}

// Download button handler
downloadBtn.addEventListener('click', async () => {
    const extractRequest = {
        sessionId: currentSessionId,
        resource: resourceSelect.value,
        namespace: namespaceInput.value,
        name: nameInput.value,
        allNamespaces: allNamespacesCheck.checked,
        format: formatSelect.value
    };

    try {
        const response = await fetch('/api/download', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify(extractRequest)
        });

        if (!response.ok) {
            throw new Error('Download failed');
        }

        // Trigger download
        const blob = await response.blob();
        const url = window.URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = `etcd-extract-${Date.now()}.zip`;
        document.body.appendChild(a);
        a.click();
        window.URL.revokeObjectURL(url);
        document.body.removeChild(a);

        showStatus('Download started', 'success');

    } catch (error) {
        showStatus(`Error downloading: ${error.message}`, 'error');
    }
});

// Helper functions
function showStatus(message, type) {
    uploadStatus.textContent = message;
    uploadStatus.className = `status ${type}`;
    uploadStatus.classList.remove('hidden');

    if (type === 'success') {
        setTimeout(() => {
            uploadStatus.classList.add('hidden');
        }, 3000);
    }
}

function formatYAML(obj, indent = 0) {
    // Simple YAML formatter
    let result = '';
    const spaces = '  '.repeat(indent);

    for (const [key, value] of Object.entries(obj)) {
        if (value === null || value === undefined) {
            result += `${spaces}${key}: null\n`;
        } else if (typeof value === 'object' && !Array.isArray(value)) {
            result += `${spaces}${key}:\n`;
            result += formatYAML(value, indent + 1);
        } else if (Array.isArray(value)) {
            result += `${spaces}${key}:\n`;
            value.forEach(item => {
                if (typeof item === 'object') {
                    result += `${spaces}- \n`;
                    result += formatYAML(item, indent + 1);
                } else {
                    result += `${spaces}- ${item}\n`;
                }
            });
        } else if (typeof value === 'string') {
            result += `${spaces}${key}: "${value}"\n`;
        } else {
            result += `${spaces}${key}: ${value}\n`;
        }
    }

    return result;
}

function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}
