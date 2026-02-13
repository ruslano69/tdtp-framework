// TDTP X-Ray - Wizard Navigation

let currentStep = 1;
const totalSteps = 7;
let appMode = 'production'; // production or mock

// Initialize wizard
document.addEventListener('DOMContentLoaded', function() {
    loadStep(1);
    updateNavigation();
});

// Switch between Mock and Production modes
async function switchMode(mode) {
    appMode = mode;
    try {
        await window.go.main.App.SetMode(mode);
        console.log(`Switched to ${mode} mode`);

        // Show notification
        showNotification(
            mode === 'mock'
                ? '🧪 Mock Mode: You can experiment freely. Warnings only.'
                : '🏭 Production Mode: Strict validation enabled.',
            mode === 'mock' ? 'warning' : 'info'
        );
    } catch (err) {
        console.error('Failed to switch mode:', err);
    }
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
    try {
        const result = await window.go.main.App.ValidateStep(currentStep);
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
    try {
        const info = await window.go.main.App.GetPipelineInfo();
        if (info.name) {
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

    try {
        await window.go.main.App.SavePipelineInfo(info);
        console.log('Pipeline info saved:', info);
    } catch (err) {
        console.error('Failed to save pipeline info:', err);
        throw err;
    }
}

// ========== STEP 2: Sources (Placeholder) ==========

function getStep2HTML() {
    return `
        <div class="step-content active">
            <div class="step-header">
                <h2>🔌 Step 2: Configure Sources</h2>
                <p>Add data sources for your pipeline</p>
            </div>

            <div class="panel">
                <p class="text-center" style="padding: 40px; color: #666;">
                    🚧 Step 2 UI coming soon...<br>
                    <small>Will include: source selection, connection testing, preview</small>
                </p>
            </div>
        </div>
    `;
}

function loadStep2Data() {}
async function saveStep2() {}

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
