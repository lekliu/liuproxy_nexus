import { serversCache, logMessages } from './state.js';

// --- UI Element References ---
const serverListBody = document.getElementById('server-list');
const dialog = document.getElementById('server-dialog');
export const form = document.getElementById('server-form');
const dialogTitle = document.getElementById('dialog-title');
const serverIdInput = document.getElementById('server-id');
const serverTypeSelect = document.getElementById('type');
const networkSelect = document.getElementById('network');
const vlessSecuritySelect = document.getElementById('security');
const transportSelect = document.getElementById('transport');
const multiplexCheckbox = document.getElementById('multiplex');
const logContent = document.getElementById('log-content');
const applyBanner = document.getElementById('apply-changes-banner');
const ruleDialog = document.getElementById('rule-dialog');
export const ruleForm = document.getElementById('rule-form');
const ruleDialogTitle = document.getElementById('rule-dialog-title');
const ruleIndexInput = document.getElementById('rule-index');
const ruleTargetSelect = document.getElementById('rule-target');

/**
 * Escapes HTML to prevent XSS.
 * @param {string} str - The string to escape.
 * @returns {string}
 */
function escapeHTML(str) {
    if (str === null || str === undefined) return '';
    return str.toString()
        .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;').replace(/'/g, '&#039;');
}

/**
 * Formats speed in bytes/sec into a human-readable string (KB/s, MB/s).
 * @param {number} bytesPerSecond
 * @returns {string}
 */
function formatSpeed(bytesPerSecond) {
    if (isNaN(bytesPerSecond) || bytesPerSecond < 0) return '0 B/s';
    if (bytesPerSecond < 1024) return `${bytesPerSecond.toFixed(0)} B/s`;
    const kbps = bytesPerSecond / 1024;
    if (kbps < 1024) return `${kbps.toFixed(1)} KB/s`;
    const mbps = kbps / 1024;
    return `${mbps.toFixed(2)} MB/s`;
}

/**
 * Renders the server list table based on the current serversCache.
 * This is the core rendering function for the server list.
 */
