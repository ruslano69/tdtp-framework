// TDTP X-Ray - Wizard Navigation

let currentStep = 1;
const totalSteps = 7;
let appMode = 'production'; // production or mock

// Wait for Wails runtime to be ready
let wailsReady = false;

// Initialize wizard when DOM and Wails are ready
document.addEventListener('DOMContentLoaded', function() {
    console.log('DOM loaded');

    // Check if Wails runtime is available
    if (window.go && window.go.main && window.go.main.App) {
        console.log('Wails runtime ready');
        wailsReady = true;
    } else {
        console.warn('Wails runtime not ready, some features may not work');
        // Try again after a short delay
        setTimeout(() => {
            if (window.go && window.go.main && window.go.main.App) {
                console.log('Wails runtime ready (delayed)');
                wailsReady = true;
            }
        }, 500);
    }

    loadStep(1);
    updateNavigation();
});

// Switch between Mock and Production modes
async function switchMode(mode) {
    appMode = mode;
    console.log(`Switching to ${mode} mode`);

    if (wailsReady && window.go) {
        try {
            await window.go.main.App.SetMode(mode);
            console.log(`Backend mode switched to ${mode}`);
        } catch (err) {
            console.error('Failed to switch backend mode:', err);
        }
    }

    // Show notification
    showNotification(
        mode === 'mock'
            ? '🧪 Mock Mode: You can experiment freely. Warnings only.'
            : '🏭 Production Mode: Strict validation enabled.',
        mode === 'mock' ? 'warning' : 'info'
    );
}

// Load specific wizard step
function loadStep(step) {
    currentStep = step;

    // Update step navigation highlights
    document.querySelectorAll('.wizard-step').forEach(el => {
        el.classList.remove('active');
        if (parseInt(el.dataset.step) === step) {
            el.classList.add('active');
        }
    });

    // Load step content
    const content = document.getElementById('wizardContent');

    switch(step) {
        case 1:
            content.innerHTML = getStep1HTML();
            loadStep1Data();
            break;
        case 2:
            content.innerHTML = getStep2HTML();
            loadStep2Data();
            break;
        case 3:
            content.innerHTML = getStep3HTML();
            loadStep3Data();
            break;
        case 4:
            content.innerHTML = getStep4HTML();
            loadStep4Data();
            break;
        case 5:
            content.innerHTML = getStep5HTML();
            loadStep5Data();
            break;
        case 6:
            content.innerHTML = getStep6HTML();
            loadStep6Data();
            break;
        case 7:
            content.innerHTML = getStep7HTML();
            loadStep7Data();
            break;
    }

    updateNavigation();
}

// Navigate to next step
async function nextStep() {
    // Validate current step
    const [isValid, message] = await validateCurrentStep();

    if (!isValid) {
        if (appMode === 'production') {
            showNotification(`❌ ${message}`, 'error');
            return;
        } else {
            // Mock mode: show warning but allow continue
            const proceed = confirm(`⚠️ Warning: ${message}\n\nContinue anyway? (Mock mode)`);
            if (!proceed) return;
        }
    }

    // Save current step data
    await saveCurrentStep();

    // Move to next step
    if (currentStep < totalSteps) {
        loadStep(currentStep + 1);
    }
}

// Navigate to previous step
function previousStep() {
    if (currentStep > 1) {
        loadStep(currentStep - 1);
    }
}

// Update navigation buttons state
function updateNavigation() {
    const btnBack = document.getElementById('btnBack');
    const btnNext = document.getElementById('btnNext');

    btnBack.disabled = currentStep === 1;

    if (currentStep === totalSteps) {
        btnNext.textContent = 'Save Configuration';
        btnNext.classList.add('btn-success');
    } else {
        btnNext.textContent = 'Next →';
        btnNext.classList.remove('btn-success');
    }
}

// Validate current step
async function validateCurrentStep() {
    if (!wailsReady || !window.go) {
        console.warn('Wails not ready, skipping backend validation');
        // Client-side validation fallback
        if (currentStep === 1) {
            const name = document.getElementById('pipelineName');
            if (name && !name.value.trim()) {
                return [false, 'Pipeline name is required'];
            }
        }
        return [true, ''];
    }

    try {
        const result = await window.go.main.App.ValidateStep(currentStep);
        console.log('Validation result:', result);
        return result; // [isValid, message]
    } catch (err) {
        console.error('Validation error:', err);
        return [false, 'Validation failed: ' + err];
    }
}

