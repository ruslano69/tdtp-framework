// TDTP X-Ray - Wizard Navigation

let currentStep = 1;
const totalSteps = 7;
let appMode = 'production'; // production or mock
let completedSteps = new Set(); // Track completed steps

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

// Navigate to a specific step (called when clicking on step in nav)
function goToStep(targetStep) {
    // Only allow navigation to current step or earlier completed steps
    if (targetStep > currentStep && !completedSteps.has(targetStep)) {
        showNotification('Please complete previous steps first', 'warning');
        return;
    }

    // Save current step before navigating
    saveCurrentStep().then(() => {
        loadStep(targetStep);
    });
}

// Load specific wizard step
function loadStep(step) {
    currentStep = step;

    // Mark previous steps as completed
    if (step > 1) {
        for (let i = 1; i < step; i++) {
            completedSteps.add(i);
        }
    }

    // Update step navigation highlights
    document.querySelectorAll('.wizard-step').forEach(el => {
        const stepNum = parseInt(el.dataset.step);
        el.classList.remove('active');

        // Mark as completed if it's in completedSteps
        if (completedSteps.has(stepNum)) {
            el.classList.add('completed');
        } else {
            el.classList.remove('completed');
        }

        // Mark current step as active
        if (stepNum === step) {
            el.classList.add('active');
        }

        // Add pointer cursor for clickable steps
        if (stepNum <= currentStep || completedSteps.has(stepNum)) {
            el.style.cursor = 'pointer';
        } else {
            el.style.cursor = 'not-allowed';
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
            // Wait for DOM to be ready before loading data
            setTimeout(() => loadStep3Data(), 50);
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
        btnNext.textContent = 'Save & Exit';
        btnNext.onclick = saveAndExit;
        btnNext.classList.add('btn-success');
    } else {
        btnNext.textContent = 'Next →';
        btnNext.onclick = nextStep;
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

// Show notification message (auto-clears after 5s)
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

// Set persistent connected status in footer (stays until clearConnectionStatus is called)
function setStatusConnected(info) {
    const status = document.getElementById('footerStatus');
    status.textContent = '● ' + info;
    status.className = 'footer-status status-connected';
}

// Clear connection status from footer (only if it shows connected state)
function clearConnectionStatus() {
    const status = document.getElementById('footerStatus');
    if (status && status.classList.contains('status-connected')) {
        status.textContent = '';
        status.className = 'footer-status';
    }
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
                <div style="margin-bottom: 15px; padding: 15px; background: #f8f9fa; border: 1px solid #dee2e6; border-radius: 3px;">
                    <h3 style="margin: 0 0 10px 0; font-size: 14px;">📁 YAML File</h3>
                    <div style="display: flex; gap: 10px;">
                        <button class="btn btn-secondary" onclick="loadConfigurationFile()" style="flex: 1;">
                            📁 Load from File...
                        </button>
                        <button class="btn btn-secondary" onclick="saveConfigurationFile()" style="flex: 1;">
                            💾 Save to File...
                        </button>
                    </div>
                    <p style="margin: 5px 0 0 0; font-size: 10px; color: #6c757d;">
                        Load existing TDTP pipeline YAML or save current configuration
                    </p>
                </div>

                <!-- Repository -->
                <div style="margin-bottom: 20px; padding: 15px; background: #eef4ff; border: 1px solid #b8d0f0; border-radius: 3px;">
                    <h3 style="margin: 0 0 10px 0; font-size: 14px;">📚 Repository (configs.db)</h3>
                    <div style="display: flex; gap: 10px;">
                        <button class="btn btn-primary" onclick="openRepositoryModal()" style="flex: 1;">
                            📚 Open Repository...
                        </button>
                    </div>
                    <p style="margin: 5px 0 0 0; font-size: 10px; color: #5a7fa0;">
                        Load configs with full canvas state (field visibility, filters, JOINs)
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
            await refreshAllSteps();

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

// Refresh all wizard steps after loading configuration
async function refreshAllSteps() {
    console.log('Refreshing all wizard steps...');
    try {
        // Load each step's data
        await loadStep1Data();
        await loadStep2Data();
        await loadStep3Data();
        await loadStep4Data();
        await loadStep5Data();
        await loadStep6Data();
        await loadStep7Data();

        // If we're currently on one of these steps, reload the content
        if (currentStep >= 1 && currentStep <= 7) {
            loadStep(currentStep);
        }

        console.log('All steps refreshed successfully');
    } catch (err) {
        console.error('Failed to refresh steps:', err);
    }
}

// Save configuration to YAML file
async function saveConfigurationFile() {
    if (!wailsReady || !window.go) {
        showNotification('File save not available (Wails not ready)', 'error');
        return;
    }

    const btn = event && event.currentTarget;
    const originalText = btn ? btn.textContent : null;
    if (btn) { btn.disabled = true; btn.textContent = '⏳ Opening dialog...'; }

    await saveCurrentStep();

    try {
        const result = await window.go.main.App.SaveConfigurationFile();
        if (result.success) {
            showNotification(`Saved: ${result.filename}  →  ${result.dir}`, 'success');
        } else if (result.error && !result.error.includes('cancelled')) {
            showNotification(`Save failed: ${result.error}`, 'error');
        }
    } catch (err) {
        console.error('Save configuration error:', err);
        showNotification('Failed to save configuration: ' + err, 'error');
    } finally {
        if (btn) { btn.disabled = false; btn.textContent = originalText; }
    }
}

// Save config to file and quit the application
async function saveAndExit() {
    if (!wailsReady || !window.go) {
        showNotification('Not available (Wails not ready)', 'error');
        return;
    }

    await saveCurrentStep();

    try {
        const result = await window.go.main.App.SaveConfigurationFile();
        if (result.success) {
            showNotification(`Saved: ${result.filename}  →  ${result.dir}`, 'success');
            await window.go.main.App.Quit();
        } else if (result.error && !result.error.includes('cancelled')) {
            showNotification(`Failed to save: ${result.error}`, 'error');
        }
        // пользователь нажал Cancel в диалоге - остаёмся молча
    } catch (err) {
        console.error('Save & Exit error:', err);
        showNotification('Failed to save configuration: ' + err, 'error');
    }
}

// ========== REPOSITORY ==========

async function saveToRepository() {
    if (!wailsReady || !window.go) {
        showNotification('Backend not ready', 'error');
        return;
    }

    // Ensure current step data is saved first
    await saveCurrentStep();

    const btn = event && event.currentTarget;
    const originalText = btn ? btn.textContent : null;
    if (btn) { btn.disabled = true; btn.textContent = '⏳ Saving...'; }

    try {
        const canvasJSON = JSON.stringify(canvasDesign || { tables: [], joins: [] });
        const result = await window.go.main.App.SaveToRepository(canvasJSON);
        if (result.success) {
            const action = result.updated ? 'Updated' : 'Saved';
            showNotification(`✅ ${action} in repository (ID: ${result.id})`, 'success');
        } else {
            showNotification(`❌ Repository save failed: ${result.error}`, 'error');
        }
    } catch (err) {
        showNotification('Repository save error: ' + err, 'error');
    } finally {
        if (btn) { btn.disabled = false; btn.textContent = originalText; }
    }
}

async function openRepositoryModal() {
    if (!wailsReady || !window.go) {
        showNotification('Backend not ready', 'error');
        return;
    }

    let entries = [];
    try {
        entries = await window.go.main.App.ListRepositoryConfigs();
    } catch (err) {
        showNotification('Failed to list repository: ' + err, 'error');
        return;
    }

    const modal = document.createElement('div');
    modal.id = 'repositoryModal';
    modal.style.cssText = `
        position: fixed; top: 0; left: 0; right: 0; bottom: 0;
        background: rgba(0,0,0,0.5); z-index: 3000;
        display: flex; justify-content: center; align-items: center;
    `;

    // Store entries for client-side filtering
    window._repoEntries = entries || [];

    const filterDefs = [
        ['fPG',     'usPg',     'PostgreSQL'],
        ['fSQLite', 'usSqlite', 'SQLite'],
        ['fMSSQL',  'usMssql',  'MSSQL'],
        ['fMySQL',  'usMysql',  'MySQL'],
        ['fRabbit', 'usRabbit', 'RabbitMQ'],
        ['fKafka',  'usKafka',  'Kafka'],
        ['fTDTP',   'usTdtp',   'TDTP'],
        ['fXLSX',   'usXlsx',   'XLSX'],
    ];

    function buildRepoRows(list) {
        if (!list || list.length === 0) {
            return `<tr><td colspan="5" style="text-align:center;color:#999;padding:20px;">
                No pipelines match the selected filters (or repository is empty).</td></tr>`;
        }
        return list.map(e => {
            const updated = e.updatedAt ? e.updatedAt.replace('T', ' ').substring(0, 16) : '';
            const desc = e.description ? `<br><small style="color:#888">${e.description}</small>` : '';
            const tagStyles = {
                usPg:     'background:#e8f4f8;color:#0066aa;border:1px solid #b0d0e8',
                usSqlite: 'background:#e8f4f8;color:#0066aa;border:1px solid #b0d0e8',
                usMssql:  'background:#e8f4f8;color:#0066aa;border:1px solid #b0d0e8',
                usMysql:  'background:#e8f4f8;color:#0066aa;border:1px solid #b0d0e8',
                usRabbit: 'background:#fff3e0;color:#c96a00;border:1px solid #f0c080',
                usKafka:  'background:#fff3e0;color:#c96a00;border:1px solid #f0c080',
                usTdtp:   'background:#eef4ee;color:#2a6a2a;border:1px solid #a0c8a0',
                usXlsx:   'background:#eef4ee;color:#2a6a2a;border:1px solid #a0c8a0',
            };
            const tagLabels = { usPg:'PostgreSQL', usSqlite:'SQLite', usMssql:'MSSQL', usMysql:'MySQL',
                                usRabbit:'RabbitMQ', usKafka:'Kafka', usTdtp:'TDTP', usXlsx:'XLSX' };
            const tags = Object.keys(tagLabels)
                .filter(k => e[k])
                .map(k => `<span style="${tagStyles[k]};padding:1px 5px;border-radius:3px;font-size:10px;margin-right:3px;">${tagLabels[k]}</span>`)
                .join('');
            return `
                <tr>
                    <td style="padding:8px 10px;border-bottom:1px solid #eee;">
                        <strong>${e.name}</strong>${desc}
                        <div style="margin-top:3px;">${tags}</div>
                    </td>
                    <td style="padding:8px 10px;border-bottom:1px solid #eee;color:#666;">${e.version || ''}</td>
                    <td style="padding:8px 10px;border-bottom:1px solid #eee;color:#888;font-size:11px;">${updated}</td>
                    <td style="padding:8px 10px;border-bottom:1px solid #eee;white-space:nowrap;">
                        <button class="btn btn-sm btn-primary" onclick="loadFromRepositoryEntry(${e.id})">Load</button>
                    </td>
                    <td style="padding:8px 10px;border-bottom:1px solid #eee;">
                        <button class="btn btn-sm" style="color:#dc3545;border-color:#dc3545;" onclick="deleteFromRepositoryEntry(${e.id})">Delete</button>
                    </td>
                </tr>
            `;
        }).join('');
    }
    window._buildRepoRows = buildRepoRows;

    const totalCount = entries ? entries.length : 0;

    modal.innerHTML = `
        <div style="background:white;border-radius:5px;min-width:700px;max-width:92%;max-height:85vh;display:flex;flex-direction:column;box-shadow:0 8px 32px rgba(0,0,0,0.2);">
            <div style="padding:15px 20px;border-bottom:1px solid #ddd;display:flex;justify-content:space-between;align-items:center;background:#0055aa;color:white;border-radius:5px 5px 0 0;">
                <h3 style="margin:0;">📚 Pipeline Repository</h3>
                <button onclick="closeRepositoryModal()" style="background:none;border:none;color:white;font-size:20px;cursor:pointer;line-height:1;">×</button>
            </div>
            <div style="padding:8px 15px;background:#f0f5ff;border-bottom:1px solid #ccd8f0;display:flex;gap:12px;align-items:center;flex-wrap:wrap;">
                <span style="font-size:12px;font-weight:600;color:#444;">Filter:</span>
                ${filterDefs.map(([id,,label]) =>
                    `<label style="font-size:12px;cursor:pointer;display:flex;gap:4px;align-items:center;">
                        <input type="checkbox" id="${id}" onchange="applyRepoFilter()"> ${label}
                    </label>`
                ).join('')}
                <select id="repoFilterLogic" onchange="applyRepoFilter()"
                        style="font-size:11px;padding:2px 5px;border:1px solid #b0c4de;border-radius:3px;background:#fff;cursor:pointer;"
                        title="How to combine selected filters">
                    <option value="AND">AND</option>
                    <option value="OR">OR</option>
                </select>
                <button class="btn btn-sm" onclick="applyRepoFilter(true)" style="margin-left:auto;font-size:11px;">Clear</button>
            </div>
            <div style="overflow-y:auto;flex:1;">
                <table style="width:100%;border-collapse:collapse;">
                    <thead>
                        <tr style="background:#f8f9fa;">
                            <th style="padding:8px 10px;text-align:left;border-bottom:2px solid #dee2e6;">Name / Technologies</th>
                            <th style="padding:8px 10px;text-align:left;border-bottom:2px solid #dee2e6;">Version</th>
                            <th style="padding:8px 10px;text-align:left;border-bottom:2px solid #dee2e6;">Last Updated</th>
                            <th colspan="2" style="padding:8px 10px;text-align:left;border-bottom:2px solid #dee2e6;">Actions</th>
                        </tr>
                    </thead>
                    <tbody id="repositoryTableBody">
                        ${buildRepoRows(entries)}
                    </tbody>
                </table>
            </div>
            <div style="padding:12px 20px;border-top:1px solid #ddd;display:flex;justify-content:space-between;align-items:center;background:#f8f9fa;border-radius:0 0 5px 5px;">
                <small style="color:#888;" id="repoCount">configs.db — ${totalCount} pipeline(s)</small>
                <button class="btn" onclick="closeRepositoryModal()">Close</button>
            </div>
        </div>
    `;

    document.body.appendChild(modal);
    modal.addEventListener('click', e => { if (e.target === modal) closeRepositoryModal(); });
}

function closeRepositoryModal() {
    const modal = document.getElementById('repositoryModal');
    if (modal) modal.remove();
}

function applyRepoFilter(clear) {
    const filterMap = [
        ['fPG',     'usPg'],
        ['fSQLite', 'usSqlite'],
        ['fMSSQL',  'usMssql'],
        ['fMySQL',  'usMysql'],
        ['fRabbit', 'usRabbit'],
        ['fKafka',  'usKafka'],
        ['fTDTP',   'usTdtp'],
        ['fXLSX',   'usXlsx'],
    ];
    if (clear) {
        filterMap.forEach(([id]) => {
            const el = document.getElementById(id);
            if (el) el.checked = false;
        });
    }
    const active = filterMap.filter(([id]) => {
        const el = document.getElementById(id);
        return el && el.checked;
    }).map(([,key]) => key);

    const logicEl = document.getElementById('repoFilterLogic');
    const logic = logicEl ? logicEl.value : 'AND';

    const all = window._repoEntries || [];
    const filtered = active.length === 0
        ? all
        : logic === 'OR'
            ? all.filter(e => active.some(k => e[k]))
            : all.filter(e => active.every(k => e[k]));

    const tbody = document.getElementById('repositoryTableBody');
    if (tbody && window._buildRepoRows) {
        tbody.innerHTML = window._buildRepoRows(filtered);
    }
    const countEl = document.getElementById('repoCount');
    if (countEl) {
        countEl.textContent = active.length === 0
            ? `configs.db — ${all.length} pipeline(s)`
            : `Showing ${filtered.length} of ${all.length} pipeline(s) [${logic}]`;
    }
}

async function loadFromRepositoryEntry(id) {
    if (!wailsReady || !window.go) return;

    try {
        const result = await window.go.main.App.LoadFromRepository(id);
        if (!result.success) {
            showNotification(`❌ Load failed: ${result.error}`, 'error');
            return;
        }

        // Restore canvas design directly from stored JSON — no SQL parsing needed!
        if (result.canvasJson && result.canvasJson !== '{}') {
            try {
                canvasDesign = JSON.parse(result.canvasJson);
            } catch (e) {
                canvasDesign = { tables: [], joins: [] };
            }
        } else {
            canvasDesign = { tables: [], joins: [] };
        }

        closeRepositoryModal();
        await refreshAllSteps();
        showNotification(
            `✅ Loaded "${result.config ? result.config.name : ''}" from repository`,
            'success'
        );
    } catch (err) {
        showNotification('Load from repository error: ' + err, 'error');
    }
}

async function deleteFromRepositoryEntry(id) {
    if (!confirm('Delete this pipeline from repository?')) return;
    try {
        await window.go.main.App.DeleteFromRepository(id);
        // Refresh the modal table
        closeRepositoryModal();
        await openRepositoryModal();
    } catch (err) {
        showNotification('Delete error: ' + err, 'error');
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
                                    <input type="text" id="msServer" value="localhost" placeholder="localhost" oninput="clearConnectionStatus()">
                                </div>
                                <div class="form-group" style="flex: 1;">
                                    <label for="msPort">Port *</label>
                                    <input type="number" id="msPort" value="1433" placeholder="1433" oninput="clearConnectionStatus()">
                                </div>
                            </div>
                            <div class="form-group">
                                <label for="msDatabase">Database *</label>
                                <input type="text" id="msDatabase" placeholder="testdb" oninput="clearConnectionStatus()">
                            </div>
                            <div class="form-group" style="margin-bottom: 8px;">
                                <label style="display: flex; align-items: center; gap: 8px; font-weight: normal; cursor: pointer;">
                                    <input type="checkbox" id="msWinAuth" onchange="onMsWinAuthChange()">
                                    Windows Authentication (Integrated Security)
                                </label>
                            </div>
                            <div id="msSqlAuthFields" class="form-row">
                                <div class="form-group">
                                    <label for="msUser">User *</label>
                                    <input type="text" id="msUser" value="sa" placeholder="sa" oninput="clearConnectionStatus()">
                                </div>
                                <div class="form-group">
                                    <label for="msPassword">Password *</label>
                                    <input type="password" id="msPassword" placeholder="password" oninput="clearConnectionStatus()">
                                </div>
                            </div>
                            <div class="form-group" style="margin-top: 4px;">
                                <label style="display: flex; align-items: center; gap: 8px; font-weight: normal; cursor: pointer; color: #555;">
                                    <input type="checkbox" id="msEncryptDisable" checked onchange="clearConnectionStatus()">
                                    Disable encryption (sslmode=disable, для локальных серверов)
                                </label>
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

        // Build a human-readable connection summary
        let connSummary = '';
        if (src.dsn) {
            if (src.type === 'postgres' || src.type === 'mysql' || src.type === 'mssql') {
                // Extract host+db from DSN for display
                const hostMatch = src.dsn.match(/host=([^\s]+)/i) ||
                                  src.dsn.match(/@(?:tcp\()?([^:/]+)/) ||
                                  src.dsn.match(/sqlserver:\/\/[^@]*@([^:/]+)/i);
                const dbMatch   = src.dsn.match(/dbname=([^\s]+)/i)  ||
                                  src.dsn.match(/\/([^?]+)$/)         ||
                                  src.dsn.match(/database=([^&]+)/i);
                const hostStr   = hostMatch ? hostMatch[1] : '';
                const dbStr     = dbMatch   ? dbMatch[1]   : '';
                if (hostStr || dbStr) connSummary = `${hostStr}${dbStr ? '/' + dbStr : ''}`;
            } else {
                // sqlite / tdtp — just show short filename
                connSummary = src.dsn.split(/[\\/]/).pop();
            }
        }
        const tableStr = src.tableName ? ` · <em>${src.tableName}</em>` : '';

        const validateBtn = !src.tested
            ? `<button class="btn btn-sm" style="background:#0066cc;color:white;" id="validateBtn-${index}" onclick="validateSource(${index})">Validate</button>`
            : '';
        html += `
            <div style="border: 1px solid #ddd; padding: 10px; border-radius: 3px; background: #fafafa;">
                <div style="display: flex; justify-content: space-between; align-items: center;">
                    <div style="overflow: hidden;">
                        <strong>${src.name}</strong> ${statusIcon}
                        <br><small style="color: #666;">${typeLabel}${connSummary ? ' · ' + connSummary : ''}${tableStr}</small>
                    </div>
                    <div style="flex-shrink: 0; margin-left: 8px;">
                        ${validateBtn}
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

async function validateSource(index) {
    const src = sources[index];
    if (!src) return;

    const btn = document.getElementById(`validateBtn-${index}`);
    if (btn) { btn.disabled = true; btn.textContent = '⏳...'; }

    try {
        if (!wailsReady || !window.go) {
            showNotification('Backend not ready', 'error');
            return;
        }
        const result = await window.go.main.App.ValidateSourceByName(src.name);
        if (result.success) {
            sources[index].tested = true;
            showNotification(`✅ ${src.name}: ${result.message || 'Connected'}`, 'success');
        } else {
            showNotification(`❌ ${src.name}: ${result.message || 'Connection failed'}`, 'error');
        }
    } catch (err) {
        showNotification(`Validation error: ${err}`, 'error');
    }
    renderSourcesList();
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
    clearConnectionStatus();

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

function onMsWinAuthChange() {
    const winAuth = document.getElementById('msWinAuth').checked;
    const sqlAuthFields = document.getElementById('msSqlAuthFields');
    sqlAuthFields.style.display = winAuth ? 'none' : 'flex';
    clearConnectionStatus();
}

function onSourceTypeChange(type) {
    // Reset test status when changing source type
    lastTestedDSN = '';
    selectedTableName = '';
    clearConnectionStatus();
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
        const database = document.getElementById('msDatabase').value;
        const winAuth = document.getElementById('msWinAuth').checked;
        const encryptDisable = document.getElementById('msEncryptDisable').checked;
        const encryptParam = encryptDisable ? '&encrypt=disable' : '';

        if (winAuth) {
            return `sqlserver://${server}:${port}?database=${database}${encryptParam}&trusted_connection=true`;
        }

        const user = document.getElementById('msUser').value;
        const password = document.getElementById('msPassword').value;

        return `sqlserver://${user}:${password}@${server}:${port}?database=${database}${encryptParam}`;

    } else if (type === 'sqlite') {
        const file = document.getElementById('sqliteFile').value;
        return file;
    } else if (type === 'tdtp') {
        const file = document.getElementById('tdtpFile').value;
        return file; // TDTP file path is used as DSN
    }

    return '';
}

// Parse DSN string and populate connection form fields
function parseDSNToFields(type, dsn) {
    if (!dsn) return;

    try {
        if (type === 'postgres') {
            // Format: host=localhost port=5432 user=postgres password=xxx dbname=testdb sslmode=disable
            const get = (key) => {
                const m = dsn.match(new RegExp(`(?:^|\\s)${key}=([^\\s]+)`));
                return m ? m[1] : '';
            };
            document.getElementById('pgHost').value     = get('host')     || 'localhost';
            document.getElementById('pgPort').value     = get('port')     || '5432';
            document.getElementById('pgUser').value     = get('user')     || '';
            document.getElementById('pgPassword').value = get('password') || '';
            document.getElementById('pgDatabase').value = get('dbname')   || '';
            const sslmode = get('sslmode') || 'disable';
            const sslSel = document.getElementById('pgSSLMode');
            if (sslSel) {
                for (const opt of sslSel.options) {
                    if (opt.value === sslmode) { sslSel.value = sslmode; break; }
                }
            }

        } else if (type === 'mysql') {
            // Format: user:password@tcp(host:port)/database
            const m = dsn.match(/^([^:]*):([^@]*)@tcp\(([^:)]+):(\d+)\)\/(.+)$/);
            if (m) {
                document.getElementById('myUser').value     = m[1];
                document.getElementById('myPassword').value = m[2];
                document.getElementById('myHost').value     = m[3];
                document.getElementById('myPort').value     = m[4];
                document.getElementById('myDatabase').value = m[5];
            }

        } else if (type === 'mssql') {
            // Format: sqlserver://user:password@server:port?database=db
            // OR:     sqlserver://server:port?database=db&trusted_connection=true
            const url = new URL(dsn);
            const server   = url.hostname || 'localhost';
            const port     = url.port || '1433';
            const database = url.searchParams.get('database') || '';
            const winAuth  = url.searchParams.get('trusted_connection') === 'true';
            const encrypt  = url.searchParams.get('encrypt') === 'disable';

            document.getElementById('msServer').value = server;
            document.getElementById('msPort').value   = port;
            document.getElementById('msDatabase').value = database;
            document.getElementById('msWinAuth').checked = winAuth;
            document.getElementById('msEncryptDisable').checked = encrypt;
            onMsWinAuthChange();

            if (!winAuth) {
                document.getElementById('msUser').value     = url.username || 'sa';
                document.getElementById('msPassword').value = url.password || '';
            }

        } else if (type === 'sqlite') {
            document.getElementById('sqliteFile').value = dsn;

        } else if (type === 'tdtp') {
            document.getElementById('tdtpFile').value = dsn;
        }
    } catch (e) {
        console.warn('parseDSNToFields error for type', type, ':', e);
    }
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
    resultEl.innerHTML = '<p style="margin: 0; color: #666;">🔄 Testing connection...</p>';

    if (!wailsReady || !window.go) {
        resultEl.innerHTML = '<p style="color: orange; margin: 0;">⚠️ Wails not ready, connection test skipped</p>';
        return;
    }

    try {
        const source = { name: 'test', type: type, dsn: dsn };
        const result = await window.go.main.App.TestSource(source);

        if (result.success) {
            // Mark this DSN as successfully tested
            lastTestedDSN = dsn;

            // Show connection info in status bar (persistent while connected)
            setStatusConnected(`${type.toUpperCase()} — ${result.message || 'Connected'}`);

            let html = '';

            // For TDTP sources, auto-select the table and mark as tested
            if (type === 'tdtp' && result.tables && result.tables.length > 0) {
                selectedTableName = result.tables[0]; // TDTP has one table per file
                html = `
                    <div style="padding: 8px 10px; background: #f8f9fa; border: 1px solid #dee2e6; border-radius: 3px;">
                        <p style="color: #155724; margin: 0;"><strong>📋 Table: ${selectedTableName}</strong></p>
                        <p style="margin: 5px 0 0 0; font-size: 11px; color: #6c757d;">Ready to use as data source</p>
                    </div>
                `;
            }
            // Show table selection for database sources
            else if (result.tables && result.tables.length > 0) {
                html = `
                    <div style="padding: 8px 10px; background: #f8f9fa; border: 1px solid #dee2e6; border-radius: 3px;">
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

            if (html) {
                resultEl.innerHTML = html;
            } else {
                resultEl.style.display = 'none';
            }
        } else {
            clearConnectionStatus();
            resultEl.innerHTML = `
                <div style="padding: 8px 10px; background: #f8d7da; border: 1px solid #f5c6cb; border-radius: 3px;">
                    <p style="color: #721c24; margin: 0;"><strong>❌ Connection Failed</strong></p>
                    <p style="color: #721c24; margin: 5px 0 0 0; font-size: 11px;">${result.message}</p>
                </div>
            `;
        }
    } catch (err) {
        console.error('Test connection error:', err);
        clearConnectionStatus();
        resultEl.innerHTML = `
            <div style="padding: 8px 10px; background: #f8d7da; border: 1px solid #f5c6cb; border-radius: 3px;">
                <p style="color: #721c24; margin: 0;"><strong>❌ Error</strong></p>
                <p style="color: #721c24; margin: 5px 0 0 0; font-size: 11px;">${err}</p>
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
        // Restore selected table
        selectedTableName = src.tableName || '';

        // Populate connection form fields from DSN
        if (src.dsn) {
            parseDSNToFields(src.type, src.dsn);
            // If table was loaded from config, show it in testResult panel
            if (selectedTableName) {
                const resultEl = document.getElementById('testResult');
                if (resultEl) {
                    resultEl.style.display = 'block';
                    resultEl.innerHTML = `
                        <div style="padding: 8px 10px; background: #f8f9fa; border: 1px solid #dee2e6; border-radius: 3px;">
                            <p style="color: #155724; margin: 0;"><strong>📋 Table: ${selectedTableName}</strong></p>
                            <p style="margin: 5px 0 0 0; font-size: 11px; color: #6c757d;">Loaded from config — click "Test Connection" to verify</p>
                        </div>`;
                    lastTestedDSN = src.dsn; // treat as pre-tested to allow saving without re-test
                }
            }
        }
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
        let html = '<div style="overflow-x: auto; max-height: 400px; overflow-y: auto;"><table style="border-collapse: collapse; font-size: 12px; min-width: 100%;">';
        html += '<thead><tr>';
        result.columns.forEach(col => {
            html += `<th style="border: 1px solid #ddd; padding: 5px; background: #f0f0f0; white-space: nowrap; max-width: 300px; overflow: hidden; text-overflow: ellipsis;">${col}</th>`;
        });
        html += '</tr></thead><tbody>';

        result.rows.forEach(row => {
            html += '<tr>';
            // Convert object to array in column order
            const values = Array.isArray(row) ? row : result.columns.map(col => row[col]);
            values.forEach(cell => {
                html += `<td style="border: 1px solid #ddd; padding: 5px; white-space: nowrap; max-width: 300px; overflow: hidden; text-overflow: ellipsis;">${cell !== null ? cell : '<i>null</i>'}</td>`;
            });
            html += '</tr>';
        });

        html += '</tbody></table></div>';
        html += `<p style="margin-top: 10px; color: #666;"><small>Showing ${result.rows.length} rows</small></p>`;

        previewContent.innerHTML = html;

        // Preview succeeded — mark source as validated automatically
        if (!sources[index].tested) {
            sources[index].tested = true;
            renderSourcesList();
            showNotification(`✅ ${src.name}: validated via preview`, 'success');
        }
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

// loadFieldsForDesign fetches/merges DB column schemas into canvas design tables.
// Tables with no fields → load all columns (all visible).
// Tables with fields but no type info (from SQL parse) → merge: restore visibility+filters, fill types from DB.
// Tables with full type info → skip (already complete).
async function loadFieldsForDesign(design) {
    if (!design || !design.tables) return;
    for (let i = 0; i < design.tables.length; i++) {
        const table = design.tables[i];
        const hasTypeInfo = table.fields && table.fields.length > 0 &&
                            table.fields.some(f => f.type && f.type !== '');
        const needsLoad  = !table.fields || table.fields.length === 0;
        const needsMerge = !needsLoad && !hasTypeInfo;

        if (!needsLoad && !needsMerge) continue;

        console.log(`  🔄 ${needsMerge ? 'Merging' : 'Loading'} fields for ${table.sourceName}...`);
        try {
            const dbTables = await window.go.main.App.GetTablesBySourceName(table.sourceName);
            if (!dbTables || dbTables.length === 0 || !dbTables[0].columns) {
                console.warn(`  ⚠️ No schema from DB for ${table.sourceName}, keeping parsed fields`);
                continue;
            }
            if (needsMerge) {
                const parsedLookup = {};
                (table.fields || []).forEach(f => { parsedLookup[f.name.toLowerCase()] = f; });
                const hasAnyParsed = Object.keys(parsedLookup).length > 0;
                table.fields = dbTables[0].columns.map(col => {
                    const parsed = parsedLookup[col.name.toLowerCase()];
                    return {
                        name: col.name,
                        type: col.type,
                        isPrimaryKey: col.isPrimaryKey || false,
                        visible: parsed ? parsed.visible : !hasAnyParsed,
                        filter: parsed ? (parsed.filter || null) : null
                    };
                });
                console.log(`  ✅ Merged: ${table.fields.filter(f=>f.visible).length} visible, ` +
                            `${table.fields.filter(f=>f.filter).length} with filters`);
            } else {
                table.fields = dbTables[0].columns.map(col => ({
                    name: col.name,
                    type: col.type,
                    isPrimaryKey: col.isPrimaryKey || false,
                    visible: true,
                    filter: null
                }));
                console.log(`  ✅ Loaded ${table.fields.length} fields for ${table.sourceName}`);
            }
        } catch (err) {
            console.error(`  ❌ Failed to load fields for ${table.sourceName}:`, err);
        }
    }
}

function loadStep3Data() {
    console.log('🔄 loadStep3Data() called');

    // Check DOM readiness
    const sourceListEl = document.getElementById('tablesSourceList');
    const canvasArea = document.getElementById('canvasArea');
    const svg = document.getElementById('canvasSVG');

    if (!sourceListEl || !canvasArea || !svg) {
        console.error('❌ DOM elements not ready, retrying in 100ms...');
        setTimeout(() => loadStep3Data(), 100);
        return;
    }

    // Load sources from Step 2 (global sources variable, not appState!)
    if (sources.length === 0) {
        sourceListEl.innerHTML = '<p style="color: #999; font-size: 13px;">No sources defined in Step 2</p>';
        return;
    }

    console.log('📋 Loading sources:', sources.length);

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

    if (!wailsReady || !window.go || !window.go.main || !window.go.main.App) {
        console.warn('⚠️ Wails not ready, rendering with existing canvasDesign');
        if (canvasDesign && canvasDesign.tables && canvasDesign.tables.length > 0) {
            renderCanvas();
        }
        return;
    }

    // If canvasDesign already populated (e.g. loaded from repository), skip backend fetch
    if (canvasDesign && canvasDesign.tables && canvasDesign.tables.length > 0) {
        console.log('✅ canvasDesign already loaded (from repository), rendering directly');
        loadFieldsForDesign(canvasDesign).then(() => renderCanvas());
        return;
    }

    window.go.main.App.GetCanvasDesign().then(async design => {
        console.log('📦 GetCanvasDesign response:', design);

        if (design && design.tables && design.tables.length > 0) {
            // Canvas design exists — load/merge field schemas then render
            canvasDesign = design;
            console.log('✅ Canvas design loaded:', {
                tables: canvasDesign.tables.length,
                joins: canvasDesign.joins ? canvasDesign.joins.length : 0
            });
            await loadFieldsForDesign(canvasDesign);
            console.log('🎨 Rendering canvas from loaded design...');
            renderCanvas();
        } else {
            // Canvas is empty (fresh load or parseSQLToCanvasDesign returned nil).
            // Ask backend to reconstruct from transform SQL or source list.
            console.log('ℹ️ Canvas design empty — attempting auto-reconstruction...');
            try {
                const reconstructed = await window.go.main.App.ReconstructCanvas();
                if (reconstructed && reconstructed.tables && reconstructed.tables.length > 0) {
                    canvasDesign = reconstructed;
                    console.log(`🔄 Reconstructed canvas: ${canvasDesign.tables.length} table(s), ` +
                                `${canvasDesign.joins ? canvasDesign.joins.length : 0} join(s)`);
                    await loadFieldsForDesign(canvasDesign);
                    console.log('🎨 Rendering reconstructed canvas...');
                    renderCanvas();
                    const fromSQL = canvasDesign.tables.some(t => t.fields && t.fields.some(f => !f.visible));
                    showNotification(
                        fromSQL
                            ? 'Canvas restored from saved SQL (field visibility & filters preserved)'
                            : 'Canvas reconstructed from sources — add JOINs as needed',
                        'info'
                    );
                } else {
                    console.log('ℹ️ No tables to reconstruct (no sources or empty config)');
                    canvasDesign.tables = canvasDesign.tables || [];
                    canvasDesign.joins  = canvasDesign.joins  || [];
                }
            } catch (err) {
                console.error('❌ Auto-reconstruction failed:', err);
                canvasDesign.tables = canvasDesign.tables || [];
                canvasDesign.joins  = canvasDesign.joins  || [];
            }
        }
    }).catch(err => {
        console.error('❌ Failed to load canvas design:', err);
    });
}

async function saveStep3() {
    // Save canvas design to backend
    try {
        if (!wailsReady || !window.go) {
            console.warn('Wails not ready, skipping canvas save');
            return true; // Allow progression anyway
        }
        await window.go.main.App.SaveCanvasDesign(canvasDesign);
        console.log('Canvas design saved:', canvasDesign);
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
    console.log(`🔧 addTableToCanvas called for: ${sourceName}`);

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

        console.log(`📞 Calling GetTablesBySourceName for: ${sourceName}`);
        const tables = await window.go.main.App.GetTablesBySourceName(sourceName);
        console.log(`📦 GetTablesBySourceName response:`, tables);

        if (!tables || tables.length === 0) {
            console.error(`❌ No tables returned for: ${sourceName}`);
            showNotification('Failed to load table schema', 'error');
            return;
        }

        const tableInfo = tables[0];
        console.log(`📋 Table info:`, tableInfo);
        console.log(`📋 Columns:`, tableInfo.columns);

        if (!tableInfo.columns || tableInfo.columns.length === 0) {
            console.error(`❌ No columns in table info for: ${sourceName}`);
            showNotification(`⚠️ Table "${sourceName}" has no columns. Check if TDTP XML schema is valid.`, 'error');
            // Still add the table but with empty fields - maybe user can add fields manually later
        }

        const fields = (tableInfo.columns || []).map(col => ({
            name: col.name,
            type: col.type,
            isPrimaryKey: col.isPrimaryKey || false,
            visible: true,
            filter: null // { operator: '=|<>|>=|<=|>|<|BW', value: '', value2: '', logic: 'AND|OR' }
        }));

        console.log(`✅ Mapped ${fields.length} fields for ${sourceName}`);

        // Calculate position (offset each new table)
        const tableCount = canvasDesign.tables.length;
        const x = 50 + (tableCount * 30) % 400;
        const y = 50 + (tableCount * 30) % 300;

        // Look up the actual DB table name for this source.
        // Source.Name is the user alias; Source.TableName is the real DB table.
        // If they differ, store it as tableRef so GenerateSQL can emit:
        //   FROM [actual_table] AS [alias]
        const sourceInfo = sources.find(s => s.name === sourceName);
        const actualTableName = (sourceInfo && sourceInfo.tableName &&
                                 sourceInfo.tableName !== sourceName)
            ? sourceInfo.tableName
            : '';

        const newTable = {
            sourceName: sourceName,   // = Source.Name (user alias, for schema lookup)
            tableRef:   actualTableName, // = Source.TableName (actual DB table, '' if same)
            alias:      sourceName,   // = Source.Name (used in field references)
            x: x,
            y: y,
            fields: fields
        };

        console.log(`➕ Adding table to canvas:`, newTable);
        canvasDesign.tables.push(newTable);
        renderCanvas();
    } catch (err) {
        console.error('❌ Failed to add table:', err);
        showNotification('Failed to add table: ' + err, 'error');
    }
}

function renderCanvas() {
    console.log('🎨 renderCanvas() called');
    const canvasArea = document.getElementById('canvasArea');
    const svg = document.getElementById('canvasSVG');

    // Check if DOM elements exist
    if (!canvasArea || !svg) {
        console.warn('⚠️ Canvas DOM elements not ready yet');
        return;
    }

    console.log('📊 Rendering canvas with:', {
        tables: canvasDesign.tables.length,
        joins: canvasDesign.joins ? canvasDesign.joins.length : 0
    });

    // Preserve scroll positions of each table's field list before clearing
    const fieldScrollTops = {};
    canvasDesign.tables.forEach((_, index) => {
        const fc = document.getElementById(`fields-${index}`);
        if (fc) fieldScrollTops[index] = fc.scrollTop;
    });

    // Clear existing
    canvasArea.innerHTML = '';
    svg.innerHTML = '';

    // Render tables
    canvasDesign.tables.forEach((table, index) => {
        console.log(`  ➕ Rendering table ${index}: ${table.sourceName}`);
        const tableCard = createTableCard(table, index);
        canvasArea.appendChild(tableCard);
        // Restore scroll position so the field the user acted on stays in view
        if (fieldScrollTops[index]) {
            const fc = document.getElementById(`fields-${index}`);
            if (fc) fc.scrollTop = fieldScrollTops[index];
        }
    });

    console.log('✅ Tables rendered, scheduling JOIN rendering...');
    // Render joins after a small delay to ensure connectors are in DOM
    setTimeout(() => renderJoins(), 50);
}

function createTableCard(table, index) {
    const card = document.createElement('div');
    card.id = `table-${index}`;
    card.className = 'table-card';
    card.style.cssText = `
        position: absolute;
        left: ${table.x}px;
        top: ${table.y}px;
        min-width: 200px;
        width: auto;
        max-width: 400px;
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
    fieldsContainer.id = `fields-${index}`;
    fieldsContainer.style.cssText = 'padding: 5px; max-height: 300px; overflow-y: auto;';

    table.fields.forEach((field, fieldIndex) => {
        const fieldEl = document.createElement('div');
        fieldEl.className = 'table-field';

        // Check if this is a primary key field
        const isPrimaryKey = field.key || field.isPrimaryKey || false;

        // Style for primary key fields
        const pkBackground = isPrimaryKey ? 'background: #fffbea; border-left: 3px solid #f59e0b;' : '';

        fieldEl.style.cssText = `
            padding: 4px 8px;
            font-size: 12px;
            border-bottom: 1px solid #f0f0f0;
            display: grid;
            grid-template-columns: 30px auto 30px 30px;
            gap: 8px;
            align-items: center;
            ${pkBackground}
        `;

        // Get filter icon
        const filterIcon = field.filter ? (field.filter.logic === 'OR' ? '^' : '&') : '☀';
        const filterColor = field.filter ? '#0066cc' : '#ccc';

        // Check if this field has a connection
        const hasConnection = canvasDesign.joins.some(j =>
            (j.leftTable === table.sourceName && j.leftField === field.name) ||
            (j.rightTable === table.sourceName && j.rightField === field.name)
        );

        // Key icon and field name styling
        const keyIcon = isPrimaryKey ? '🔑 ' : '';
        const fieldNameStyle = isPrimaryKey ? 'color: #f59e0b; font-weight: 700;' : '';

        fieldEl.innerHTML = `
            <span onclick="toggleFieldVisibility(${index}, ${fieldIndex})"
                  style="cursor: pointer; font-size: 14px; color: ${field.visible ? '#28a745' : '#999'}; user-select: none;"
                  title="${field.visible ? 'Hide field' : 'Show field'}">
                👁
            </span>
            <span class="field-name-wrapper"
                  data-type="${field.type}"
                  style="${field.visible ? '' : 'opacity: 0.4;'}"
                  title="${keyIcon}${field.name} (${field.type})${isPrimaryKey ? ' - PRIMARY KEY' : ''}">
                <strong class="field-name" style="${fieldNameStyle}">${keyIcon}${field.name}</strong>
                <small class="field-type">${field.type}</small>
            </span>
            <span onclick="openFilterBuilder(${index}, ${fieldIndex})"
                  style="cursor: pointer; font-size: 16px; font-weight: bold; color: ${filterColor}; user-select: none; text-align: center;"
                  title="${field.filter ? 'Edit filter' : 'Add filter'}">
                ${filterIcon}
            </span>
            <div class="join-connector ${hasConnection ? 'connected' : ''}"
                 data-table="${index}"
                 data-field="${fieldIndex}"
                 draggable="true"
                 ondragstart="startConnectorDrag(event, ${index}, ${fieldIndex})"
                 ondragend="endConnectorDrag(event)"
                 ondrop="connectFields(event, ${index}, ${fieldIndex})"
                 ondragover="allowDrop(event)"
                 onclick="showFieldConnections(${index}, ${fieldIndex})"
                 title="Drag to another connector to create JOIN">
                ⚡
            </div>
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
        // Ignore buttons and inputs
        if (e.target.tagName === 'BUTTON' || e.target.tagName === 'INPUT') return;

        // Check if target or its parent is draggable (for field drag-and-drop)
        let target = e.target;
        while (target && target !== element) {
            if (target.draggable === true) return;
            target = target.parentElement;
        }

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
    renderCanvas(); // Перерисовываем canvas после изменения видимости
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
    console.log('🔗 renderJoins() called');
    const svg = document.getElementById('canvasSVG');

    // Check if SVG exists
    if (!svg) {
        console.warn('⚠️ SVG element not found');
        return;
    }

    console.log(`📍 Rendering ${canvasDesign.joins.length} JOINs`);
    svg.innerHTML = '';

    canvasDesign.joins.forEach((join, joinIndex) => {
        console.log(`  🔗 JOIN ${joinIndex}: ${join.leftTable}.${join.leftField} → ${join.rightTable}.${join.rightField}`);
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
        line.oncontextmenu = (e) => {
            e.preventDefault();
            showJoinContextMenu(e, joinIndex);
        };

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
        text.oncontextmenu = (e) => {
            e.preventDefault();
            showJoinContextMenu(e, joinIndex);
        };

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

    let castInfo = '';
    if (join.castLeft && join.castRight) {
        castInfo = `<div style="background: #fff3cd; padding: 8px; border-radius: 3px; margin-bottom: 10px; font-size: 12px;">
            ⚠️ Cast both fields to ${join.castLeft}
        </div>`;
    } else if (join.castLeft) {
        castInfo = `<div style="background: #fff3cd; padding: 8px; border-radius: 3px; margin-bottom: 10px; font-size: 12px;">
            ⚠️ Cast left field to ${join.castLeft}
        </div>`;
    } else if (join.castRight) {
        castInfo = `<div style="background: #fff3cd; padding: 8px; border-radius: 3px; margin-bottom: 10px; font-size: 12px;">
            ⚠️ Cast right field to ${join.castRight}
        </div>`;
    }

    const propertiesPanel = document.getElementById('propertiesPanel');
    propertiesPanel.innerHTML = `
        <h4 style="margin-top: 0;">Join Properties</h4>
        <div style="margin-bottom: 10px;">
            <strong>${join.leftTable}.${join.leftField}</strong>
            <br>↓<br>
            <strong>${join.rightTable}.${join.rightField}</strong>
        </div>

        ${castInfo}

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

// ========== FILTER BUILDER ==========

function openFilterBuilder(tableIndex, fieldIndex) {
    const field = canvasDesign.tables[tableIndex].fields[fieldIndex];
    const currentFilter = field.filter || { operator: '=', value: '', value2: '', logic: 'AND' };

    // Create modal
    const modal = document.createElement('div');
    modal.id = 'filterBuilderModal';
    modal.style.cssText = `
        position: fixed;
        top: 0;
        left: 0;
        right: 0;
        bottom: 0;
        background: rgba(0,0,0,0.5);
        z-index: 2000;
        display: flex;
        justify-content: center;
        align-items: center;
    `;

    modal.innerHTML = `
        <div style="background: white; padding: 20px; border-radius: 5px; min-width: 400px; max-width: 500px;">
            <h3 style="margin-top: 0;">Filter: ${field.name}</h3>

            <div style="margin-bottom: 15px;">
                <label style="display: block; margin-bottom: 5px; font-weight: 600;">Operator:</label>
                <select id="filterOperator" style="width: 100%; padding: 8px; border: 1px solid #ddd; border-radius: 3px;">
                    <optgroup label="Comparison">
                        <option value="=" ${currentFilter.operator === '=' ? 'selected' : ''}>= (Equal)</option>
                        <option value="<>" ${currentFilter.operator === '<>' ? 'selected' : ''}>&lt;&gt; (Not Equal)</option>
                        <option value=">" ${currentFilter.operator === '>' ? 'selected' : ''}>&gt; (Greater Than)</option>
                        <option value="<" ${currentFilter.operator === '<' ? 'selected' : ''}>&lt; (Less Than)</option>
                        <option value=">=" ${currentFilter.operator === '>=' ? 'selected' : ''}>&gt;= (Greater or Equal)</option>
                        <option value="<=" ${currentFilter.operator === '<=' ? 'selected' : ''}>&lt;= (Less or Equal)</option>
                        <option value="BW" ${currentFilter.operator === 'BW' ? 'selected' : ''}>BETWEEN</option>
                    </optgroup>
                    <optgroup label="NULL checks">
                        <option value="IS_NULL" ${currentFilter.operator === 'IS_NULL' ? 'selected' : ''}>IS NULL</option>
                        <option value="IS_NOT_NULL" ${currentFilter.operator === 'IS_NOT_NULL' ? 'selected' : ''}>IS NOT NULL</option>
                    </optgroup>
                    <optgroup label="Empty string checks">
                        <option value="IS_EMPTY" ${currentFilter.operator === 'IS_EMPTY' ? 'selected' : ''}> = '' (Empty string)</option>
                        <option value="IS_NOT_EMPTY" ${currentFilter.operator === 'IS_NOT_EMPTY' ? 'selected' : ''}>&lt;&gt; '' (Not empty string)</option>
                    </optgroup>
                </select>
            </div>

            <div id="filterValue1Container" style="margin-bottom: 15px; display: ${['IS_NULL','IS_NOT_NULL','IS_EMPTY','IS_NOT_EMPTY'].includes(currentFilter.operator) ? 'none' : 'block'};">
                <label style="display: block; margin-bottom: 5px; font-weight: 600;">Value:</label>
                <input type="text" id="filterValue1" value="${currentFilter.value || ''}"
                       style="width: 100%; padding: 8px; border: 1px solid #ddd; border-radius: 3px;">
            </div>

            <div id="filterValue2Container" style="margin-bottom: 15px; display: ${currentFilter.operator === 'BW' ? 'block' : 'none'};">
                <label style="display: block; margin-bottom: 5px; font-weight: 600;">End Value:</label>
                <input type="text" id="filterValue2" value="${currentFilter.value2 || ''}"
                       style="width: 100%; padding: 8px; border: 1px solid #ddd; border-radius: 3px;">
            </div>

            <div style="margin-bottom: 15px;">
                <label style="display: block; margin-bottom: 5px; font-weight: 600;">Logic Operator:</label>
                <select id="filterLogic" style="width: 100%; padding: 8px; border: 1px solid #ddd; border-radius: 3px;">
                    <option value="AND" ${currentFilter.logic === 'AND' ? 'selected' : ''}>AND (&amp;)</option>
                    <option value="OR" ${currentFilter.logic === 'OR' ? 'selected' : ''}>OR (^)</option>
                </select>
                <small style="color: #666;">How this filter combines with other filters</small>
            </div>

            <div style="display: flex; gap: 10px; margin-top: 20px;">
                <button class="btn" onclick="saveFilter(${tableIndex}, ${fieldIndex})" style="flex: 1; background: #28a745; color: white;">Apply Filter</button>
                <button class="btn" onclick="clearFilter(${tableIndex}, ${fieldIndex})" style="flex: 1; background: #dc3545; color: white;">Clear Filter</button>
                <button class="btn" onclick="closeFilterBuilder()" style="flex: 1;">Cancel</button>
            </div>
        </div>
    `;

    document.body.appendChild(modal);

    // Handle operator change — show/hide value inputs based on selected operator
    document.getElementById('filterOperator').addEventListener('change', function() {
        const noValueOps = ['IS_NULL', 'IS_NOT_NULL', 'IS_EMPTY', 'IS_NOT_EMPTY'];
        const needsValue = !noValueOps.includes(this.value);
        const isBetween  = this.value === 'BW';
        document.getElementById('filterValue1Container').style.display = needsValue ? 'block' : 'none';
        document.getElementById('filterValue2Container').style.display = isBetween  ? 'block' : 'none';
    });

    // Close on backdrop click
    modal.addEventListener('click', function(e) {
        if (e.target === modal) {
            closeFilterBuilder();
        }
    });

    // Close on Escape key
    const escapeHandler = function(e) {
        if (e.key === 'Escape') {
            closeFilterBuilder();
            document.removeEventListener('keydown', escapeHandler);
        }
    };
    document.addEventListener('keydown', escapeHandler);
}

function saveFilter(tableIndex, fieldIndex) {
    const operator = document.getElementById('filterOperator').value;
    const value1 = document.getElementById('filterValue1').value.trim();
    const value2 = document.getElementById('filterValue2').value.trim();
    const logic = document.getElementById('filterLogic').value;

    const noValueOps = ['IS_NULL', 'IS_NOT_NULL', 'IS_EMPTY', 'IS_NOT_EMPTY'];
    if (!noValueOps.includes(operator) && !value1) {
        alert('Please enter a value');
        return;
    }

    if (operator === 'BW' && !value2) {
        alert('Please enter end value for BETWEEN');
        return;
    }

    canvasDesign.tables[tableIndex].fields[fieldIndex].filter = {
        operator: operator,
        value: value1,
        value2: value2,
        logic: logic
    };

    closeFilterBuilder();
    renderCanvas();
    showNotification('Filter applied successfully', 'success');
}

function clearFilter(tableIndex, fieldIndex) {
    canvasDesign.tables[tableIndex].fields[fieldIndex].filter = null;
    closeFilterBuilder();
    renderCanvas();
    showNotification('Filter cleared', 'info');
}

function closeFilterBuilder() {
    const modal = document.getElementById('filterBuilderModal');
    if (modal) {
        modal.remove();
    }
}

// ========== DRAG-AND-DROP JOIN (Connector Box) ==========

let activeConnector = null;

function startConnectorDrag(event, tableIndex, fieldIndex) {
    const table = canvasDesign.tables[tableIndex];
    const field = table.fields[fieldIndex];

    activeConnector = {
        tableIndex: tableIndex,
        fieldIndex: fieldIndex,
        tableName: table.sourceName,
        fieldName: field.name,
        fieldType: field.type
    };

    event.dataTransfer.effectAllowed = 'link';
    event.dataTransfer.setData('text/plain', `${table.sourceName}.${field.name}`);

    // Visual feedback
    event.target.classList.add('dragging-connector');
    showNotification(`Drag to another ⚡ to create JOIN`, 'info');
}

function endConnectorDrag(event) {
    event.target.classList.remove('dragging-connector');
    activeConnector = null;
}

function allowDrop(event) {
    event.preventDefault();
    event.dataTransfer.dropEffect = 'link';
}

function connectFields(event, tableIndex, fieldIndex) {
    event.preventDefault();
    event.stopPropagation();

    if (!activeConnector) {
        return;
    }

    const targetTable = canvasDesign.tables[tableIndex];
    const targetField = targetTable.fields[fieldIndex];

    // Check if same table
    if (activeConnector.tableName === targetTable.sourceName) {
        showNotification('Cannot join table to itself', 'warning');
        activeConnector = null;
        return;
    }

    // Check if join already exists
    const existingJoin = canvasDesign.joins.find(j =>
        (j.leftTable === activeConnector.tableName && j.leftField === activeConnector.fieldName &&
         j.rightTable === targetTable.sourceName && j.rightField === targetField.name) ||
        (j.leftTable === targetTable.sourceName && j.leftField === targetField.name &&
         j.rightTable === activeConnector.tableName && j.rightField === activeConnector.fieldName)
    );

    if (existingJoin) {
        showNotification('Join already exists', 'warning');
        activeConnector = null;
        return;
    }

    // Check if types match
    if (activeConnector.fieldType === targetField.type) {
        // Types match - create join directly
        createJoinBetweenFields(activeConnector.tableName, activeConnector.fieldName,
                                targetTable.sourceName, targetField.name, null);
        activeConnector = null;
    } else {
        // Types don't match - show CAST dialog
        showCastDialog(activeConnector, targetTable.sourceName, targetField);
    }
}

function showCastDialog(sourceField, targetTableName, targetField) {
    const modal = document.createElement('div');
    modal.id = 'castDialogModal';
    modal.style.cssText = `
        position: fixed;
        top: 0;
        left: 0;
        right: 0;
        bottom: 0;
        background: rgba(0,0,0,0.5);
        z-index: 2000;
        display: flex;
        justify-content: center;
        align-items: center;
    `;

    modal.innerHTML = `
        <div style="background: white; padding: 20px; border-radius: 5px; min-width: 400px;">
            <h3 style="margin-top: 0; color: #ff6b00;">⚠️ Type Mismatch</h3>
            <p>Field types don't match:</p>
            <ul style="margin: 10px 0;">
                <li><strong>${sourceField.tableName}.${sourceField.fieldName}</strong>: ${sourceField.fieldType}</li>
                <li><strong>${targetTableName}.${targetField.name}</strong>: ${targetField.type}</li>
            </ul>
            <p>Select cast option:</p>
            <div style="display: flex; flex-direction: column; gap: 10px; margin: 15px 0;">
                <button class="btn" onclick="createJoinWithCast('${sourceField.tableName}', '${sourceField.fieldName}', '${targetTableName}', '${targetField.name}', 'CAST_LEFT', '${sourceField.fieldType}', '${targetField.type}')"
                        style="background: #0066cc; color: white; text-align: left; padding: 10px;">
                    Cast ${sourceField.tableName}.${sourceField.fieldName} to ${targetField.type}
                </button>
                <button class="btn" onclick="createJoinWithCast('${sourceField.tableName}', '${sourceField.fieldName}', '${targetTableName}', '${targetField.name}', 'CAST_RIGHT', '${sourceField.fieldType}', '${targetField.type}')"
                        style="background: #0066cc; color: white; text-align: left; padding: 10px;">
                    Cast ${targetTableName}.${targetField.name} to ${sourceField.fieldType}
                </button>
                <button class="btn" onclick="createJoinWithCast('${sourceField.tableName}', '${sourceField.fieldName}', '${targetTableName}', '${targetField.name}', 'CAST_STRING', '${sourceField.fieldType}', '${targetField.type}')"
                        style="background: #0066cc; color: white; text-align: left; padding: 10px;">
                    Cast both to STRING
                </button>
            </div>
            <button class="btn" onclick="closeCastDialog()" style="width: 100%;">Cancel</button>
        </div>
    `;

    document.body.appendChild(modal);

    // Close on backdrop click
    modal.addEventListener('click', function(e) {
        if (e.target === modal) {
            closeCastDialog();
        }
    });

    // Close on Escape key
    const escapeHandler = function(e) {
        if (e.key === 'Escape') {
            closeCastDialog();
            document.removeEventListener('keydown', escapeHandler);
        }
    };
    document.addEventListener('keydown', escapeHandler);
}

function createJoinWithCast(leftTable, leftField, rightTable, rightField, castType, leftType, rightType) {
    let castInfo = null;

    if (castType === 'CAST_LEFT') {
        castInfo = { field: 'left', toType: rightType };
    } else if (castType === 'CAST_RIGHT') {
        castInfo = { field: 'right', toType: leftType };
    } else if (castType === 'CAST_STRING') {
        castInfo = { field: 'both', toType: 'STRING' };
    }

    createJoinBetweenFields(leftTable, leftField, rightTable, rightField, castInfo);
    closeCastDialog();
    activeConnector = null;
}

function createJoinBetweenFields(leftTable, leftField, rightTable, rightField, castInfo) {
    const newJoin = {
        leftTable: leftTable,
        leftField: leftField,
        rightTable: rightTable,
        rightField: rightField,
        joinType: 'INNER'
    };

    // Convert cast object to castLeft/castRight for backend compatibility
    if (castInfo) {
        if (castInfo.field === 'left') {
            newJoin.castLeft = castInfo.toType;
        } else if (castInfo.field === 'right') {
            newJoin.castRight = castInfo.toType;
        } else if (castInfo.field === 'both') {
            newJoin.castLeft = castInfo.toType;
            newJoin.castRight = castInfo.toType;
        }
    }

    canvasDesign.joins.push(newJoin);
    renderCanvas();
    showNotification('Join created successfully', 'success');
}

function showFieldConnections(tableIndex, fieldIndex) {
    const table = canvasDesign.tables[tableIndex];
    const field = table.fields[fieldIndex];

    // Find all joins for this field
    const fieldJoins = canvasDesign.joins.filter(j =>
        (j.leftTable === table.sourceName && j.leftField === field.name) ||
        (j.rightTable === table.sourceName && j.rightField === field.name)
    );

    if (fieldJoins.length === 0) {
        showNotification(`No joins for ${table.sourceName}.${field.name}`, 'info');
        return;
    }

    // Show first join's properties
    const joinIndex = canvasDesign.joins.indexOf(fieldJoins[0]);
    selectJoin(joinIndex);
}

function closeCastDialog() {
    const modal = document.getElementById('castDialogModal');
    if (modal) {
        modal.remove();
    }
    activeConnector = null;
}

// ========== JOIN CONTEXT MENU ==========

function showJoinContextMenu(event, joinIndex) {
    // Remove existing menu if any
    const existingMenu = document.getElementById('joinContextMenu');
    if (existingMenu) {
        existingMenu.remove();
    }

    const menu = document.createElement('div');
    menu.id = 'joinContextMenu';
    menu.style.cssText = `
        position: fixed;
        left: ${event.pageX}px;
        top: ${event.pageY}px;
        background: white;
        border: 1px solid #ddd;
        border-radius: 5px;
        box-shadow: 0 2px 10px rgba(0,0,0,0.2);
        z-index: 3000;
        min-width: 150px;
    `;

    menu.innerHTML = `
        <div onclick="selectJoin(${joinIndex}); closeJoinContextMenu();"
             style="padding: 10px 15px; cursor: pointer; border-bottom: 1px solid #f0f0f0;"
             onmouseover="this.style.background='#f0f0f0'"
             onmouseout="this.style.background='white'">
            ✏️ Edit
        </div>
        <div onclick="removeJoin(${joinIndex}); closeJoinContextMenu();"
             style="padding: 10px 15px; cursor: pointer; color: #dc3545;"
             onmouseover="this.style.background='#f0f0f0'"
             onmouseout="this.style.background='white'">
            🗑️ Delete
        </div>
    `;

    document.body.appendChild(menu);

    // Close menu on click outside
    setTimeout(() => {
        document.addEventListener('click', closeJoinContextMenu);
    }, 0);
}

function closeJoinContextMenu() {
    const menu = document.getElementById('joinContextMenu');
    if (menu) {
        menu.remove();
    }
    document.removeEventListener('click', closeJoinContextMenu);
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
    return `
        <div class="step-content active">
            <div class="step-header">
                <h2>🔄 Step 4: Transform SQL</h2>
                <p>Define transformation logic using SQL</p>
            </div>

            <div class="panel">
                <div class="form-group">
                    <label for="resultTable">Result Table Name *</label>
                    <input type="text" id="resultTable" placeholder="e.g., transformed_data" required oninput="validateStep4()">
                    <small>The name of the temporary table to store transformation results</small>
                </div>

                <div class="form-group">
                    <label for="transformSQL">SQL Query *</label>
                    <textarea id="transformSQL" rows="15" placeholder="SELECT * FROM source_table WHERE ..." required oninput="validateStep4()" style="font-family: 'Courier New', monospace; font-size: 13px;"></textarea>
                    <small>Write SQL to transform data from loaded sources. Available tables are shown in Step 3.</small>
                </div>

                <div style="margin-top: 10px;">
                    <button class="btn btn-secondary" onclick="previewTransform()" id="previewTransformBtn">
                        👁️ Preview SQL Result
                    </button>
                    <button class="btn btn-secondary" onclick="useGeneratedSQL()" style="margin-left: 10px;">
                        📋 Use Generated SQL from Step 3
                    </button>
                </div>

                <div id="step4PreviewArea" style="margin-top: 15px; display: none; border: 1px solid #e0e0e0; border-radius: 6px; overflow: hidden;"></div>
            </div>
        </div>
    `;
}

async function loadStep4Data() {
    if (!wailsReady || !window.go) {
        console.log('Wails not ready, skipping Step 4 data load');
        return;
    }

    try {
        const transform = await window.go.main.App.GetTransform();
        console.log('Loaded transform:', transform);
        if (transform) {
            document.getElementById('resultTable').value = transform.resultTable || '';
            document.getElementById('transformSQL').value = transform.sql || '';
        }
        validateStep4();
    } catch (err) {
        console.error('Failed to load transform:', err);
    }
}

async function saveStep4() {
    const transform = {
        resultTable: document.getElementById('resultTable').value.trim(),
        sql: document.getElementById('transformSQL').value.trim(),
    };

    console.log('Saving transform:', transform);

    if (!wailsReady || !window.go) {
        console.warn('Wails not ready, data saved locally only');
        localStorage.setItem('transform', JSON.stringify(transform));
        return;
    }

    try {
        await window.go.main.App.SaveTransform(transform);
        console.log('Transform saved to backend');
    } catch (err) {
        console.error('Failed to save transform:', err);
        if (appMode === 'production') {
            throw err;
        }
    }
}

function validateStep4() {
    const resultTable = document.getElementById('resultTable');
    const transformSQL = document.getElementById('transformSQL');

    const tableValid = resultTable.value.trim().length > 0;
    const sqlValid = transformSQL.value.trim().length > 0;

    // Visual feedback
    resultTable.style.borderColor = tableValid ? '#28a745' : '#dc3545';
    resultTable.style.borderWidth = '2px';
    transformSQL.style.borderColor = sqlValid ? '#28a745' : '#dc3545';
    transformSQL.style.borderWidth = '2px';

    return tableValid && sqlValid;
}

function renderPreviewResult(result, container) {
    if (!container) return;
    container.style.display = 'block';

    if (!result || !result.success) {
        const msg = (result && result.message) ? result.message : 'No data returned';
        container.innerHTML = `<p style="text-align:center;padding:20px;color:#ff6b6b;">${msg}</p>`;
        return;
    }

    if (!result.rows || result.rows.length === 0) {
        container.innerHTML = '<p style="text-align:center;padding:20px;color:#999;">Query returned 0 rows.</p>';
        return;
    }

    let html = '<div style="overflow-x:auto;max-height:300px;overflow-y:auto;"><table style="min-width:100%;border-collapse:collapse;font-size:12px;">';
    html += '<thead><tr style="background:#f5f5f5;border-bottom:2px solid #ddd;">';
    result.columns.forEach(col => {
        html += `<th style="padding:8px;text-align:left;font-weight:600;border-right:1px solid #eee;white-space:nowrap;">${col}</th>`;
    });
    html += '</tr></thead><tbody>';
    result.rows.slice(0, 50).forEach((row, idx) => {
        html += `<tr style="border-bottom:1px solid #eee;${idx % 2 === 0 ? 'background:white;' : 'background:#fafafa;'}">`;
        const values = Array.isArray(row) ? row : result.columns.map(col => row[col]);
        values.forEach(cell => {
            const value = cell === null ? '<span style="color:#999;font-style:italic;">NULL</span>' : String(cell);
            html += `<td style="padding:6px 8px;border-right:1px solid #eee;white-space:nowrap;max-width:300px;overflow:hidden;text-overflow:ellipsis;">${value}</td>`;
        });
        html += '</tr>';
    });
    html += '</tbody></table></div>';
    if (result.rows.length > 50) {
        html += `<p style="text-align:center;margin:6px 0;color:#666;font-size:11px;">Showing first 50 of ${result.rows.length} rows</p>`;
    }
    container.innerHTML = html;
}

async function previewTransform() {
    const sql = document.getElementById('transformSQL').value.trim();
    if (!sql) {
        showNotification('Please enter SQL query first', 'warning');
        return;
    }

    if (!wailsReady || !window.go) {
        showNotification('Preview not available (Wails not ready)', 'error');
        return;
    }

    const previewArea = document.getElementById('step4PreviewArea');

    try {
        showNotification('Executing SQL preview...', 'info');
        const result = await window.go.main.App.PreviewTransform(sql);
        console.log('Step 4 PreviewTransform result:', result);
        renderPreviewResult(result, previewArea);
    } catch (err) {
        console.error('Transform preview error:', err);
        showNotification('Failed to preview transform: ' + err, 'error');
        if (previewArea) {
            previewArea.style.display = 'block';
            previewArea.innerHTML = `<p style="text-align:center;padding:20px;color:#ff6b6b;">Failed to preview: ${err}</p>`;
        }
    }
}

async function useGeneratedSQL() {
    if (!wailsReady || !window.go) {
        showNotification('Cannot load generated SQL (Wails not ready)', 'error');
        return;
    }

    try {
        // Pass canvasDesign to GenerateSQL
        const result = await window.go.main.App.GenerateSQL(canvasDesign);
        if (result && result.sql) {
            document.getElementById('transformSQL').value = result.sql;
            showNotification('Generated SQL loaded successfully', 'success');
            validateStep4();
        } else if (result && result.error) {
            showNotification('Error generating SQL: ' + result.error, 'error');
        } else {
            showNotification('No SQL generated. Please complete Step 3 first.', 'warning');
        }
    } catch (err) {
        console.error('Failed to load generated SQL:', err);
        showNotification('Failed to load generated SQL: ' + err, 'error');
    }
}

function getStep5HTML() {
    return `
        <div class="step-content active">
            <div class="step-header">
                <h2>📤 Step 5: Configure Output</h2>
                <p>Choose where to send transformed data</p>
            </div>

            <div style="display: grid; grid-template-columns: 1fr 1fr; gap: 15px; height: calc(100vh - 250px);">
                <!-- LEFT: Output Configuration -->
                <div class="panel" style="overflow-y: auto;">
                    <div class="form-group">
                        <label>Output Type *</label>
                        <div style="display: flex; flex-wrap: wrap; gap: 10px; margin-top: 10px;">
                            <label style="flex: 1; min-width: 150px;">
                                <input type="radio" name="outputType" value="tdtp_file" onchange="onOutputTypeChange()" checked>
                                📁 TDTP File
                            </label>
                            <label style="flex: 1; min-width: 150px;">
                                <input type="radio" name="outputType" value="rabbitmq" onchange="onOutputTypeChange()">
                                🐰 RabbitMQ
                            </label>
                            <label style="flex: 1; min-width: 150px;">
                                <input type="radio" name="outputType" value="kafka" onchange="onOutputTypeChange()">
                                📨 Kafka
                            </label>
                            <label style="flex: 1; min-width: 150px;">
                                <input type="radio" name="outputType" value="database" onchange="onOutputTypeChange()">
                                🗄️ Database
                            </label>
                            <label style="flex: 1; min-width: 150px;">
                                <input type="radio" name="outputType" value="xlsx" onchange="onOutputTypeChange()">
                                📊 XLSX File
                            </label>
                        </div>
                    </div>

                    <div id="outputConfigPanel" style="margin-top: 20px;">
                        <!-- Dynamic form will be inserted here -->
                    </div>
                </div>

                <!-- RIGHT: Result Preview -->
                <div class="panel" style="overflow-y: auto;">
                    <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 15px;">
                        <h3 style="margin: 0; font-size: 16px;">📊 Query Result Preview</h3>
                        <button onclick="refreshStep5Preview()"
                                style="padding: 6px 12px; font-size: 13px; background: #0066cc; color: white; border: none; border-radius: 3px; cursor: pointer;">
                            🔄 Refresh
                        </button>
                    </div>
                    <div id="step5PreviewArea" style="font-size: 13px; color: #666;">
                        <p style="text-align: center; padding: 40px 20px; color: #999;">
                            Loading preview...
                        </p>
                    </div>
                </div>
            </div>
        </div>
    `;
}

async function loadStep5Data() {
    if (!wailsReady || !window.go) {
        console.log('Wails not ready, using default output type');
        onOutputTypeChange(); // Show default form
        return;
    }

    try {
        const output = await window.go.main.App.GetOutput();
        console.log('Loaded output:', output);
        if (output && output.type) {
            // Select the correct radio button
            const radio = document.querySelector(`input[name="outputType"][value="${output.type}"]`);
            if (radio) {
                radio.checked = true;
            }
            onOutputTypeChange();

            // Load type-specific data
            setTimeout(() => loadOutputFormData(output), 100);
        } else {
            onOutputTypeChange(); // Show default form
        }

        // Load preview
        await refreshStep5Preview();
    } catch (err) {
        console.error('Failed to load output:', err);
        onOutputTypeChange();
    }
}

async function refreshStep5Preview() {
    const previewArea = document.getElementById('step5PreviewArea');
    if (!previewArea) return;

    previewArea.innerHTML = '<p style="text-align: center; padding: 40px 20px; color: #999;">⏳ Loading preview...</p>';

    if (!wailsReady || !window.go) {
        previewArea.innerHTML = '<p style="text-align: center; padding: 40px 20px; color: #ff6b6b;">Backend not ready</p>';
        return;
    }

    try {
        const result = await window.go.main.App.PreviewQueryResult();
        console.log('Step 5 Preview result:', result);
        renderPreviewResult(result, previewArea);
    } catch (err) {
        console.error('Failed to load Step 5 preview:', err);
        previewArea.innerHTML = `<p style="text-align: center; padding: 40px 20px; color: #ff6b6b;">Failed to load preview:<br/>${err}</p>`;
    }
}

function loadOutputFormData(output) {
    switch (output.type) {
        case 'tdtp_file':
            if (output.file) {
                document.getElementById('tdtpDestination').value = output.file.destination || '';
                document.getElementById('tdtpCompression').checked = output.file.compression || false;
                document.getElementById('tdtpCompressLevel').value = output.file.compressLevel || 3;
            }
            break;
        case 'rabbitmq':
            if (output.broker) {
                document.getElementById('rabbitmqConfig').value = output.broker.config || '';
                document.getElementById('rabbitmqQueue').value = output.broker.queue || '';
                document.getElementById('rabbitmqCompression').checked = output.broker.compression || false;
            }
            break;
        case 'kafka':
            if (output.broker) {
                document.getElementById('kafkaBrokers').value = output.broker.config || '';
                document.getElementById('kafkaTopic').value = output.broker.queue || '';
                document.getElementById('kafkaCompression').checked = output.broker.compression || false;
            }
            break;
        case 'database':
            if (output.database) {
                document.getElementById('dbType').value = output.database.type || 'postgres';
                document.getElementById('dbDSN').value = output.database.dsn || '';
                document.getElementById('dbTable').value = output.database.table || '';
                document.getElementById('dbStrategy').value = output.database.strategy || 'replace';
            }
            break;
        case 'xlsx':
            if (output.xlsx) {
                document.getElementById('xlsxDestination').value = output.xlsx.destination || '';
                document.getElementById('xlsxSheet').value = output.xlsx.sheet || 'Sheet1';
            }
            break;
    }
}

async function saveStep5() {
    const outputType = document.querySelector('input[name="outputType"]:checked').value;
    const output = { type: outputType };

    switch (outputType) {
        case 'tdtp_file':
            output.file = {
                destination: document.getElementById('tdtpDestination').value.trim(),
                compression: document.getElementById('tdtpCompression').checked,
                compressLevel: parseInt(document.getElementById('tdtpCompressLevel').value) || 3,
            };
            break;
        case 'rabbitmq':
            output.broker = {
                type: 'rabbitmq',
                config: document.getElementById('rabbitmqConfig').value.trim(),
                queue: document.getElementById('rabbitmqQueue').value.trim(),
                compression: document.getElementById('rabbitmqCompression').checked,
                compressLevel: 3,
            };
            break;
        case 'kafka':
            output.broker = {
                type: 'kafka',
                config: document.getElementById('kafkaBrokers').value.trim(),
                queue: document.getElementById('kafkaTopic').value.trim(),
                compression: document.getElementById('kafkaCompression').checked,
                compressLevel: 3,
            };
            break;
        case 'database':
            output.database = {
                type: document.getElementById('dbType').value,
                dsn: document.getElementById('dbDSN').value.trim(),
                table: document.getElementById('dbTable').value.trim(),
                strategy: document.getElementById('dbStrategy').value,
            };
            break;
        case 'xlsx':
            output.xlsx = {
                destination: document.getElementById('xlsxDestination').value.trim(),
                sheet: document.getElementById('xlsxSheet').value.trim() || 'Sheet1',
            };
            break;
    }

    console.log('Saving output:', output);

    if (!wailsReady || !window.go) {
        console.warn('Wails not ready, data saved locally only');
        localStorage.setItem('output', JSON.stringify(output));
        return;
    }

    try {
        await window.go.main.App.SaveOutput(output);
        console.log('Output saved to backend');
    } catch (err) {
        console.error('Failed to save output:', err);
        if (appMode === 'production') {
            throw err;
        }
    }
}

function onOutputTypeChange() {
    const outputType = document.querySelector('input[name="outputType"]:checked').value;
    const panel = document.getElementById('outputConfigPanel');

    let html = '';
    switch (outputType) {
        case 'tdtp_file':
            html = `
                <h3>TDTP File Output</h3>
                <div class="form-group">
                    <label for="tdtpDestination">Destination Path *</label>
                    <input type="text" id="tdtpDestination" placeholder="/path/to/output.tdtp" required>
                </div>
                <div class="form-group">
                    <label>
                        <input type="checkbox" id="tdtpCompression" checked>
                        Enable Compression
                    </label>
                </div>
                <div class="form-group">
                    <label for="tdtpCompressLevel">Compression Level (1-9)</label>
                    <input type="number" id="tdtpCompressLevel" value="3" min="1" max="9">
                </div>
            `;
            break;
        case 'rabbitmq':
            html = `
                <h3>RabbitMQ Output</h3>
                <div class="form-group">
                    <label for="rabbitmqConfig">Connection String *</label>
                    <input type="text" id="rabbitmqConfig" placeholder="amqp://user:pass@localhost:5672/" required>
                </div>
                <div class="form-group">
                    <label for="rabbitmqQueue">Queue Name *</label>
                    <input type="text" id="rabbitmqQueue" placeholder="tdtp_queue" required>
                </div>
                <div class="form-group">
                    <label>
                        <input type="checkbox" id="rabbitmqCompression">
                        Enable Compression
                    </label>
                </div>
            `;
            break;
        case 'kafka':
            html = `
                <h3>Kafka Output</h3>
                <div class="form-group">
                    <label for="kafkaBrokers">Brokers *</label>
                    <input type="text" id="kafkaBrokers" placeholder="localhost:9092,localhost:9093" required>
                </div>
                <div class="form-group">
                    <label for="kafkaTopic">Topic *</label>
                    <input type="text" id="kafkaTopic" placeholder="tdtp_topic" required>
                </div>
                <div class="form-group">
                    <label>
                        <input type="checkbox" id="kafkaCompression">
                        Enable Compression
                    </label>
                </div>
            `;
            break;
        case 'database':
            html = `
                <h3>Database Output</h3>
                <div class="form-group">
                    <label for="dbType">Database Type *</label>
                    <select id="dbType">
                        <option value="postgres">PostgreSQL</option>
                        <option value="mssql">MS SQL Server</option>
                        <option value="mysql">MySQL</option>
                        <option value="sqlite">SQLite</option>
                    </select>
                </div>
                <div class="form-group">
                    <label for="dbDSN">Connection String (DSN) *</label>
                    <input type="text" id="dbDSN" placeholder="postgres://user:pass@localhost/dbname" required>
                </div>
                <div class="form-group">
                    <label for="dbTable">Target Table *</label>
                    <input type="text" id="dbTable" placeholder="output_table" required>
                </div>
                <div class="form-group">
                    <label for="dbStrategy">Write Strategy *</label>
                    <select id="dbStrategy">
                        <option value="replace">Replace (DROP + CREATE)</option>
                        <option value="ignore">Ignore Duplicates</option>
                        <option value="copy">Copy (Append)</option>
                        <option value="fail">Fail on Duplicates</option>
                    </select>
                </div>
            `;
            break;
        case 'xlsx':
            html = `
                <h3>XLSX File Output</h3>
                <div class="form-group">
                    <label for="xlsxDestination">Destination Path *</label>
                    <input type="text" id="xlsxDestination" placeholder="/path/to/output.xlsx" required>
                </div>
                <div class="form-group">
                    <label for="xlsxSheet">Sheet Name</label>
                    <input type="text" id="xlsxSheet" value="Sheet1" placeholder="Sheet1">
                </div>
            `;
            break;
    }

    panel.innerHTML = html;
}

function getStep6HTML() {
    return `
        <div class="step-content active">
            <div class="step-header">
                <h2>⚙️ Step 6: Settings</h2>
                <p>Configure performance, audit, and error handling</p>
            </div>

            <!-- Performance Settings -->
            <div class="panel">
                <h3>⚡ Performance</h3>
                <div class="form-row">
                    <div class="form-group">
                        <label for="timeout">Timeout (seconds)</label>
                        <input type="number" id="timeout" value="300" min="1">
                        <small>Maximum execution time</small>
                    </div>
                    <div class="form-group">
                        <label for="batchSize">Batch Size (rows)</label>
                        <input type="number" id="batchSize" value="1000" min="100">
                        <small>Rows per batch</small>
                    </div>
                </div>
                <div class="form-row">
                    <div class="form-group">
                        <label for="maxMemoryMB">Max Memory (MB)</label>
                        <input type="number" id="maxMemoryMB" value="512" min="64">
                        <small>Memory limit for processing</small>
                    </div>
                    <div class="form-group">
                        <label>
                            <input type="checkbox" id="parallelSources">
                            Parallel Sources
                        </label>
                        <small>Load sources in parallel</small>
                    </div>
                </div>
            </div>

            <!-- Audit Settings -->
            <div class="panel">
                <h3>📝 Audit</h3>
                <div class="form-group">
                    <label>
                        <input type="checkbox" id="auditEnabled">
                        Enable Audit Logging
                    </label>
                </div>
                <div class="form-group">
                    <label for="auditLogFile">Log File Path</label>
                    <input type="text" id="auditLogFile" placeholder="/path/to/audit.log">
                </div>
                <div class="form-row">
                    <div class="form-group">
                        <label>
                            <input type="checkbox" id="logQueries">
                            Log SQL Queries
                        </label>
                    </div>
                    <div class="form-group">
                        <label>
                            <input type="checkbox" id="logErrors" checked>
                            Log Errors
                        </label>
                    </div>
                </div>
            </div>

            <!-- Error Handling Settings -->
            <div class="panel">
                <h3>🚨 Error Handling</h3>
                <div class="form-row">
                    <div class="form-group">
                        <label for="onSourceError">On Source Error</label>
                        <select id="onSourceError">
                            <option value="fail">Fail Immediately</option>
                            <option value="continue">Continue Processing</option>
                        </select>
                    </div>
                    <div class="form-group">
                        <label for="onTransformError">On Transform Error</label>
                        <select id="onTransformError">
                            <option value="fail">Fail Immediately</option>
                            <option value="continue">Continue Processing</option>
                        </select>
                    </div>
                </div>
                <div class="form-row">
                    <div class="form-group">
                        <label for="onExportError">On Export Error</label>
                        <select id="onExportError">
                            <option value="fail">Fail Immediately</option>
                            <option value="continue">Continue Processing</option>
                        </select>
                    </div>
                    <div class="form-group">
                        <label for="retryCount">Retry Count</label>
                        <input type="number" id="retryCount" value="3" min="0" max="10">
                    </div>
                </div>
                <div class="form-group">
                    <label for="retryDelaySec">Retry Delay (seconds)</label>
                    <input type="number" id="retryDelaySec" value="5" min="1" max="60">
                </div>
            </div>
        </div>
    `;
}

async function loadStep6Data() {
    if (!wailsReady || !window.go) {
        console.log('Wails not ready, using default settings');
        setDefaultSettings();
        return;
    }

    try {
        const settings = await window.go.main.App.GetSettings();
        console.log('Loaded settings:', settings);

        if (settings && settings.performance) {
            // Performance
            document.getElementById('timeout').value = settings.performance.timeout || 300;
            document.getElementById('batchSize').value = settings.performance.batchSize || 1000;
            document.getElementById('maxMemoryMB').value = settings.performance.maxMemoryMB || 512;
            document.getElementById('parallelSources').checked = settings.performance.parallelSources || false;

            // Audit
            if (settings.audit) {
                document.getElementById('auditEnabled').checked = settings.audit.enabled || false;
                document.getElementById('auditLogFile').value = settings.audit.logFile || '';
                document.getElementById('logQueries').checked = settings.audit.logQueries || false;
                document.getElementById('logErrors').checked = settings.audit.logErrors !== false; // default true
            }

            // Error Handling
            if (settings.errorHandling) {
                document.getElementById('onSourceError').value = settings.errorHandling.onSourceError || 'fail';
                document.getElementById('onTransformError').value = settings.errorHandling.onTransformError || 'fail';
                document.getElementById('onExportError').value = settings.errorHandling.onExportError || 'fail';
                document.getElementById('retryCount').value = settings.errorHandling.retryCount || 3;
                document.getElementById('retryDelaySec').value = settings.errorHandling.retryDelaySec || 5;
            }
        } else {
            setDefaultSettings();
        }
    } catch (err) {
        console.error('Failed to load settings:', err);
        setDefaultSettings();
    }
}

function setDefaultSettings() {
    // Performance defaults
    document.getElementById('timeout').value = 300;
    document.getElementById('batchSize').value = 1000;
    document.getElementById('maxMemoryMB').value = 512;
    document.getElementById('parallelSources').checked = false;

    // Audit defaults
    document.getElementById('auditEnabled').checked = false;
    document.getElementById('auditLogFile').value = '';
    document.getElementById('logQueries').checked = false;
    document.getElementById('logErrors').checked = true;

    // Error Handling defaults
    document.getElementById('onSourceError').value = 'fail';
    document.getElementById('onTransformError').value = 'fail';
    document.getElementById('onExportError').value = 'fail';
    document.getElementById('retryCount').value = 3;
    document.getElementById('retryDelaySec').value = 5;
}

async function saveStep6() {
    const settings = {
        performance: {
            timeout: parseInt(document.getElementById('timeout').value) || 300,
            batchSize: parseInt(document.getElementById('batchSize').value) || 1000,
            maxMemoryMB: parseInt(document.getElementById('maxMemoryMB').value) || 512,
            parallelSources: document.getElementById('parallelSources').checked,
        },
        workspace: {
            type: 'sqlite',
            mode: ':memory:',
        },
        audit: {
            enabled: document.getElementById('auditEnabled').checked,
            logFile: document.getElementById('auditLogFile').value.trim(),
            logQueries: document.getElementById('logQueries').checked,
            logErrors: document.getElementById('logErrors').checked,
        },
        errorHandling: {
            onSourceError: document.getElementById('onSourceError').value,
            onTransformError: document.getElementById('onTransformError').value,
            onExportError: document.getElementById('onExportError').value,
            retryCount: parseInt(document.getElementById('retryCount').value) || 3,
            retryDelaySec: parseInt(document.getElementById('retryDelaySec').value) || 5,
        },
        dataProcessors: {
            // Empty for now - Phase 4 feature
        },
    };

    console.log('Saving settings:', settings);

    if (!wailsReady || !window.go) {
        console.warn('Wails not ready, data saved locally only');
        localStorage.setItem('settings', JSON.stringify(settings));
        return;
    }

    try {
        await window.go.main.App.SaveSettings(settings);
        console.log('Settings saved to backend');
    } catch (err) {
        console.error('Failed to save settings:', err);
        if (appMode === 'production') {
            throw err;
        }
    }
}

function getStep7HTML() {
    return `
        <div class="step-content active">
            <div class="step-header">
                <h2>✅ Step 7: Review & Generate YAML</h2>
                <p>Review configuration and generate TDTP pipeline YAML</p>
            </div>

            <div class="panel">
                <h3>📋 Configuration Summary</h3>
                <div id="configSummary" style="background: #f8f9fa; padding: 15px; border-radius: 3px; margin-bottom: 20px;">
                    <p style="color: #666;">Loading configuration summary...</p>
                </div>

                <div style="display: flex; gap: 10px; flex-wrap: wrap; margin-bottom: 10px;">
                    <button class="btn btn-primary" onclick="generateAndShowYAML()" style="flex: 1;">
                        📄 Generate YAML
                    </button>
                    <button class="btn btn-secondary" onclick="saveYAMLToFile()" style="flex: 1;">
                        💾 Save YAML to File
                    </button>
                    <button class="btn btn-secondary" onclick="copyYAMLToClipboard()" style="flex: 1;">
                        📋 Copy to Clipboard
                    </button>
                </div>
                <div style="padding: 12px; background: #eef4ff; border: 1px solid #b8d0f0; border-radius: 3px;">
                    <button class="btn btn-primary" onclick="saveToRepository()" style="width: 100%; background: #0055aa;">
                        📚 Save to Repository (configs.db)
                    </button>
                    <p style="margin: 6px 0 0 0; font-size: 10px; color: #5a7fa0; text-align: center;">
                        Saves YAML + full canvas state (visibility, filters, JOINs, positions)
                    </p>
                </div>
            </div>

            <!-- YAML Preview Modal -->
            <div id="yamlPreviewModal" class="modal">
                <div class="modal-content" style="max-width: 800px; max-height: 80vh;">
                    <div class="modal-header">
                        <h3>Generated YAML</h3>
                        <button class="btn-close" onclick="closeYAMLPreview()">×</button>
                    </div>
                    <div class="modal-body">
                        <pre id="yamlPreview" style="background: #f8f9fa; padding: 15px; overflow: auto; max-height: 60vh; font-family: 'Courier New', monospace; font-size: 12px;"></pre>
                    </div>
                    <div class="modal-footer">
                        <button class="btn btn-secondary" onclick="copyYAMLToClipboard()">📋 Copy</button>
                        <button class="btn btn-primary" onclick="closeYAMLPreview()">Close</button>
                    </div>
                </div>
            </div>
        </div>
    `;
}

async function loadStep7Data() {
    await renderConfigSummary();
}

async function saveStep7() {
    // No data to save on Step 7, it's a review step
    console.log('Step 7 has no data to save');
}

async function renderConfigSummary() {
    const summaryDiv = document.getElementById('configSummary');

    if (!wailsReady || !window.go) {
        summaryDiv.innerHTML = '<p style="color: #dc3545;">Cannot load configuration (Wails not ready)</p>';
        return;
    }

    try {
        const pipelineInfo = await window.go.main.App.GetPipelineInfo();
        const sources = await window.go.main.App.GetSources();
        const transform = await window.go.main.App.GetTransform();
        const output = await window.go.main.App.GetOutput();
        const settings = await window.go.main.App.GetSettings();

        let html = '<table style="width: 100%; font-size: 13px;">';

        // Pipeline Info
        html += `<tr><td style="padding: 5px; font-weight: bold;">Pipeline Name:</td><td style="padding: 5px;">${pipelineInfo.name || '<span style="color: #dc3545;">Not set</span>'}</td></tr>`;
        html += `<tr><td style="padding: 5px; font-weight: bold;">Version:</td><td style="padding: 5px;">${pipelineInfo.version || '1.0'}</td></tr>`;
        if (pipelineInfo.description) {
            html += `<tr><td style="padding: 5px; font-weight: bold;">Description:</td><td style="padding: 5px;">${pipelineInfo.description}</td></tr>`;
        }

        // Sources
        html += `<tr><td style="padding: 5px; font-weight: bold;">Sources:</td><td style="padding: 5px;">${sources.length} source(s) configured</td></tr>`;
        if (sources.length > 0) {
            sources.forEach((src, idx) => {
                html += `<tr><td style="padding: 5px 5px 5px 20px;">Source ${idx + 1}:</td><td style="padding: 5px;">${src.name} (${src.type})</td></tr>`;
            });
        }

        // Transform
        if (transform && transform.resultTable) {
            html += `<tr><td style="padding: 5px; font-weight: bold;">Transform:</td><td style="padding: 5px;">Result table: ${transform.resultTable}</td></tr>`;
            html += `<tr><td style="padding: 5px 5px 5px 20px;">SQL:</td><td style="padding: 5px;"><code style="font-size: 11px;">${transform.sql.substring(0, 100)}${transform.sql.length > 100 ? '...' : ''}</code></td></tr>`;
        } else {
            html += `<tr><td style="padding: 5px; font-weight: bold;">Transform:</td><td style="padding: 5px; color: #dc3545;">Not configured</td></tr>`;
        }

        // Output
        if (output && output.type) {
            html += `<tr><td style="padding: 5px; font-weight: bold;">Output:</td><td style="padding: 5px;">${output.type}</td></tr>`;
            if (output.type === 'tdtp_file' && output.file) {
                html += `<tr><td style="padding: 5px 5px 5px 20px;">Destination:</td><td style="padding: 5px;">${output.file.destination}</td></tr>`;
            } else if (output.type === 'rabbitmq' && output.broker) {
                html += `<tr><td style="padding: 5px 5px 5px 20px;">Queue:</td><td style="padding: 5px;">${output.broker.queue}</td></tr>`;
            } else if (output.type === 'kafka' && output.broker) {
                html += `<tr><td style="padding: 5px 5px 5px 20px;">Topic:</td><td style="padding: 5px;">${output.broker.queue}</td></tr>`;
            } else if (output.type === 'database' && output.database) {
                html += `<tr><td style="padding: 5px 5px 5px 20px;">Table:</td><td style="padding: 5px;">${output.database.table}</td></tr>`;
            }
        } else {
            html += `<tr><td style="padding: 5px; font-weight: bold;">Output:</td><td style="padding: 5px; color: #dc3545;">Not configured</td></tr>`;
        }

        // Settings summary
        if (settings && settings.performance) {
            html += `<tr><td style="padding: 5px; font-weight: bold;">Performance:</td><td style="padding: 5px;">Timeout: ${settings.performance.timeout}s, Batch: ${settings.performance.batchSize}, Memory: ${settings.performance.maxMemoryMB}MB</td></tr>`;
        }

        html += '</table>';
        summaryDiv.innerHTML = html;
    } catch (err) {
        console.error('Failed to render config summary:', err);
        summaryDiv.innerHTML = `<p style="color: #dc3545;">Error loading configuration: ${err}</p>`;
    }
}

let generatedYAML = '';

async function generateAndShowYAML() {
    if (!wailsReady || !window.go) {
        showNotification('YAML generation not available (Wails not ready)', 'error');
        return;
    }

    try {
        showNotification('Generating YAML...', 'info');

        // Save all current step data first
        await saveCurrentStep();

        const yaml = await window.go.main.App.GenerateYAML();
        generatedYAML = yaml;

        document.getElementById('yamlPreview').textContent = yaml;
        document.getElementById('yamlPreviewModal').style.display = 'flex';

        showNotification('YAML generated successfully', 'success');
    } catch (err) {
        console.error('Failed to generate YAML:', err);
        showNotification('Failed to generate YAML: ' + err, 'error');
    }
}

async function saveYAMLToFile() {
    if (!wailsReady || !window.go) {
        showNotification('File save not available (Wails not ready)', 'error');
        return;
    }

    const btn = event && event.currentTarget;
    const originalText = btn ? btn.textContent : null;
    if (btn) { btn.disabled = true; btn.textContent = '⏳ Opening dialog...'; }

    try {
        if (!generatedYAML) {
            await generateAndShowYAML();
        }
        const result = await window.go.main.App.SaveConfigurationFile();
        if (result.success) {
            showNotification(`Saved: ${result.filename}  →  ${result.dir}`, 'success');
        } else if (result.error && !result.error.includes('cancelled')) {
            showNotification(`Failed to save: ${result.error}`, 'error');
        }
    } catch (err) {
        console.error('Failed to save YAML:', err);
        showNotification('Failed to save YAML: ' + err, 'error');
    } finally {
        if (btn) { btn.disabled = false; btn.textContent = originalText; }
    }
}

async function copyYAMLToClipboard() {
    if (!generatedYAML) {
        await generateAndShowYAML();
    }

    try {
        await navigator.clipboard.writeText(generatedYAML);
        showNotification('YAML copied to clipboard', 'success');
    } catch (err) {
        console.error('Failed to copy to clipboard:', err);
        showNotification('Failed to copy to clipboard', 'error');
    }
}

function closeYAMLPreview() {
    document.getElementById('yamlPreviewModal').style.display = 'none';
}

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
