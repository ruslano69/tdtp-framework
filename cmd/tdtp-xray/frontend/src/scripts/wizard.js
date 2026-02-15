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

    // Show/hide Mock (JSON) source option based on mode
    const mockOption = document.getElementById('mockSourceOption');
    const tdtpOption = document.getElementById('tdtpSourceOption');
    if (mockOption) {
        mockOption.style.display = mode === 'mock' ? 'inline-block' : 'none';
    }
    if (tdtpOption) {
        tdtpOption.style.display = mode === 'production' ? 'inline-block' : 'none';
    }

    // Show notification
    showNotification(
        mode === 'mock'
            ? '🧪 Mock Mode: You can experiment freely. Mock sources (JSON) available for testing.'
            : '🏭 Production Mode: Real data sources only (DB, TDTP XML).',
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
                                <label id="tdtpSourceOption">
                                    <input type="radio" name="sourceType" value="tdtp" onchange="onSourceTypeChange('tdtp')">
                                    <span>TDTP (XML)</span>
                                </label>
                                <label id="mockSourceOption" style="display: none;">
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

                        <!-- TDTP Fields -->
                        <div id="tdtpFields" class="db-connection-fields" style="display: none;">
                            <div class="form-group">
                                <label for="tdtpFile">TDTP XML File *</label>
                                <div style="display: flex; gap: 5px; flex: 1;">
                                    <input type="text" id="tdtpFile" placeholder="C:\\path\\to\\data.xml" style="flex: 1;">
                                    <button class="btn btn-secondary" onclick="browseTDTPFile()" style="padding: 6px 15px;">Browse...</button>
                                </div>
                            </div>
                            <p style="margin: 5px 0 0 0; font-size: 10px; color: #6c757d;">
                                💡 TDTP XML format - exported data from another pipeline
                            </p>
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
    lastTestedDSN = ''; // Reset test status

    // Uncheck all radio buttons
    const radios = document.getElementsByName('sourceType');
    radios.forEach(r => r.checked = false);

    // Hide all field groups
    document.getElementById('postgresFields').style.display = 'none';
    document.getElementById('mysqlFields').style.display = 'none';
    document.getElementById('mssqlFields').style.display = 'none';
    document.getElementById('sqliteFields').style.display = 'none';
    document.getElementById('tdtpFields').style.display = 'none';
    document.getElementById('connectionTestPanel').style.display = 'none';
    document.getElementById('mockFields').style.display = 'none';
    document.getElementById('testResult').style.display = 'none';
}