// Save current step data
async function saveCurrentStep() {
    switch(currentStep) {
        case 1:
            await saveStep1();
            break;
        case 2:
            await saveStep2();
            break;
        case 3:
            await saveStep3();
            break;
        case 4:
            await saveStep4();
            break;
        case 5:
            await saveStep5();
            break;
        case 6:
            await saveStep6();
            break;
    }
}

// Show notification message
function showNotification(message, type = 'info') {
    const status = document.getElementById('footerStatus');
    status.textContent = message;
    status.className = `footer-status message-${type}`;

    // Clear after 5 seconds
    setTimeout(() => {
        status.textContent = '';
        status.className = 'footer-status';
    }, 5000);
}

// ========== STEP 1: Project Info ==========

function getStep1HTML() {
    return `
        <div class="step-content active">
            <div class="step-header">
                <h2>📋 Step 1: Project Information</h2>
                <p>Enter basic metadata for your ETL pipeline</p>
            </div>

            <div class="panel">
                <div class="form-group">
                    <label for="pipelineName">Pipeline Name *</label>
                    <input type="text" id="pipelineName" placeholder="e.g., User Orders Report" required>
                </div>

                <div class="form-row">
                    <div class="form-group">
                        <label for="pipelineVersion">Version</label>
                        <input type="text" id="pipelineVersion" value="1.0" placeholder="1.0">
                    </div>

                    <div class="form-group">
                        <label for="pipelineDescription">Description</label>
                        <textarea id="pipelineDescription" rows="3" placeholder="What does this pipeline do?"></textarea>
                    </div>
                </div>
            </div>
        </div>
    `;
}

async function loadStep1Data() {
    if (!wailsReady || !window.go) {
        console.log('Wails not ready, using default values');
        return;
    }

    try {
        const info = await window.go.main.App.GetPipelineInfo();
        console.log('Loaded pipeline info:', info);
        if (info && info.name) {
            document.getElementById('pipelineName').value = info.name;
            document.getElementById('pipelineVersion').value = info.version || '1.0';
            document.getElementById('pipelineDescription').value = info.description || '';
        }
    } catch (err) {
        console.error('Failed to load pipeline info:', err);
    }
}

async function saveStep1() {
    const info = {
        name: document.getElementById('pipelineName').value,
        version: document.getElementById('pipelineVersion').value,
        description: document.getElementById('pipelineDescription').value,
    };

    console.log('Saving pipeline info:', info);

    if (!wailsReady || !window.go) {
        console.warn('Wails not ready, data saved locally only');
        // Store locally for now
        localStorage.setItem('pipelineInfo', JSON.stringify(info));
        return;
    }

    try {
        await window.go.main.App.SavePipelineInfo(info);
        console.log('Pipeline info saved to backend');
    } catch (err) {
        console.error('Failed to save pipeline info:', err);
        // Don't throw - allow continuation in mock mode
        if (appMode === 'production') {
            throw err;
        }
    }
}

// ========== STEP 2: Sources ==========

let sources = [];
let currentSource = null;
let editingSourceIndex = -1;