export function renderServers(filterText = '', sortBy = 'remarks', sortDir = 'asc') {
    // 1. Filter data
    const filteredServers = serversCache.filter(server => {
        const searchText = filterText.toLowerCase();
        return server.remarks.toLowerCase().includes(searchText) ||
               server.address.toLowerCase().includes(searchText);
    });

    // 2. Sort data
    filteredServers.sort((a, b) => {
        let valA, valB;
        if (sortBy === 'latency') {
            valA = a.latency < 0 ? Infinity : a.latency;
            valB = b.latency < 0 ? Infinity : b.latency;
        } else { // default to remarks
            valA = (a.remarks || '').toLowerCase();
            valB = (b.remarks || '').toLowerCase();
        }

        if (valA < valB) return sortDir === 'asc' ? -1 : 1;
        if (valA > valB) return sortDir === 'asc' ? 1 : -1;
        return 0;
    });

    // 3. Render table body
    serverListBody.innerHTML = '';
    filteredServers.forEach(server => {
        const row = document.createElement('tr');

        // Determine row class based on state
        let rowClass = 'inactive-row';
        if (server.active) {
            rowClass = 'active-row';
            if (server.health === 2) { // StatusDown
                rowClass = 'down-row';
            }
        }
        row.className = rowClass;
        row.dataset.id = server.id;

        // --- Column 1: Status Indicator ---
        let statusIndicator = '';
        if (server.active) {
            let statusClass = 'status-unknown';
            if (server.health === 1) statusClass = 'status-up';
            if (server.health === 2) statusClass = 'status-down';
            statusIndicator = `<span class="status-indicator ${statusClass}"></span>`;
        }

        // --- Column 2: Remarks with Type Icon ---
        let typeIcon = '';
        switch (server.type) {
            case 'goremote': typeIcon = '🚀'; break;
            case 'vless': typeIcon = '🚘'; break;
            case 'worker': typeIcon = '🚖'; break;
            case 'http': typeIcon = '🌐'; break;
        }
        const remarksHTML = `${escapeHTML(server.remarks)} <span class="type-icon">${typeIcon}</span>`;

        // --- Column 4: Latency ---
        let latencyHTML = '-';
        if (server.active) {
            if (server.latency >= 0) {
                latencyHTML = `${server.latency}ms`;
            } else if (server.health === 2) { // If it's down, show FAIL
                latencyHTML = `<span class="latency-fail">FAIL</span>`;
            }
        }

        // --- Column 5: Activity Indicator ---
        let upRate = 0, downRate = 0;
        if (server.active && server.updateTime > server.lastUpdateTime) {
            const timeDiff = (server.updateTime - server.lastUpdateTime) / 1000;
            if (timeDiff > 0) {
                upRate = Math.max(0, (server.uplink - server.lastUplink)) / timeDiff;
                downRate = Math.max(0, (server.downlink - server.lastDownlink)) / timeDiff;
            }
        }
        // Map rate to a percentage height (logarithmic scale)
        const upHeight = Math.min(100, Math.log2(upRate / 1024 + 1) * 10); // kbps based log scale
        const downHeight = Math.min(100, Math.log2(downRate / 1024 + 1) * 10);

        const activityHTML = `
            <div class="activity-container" title="↑ ${formatSpeed(upRate)} | ↓ ${formatSpeed(downRate)}">
                <div class="activity-bar uplink" style="height: ${upHeight}%;"></div>
                <div class="activity-bar downlink" style="height: ${downHeight}%;"></div>
            </div>
        `;

        // --- Column 6: Exit IP ---
        const exitIpHTML = server.active && server.exitIP ? escapeHTML(server.exitIP) : '-';

        // --- Column 7: Actions ---
        const primaryActionClass = server.active ? 'deactivate-btn' : 'primary-action';
        const primaryActionText = server.active ? 'Deactivate' : 'Activate';

        const actionsHTML = `
            <div class="action-btn-group">
                <button class="${primaryActionClass} toggle-active-btn" data-id="${server.id}" data-active="${!server.active}">${primaryActionText}</button>
                <button class="edit-btn" data-id="${server.id}">Edit</button>
                <button class="copy-btn" data-id="${server.id}">Copy</button>
                <button class="delete-btn" data-id="${server.id}">Delete</button>
            </div>
        `;

        // --- Assemble Row ---
        row.innerHTML = `
            <td>${statusIndicator}</td>
            <td title="${escapeHTML(server.remarks)}">${remarksHTML}</td>
            <td title="${escapeHTML(server.address)}:${server.port}">${escapeHTML(server.address)}:${server.port}</td>
            <td>${latencyHTML}</td>
            <td>${activityHTML}</td>
            <td>${exitIpHTML}</td>
            <td>${actionsHTML}</td>
        `;
        serverListBody.appendChild(row);
    });

     // --- NEW: Update sort indicators in table header ---
    document.querySelectorAll('th[data-sortable="true"]').forEach(th => {
        const indicator = th.querySelector('.sort-indicator');
        if (th.id === `${sortBy}-header`) {
            indicator.classList.add('active');
            indicator.textContent = sortDir === 'asc' ? '▲' : '▼';
        } else {
            indicator.classList.remove('active');
            indicator.textContent = '';
        }
    });
}


// --- Functions for dialogs and forms ---

export function renderLogPanel() {
    if (logContent) {
        logContent.textContent = logMessages.join('\n');
        logContent.scrollTop = logContent.scrollHeight;
    }
}

export function updateStatusMessage(message) {
    const timestamp = new Date().toLocaleTimeString();
    logMessages.push(`[${timestamp}] [UI] ${message}`);
    if (logMessages.length > 50) {
        logMessages.shift();
    }
    renderLogPanel();
}

export function showDialog(server = null) {
    form.reset();
    dialogTitle.textContent = server ? 'Edit Server' : 'Add Server';
    serverIdInput.value = server ? server.id : '';

    if (server) {
        Object.keys(server).forEach(key => {
            const input = form.elements[key];
            if (input) {
                if (input.type === 'checkbox') {
                    input.checked = !!server[key];
                } else {
                    input.value = server[key] || '';
                }
            }
        });
        if (server.type === 'goremote' && !form.elements.transport.value) {
            form.elements.transport.value = 'tcp';
        }
    } else {
        serverTypeSelect.value = 'goremote';
        transportSelect.value = 'tcp';
    }
    updateFormVisibility();
    dialog.showModal();
}

export function closeDialog() {
    dialog.close();
}

