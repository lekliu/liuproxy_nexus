import { fetchAllSettings, saveSettings, fetchAvailableClientIPs, fetchRecentTargets } from './api.js';
import { updateStatusMessage, showRuleDialog, populateRuleTargetOptions } from './ui.js';
import { serversCache } from './state.js';

// ---  API 调用 ---
async function fetchSystemEnv() {
    const response = await fetch('/api/system/env');
    if (!response.ok) {
        throw new Error(`Failed to fetch system env: ${response.status} ${response.statusText}`);
    }
    return response.json();
}

// ---  UI 填充函数 ---
function populateDeploymentSettings(envSettings) {
    const tcpInput = document.getElementById('env-tcp-enabled');
    const udpInput = document.getElementById('env-udp-enabled');

    tcpInput.value = envSettings.tcp_enabled ? 'Enabled' : 'Disabled';
    udpInput.value = envSettings.udp_enabled ? 'Enabled' : 'Disabled';

    // 根据状态改变样式
    tcpInput.style.color = envSettings.tcp_enabled ? 'var(--status-up-color)' : 'var(--status-down-color)';
    udpInput.style.color = envSettings.udp_enabled ? 'var(--status-up-color)' : 'var(--status-down-color)';

    document.getElementById('env-tproxy-port').value = envSettings.tproxy_port;
    document.getElementById('env-excluded-ips').value = (envSettings.excluded_ips || '').replace(/,/g, ',\n');
}

// --- UI Element References ---
const gatewaySettingsForm = document.getElementById('gateway-settings-form');
const routingSettingsForm = document.getElementById('routing-settings-form');
const stickyRulesTextarea = document.getElementById('sticky_rules');
const ruleListBody = document.getElementById('rule-list-body');

// --- 新增防火墙 UI 引用 ---
const firewallSettingsForm = document.getElementById('firewall-settings-form');
const firewallRuleListBody = document.getElementById('firewall-rule-list-body');
const firewallRuleDialog = document.getElementById('firewall-rule-dialog');
const firewallRuleForm = document.getElementById('firewall-rule-form');


// --- State ---
let routingRulesCache = []; // Data Model: The single source of truth for all rules.
let filterText = '';          // View State: Current text for value filtering.
let filterType = 'all';       // View State: Current selected type for filtering.
let sortDirection = 'asc';    // View State: Current sort direction ('asc' or 'desc').
// --- 新增防火墙状态 ---
let firewallRulesCache = [];

/**
 * Loads all settings from the backend and populates the form.
 */
export async function loadSettings() {
    try {
        const settings = await fetchAllSettings();
        if (settings) {
            if (settings.gateway) {
                populateGatewaySettings(settings.gateway);
            }
            if (settings.routing) {
                routingRulesCache = JSON.parse(JSON.stringify(settings.routing.rules || []));
                renderRulesTable(); // Initial render
            }
            if (settings.firewall) {
                populateFirewallSettings(settings.firewall);
            }
        }
        // 加载并填充环境变量
        const envSettings = await fetchSystemEnv();
        if (envSettings) {
            populateDeploymentSettings(envSettings);
        }
    } catch (error) {
        console.error('Failed to load settings:', error);
        updateStatusMessage('Error: Could not load system settings.', 'failed');
    }
}

/**
 * Populates the Gateway Settings card with data.
 * @param {object} gatewaySettings - The gateway settings object from the API.
 */
function populateGatewaySettings(gatewaySettings) {
    const form = gatewaySettingsForm;
    // Set radio button for mode
    const mode = gatewaySettings.sticky_session_mode || 'disabled';
    const radio = form.querySelector(`input[name="sticky_session_mode"][value="${mode}"]`);
    if (radio) radio.checked = true;

    // Set TTL
    form.elements.sticky_session_ttl.value = gatewaySettings.sticky_session_ttl || 300;

    // Set rules in textarea
    stickyRulesTextarea.value = (gatewaySettings.sticky_rules || []).join('\n');

    // Set load balancer strategy
    form.elements.load_balancer_strategy.value = gatewaySettings.load_balancer_strategy || 'least_connections';
}

/**
 * Collects data from the Gateway Settings card and formats it for the API.
 * @returns {object} The gateway settings object to be sent.
 */
function getGatewaySettingsData() {
    const formData = new FormData(gatewaySettingsForm);
    const rules = stickyRulesTextarea.value.split('\n').map(rule => rule.trim()).filter(rule => rule);
    return {
        sticky_session_mode: formData.get('sticky_session_mode'),
        sticky_session_ttl: parseInt(formData.get('sticky_session_ttl'), 10),
        sticky_rules: rules,
        load_balancer_strategy: formData.get('load_balancer_strategy'),
    };
}