function getStep2HTML() {
    return `
        <div class="step-content active">
            <div class="step-header">
                <h2>🔌 Step 2: Configure Sources</h2>
                <p>Add data sources for your pipeline</p>
            </div>

            <div class="panel">
                <div style="display: flex; gap: 15px;">
                    <!-- Source List -->
                    <div style="flex: 1;">
                        <h3>Data Sources</h3>
                        <div id="sourceList" style="min-height: 200px; border: 1px solid #ccc; padding: 10px; border-radius: 3px; background: white;">
                            <p style="color: #999; text-align: center; padding: 40px;">No sources added yet</p>
                        </div>
                        <button class="btn btn-primary" onclick="showAddSourceForm()" style="margin-top: 10px;">
                            ➕ Add Source
                        </button>
                    </div>

                    <!-- Source Form -->
                    <div id="sourceFormPanel" style="flex: 1; display: none;">
                        <h3 id="sourceFormTitle">Add New Source</h3>
                        <div class="form-group">
                            <label for="sourceName">Source Name *</label>
                            <input type="text" id="sourceName" placeholder="e.g., users_db">
                        </div>

                        <div class="form-group">
                            <label for="sourceType">Source Type *</label>
                            <select id="sourceType" onchange="onSourceTypeChange()">
                                <option value="">-- Select Type --</option>
                                <option value="postgres">PostgreSQL</option>
                                <option value="mysql">MySQL</option>
                                <option value="mssql">MS SQL Server</option>
                                <option value="sqlite">SQLite</option>
                                <option value="mock">Mock (JSON)</option>
                            </select>
                        </div>

                        <!-- Database Connection Fields -->
                        <div id="dbFields" style="display: none;">
                            <div class="form-group">
                                <label for="sourceDSN">Connection String (DSN) *</label>
                                <textarea id="sourceDSN" rows="2" placeholder="e.g., host=localhost port=5432 user=postgres password=pwd dbname=mydb"></textarea>
                            </div>

                            <div class="form-group">
                                <label for="sourceQuery">SQL Query *</label>
                                <textarea id="sourceQuery" rows="4" placeholder="SELECT * FROM users WHERE active = 1"></textarea>
                            </div>

                            <button class="btn btn-secondary" onclick="testConnection()" id="btnTestConnection">
                                🔍 Test Connection
                            </button>
                            <div id="testResult" style="margin-top: 10px; display: none;"></div>
                        </div>

                        <!-- Mock Source Fields -->
                        <div id="mockFields" style="display: none;">
                            <p style="color: #666; font-size: 12px;">
                                Mock sources use JSON data for prototyping.<br>
                                Upload a JSON file or create inline data.
                            </p>
                            <div class="form-group">
                                <label>Mock Data (JSON)</label>
                                <textarea id="mockDataJson" rows="8" placeholder='{"schema": [...], "data": [...]}'></textarea>
                            </div>
                        </div>

                        <div style="margin-top: 20px; display: flex; gap: 10px;">
                            <button class="btn btn-success" onclick="saveSourceForm()">
                                💾 Save Source
                            </button>
                            <button class="btn btn-secondary" onclick="cancelSourceForm()">
                                ❌ Cancel
                            </button>
                        </div>
                    </div>
                </div>

                <!-- Preview Panel -->
                <div id="previewPanel" style="margin-top: 20px; display: none;">
                    <h3>Data Preview</h3>
                    <div id="previewContent" style="border: 1px solid #ccc; padding: 10px; background: white; border-radius: 3px;">
                    </div>
                </div>
            </div>
        </div>
    `;
}

async function loadStep2Data() {
    console.log('Loading Step 2 data');

    if (!wailsReady || !window.go) {
        console.warn('Wails not ready, loading sources from localStorage');
        const stored = localStorage.getItem('sources');
        if (stored) {
            sources = JSON.parse(stored);
            renderSourceList();
        }
        return;
    }

    try {
        sources = await window.go.main.App.GetSources();
        console.log('Loaded sources:', sources);
        renderSourceList();
    } catch (err) {
        console.error('Failed to load sources:', err);
    }
}

async function saveStep2() {
    console.log('Saving Step 2 data:', sources);

    if (!wailsReady || !window.go) {
        console.warn('Wails not ready, saving sources locally');
        localStorage.setItem('sources', JSON.stringify(sources));
        return;
    }

    // Sources are saved individually via AddSource/UpdateSource
    // Nothing to do here
}