export function updateFormVisibility() {
    const type = serverTypeSelect.value;
    const isGoRemote = type === 'goremote';
    const isWorker = type === 'worker';
    const isVless = type === 'vless';
    const isHttp = type === 'http';

    document.getElementById('goremote-fields').style.display = isGoRemote ? '' : 'none';
    document.getElementById('worker-fields').style.display = isWorker ? '' : 'none';
    document.getElementById('vless-fields').style.display = isVless ? '' : 'none';
    document.getElementById('http-fields').style.display = isHttp ? '' : 'none';

    // WebSocket fields visibility
    const goremoteTransport = transportSelect.value;
    const vlessNetwork = networkSelect.value;
    document.getElementById('common-ws-fields').style.display = (isGoRemote && goremoteTransport === 'ws') || isWorker || (isVless && vlessNetwork === 'ws') ? '' : 'none';

    // Multiplexing logic for GoRemote
    if (isGoRemote) {
        const isWebSocket = goremoteTransport === 'ws';
        multiplexCheckbox.checked = isWebSocket || multiplexCheckbox.checked;
        multiplexCheckbox.disabled = isWebSocket;
    }

    // VLESS specific fields
    if (isVless) {
        const securityType = vlessSecuritySelect.value;
        const isTls = securityType === 'tls';
        const isReality = securityType === 'reality';

        document.getElementById('vless-grpc-fields').style.display = (vlessNetwork === 'grpc') ? '' : 'none';
        document.getElementById('sni-wrapper').style.display = (isTls || isReality) ? '' : 'none';
        document.getElementById('fingerprint-wrapper').style.display = (isTls || isReality) ? '' : 'none';
        document.getElementById('publicKey-wrapper').style.display = isReality ? '' : 'none';
        document.getElementById('shortId-wrapper').style.display = isReality ? '' : 'none';
    }
}

export function getFormServerData() {
    const formData = new FormData(form);
    const serverData = Object.fromEntries(formData.entries());
    serverData.multiplex = form.elements.multiplex.checked;

    if (serverData.type === 'goremote' && serverData.transport === 'ws') {
        serverData.multiplex = true;
    }
    if (serverData.type === 'vless' && serverData.network === 'grpc') {
        delete serverData.path;
        delete serverData.host;
        delete serverData.scheme;
    }

    serverData.port = parseInt(serverData.port, 10) || 0;
    delete serverData.active;
    return serverData;
}

export function showApplyBanner() {
    if (applyBanner) applyBanner.style.display = 'flex';
}

export function hideApplyBanner() {
    if (applyBanner) applyBanner.style.display = 'none';
}

export function populateRuleTargetOptions(servers) {
    ruleTargetSelect.innerHTML = `<option value="DIRECT">DIRECT</option><option value="REJECT">REJECT</option>`;
    servers.forEach(server => {
        const option = document.createElement('option');
        option.value = server.remarks;
        option.textContent = server.remarks;
        ruleTargetSelect.appendChild(option);
    });
}

export function showRuleDialog(rule = null, index = null) {
    ruleForm.reset();
    const fetchClientsBtn = document.getElementById('fetch-clients-btn');
    const fetchTargetsBtn = document.getElementById('fetch-targets-btn');

    const ruleType = rule ? rule.type : ruleForm.elements.type.value;
    fetchClientsBtn.style.display = ruleType === 'source_ip' ? 'inline-block' : 'none';
    fetchTargetsBtn.style.display = ruleType !== 'source_ip' ? 'inline-block' : 'none';

    ruleDialogTitle.textContent = rule ? 'Edit Rule' : 'Add Rule';
    ruleIndexInput.value = index !== null ? index : '';

    if (rule) {
        ruleForm.elements.type.value = rule.type;
        ruleForm.elements.target.value = rule.target;
        ruleForm.elements.priority.value = rule.priority;
        if (Array.isArray(rule.value)) {
            ruleForm.elements.value.value = rule.value.join('\n');
        }
    }
    ruleDialog.showModal();
}

export function closeRuleDialog() {
    ruleDialog.close();
}

export function getRuleFormData() {
    const formData = new FormData(ruleForm);
    const valueText = formData.get('value') || '';
    const values = valueText.split('\n').map(v => v.trim()).filter(Boolean);

    const ruleData = {
        priority: parseInt(formData.get('priority'), 10) || 99,
        type: formData.get('type'),
        value: values,
        target: formData.get('target'),
    };
    const indexStr = formData.get('rule-index');
    return {
        data: ruleData,
        index: indexStr ? parseInt(indexStr, 10) : null
    };
}