/**
 * Renders the rules table by filtering and sorting the master `routingRulesCache`.
 */
function renderRulesTable() {
    ruleListBody.innerHTML = '';

    // 1. Filter the data
    let rulesToRender = routingRulesCache.filter(rule => {
        const typeMatch = filterType === 'all' || rule.type === filterType;
        const textMatch = !filterText || (Array.isArray(rule.value) && rule.value.join(' ').toLowerCase().includes(filterText.toLowerCase()));
        return typeMatch && textMatch;
    });

    // 2. Sort the filtered data
    rulesToRender.sort((a, b) => {
        const priorityA = a.priority || 99;
        const priorityB = b.priority || 99;
        return sortDirection === 'asc' ? priorityA - priorityB : priorityB - priorityA;
    });

    // 3. Render the view model
    rulesToRender.forEach(rule => {
        // IMPORTANT: Find the index from the ORIGINAL cache to ensure edits/deletes work correctly
        const originalIndex = routingRulesCache.findIndex(r => r === rule);

        const row = document.createElement('tr');
        row.innerHTML = `
            <td>${rule.priority}</td>
            <td>${rule.type}</td>
            <td>${Array.isArray(rule.value) ? rule.value.join(', ') : rule.value}</td>
            <td>${rule.target}</td>
            <td class="actions">
                <button type="button" class="edit-rule-btn" data-original-index="${originalIndex}">Edit</button>
                <button type="button" class="delete-rule-btn" data-original-index="${originalIndex}">Delete</button>
            </td>
        `;
        ruleListBody.appendChild(row);
    });

    // Update sort indicator
    const indicator = document.getElementById('priority-sort-indicator');
    indicator.textContent = sortDirection === 'asc' ? '▲' : '▼';
}

/**
 * Collects the complete, unfiltered routing data for saving.
 * @returns {object} The routing settings object to be sent.
 */
export function getRoutingSettingsData() {
    return {
        rules: routingRulesCache, // Always save the complete data model
    };
}

/**
 * Saves or updates a rule in the local cache and re-renders the table.
 * @param {object} ruleData - The rule object from the form.
 * @param {number|null} index - The original index of the rule to update, or null for a new rule.
 */
export function saveRuleToCache(ruleData, originalIndex) {
    if (originalIndex !== null && originalIndex >= 0) {
        // Directly update the rule at its original, correct index
        routingRulesCache[originalIndex] = ruleData;
    } else {
        // This is a new rule, add it to the cache
        routingRulesCache.push(ruleData);
    }
    renderRulesTable(); // Re-render with new data
}

function populateFirewallSettings(firewallSettings) {
    const form = firewallSettingsForm;
    const enabled = firewallSettings.enabled ? 'true' : 'false';
    const radio = form.querySelector(`input[name="firewall_enabled"][value="${enabled}"]`);
    if (radio) radio.checked = true;

    firewallRulesCache = JSON.parse(JSON.stringify(firewallSettings.rules || []));
    renderFirewallRulesTable();
}

function getFirewallSettingsData() {
    const formData = new FormData(firewallSettingsForm);
    return {
        enabled: formData.get('firewall_enabled') === 'true',
        rules: firewallRulesCache,
    };
}

function renderFirewallRulesTable() {
    firewallRuleListBody.innerHTML = '';
    // 防火墙规则总是按优先级排序
    firewallRulesCache.sort((a, b) => (a.priority || 9999) - (b.priority || 9999));

    firewallRulesCache.forEach((rule, index) => {
        const row = document.createElement('tr');
        row.innerHTML = `
            <td>${rule.priority}</td>
            <td class="action-${rule.action}">${rule.action}</td>
            <td>${rule.protocol || 'any'}</td>
            <td>${(rule.source_cidr || []).join(', ') || 'any'}</td>
            <td>${(rule.dest_cidr || []).join(', ') || 'any'}</td>
            <td>${rule.dest_port || 'any'}</td>
            <td class="actions">
                <button type="button" class="edit-fw-rule-btn" data-index="${index}">Edit</button>
                <button type="button" class="delete-fw-rule-btn" data-index="${index}">Delete</button>
            </td>
        `;
        firewallRuleListBody.appendChild(row);
    });
}