function renderSourceList() {
    const listEl = document.getElementById('sourceList');

    if (sources.length === 0) {
        listEl.innerHTML = '<p style="color: #999; text-align: center; padding: 40px;">No sources added yet</p>';
        return;
    }

    let html = '<div style="display: flex; flex-direction: column; gap: 10px;">';
    sources.forEach((src, index) => {
        const statusIcon = src.tested ? '✅' : '⚠️';
        const typeLabel = src.type.toUpperCase();

        html += `
            <div style="border: 1px solid #ddd; padding: 10px; border-radius: 3px; background: #fafafa;">
                <div style="display: flex; justify-content: space-between; align-items: center;">
                    <div>
                        <strong>${src.name}</strong> ${statusIcon}
                        <br><small style="color: #666;">${typeLabel}</small>
                    </div>
                    <div>
                        <button class="btn btn-sm" onclick="editSource(${index})">Edit</button>
                        <button class="btn btn-sm" onclick="previewSource(${index})">Preview</button>
                        <button class="btn btn-sm" onclick="removeSource(${index})">Remove</button>
                    </div>
                </div>
            </div>
        `;
    });
    html += '</div>';

    listEl.innerHTML = html;
}

function showAddSourceForm() {
    document.getElementById('sourceFormTitle').textContent = 'Add New Source';
    document.getElementById('sourceFormPanel').style.display = 'block';
    editingSourceIndex = -1;
    clearSourceForm();
}

function clearSourceForm() {
    document.getElementById('sourceName').value = '';
    document.getElementById('sourceType').value = '';
    document.getElementById('sourceDSN').value = '';
    document.getElementById('sourceQuery').value = '';
    document.getElementById('mockDataJson').value = '';
    document.getElementById('dbFields').style.display = 'none';
    document.getElementById('mockFields').style.display = 'none';
    document.getElementById('testResult').style.display = 'none';
}

function onSourceTypeChange() {
    const type = document.getElementById('sourceType').value;
    const dbFields = document.getElementById('dbFields');
    const mockFields = document.getElementById('mockFields');

    if (type === 'mock') {
        dbFields.style.display = 'none';
        mockFields.style.display = 'block';
    } else if (type) {
        dbFields.style.display = 'block';
        mockFields.style.display = 'none';
    } else {
        dbFields.style.display = 'none';
        mockFields.style.display = 'none';
    }
}

async function testConnection() {
    const type = document.getElementById('sourceType').value;
    const dsn = document.getElementById('sourceDSN').value;

    if (!type || !dsn) {
        showNotification('Please fill in type and DSN', 'error');
        return;
    }

    const resultEl = document.getElementById('testResult');
    resultEl.style.display = 'block';
    resultEl.innerHTML = '<p>Testing connection...</p>';

    if (!wailsReady || !window.go) {
        resultEl.innerHTML = '<p style="color: orange;">⚠️ Wails not ready, connection test skipped</p>';
        return;
    }

    try {
        const source = { name: 'test', type: type, dsn: dsn };
        const result = await window.go.main.App.TestSource(source);

        if (result.success) {
            resultEl.innerHTML = `
                <p style="color: green;">✅ ${result.message}</p>
                <p><small>Tables found: ${result.tables ? result.tables.length : 0}</small></p>
            `;
        } else {
            resultEl.innerHTML = `<p style="color: red;">❌ ${result.message}</p>`;
        }
    } catch (err) {
        console.error('Test connection error:', err);
        resultEl.innerHTML = `<p style="color: red;">❌ Error: ${err}</p>`;
    }
}

async function saveSourceForm() {
    const name = document.getElementById('sourceName').value.trim();
    const type = document.getElementById('sourceType').value;

    if (!name || !type) {
        showNotification('Name and type are required', 'error');
        return;
    }

    const source = {
        name: name,
        type: type,
        tested: false,
    };

    if (type === 'mock') {
        const jsonData = document.getElementById('mockDataJson').value;
        if (jsonData) {
            try {
                source.mockData = JSON.parse(jsonData);
            } catch (err) {
                showNotification('Invalid JSON format', 'error');
                return;
            }
        }
    } else {
        source.dsn = document.getElementById('sourceDSN').value;
        source.query = document.getElementById('sourceQuery').value;
    }

    if (editingSourceIndex >= 0) {
        // Update existing
        sources[editingSourceIndex] = source;
    } else {
        // Add new
        sources.push(source);
    }

    // Save to backend if available
    if (wailsReady && window.go) {
        try {
            await window.go.main.App.AddSource(source);
        } catch (err) {
            console.error('Failed to save source to backend:', err);
        }
    }

    cancelSourceForm();
    renderSourceList();
    showNotification(`Source '${name}' saved`, 'info');
}