function onSourceTypeChange(type) {
    // Reset test status when changing source type
    lastTestedDSN = '';
    selectedTableName = '';
    document.getElementById('testResult').style.display = 'none';

    // Hide all field groups
    document.getElementById('postgresFields').style.display = 'none';
    document.getElementById('mysqlFields').style.display = 'none';
    document.getElementById('mssqlFields').style.display = 'none';
    document.getElementById('sqliteFields').style.display = 'none';
    document.getElementById('tdtpFields').style.display = 'none';
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
    } else if (type === 'tdtp') {
        document.getElementById('tdtpFields').style.display = 'block';
        document.getElementById('connectionTestPanel').style.display = 'block'; // TDTP needs testing too!
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
    } else if (type === 'tdtp') {
        const file = document.getElementById('tdtpFile').value;
        return file; // TDTP file path is used as DSN
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
            // Mark this DSN as successfully tested
            lastTestedDSN = dsn;

            let html = `
                <div style="padding: 10px; background: #d4edda; border: 1px solid #c3e6cb; border-radius: 3px;">
                    <p style="color: #155724; margin: 0;"><strong>✅ Connection Successful!</strong></p>
                    <p style="color: #155724; margin: 5px 0 0 0;"><small>${result.message || 'Test passed'}</small></p>
                </div>
            `;

            // For TDTP sources, auto-select the table and mark as tested
            if (type === 'tdtp' && result.tables && result.tables.length > 0) {
                selectedTableName = result.tables[0]; // TDTP has one table per file
                html += `
                    <div style="margin-top: 10px; padding: 10px; background: #f8f9fa; border: 1px solid #dee2e6; border-radius: 3px;">
                        <p style="color: #155724; margin: 0;"><strong>📋 Table: ${selectedTableName}</strong></p>
                        <p style="margin: 5px 0 0 0; font-size: 11px; color: #6c757d;">Ready to use as data source</p>
                    </div>
                `;
            }
            // Show table selection for database sources
            else if (result.tables && result.tables.length > 0) {
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

// Store DSN of last successful test to mark source as tested
let lastTestedDSN = '';

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
        tested: false, // Will be updated below based on test status
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
    } else if (type === 'tdtp') {
        // TDTP XML file source
        source.dsn = generateDSN();

        if (!source.dsn) {
            showNotification('Please select a TDTP XML file', 'error');
            return;
        }

        // TDTP files don't need tableName - they contain complete data sets
        // Auto-select table name from test result
        source.tableName = selectedTableName;

        // Mark as tested if this DSN was successfully tested
        source.tested = (source.dsn === lastTestedDSN);
    } else {
        // Database sources (postgres, mysql, mssql, sqlite)
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

        // Mark as tested if this DSN was successfully tested
        source.tested = (source.dsn === lastTestedDSN);
    }

    // Save to backend first (before updating local array)
    if (wailsReady && window.go) {
        try {
            if (editingSourceIndex >= 0) {
                // Update existing source in backend
                const oldName = sources[editingSourceIndex].name;
                await window.go.main.App.UpdateSource(oldName, source);
            } else {
                // Add new source to backend
                await window.go.main.App.AddSource(source);
            }
        } catch (err) {
            console.error('Failed to sync source to backend:', err);
            showNotification(`Warning: Source saved locally but backend sync failed: ${err}`, 'warning');
        }
    }

    if (editingSourceIndex >= 0) {
        // Update existing in local array
        sources[editingSourceIndex] = source;
    } else {
        // Add new to local array
        sources.push(source);
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

async function removeSource(index) {
    const src = sources[index];
    if (confirm(`Remove source '${src.name}'?`)) {
        // Remove from backend first
        if (wailsReady && window.go) {
            try {
                await window.go.main.App.RemoveSource(src.name);
            } catch (err) {
                console.error('Failed to remove source from backend:', err);
                showNotification(`Warning: Source removed locally but backend sync failed: ${err}`, 'warning');
            }
        }

        // Remove from local array
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
    return `
<div class="step-content active">
    <div class="panel">
        <div class="panel-header">Visual Query Designer</div>
        <p style="color: #666; margin-bottom: 15px;">
            Drag tables onto the canvas and connect fields to create JOINs
        </p>

        <div style="display: flex; gap: 15px; height: 600px;">
            <!-- Tables Palette -->
            <div style="width: 250px; border-right: 1px solid #ddd; padding-right: 15px; overflow-y: auto;">
                <h4 style="margin-top: 0;">Available Tables</h4>
                <div id="tablesSourceList"></div>
            </div>

            <!-- Canvas Area -->
            <div style="flex: 1; position: relative; background: #f9f9f9; border: 1px solid #ddd; border-radius: 3px; overflow: hidden;">
                <svg id="canvasSVG" width="100%" height="100%" style="position: absolute; top: 0; left: 0; z-index: 1;">
                    <!-- JOIN lines will be drawn here -->
                </svg>
                <div id="canvasArea" style="position: relative; width: 100%; height: 100%; z-index: 2;">
                    <!-- Table cards will be placed here -->
                </div>
            </div>

            <!-- Properties Panel -->
            <div style="width: 250px; border-left: 1px solid #ddd; padding-left: 15px; overflow-y: auto;">
                <h4 style="margin-top: 0;">Properties</h4>
                <div id="propertiesPanel">
                    <p style="color: #999; font-size: 13px;">Select a table or join to edit properties</p>
                </div>
            </div>
        </div>

        <div style="margin-top: 15px; display: flex; gap: 10px;">
            <button class="btn" onclick="clearCanvas()">Clear Canvas</button>
            <button class="btn" onclick="autoLayout()">Auto Layout</button>
            <button class="btn" onclick="previewSQL()">Preview SQL</button>
        </div>
    </div>

    <!-- SQL Preview Modal -->
    <div id="sqlPreviewModal" style="display: none; position: fixed; top: 0; left: 0; right: 0; bottom: 0; background: rgba(0,0,0,0.5); z-index: 1000; justify-content: center; align-items: center;">
        <div style="background: white; padding: 20px; border-radius: 5px; max-width: 800px; width: 90%; max-height: 80%; overflow-y: auto;">
            <h3>Generated SQL Preview</h3>
            <pre id="sqlPreviewContent" style="background: #f5f5f5; padding: 15px; border-radius: 3px; overflow-x: auto;"></pre>
            <button class="btn" onclick="closeSQLPreview()">Close</button>
        </div>
    </div>
</div>
    `;
}

let canvasDesign = {
    tables: [],
    joins: []
};

let selectedTableId = null;
let selectedJoinId = null;
let joinStartField = null; // For creating joins

function loadStep3Data() {
    // Load sources from Step 2 (global sources variable, not appState!)
    const sourceListEl = document.getElementById('tablesSourceList');

    if (sources.length === 0) {
        sourceListEl.innerHTML = '<p style="color: #999; font-size: 13px;">No sources defined in Step 2</p>';
        return;
    }

    let html = '';
    sources.forEach(src => {
        html += `
            <div style="border: 1px solid #ddd; padding: 10px; margin-bottom: 8px; background: white; border-radius: 3px; cursor: pointer;"
                 draggable="true"
                 ondragstart="handleTableDragStart(event, '${src.name}')"
                 onclick="addTableToCanvas('${src.name}')">
                <strong>${src.name}</strong>
                <div style="font-size: 11px; color: #666;">${src.type.toUpperCase()}</div>
            </div>
        `;
    });
    sourceListEl.innerHTML = html;

    // Load existing canvas design from backend
    window.GetCanvasDesign().then(design => {
        if (design && design.tables) {
            canvasDesign = design;
            renderCanvas();
        }
    }).catch(err => {
        console.log('No existing canvas design');
    });
}

async function saveStep3() {
    // Save canvas design to backend
    try {
        await window.SaveCanvasDesign(canvasDesign);
        showNotification('Canvas design saved successfully', 'success');
        return true;
    } catch (err) {
        console.error('Failed to save canvas design:', err);
        showNotification('Failed to save canvas design: ' + err, 'error');
        return false;
    }
}

// Canvas helper functions
function handleTableDragStart(event, sourceName) {
    event.dataTransfer.setData('sourceName', sourceName);
}

async function addTableToCanvas(sourceName) {
    // Check if table already exists
    if (canvasDesign.tables.find(t => t.sourceName === sourceName)) {
        showNotification('Table already on canvas', 'warning');
        return;
    }

    // Get table schema from backend
    try {
        if (!wailsReady || !window.go) {
            showNotification('Backend not ready', 'error');
            return;
        }

        const tables = await window.go.main.App.GetTablesBySourceName(sourceName);
        if (!tables || tables.length === 0) {
            showNotification('Failed to load table schema', 'error');
            return;
        }

        const tableInfo = tables[0];
        const fields = tableInfo.columns.map(col => ({
            name: col.name,
            type: col.type,
            visible: true,
            condition: null
        }));

        // Calculate position (offset each new table)
        const tableCount = canvasDesign.tables.length;
        const x = 50 + (tableCount * 30) % 400;
        const y = 50 + (tableCount * 30) % 300;

        const newTable = {
            sourceName: sourceName,
            alias: sourceName,
            x: x,
            y: y,
            fields: fields
        };

        canvasDesign.tables.push(newTable);
        renderCanvas();
    } catch (err) {
        console.error('Failed to add table:', err);
        showNotification('Failed to add table: ' + err, 'error');
    }
}

function renderCanvas() {
    const canvasArea = document.getElementById('canvasArea');
    const svg = document.getElementById('canvasSVG');

    // Clear existing
    canvasArea.innerHTML = '';
    svg.innerHTML = '';

    // Render tables
    canvasDesign.tables.forEach((table, index) => {
        const tableCard = createTableCard(table, index);
        canvasArea.appendChild(tableCard);
    });

    // Render joins
    renderJoins();
}

function createTableCard(table, index) {
    const card = document.createElement('div');
    card.id = `table-${index}`;
    card.className = 'table-card';
    card.style.cssText = `
        position: absolute;
        left: ${table.x}px;
        top: ${table.y}px;
        width: 200px;
        background: white;
        border: 2px solid #0066cc;
        border-radius: 5px;
        box-shadow: 0 2px 8px rgba(0,0,0,0.15);
        cursor: move;
        z-index: 10;
    `;

    // Header
    const header = document.createElement('div');
    header.style.cssText = `
        background: #0066cc;
        color: white;
        padding: 8px 10px;
        font-weight: 600;
        font-size: 13px;
        border-radius: 3px 3px 0 0;
        display: flex;
        justify-content: space-between;
        align-items: center;
    `;
    header.innerHTML = `
        <span>${table.alias || table.sourceName}</span>
        <button onclick="removeTableFromCanvas(${index})" style="background: none; border: none; color: white; cursor: pointer; font-size: 16px; padding: 0; line-height: 1;">&times;</button>
    `;
    card.appendChild(header);

    // Fields
    const fieldsContainer = document.createElement('div');
    fieldsContainer.style.cssText = 'padding: 5px; max-height: 300px; overflow-y: auto;';

    table.fields.forEach((field, fieldIndex) => {
        const fieldEl = document.createElement('div');
        fieldEl.className = 'table-field';
        fieldEl.style.cssText = `
            padding: 6px 8px;
            font-size: 12px;
            border-bottom: 1px solid #f0f0f0;
            cursor: pointer;
            display: flex;
            justify-content: space-between;
            align-items: center;
        `;
        fieldEl.innerHTML = `
            <span>
                <input type="checkbox"
                       ${field.visible ? 'checked' : ''}
                       onchange="toggleFieldVisibility(${index}, ${fieldIndex})"
                       style="margin-right: 5px;">
                <strong>${field.name}</strong>
                <br><small style="color: #999; margin-left: 20px;">${field.type}</small>
            </span>
            <button onclick="startJoin(${index}, ${fieldIndex})"
                    style="background: #f0f0f0; border: none; border-radius: 3px; padding: 2px 6px; cursor: pointer; font-size: 11px;"
                    title="Create JOIN">⚡</button>
        `;
        fieldsContainer.appendChild(fieldEl);
    });

    card.appendChild(fieldsContainer);

    // Make draggable
    makeDraggable(card, index);

    return card;
}

function makeDraggable(element, tableIndex) {
    let isDragging = false;
    let startX, startY, initialX, initialY;

    element.addEventListener('mousedown', function(e) {
        if (e.target.tagName === 'BUTTON' || e.target.tagName === 'INPUT') return;

        isDragging = true;
        startX = e.clientX;
        startY = e.clientY;
        initialX = canvasDesign.tables[tableIndex].x;
        initialY = canvasDesign.tables[tableIndex].y;

        element.style.zIndex = '100';
        selectedTableId = tableIndex;

        e.preventDefault();
    });

    document.addEventListener('mousemove', function(e) {
        if (!isDragging) return;

        const dx = e.clientX - startX;
        const dy = e.clientY - startY;

        canvasDesign.tables[tableIndex].x = initialX + dx;
        canvasDesign.tables[tableIndex].y = initialY + dy;

        element.style.left = canvasDesign.tables[tableIndex].x + 'px';
        element.style.top = canvasDesign.tables[tableIndex].y + 'px';

        renderJoins();
    });

    document.addEventListener('mouseup', function() {
        if (isDragging) {
            isDragging = false;
            element.style.zIndex = '10';
        }
    });
}

function toggleFieldVisibility(tableIndex, fieldIndex) {
    canvasDesign.tables[tableIndex].fields[fieldIndex].visible =
        !canvasDesign.tables[tableIndex].fields[fieldIndex].visible;
}

function removeTableFromCanvas(tableIndex) {
    if (!confirm(`Remove table "${canvasDesign.tables[tableIndex].sourceName}" from canvas?`)) {
        return;
    }

    const tableName = canvasDesign.tables[tableIndex].sourceName;

    // Remove associated joins
    canvasDesign.joins = canvasDesign.joins.filter(join =>
        join.leftTable !== tableName && join.rightTable !== tableName
    );

    // Remove table
    canvasDesign.tables.splice(tableIndex, 1);

    renderCanvas();
}

function startJoin(tableIndex, fieldIndex) {
    const table = canvasDesign.tables[tableIndex];
    const field = table.fields[fieldIndex];

    if (!joinStartField) {
        // First field selected
        joinStartField = {
            table: table.sourceName,
            field: field.name,
            tableIndex: tableIndex,
            fieldIndex: fieldIndex
        };
        showNotification(`Select target field to join with ${table.sourceName}.${field.name}`, 'info');
    } else {
        // Second field selected - create join
        if (joinStartField.table === table.sourceName) {
            showNotification('Cannot join table to itself', 'warning');
            joinStartField = null;
            return;
        }

        // Check if join already exists
        const existingJoin = canvasDesign.joins.find(j =>
            (j.leftTable === joinStartField.table && j.leftField === joinStartField.field &&
             j.rightTable === table.sourceName && j.rightField === field.name) ||
            (j.leftTable === table.sourceName && j.leftField === field.name &&
             j.rightTable === joinStartField.table && j.rightField === joinStartField.field)
        );

        if (existingJoin) {
            showNotification('Join already exists', 'warning');
            joinStartField = null;
            return;
        }

        const newJoin = {
            leftTable: joinStartField.table,
            leftField: joinStartField.field,
            rightTable: table.sourceName,
            rightField: field.name,
            joinType: 'INNER'
        };

        canvasDesign.joins.push(newJoin);
        joinStartField = null;

        renderCanvas();
        showNotification('Join created successfully', 'success');
    }
}

function renderJoins() {
    const svg = document.getElementById('canvasSVG');
    svg.innerHTML = '';

    canvasDesign.joins.forEach((join, joinIndex) => {
        const leftTableIdx = canvasDesign.tables.findIndex(t => t.sourceName === join.leftTable);
        const rightTableIdx = canvasDesign.tables.findIndex(t => t.sourceName === join.rightTable);

        if (leftTableIdx === -1 || rightTableIdx === -1) return;

        const leftTable = canvasDesign.tables[leftTableIdx];
        const rightTable = canvasDesign.tables[rightTableIdx];

        // Calculate line coordinates (simplified - center of tables)
        const x1 = leftTable.x + 100;
        const y1 = leftTable.y + 50;
        const x2 = rightTable.x + 100;
        const y2 = rightTable.y + 50;

        // Create line
        const line = document.createElementNS('http://www.w3.org/2000/svg', 'line');
        line.setAttribute('x1', x1);
        line.setAttribute('y1', y1);
        line.setAttribute('x2', x2);
        line.setAttribute('y2', y2);
        line.setAttribute('stroke', '#0066cc');
        line.setAttribute('stroke-width', '2');
        line.setAttribute('marker-end', 'url(#arrowhead)');
        line.style.cursor = 'pointer';
        line.onclick = () => selectJoin(joinIndex);

        svg.appendChild(line);

        // Add label
        const midX = (x1 + x2) / 2;
        const midY = (y1 + y2) / 2;

        const text = document.createElementNS('http://www.w3.org/2000/svg', 'text');
        text.setAttribute('x', midX);
        text.setAttribute('y', midY - 5);
        text.setAttribute('fill', '#0066cc');
        text.setAttribute('font-size', '11');
        text.setAttribute('font-weight', 'bold');
        text.textContent = join.joinType;
        text.style.cursor = 'pointer';
        text.onclick = () => selectJoin(joinIndex);

        svg.appendChild(text);
    });

    // Define arrowhead marker
    if (canvasDesign.joins.length > 0 && !svg.querySelector('#arrowhead')) {
        const defs = document.createElementNS('http://www.w3.org/2000/svg', 'defs');
        const marker = document.createElementNS('http://www.w3.org/2000/svg', 'marker');
        marker.setAttribute('id', 'arrowhead');
        marker.setAttribute('markerWidth', '10');
        marker.setAttribute('markerHeight', '10');
        marker.setAttribute('refX', '9');
        marker.setAttribute('refY', '3');
        marker.setAttribute('orient', 'auto');

        const polygon = document.createElementNS('http://www.w3.org/2000/svg', 'polygon');
        polygon.setAttribute('points', '0 0, 10 3, 0 6');
        polygon.setAttribute('fill', '#0066cc');

        marker.appendChild(polygon);
        defs.appendChild(marker);
        svg.insertBefore(defs, svg.firstChild);
    }
}

function selectJoin(joinIndex) {
    selectedJoinId = joinIndex;
    const join = canvasDesign.joins[joinIndex];

    const propertiesPanel = document.getElementById('propertiesPanel');
    propertiesPanel.innerHTML = `
        <h4 style="margin-top: 0;">Join Properties</h4>
        <div style="margin-bottom: 10px;">
            <strong>${join.leftTable}.${join.leftField}</strong>
            <br>↓<br>
            <strong>${join.rightTable}.${join.rightField}</strong>
        </div>

        <label style="display: block; margin-bottom: 10px;">
            Join Type:
            <select id="joinTypeSelect" onchange="updateJoinType(${joinIndex})" style="width: 100%; margin-top: 5px; padding: 5px;">
                <option value="INNER" ${join.joinType === 'INNER' ? 'selected' : ''}>INNER JOIN</option>
                <option value="LEFT" ${join.joinType === 'LEFT' ? 'selected' : ''}>LEFT JOIN</option>
                <option value="RIGHT" ${join.joinType === 'RIGHT' ? 'selected' : ''}>RIGHT JOIN</option>
            </select>
        </label>

        <button class="btn" onclick="removeJoin(${joinIndex})" style="width: 100%; background: #dc3545; color: white;">Remove Join</button>
    `;
}

function updateJoinType(joinIndex) {
    const select = document.getElementById('joinTypeSelect');
    canvasDesign.joins[joinIndex].joinType = select.value;
    renderJoins();
}

function removeJoin(joinIndex) {
    canvasDesign.joins.splice(joinIndex, 1);
    document.getElementById('propertiesPanel').innerHTML = '<p style="color: #999; font-size: 13px;">Select a table or join to edit properties</p>';
    renderCanvas();
}

function clearCanvas() {
    if (!confirm('Clear entire canvas? This will remove all tables and joins.')) {
        return;
    }

    canvasDesign.tables = [];
    canvasDesign.joins = [];
    renderCanvas();
}

function autoLayout() {
    // Simple grid layout
    const cols = Math.ceil(Math.sqrt(canvasDesign.tables.length));
    canvasDesign.tables.forEach((table, index) => {
        const row = Math.floor(index / cols);
        const col = index % cols;
        table.x = 50 + col * 250;
        table.y = 50 + row * 200;
    });

    renderCanvas();
}

async function previewSQL() {
    try {
        if (!wailsReady || !window.go) {
            showNotification('Backend not ready', 'error');
            return;
        }

        const result = await window.go.main.App.GenerateSQL(canvasDesign);

        if (result.error) {
            showNotification('Failed to generate SQL: ' + result.error, 'error');
            return;
        }

        document.getElementById('sqlPreviewContent').textContent = result.sql;
        document.getElementById('sqlPreviewModal').style.display = 'flex';
    } catch (err) {
        console.error('SQL preview error:', err);
        showNotification('Failed to generate SQL: ' + err, 'error');
    }
}

function closeSQLPreview() {
    document.getElementById('sqlPreviewModal').style.display = 'none';
}

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

// Browse for TDTP XML file
async function browseTDTPFile() {
    if (!wailsReady || !window.go) {
        showNotification('File picker not available (Wails not ready)', 'error');
        return;
    }

    try {
        const path = await window.go.main.App.SelectTDTPFile();
        if (path) {
            document.getElementById('tdtpFile').value = path;
            showNotification('TDTP file selected: ' + path, 'info');
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