function showFirewallRuleDialog(rule = null, index = null) {
    firewallRuleForm.reset();
    document.getElementById('firewall-rule-dialog-title').textContent = rule ? 'Edit Firewall Rule' : 'Add Firewall Rule';
    document.getElementById('firewall-rule-index').value = index !== null ? index : '';

    if (rule) {
        firewallRuleForm.elements.priority.value = rule.priority;
        firewallRuleForm.elements.action.value = rule.action;
        firewallRuleForm.elements.protocol.value = rule.protocol || '';
        firewallRuleForm.elements.source_cidr.value = (rule.source_cidr || []).join('\n');
        firewallRuleForm.elements.dest_cidr.value = (rule.dest_cidr || []).join('\n');
        firewallRuleForm.elements.dest_port.value = rule.dest_port || '';
    }
    firewallRuleDialog.showModal();
}

function getFirewallRuleFormData() {
    const formData = new FormData(firewallRuleForm);
    const index = formData.get('rule-index');
    const rule = {
        priority: parseInt(formData.get('priority'), 10) || 100,
        action: formData.get('action'),
        protocol: formData.get('protocol') || '',
        source_cidr: (formData.get('source_cidr') || '').split('\n').map(s => s.trim()).filter(Boolean),
        dest_cidr: (formData.get('dest_cidr') || '').split('\n').map(s => s.trim()).filter(Boolean),
        dest_port: formData.get('dest_port') || '',
    };
    return { data: rule, index: index ? parseInt(index, 10) : null };
}

// --- 新增防火墙页面初始化函数 ---
export function initializeFirewallPage() {
    const page = document.getElementById('main-firewall');

    page.addEventListener('click', async e => {
        const target = e.target;
        if (target.id === 'add-firewall-rule-btn') {
            showFirewallRuleDialog(null, null);
        } else if (target.classList.contains('save-btn') && target.dataset.module === 'firewall') {
            const settingsData = getFirewallSettingsData();
            target.textContent = 'Saving...';
            target.disabled = true;
            try {
                await saveSettings('firewall', settingsData);
                updateStatusMessage('Successfully saved Firewall settings.');
            } catch (error) {
                alert(`Error saving Firewall settings: ${error.message}`);
            } finally {
                target.textContent = 'Save Firewall Settings';
                target.disabled = false;
            }
        } else if (target.classList.contains('edit-fw-rule-btn')) {
            const index = parseInt(target.dataset.index, 10);
            showFirewallRuleDialog(firewallRulesCache[index], index);
        } else if (target.classList.contains('delete-fw-rule-btn')) {
            if (confirm('Are you sure you want to delete this firewall rule?')) {
                const index = parseInt(target.dataset.index, 10);
                firewallRulesCache.splice(index, 1);
                renderFirewallRulesTable();

                // 【新增】删除后立即保存
                const settingsData = getFirewallSettingsData();
                try {
                    updateStatusMessage('Deleting and saving firewall rule...');
                    await saveSettings('firewall', settingsData);
                    updateStatusMessage('Firewall rule deleted successfully.');
                } catch (error) {
                    alert('Error deleting firewall rule: ' + error.message);
                    loadSettings(); // 失败时回滚
                }
            }
        }
    });

    // 【错误修正】只在这里为 firewallRuleForm 添加监听器
    firewallRuleForm.addEventListener('submit', async e => {
        e.preventDefault();
        const { data, index } = getFirewallRuleFormData();

        if (index !== null) {
            firewallRulesCache[index] = data;
        } else {
            firewallRulesCache.push(data);
        }
        renderFirewallRulesTable();

        const firewallSettingsPayload = getFirewallSettingsData();

        try {
            updateStatusMessage('Saving firewall rule to server...');
            await saveSettings('firewall', firewallSettingsPayload);
            updateStatusMessage('Firewall rule saved and applied successfully.');
        } catch (error) {
            alert('Error saving firewall rule: ' + error.message);
            updateStatusMessage('Failed to save firewall rule.', 'failed');
            loadSettings();
            return;
        }

        firewallRuleDialog.close();
    });

    document.getElementById('cancel-firewall-rule-btn').addEventListener('click', () => firewallRuleDialog.close());
}

/**
 * Initializes all event listeners for the Settings page.
 */
