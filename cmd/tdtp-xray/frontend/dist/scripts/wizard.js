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
    // Save current step data BEFORE validation so backend has up-to-date state
    await saveCurrentStep();

    // Validate current step
    const validation = await validateCurrentStep();

    if (!validation.isValid) {
        if (appMode === 'production') {
            showNotification(`❌ ${validation.message}`, 'error');
            return;
        } else {
            // Mock mode: show warning but allow continue
            const proceed = confirm(`⚠️ Warning: ${validation.message}\n\nContinue anyway? (Mock mode)`);
            if (!proceed) return;
        }
    }

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
                return { isValid: false, message: 'Pipeline name is required' };
            }
        }
        return { isValid: true, message: '' };
    }

    try {
        const result = await window.go.main.App.ValidateStep(currentStep);
        console.log('Validation result:', result);
        return result; // { isValid: bool, message: string }
    } catch (err) {
        console.error('Validation error:', err);
        return { isValid: false, message: 'Validation failed: ' + err };
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
                <p>Create new pipeline or load existing configuration</p>
            </div>

            <div class="panel">
                <!-- Load/Save Configuration -->
                <div style="margin-bottom: 20px; padding: 15px; background: #f8f9fa; border: 1px solid #dee2e6; border-radius: 3px;">
                    <h3 style="margin: 0 0 10px 0; font-size: 14px;">Configuration File</h3>
                    <div style="display: flex; gap: 10px;">
                        <button class="btn btn-secondary" onclick="loadConfigurationFile()" style="flex: 1;">
                            📁 Load Configuration...
                        </button>
                        <button class="btn btn-secondary" onclick="saveConfigurationFile()" style="flex: 1;">
                            💾 Save Configuration...
                        </button>
                    </div>
                    <p style="margin: 5px 0 0 0; font-size: 10px; color: #6c757d;">
                        Load existing TDTP pipeline YAML or save current configuration
                    </p>
                </div>

                <div class="form-group">
                    <label for="pipelineName">Pipeline Name *</label>
                    <input type="text" id="pipelineName" placeholder="e.g., User Orders Report" required oninput="validatePipelineName()">
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

// Validate pipeline name in real-time
function validatePipelineName() {
    const input = document.getElementById('pipelineName');
    const value = input.value.trim();

    if (!value) {
        input.style.borderColor = '#dc3545'; // Red
        input.style.borderWidth = '2px';
        return false;
    } else {
        input.style.borderColor = '#28a745'; // Green
        input.style.borderWidth = '2px';
        return true;
    }
}

// Load configuration from YAML file
async function loadConfigurationFile() {
    if (!wailsReady || !window.go) {
        showNotification('File picker not available (Wails not ready)', 'error');
        return;
    }

    try {
        const result = await window.go.main.App.LoadConfigurationFile();
        if (result.success) {
            showNotification(`✅ Configuration loaded: ${result.filename}`, 'success');

            // Reload all steps with loaded data
            loadStep1Data();
            loadStep2Data();

            // Update UI
            showNotification(`Configuration '${result.config.name}' loaded successfully`, 'success');
        } else {
            showNotification(`❌ Failed to load configuration: ${result.error}`, 'error');
        }
    } catch (err) {
        console.error('Load configuration error:', err);
        showNotification('Failed to load configuration: ' + err, 'error');
    }
}

// Save configuration to YAML file
async function saveConfigurationFile() {
    if (!wailsReady || !window.go) {
        showNotification('File save not available (Wails not ready)', 'error');
        return;
    }

    // Save current step data first
    await saveCurrentStep();

    try {
        const result = await window.go.main.App.SaveConfigurationFile();
        if (result.success) {
            showNotification(`✅ Configuration saved: ${result.filename}`, 'success');
        } else {
            showNotification(`❌ Failed to save: ${result.error}`, 'error');
        }
    } catch (err) {
        console.error('Save configuration error:', err);
        showNotification('Failed to save configuration: ' + err, 'error');
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

                        <!-- Database Type Radio Buttons -->
                        <div class="form-group">
                            <label>Database Type *</label>
                            <div class="radio-group">
                                <label>
                                    <input type="radio" name="sourceType" value="postgres" onchange="onSourceTypeChange('postgres')">
                                    <span>PostgreSQL</span>
                                </label>
                                <label>
                                    <input type="radio" name="sourceType" value="mysql" onchange="onSourceTypeChange('mysql')">
                                    <span>MySQL</span>
                                </label>
                                <label>
                                    <input type="radio" name="sourceType" value="mssql" onchange="onSourceTypeChange('mssql')">
                                    <span>Microsoft SQL Server</span>
                                </label>
                                <label>
                                    <input type="radio" name="sourceType" value="sqlite" onchange="onSourceTypeChange('sqlite')">
                                    <span>SQLite</span>
                                </label>
                                <label>
                                    <input type="radio" name="sourceType" value="mock" onchange="onSourceTypeChange('mock')">
                                    <span>Mock (JSON) - Development only</span>
                                </label>
                            </div>
                        </div>

                        <!-- PostgreSQL Fields -->
                        <div id="postgresFields" class="db-connection-fields" style="display: none;">
                            <div class="form-row">
                                <div class="form-group" style="flex: 2;">
                                    <label for="pgHost">Server *</label>
                                    <input type="text" id="pgHost" value="localhost" placeholder="localhost">
                                </div>
                                <div class="form-group" style="flex: 1;">
                                    <label for="pgPort">Port *</label>
                                    <input type="number" id="pgPort" value="5432" placeholder="5432">
                                </div>
                            </div>
                            <div class="form-group">
                                <label for="pgDatabase">Database *</label>
                                <input type="text" id="pgDatabase" placeholder="testdb">
                            </div>
                            <div class="form-row">
                                <div class="form-group">
                                    <label for="pgUser">User *</label>
                                    <input type="text" id="pgUser" placeholder="postgres">
                                </div>
                                <div class="form-group">
                                    <label for="pgPassword">Password *</label>
                                    <input type="password" id="pgPassword" placeholder="password">
                                </div>
                            </div>
                            <div class="form-group">
                                <label for="pgSSLMode">SSL Mode</label>
                                <select id="pgSSLMode">
                                    <option value="disable">Disable</option>
                                    <option value="require">Require</option>
                                    <option value="verify-ca">Verify CA</option>
                                    <option value="verify-full">Verify Full</option>
                                </select>
                            </div>
                        </div>

                        <!-- MySQL Fields -->
                        <div id="mysqlFields" class="db-connection-fields" style="display: none;">
                            <div class="form-row">
                                <div class="form-group" style="flex: 2;">
                                    <label for="myHost">Server *</label>
                                    <input type="text" id="myHost" value="localhost" placeholder="localhost">
                                </div>
                                <div class="form-group" style="flex: 1;">
                                    <label for="myPort">Port *</label>
                                    <input type="number" id="myPort" value="3306" placeholder="3306">
                                </div>
                            </div>
                            <div class="form-group">
                                <label for="myDatabase">Database *</label>
                                <input type="text" id="myDatabase" placeholder="testdb">
                            </div>
                            <div class="form-row">
                                <div class="form-group">
                                    <label for="myUser">User *</label>
                                    <input type="text" id="myUser" placeholder="root">
                                </div>
                                <div class="form-group">
                                    <label for="myPassword">Password *</label>
                                    <input type="password" id="myPassword" placeholder="password">
                                </div>
                            </div>
                        </div>

                        <!-- MSSQL Fields -->
                        <div id="mssqlFields" class="db-connection-fields" style="display: none;">
                            <div class="form-row">
                                <div class="form-group" style="flex: 2;">
                                    <label for="msServer">Server *</label>
                                    <input type="text" id="msServer" value="localhost" placeholder="localhost">
                                </div>
                                <div class="form-group" style="flex: 1;">
                                    <label for="msPort">Port *</label>
                                    <input type="number" id="msPort" value="1433" placeholder="1433">
                                </div>
                            </div>
                            <div class="form-group">
                                <label for="msDatabase">Database *</label>
                                <input type="text" id="msDatabase" placeholder="testdb">
                            </div>
                            <div class="form-row">
                                <div class="form-group">
                                    <label for="msUser">User *</label>
                                    <input type="text" id="msUser" value="sa" placeholder="sa">
                                </div>
                                <div class="form-group">
                                    <label for="msPassword">Password *</label>
                                    <input type="password" id="msPassword" placeholder="password">
                                </div>
                            </div>
                        </div>

                        <!-- SQLite Fields -->
                        <div id="sqliteFields" class="db-connection-fields" style="display: none;">
                            <div class="form-group">
                                <label for="sqliteFile">Database File *</label>
                                <div style="display: flex; gap: 5px; flex: 1;">
                                    <input type="text" id="sqliteFile" placeholder="C:\\path\\to\\database.db" style="flex: 1;">
                                    <button class="btn btn-secondary" onclick="browseDatabaseFile()" style="padding: 6px 15px;">Browse...</button>
                                </div>
                            </div>
                        </div>

                        <!-- Connection Test Button -->
                        <div id="connectionTestPanel" style="display: none;">
                            <button class="btn btn-secondary" onclick="testConnection()" id="btnTestConnection">
                                🔍 Test Connection
                            </button>
                            <div id="testResult" style="margin-top: 10px; display: none;"></div>
                        </div>

                        <!-- Mock Source Fields -->
                        <div id="mockFields" style="display: none;">
                            <p style="color: #666; font-size: 12px; padding: 10px; background: #fff3cd; border: 1px solid #ffc107; border-radius: 3px;">
                                ⚠️ <strong>Development Mode Only</strong><br>
                                Mock sources use JSON data for prototyping without real database connections.
                            </p>
                            <div class="form-group-full">
                                <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 5px;">
                                    <label style="margin: 0;">Mock Data (JSON)</label>
                                    <button class="btn btn-secondary btn-sm" onclick="loadJSONFile()">📁 Load from File...</button>
                                </div>
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
                    <div id="previewContent" style="border: 1px solid #ccc; padding: 10px; background: white; border-radius: 3px; overflow-x: auto; max-width: 100%;">
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
    selectedTableName = '';

    // Uncheck all radio buttons
    const radios = document.getElementsByName('sourceType');
    radios.forEach(r => r.checked = false);

    // Hide all field groups
    document.getElementById('postgresFields').style.display = 'none';
    document.getElementById('mysqlFields').style.display = 'none';
    document.getElementById('mssqlFields').style.display = 'none';
    document.getElementById('sqliteFields').style.display = 'none';
    document.getElementById('connectionTestPanel').style.display = 'none';
    document.getElementById('mockFields').style.display = 'none';
    document.getElementById('testResult').style.display = 'none';
}

function onSourceTypeChange(type) {
    // Hide all field groups
    document.getElementById('postgresFields').style.display = 'none';
    document.getElementById('mysqlFields').style.display = 'none';
    document.getElementById('mssqlFields').style.display = 'none';
    document.getElementById('sqliteFields').style.display = 'none';
    document.getElementById('connectionTestPanel').style.display = 'none';
    document.getElementById('mockFields').style.display = 'none';

    // Show fields for selected type
    if (type === 'postgres') {
        document.getElementById('postgresFields').style.display = 'block';
        document.getElementById('connectionTestPanel').style.display = 'block';
    } else if (type === 'mysql') {
        document.getElementById('mysqlFields').style.display = 'block';
        document.getElementById('connectionTestPanel').style.display = 'block';
    } else if (type === 'mssql') {
        document.getElementById('mssqlFields').style.display = 'block';
        document.getElementById('connectionTestPanel').style.display = 'block';
    } else if (type === 'sqlite') {
        document.getElementById('sqliteFields').style.display = 'block';
        document.getElementById('connectionTestPanel').style.display = 'block';
    } else if (type === 'mock') {
        document.getElementById('mockFields').style.display = 'block';
    }
}

// Generate DSN from individual fields
function generateDSN() {
    const type = getSelectedSourceType();

    if (type === 'postgres') {
        const host = document.getElementById('pgHost').value || 'localhost';
        const port = document.getElementById('pgPort').value || '5432';
        const user = document.getElementById('pgUser').value;
        const password = document.getElementById('pgPassword').value;
        const database = document.getElementById('pgDatabase').value;
        const sslmode = document.getElementById('pgSSLMode').value || 'disable';

        return `host=${host} port=${port} user=${user} password=${password} dbname=${database} sslmode=${sslmode}`;

    } else if (type === 'mysql') {
        const host = document.getElementById('myHost').value || 'localhost';
        const port = document.getElementById('myPort').value || '3306';
        const user = document.getElementById('myUser').value;
        const password = document.getElementById('myPassword').value;
        const database = document.getElementById('myDatabase').value;

        return `${user}:${password}@tcp(${host}:${port})/${database}`;

    } else if (type === 'mssql') {
        const server = document.getElementById('msServer').value || 'localhost';
        const port = document.getElementById('msPort').value || '1433';
        const user = document.getElementById('msUser').value;
        const password = document.getElementById('msPassword').value;
        const database = document.getElementById('msDatabase').value;

        return `sqlserver://${user}:${password}@${server}:${port}?database=${database}`;

    } else if (type === 'sqlite') {
        const file = document.getElementById('sqliteFile').value;
        return file;
    }

    return '';
}

// Get selected source type from radio buttons
function getSelectedSourceType() {
    const radios = document.getElementsByName('sourceType');
    for (const radio of radios) {
        if (radio.checked) {
            return radio.value;
        }
    }
    return '';
}

async function testConnection() {
    const type = getSelectedSourceType();

    if (!type) {
        showNotification('Please select a database type', 'error');
        return;
    }

    // Generate DSN from individual fields
    const dsn = generateDSN();

    if (!dsn) {
        showNotification('Please fill in all required connection fields', 'error');
        return;
    }

    const resultEl = document.getElementById('testResult');
    resultEl.style.display = 'block';
    resultEl.innerHTML = '<p>🔄 Testing connection...</p>';

    if (!wailsReady || !window.go) {
        resultEl.innerHTML = '<p style="color: orange;">⚠️ Wails not ready, connection test skipped</p>';
        return;
    }

    try {
        const source = { name: 'test', type: type, dsn: dsn };
        const result = await window.go.main.App.TestSource(source);

        if (result.success) {
            let html = `
                <div style="padding: 10px; background: #d4edda; border: 1px solid #c3e6cb; border-radius: 3px;">
                    <p style="color: #155724; margin: 0;"><strong>✅ Connection Successful!</strong></p>
                    <p style="color: #155724; margin: 5px 0 0 0;"><small>Duration: ${result.duration}ms | Tables: ${result.tables ? result.tables.length : 0}</small></p>
                </div>
            `;

            // Show table selection if tables are available
            if (result.tables && result.tables.length > 0) {
                html += `
                    <div style="margin-top: 10px; padding: 10px; background: #f8f9fa; border: 1px solid #dee2e6; border-radius: 3px;">
                        <label style="display: block; margin-bottom: 5px; font-weight: 600;">📋 Select Table/View:</label>
                        <select id="tableSelector" onchange="selectTable()" style="width: 100%; padding: 6px; border: 1px solid #ced4da; border-radius: 3px;">
                            <option value="">-- Select a table --</option>
                `;

                result.tables.forEach(table => {
                    html += `<option value="${table}">${table}</option>`;
                });

                html += `
                        </select>
                        <p style="margin: 5px 0 0 0; font-size: 10px; color: #6c757d;">💡 Select table to use as data source</p>
                    </div>
                `;
            }

            resultEl.innerHTML = html;
        } else {
            resultEl.innerHTML = `
                <div style="padding: 10px; background: #f8d7da; border: 1px solid #f5c6cb; border-radius: 3px;">
                    <p style="color: #721c24; margin: 0;"><strong>❌ Connection Failed</strong></p>
                    <p style="color: #721c24; margin: 5px 0 0 0;"><small>${result.message}</small></p>
                </div>
            `;
        }
    } catch (err) {
        console.error('Test connection error:', err);
        resultEl.innerHTML = `
            <div style="padding: 10px; background: #f8d7da; border: 1px solid #f5c6cb; border-radius: 3px;">
                <p style="color: #721c24; margin: 0;"><strong>❌ Error</strong></p>
                <p style="color: #721c24; margin: 5px 0 0 0;"><small>${err}</small></p>
            </div>
        `;
    }
}

// Store selected table name
let selectedTableName = '';

function selectTable() {
    const selector = document.getElementById('tableSelector');
    if (!selector) return;

    selectedTableName = selector.value;
    if (selectedTableName) {
        // Auto-fill source name if empty
        const sourceNameField = document.getElementById('sourceName');
        if (sourceNameField && !sourceNameField.value.trim()) {
            sourceNameField.value = selectedTableName;
        }
        showNotification(`Table selected: ${selectedTableName}`, 'success');
    }
}

async function saveSourceForm() {
    const name = document.getElementById('sourceName').value.trim();
    const type = getSelectedSourceType();

    if (!name) {
        showNotification('Source name is required', 'error');
        return;
    }

    if (!type) {
        showNotification('Please select a database type', 'error');
        return;
    }

    // Check for duplicate source names (except when editing)
    if (editingSourceIndex === -1) {
        const existingSource = sources.find(s => s.name === name);
        if (existingSource) {
            showNotification(`❌ Source '${name}' already exists! Choose a different name.`, 'error');
            return;
        }
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
        // Generate DSN from individual fields
        source.dsn = generateDSN();
        source.tableName = selectedTableName;

        if (!source.dsn) {
            showNotification('Please fill in all required connection fields', 'error');
            return;
        }

        if (!source.tableName) {
            showNotification('Please test connection and select a table', 'error');
            return;
        }
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

    // Select the radio button for this type
    const radios = document.getElementsByName('sourceType');
    for (const radio of radios) {
        if (radio.value === src.type) {
            radio.checked = true;
            break;
        }
    }

    // Show appropriate fields
    onSourceTypeChange(src.type);

    if (src.type === 'mock' && src.mockData) {
        document.getElementById('mockDataJson').value = JSON.stringify(src.mockData, null, 2);
    } else {
        // Restore selected table if available
        selectedTableName = src.tableName || '';

        // For now, show a warning that editing existing sources is limited
        showNotification('Note: Editing existing sources - please re-enter connection details and re-test connection', 'info');
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

        // Render table with horizontal scroll support
        let html = '<table style="border-collapse: collapse; font-size: 12px; min-width: 100%;">';
        html += '<thead><tr>';
        result.columns.forEach(col => {
            html += `<th style="border: 1px solid #ddd; padding: 5px; background: #f0f0f0; white-space: nowrap;">${col}</th>`;
        });
        html += '</tr></thead><tbody>';

        result.rows.forEach(row => {
            html += '<tr>';
            row.forEach(cell => {
                html += `<td style="border: 1px solid #ddd; padding: 5px; white-space: nowrap;">${cell !== null ? cell : '<i>null</i>'}</td>`;
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
async function saveStep7() {}

// ========== FILE PICKERS ==========

// Browse for SQLite database file
async function browseDatabaseFile() {
    if (!wailsReady || !window.go) {
        showNotification('File picker not available (Wails not ready)', 'error');
        return;
    }

    try {
        const path = await window.go.main.App.SelectDatabaseFile();
        if (path) {
            document.getElementById('sqliteFile').value = path;
            showNotification('File selected: ' + path, 'info');
        }
    } catch (err) {
        console.error('File picker error:', err);
        showNotification('Failed to open file picker: ' + err, 'error');
    }
}

// Load Mock JSON from file
async function loadJSONFile() {
    if (!wailsReady || !window.go) {
        showNotification('File picker not available (Wails not ready)', 'error');
        return;
    }

    try {
        const path = await window.go.main.App.SelectJSONFile();
        if (path) {
            // Read file content using Wails runtime
            // For now, just show the path - user can manually load
            showNotification('Selected file: ' + path + ' (TODO: auto-load content)', 'info');
            
            // TODO: Add backend method to read file content
            // const content = await window.go.main.App.ReadJSONFile(path);
            // document.getElementById('mockDataJson').value = content;
        }
    } catch (err) {
        console.error('File picker error:', err);
        showNotification('Failed to open file picker: ' + err, 'error');
    }
}