function cancelSourceForm() {
    document.getElementById('sourceFormPanel').style.display = 'none';
    editingSourceIndex = -1;
}

function editSource(index) {
    const src = sources[index];
    editingSourceIndex = index;

    document.getElementById('sourceFormTitle').textContent = 'Edit Source';
    document.getElementById('sourceFormPanel').style.display = 'block';
    document.getElementById('sourceName').value = src.name;
    document.getElementById('sourceType').value = src.type;

    if (src.type === 'mock' && src.mockData) {
        document.getElementById('mockDataJson').value = JSON.stringify(src.mockData, null, 2);
        onSourceTypeChange();
    } else {
        document.getElementById('sourceDSN').value = src.dsn || '';
        document.getElementById('sourceQuery').value = src.query || '';
        onSourceTypeChange();
    }
}

function removeSource(index) {
    const src = sources[index];
    if (confirm(`Remove source '${src.name}'?`)) {
        sources.splice(index, 1);
        renderSourceList();
        showNotification(`Source '${src.name}' removed`, 'info');
    }
}

async function previewSource(index) {
    const src = sources[index];
    const previewPanel = document.getElementById('previewPanel');
    const previewContent = document.getElementById('previewContent');

    previewPanel.style.display = 'block';
    previewContent.innerHTML = '<p>Loading preview...</p>';

    if (!wailsReady || !window.go) {
        previewContent.innerHTML = '<p style="color: orange;">⚠️ Preview not available (Wails not ready)</p>';
        return;
    }

    try {
        const result = await window.go.main.App.PreviewSource({
            sourceName: src.name,
            limit: 10
        });

        if (result.error) {
            previewContent.innerHTML = `<p style="color: red;">❌ ${result.error}</p>`;
            return;
        }

        // Render table
        let html = '<table style="width: 100%; border-collapse: collapse; font-size: 12px;">';
        html += '<thead><tr>';
        result.columns.forEach(col => {
            html += `<th style="border: 1px solid #ddd; padding: 5px; background: #f0f0f0;">${col}</th>`;
        });
        html += '</tr></thead><tbody>';

        result.rows.forEach(row => {
            html += '<tr>';
            row.forEach(cell => {
                html += `<td style="border: 1px solid #ddd; padding: 5px;">${cell !== null ? cell : '<i>null</i>'}</td>`;
            });
            html += '</tr>';
        });

        html += '</tbody></table>';
        html += `<p style="margin-top: 10px; color: #666;"><small>Showing ${result.rows.length} rows</small></p>`;

        previewContent.innerHTML = html;
    } catch (err) {
        console.error('Preview error:', err);
        previewContent.innerHTML = `<p style="color: red;">❌ Error: ${err}</p>`;
    }
}

// ========== STEP 3-7: Placeholders ==========

function getStep3HTML() {
    return `<div class="step-content active"><div class="panel"><p class="text-center" style="padding: 40px; color: #666;">🚧 Step 3: Visual Designer - Coming soon...</p></div></div>`;
}
function loadStep3Data() {}
async function saveStep3() {}

function getStep4HTML() {
    return `<div class="step-content active"><div class="panel"><p class="text-center" style="padding: 40px; color: #666;">🚧 Step 4: Transform SQL - Coming soon...</p></div></div>`;
}
function loadStep4Data() {}
async function saveStep4() {}

function getStep5HTML() {
    return `<div class="step-content active"><div class="panel"><p class="text-center" style="padding: 40px; color: #666;">🚧 Step 5: Configure Output - Coming soon...</p></div></div>`;
}
function loadStep5Data() {}
async function saveStep5() {}

function getStep6HTML() {
    return `<div class="step-content active"><div class="panel"><p class="text-center" style="padding: 40px; color: #666;">🚧 Step 6: Settings - Coming soon...</p></div></div>`;
}
function loadStep6Data() {}
async function saveStep6() {}

function getStep7HTML() {
    return `<div class="step-content active"><div class="panel"><p class="text-center" style="padding: 40px; color: #666;">🚧 Step 7: Review & Save - Coming soon...</p></div></div>`;
}
function loadStep7Data() {}