export function initializeSettingsPage() {
    // Note: All interactive elements are now children of either gatewaySettingsForm or routingSettingsForm
    const gatewayPage = document.getElementById('main-gateway');
    const routingPage = document.getElementById('main-routing');

    // --- Gateway Page Listeners ---
    gatewayPage.addEventListener('click', async (e) => {
        if (e.target.classList.contains('save-btn') && e.target.dataset.module === 'gateway') {
            const settingsData = getGatewaySettingsData();
            e.target.textContent = 'Saving...';
            e.target.disabled = true;
            try {
                await saveSettings('gateway', settingsData);
                updateStatusMessage(`Successfully saved Gateway settings.`);
            } catch (error) {
                alert(`Error saving Gateway settings: ${error.message}`);
            } finally {
                e.target.textContent = 'Save Gateway Settings';
                e.target.disabled = false;
            }
        }
    });

    // --- Routing Page Listeners ---
    routingPage.addEventListener('click', async (e) => {
        const target = e.target;
        if (target.id === 'add-rule-btn') {
            populateRuleTargetOptions(serversCache);
            showRuleDialog(null, null);
        } else if (target.classList.contains('save-btn') && target.dataset.module === 'routing') {
            const settingsData = getRoutingSettingsData();
            target.textContent = 'Saving...';
            target.disabled = true;
            try {
                await saveSettings('routing', settingsData);
                updateStatusMessage(`Successfully saved Routing settings.`);
            } catch (error) {
                alert(`Error saving Routing settings: ${error.message}`);
            } finally {
                target.textContent = 'Save All Routing Changes';
                target.disabled = false;
            }
        }
    });

    const valueFilterInput = document.getElementById('rule-value-filter');
    const typeFilterSelect = document.getElementById('rule-type-filter');
    const priorityHeader = document.getElementById('priority-header');
    const fetchClientsBtn = document.getElementById('fetch-clients-btn');
    const fetchTargetsBtn = document.getElementById('fetch-targets-btn');
    const ruleTypeSelect = document.getElementById('rule-type');
    const ruleValueTextarea = document.getElementById('rule-value');

    valueFilterInput.addEventListener('input', () => { filterText = valueFilterInput.value; renderRulesTable(); });
    typeFilterSelect.addEventListener('change', () => { filterType = typeFilterSelect.value; renderRulesTable(); });
    priorityHeader.addEventListener('click', () => { sortDirection = sortDirection === 'asc' ? 'desc' : 'asc'; renderRulesTable(); });

    // Show/hide fetch button when rule type changes in the dialog
    ruleTypeSelect.addEventListener('change', () => {
        const isSource = ruleTypeSelect.value === 'source_ip';
        fetchClientsBtn.style.display = isSource ? 'inline-block' : 'none';
        fetchTargetsBtn.style.display = !isSource ? 'inline-block' : 'none';
    });

    // Fetch clients button listener
    fetchClientsBtn.addEventListener('click', async () => {
        try {
            fetchClientsBtn.textContent = 'Fetching...';
            fetchClientsBtn.disabled = true;
            const ips = await fetchAvailableClientIPs();
            if (ips.length > 0) {
                ruleValueTextarea.value = ips.join('\n');
                updateStatusMessage(`Fetched ${ips.length} available client IP(s).`);
            } else {
                updateStatusMessage('No new online client IPs found.');
            }
        } catch (error) {
            alert('Error fetching client IPs: ' + error.message);
        } finally {
            fetchClientsBtn.textContent = 'Fetch IPs';
            fetchClientsBtn.disabled = false;
        }
    });

    // --- NEW: Fetch recent targets button listener ---
    fetchTargetsBtn.addEventListener('click', async () => {
        try {
            fetchTargetsBtn.textContent = 'Fetching...';
            fetchTargetsBtn.disabled = true;
            const targets = await fetchRecentTargets(); // Call the new API
            if (targets.length > 0) {
                // Extract just the host/domain part for convenience
                const domains = targets.map(t => t.split(':')[0]);
                ruleValueTextarea.value = [...new Set(domains)].join('\n'); // Use Set to remove duplicates
                updateStatusMessage(`Fetched ${targets.length} recent target(s).`);
            } else {
                updateStatusMessage('No recent targets found.');
            }
        } catch (error) {
            alert('Error fetching recent targets: ' + error.message);
        } finally {
            fetchTargetsBtn.textContent = 'Fetch Recent';
            fetchTargetsBtn.disabled = false;
        }
    });

    ruleListBody.addEventListener('click', async (e) => {
        const target = e.target;
        const originalIndex = parseInt(target.dataset.originalIndex, 10);

        if (target.classList.contains('edit-rule-btn')) {
            populateRuleTargetOptions(serversCache);
            showRuleDialog(routingRulesCache[originalIndex], originalIndex);
        } else if (target.classList.contains('delete-rule-btn')) {
            if (confirm('Are you sure you want to delete this rule?')) {
                routingRulesCache.splice(originalIndex, 1);
                renderRulesTable();

                const routingSettingsPayload = getRoutingSettingsData();
                try {
                    updateStatusMessage('Deleting rule from server...');
                    await saveSettings('routing', routingSettingsPayload);
                    updateStatusMessage('Rule deleted successfully.');
                } catch (error) {
                    alert('Error deleting rule: ' + error.message);
                    updateStatusMessage('Failed to delete rule.', 'failed');
                    loadSettings(); // Revert on failure
                }
            }
        }
    });
}