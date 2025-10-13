import { serversCache, clearLogMessages } from './state.js';
import { fetchServers, fetchStatus, saveSettings, applyChanges } from './api.js';
import { initializeSettingsPage, loadSettings, saveRuleToCache, getRoutingSettingsData, initializeFirewallPage } from './settings.js';
import { initializeMonitorPage } from './monitor.js';
import { initializeDashboardPage } from './dashboard.js';
import {
    form, 
    ruleForm,
    showDialog,
    closeDialog,
    closeRuleDialog,
    getRuleFormData,
    updateFormVisibility,
    getFormServerData,
    updateStatusMessage,
    renderLogPanel,
    renderServers,
    showApplyBanner,
    hideApplyBanner,
} from './ui.js';

document.addEventListener('DOMContentLoaded', () => {
    // --- UI Element References ---
    const mainDashboard = document.getElementById('main-dashboard');
    const mainServers = document.getElementById('main-servers');
    const mainGateway = document.getElementById('main-gateway');
    const mainRouting = document.getElementById('main-routing');
    const mainFirewall = document.getElementById('main-firewall');
    const mainMonitor = document.getElementById('main-monitor');

    const navDashboardLink = document.getElementById('nav-dashboard');
    const navServersLink = document.getElementById('nav-servers');
    const navGatewayLink = document.getElementById('nav-gateway');
    const navRoutingLink = document.getElementById('nav-routing');
    const navFirewallLink = document.getElementById('nav-firewall');
    const navMonitorLink = document.getElementById('nav-monitor');

    const serverListBody = document.getElementById('server-list');
    const addServerBtn = document.getElementById('add-server-btn');
    const cancelBtn = document.getElementById('cancel-btn');
    const clearLogBtn = document.getElementById('clear-log-btn');
    const serverTypeSelect = document.getElementById('type');
    const networkSelect = document.getElementById('network');
    const vlessSecuritySelect = document.getElementById('security');
    const hostInput = document.getElementById('host');
    const sniInput = document.getElementById('sni');
    const cancelRuleBtn = document.getElementById('cancel-rule-btn');
    const applyChangesBtn = document.getElementById('apply-changes-btn');

    // --- NEW: References for filtering and sorting ---
    const serverFilterInput = document.getElementById('server-filter-input');
    const remarksHeader = document.getElementById('remarks-header');
    const latencyHeader = document.getElementById('latency-header');

    // --- State Management ---
    let statusInterval = null;
    let currentFilterText = ''; // Persisted filter text
    let currentSortBy = 'remarks';    // Persisted sort column
    let currentSortDir = 'asc';       // Persisted sort direction

    // --- Core Functions ---
    async function startStatusPolling() {
        if (statusInterval) clearInterval(statusInterval);

        const poll = async () => {
            await fetchStatus();
            // ALWAYS use the persisted state for rendering
            renderServers(currentFilterText, currentSortBy, currentSortDir);
        };

        poll(); // Fetch immediately
        statusInterval = setInterval(poll, 3000); // Then poll every 3 seconds
    }

    function showPage(pageName) {
         // Hide all main sections
        mainDashboard.style.display = 'none';
        mainServers.style.display = 'none';
        mainGateway.style.display = 'none';
        mainRouting.style.display = 'none';
        mainFirewall.style.display = 'none';
        mainMonitor.style.display = 'none';

        // Deactivate all nav links
        navDashboardLink.classList.remove('active');
        navServersLink.classList.remove('active');
        navGatewayLink.classList.remove('active');
        navRoutingLink.classList.remove('active');
        navFirewallLink.classList.remove('active');
        navMonitorLink.classList.remove('active');

        // Show the selected page and activate the corresponding link
        switch (pageName) {
            case 'gateway':
                mainGateway.style.display = 'block';
                navGatewayLink.classList.add('active');
                loadSettings(); // Load settings for this page
                break;
            case 'routing':
                mainRouting.style.display = 'block';
                navRoutingLink.classList.add('active');
                loadSettings(); // Also load settings for this page
                break;
            case 'firewall':
                mainFirewall.style.display = 'block';
                navFirewallLink.classList.add('active');
                loadSettings();
                break;
            case 'monitor':
                mainMonitor.style.display = 'block';
                navMonitorLink.classList.add('active');
                break;
            case 'servers':
                mainServers.style.display = 'block';
                navServersLink.classList.add('active');
                break;
            case 'dashboard':
            default:
                mainDashboard.style.display = 'block';
                navDashboardLink.classList.add('active');
                break;
        }
    }

    // --- Event Listeners ---
    navDashboardLink.addEventListener('click', (e) => { e.preventDefault(); showPage('dashboard'); });
    navServersLink.addEventListener('click', (e) => { e.preventDefault(); showPage('servers'); });
    navGatewayLink.addEventListener('click', (e) => { e.preventDefault(); showPage('gateway'); });
    navRoutingLink.addEventListener('click', (e) => { e.preventDefault(); showPage('routing'); });
    navFirewallLink.addEventListener('click', (e) => { e.preventDefault(); showPage('firewall'); });
    navMonitorLink.addEventListener('click', (e) => { e.preventDefault(); showPage('monitor'); });

    // --- Filter input listener ---
    serverFilterInput.addEventListener('input', () => {
        currentFilterText = serverFilterInput.value;
        renderServers(currentFilterText, currentSortBy, currentSortDir);
    });

    // --- Sorting listeners ---
    remarksHeader.addEventListener('click', () => {
        if (currentSortBy === 'remarks') {
            currentSortDir = currentSortDir === 'asc' ? 'desc' : 'asc';
        } else {
            currentSortBy = 'remarks';
            currentSortDir = 'asc';
        }
        renderServers(currentFilterText, currentSortBy, currentSortDir);
    });

    latencyHeader.addEventListener('click', () => {
        if (currentSortBy === 'latency') {
            currentSortDir = currentSortDir === 'asc' ? 'desc' : 'asc';
        } else {
            currentSortBy = 'latency';
            currentSortDir = 'asc';
        }
        renderServers(currentFilterText, currentSortBy, currentSortDir);
    });

    addServerBtn.addEventListener('click', () => showDialog());
    cancelBtn.addEventListener('click', (e) => {
        e.preventDefault();
        closeDialog();
    });

    clearLogBtn.addEventListener('click', () => {
        clearLogMessages();
        renderLogPanel();
    });

    // Form visibility listeners
    serverTypeSelect.addEventListener('change', updateFormVisibility);
    networkSelect.addEventListener('change', updateFormVisibility);
    vlessSecuritySelect.addEventListener('change', updateFormVisibility);

    // Auto-fill SNI from Host
    hostInput.addEventListener('blur', () => {
        if (hostInput.value && !sniInput.value) {
            sniInput.value = hostInput.value;
        }
    });

    // Form submission
    form.addEventListener('submit', async (e) => {
        e.preventDefault();
        const serverData = getFormServerData();
        const id = form.elements.id.value; // 从form元素中获取id
        
        const method = id ? 'PUT' : 'POST';
        const url = id ? `/api/servers?id=${id}` : '/api/servers';

        try {
            updateStatusMessage(`Saving server: ${serverData.remarks}...`);
            const response = await fetch(url, {
                method,
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(serverData)
            });
            if (!response.ok) {
                const errorText = await response.text();
                throw new Error(`Failed to save server: ${errorText}`);
            }
            closeDialog();
            await fetchServers();
            // Use persisted state after fetching
            renderServers(currentFilterText, currentSortBy, currentSortDir);
            showApplyBanner();
            updateStatusMessage('Server configuration saved. Click "Apply Changes" to activate.');
        } catch (error) {
            alert('Error: ' + error.message);
            updateStatusMessage(`Failed to save server.`, 'failed');
        }
    });

    // Event delegation for server list actions
    serverListBody.addEventListener('click', async (e) => {
        const target = e.target;
        // Handle clicks inside the "more actions" menu
        const actionTarget = target.closest('a') || target;
        const id = actionTarget.dataset.id;
        if (!id) return;

        if (actionTarget.classList.contains('delete-btn')) {
            if (confirm('Are you sure you want to delete this server?')) {
                try {
                    updateStatusMessage(`Deleting server...`);
                    const response = await fetch(`/api/servers?id=${id}`, { method: 'DELETE' });
                    if (!response.ok) throw new Error('Failed to delete');
                    await fetchServers();
                    // Use persisted state after fetching
                    renderServers(currentFilterText, currentSortBy, currentSortDir);
                    showApplyBanner();    // 显示横幅
                    updateStatusMessage('Server deletion saved. Click "Apply Changes".');
                } catch (error) {
                    alert('Error deleting server: ' + error.message);
                }
            }
        } else if (actionTarget.classList.contains('edit-btn')) {
            const serverToEdit = serversCache.find(s => s.id === id);
            if (serverToEdit) {
                // 如果是 goremote 类型且没有设置连接数，则默认为1
                if (serverToEdit.type === 'goremote' && !serverToEdit.goremote_connections) {
                    serverToEdit.goremote_connections = 1;
                }
                showDialog(serverToEdit);
            }
         } else if (actionTarget.classList.contains('copy-btn')) {
            try {
                updateStatusMessage(`Duplicating server...`);
                const response = await fetch(`/api/servers/${id}/duplicate`, { method: 'POST' });
                if (!response.ok) {
                    const errorText = await response.text();
                    throw new Error(`Failed to duplicate: ${errorText}`);
                }
                await fetchServers(); // Refresh the list from the server
                renderServers(currentFilterText, currentSortBy, currentSortDir);
                showApplyBanner();    // Show the "Apply Changes" banner
                updateStatusMessage('Server duplicated. Click "Apply Changes" to make it live.');
            } catch (error) {
                alert('Error duplicating server: ' + error.message);
            }
        } else if (target.classList.contains('toggle-active-btn')) {
            const setActive = target.dataset.active === 'true';
            const actionText = setActive ? 'Activating' : 'Deactivating';
            try {
                updateStatusMessage(`${actionText} server...`);
                const response = await fetch(`/api/servers/set_active_state?id=${id}&active=${setActive}`, { method: 'POST' });
                if (!response.ok) {
                    throw new Error(`Failed to set active state`);
                }

                // API调用成功后，从后端重新获取权威列表
                await fetchServers();
                renderServers(currentFilterText, currentSortBy, currentSortDir);
                showApplyBanner(); // 显示横幅
                updateStatusMessage(`Server ${actionText.toLowerCase()} saved. Click "Apply Changes".`);
            } catch (error) {
                alert(`Error: ${error.message}`);
                // Re-fetch servers and status to ensure UI consistency on error
                await fetchServers();
                await fetchStatus();
                renderServers(currentFilterText, currentSortBy, currentSortDir);
            }
        } else if (target.classList.contains('more-actions-btn')) {
            // ... (more actions logic remains the same) ...
        }
    });

     cancelRuleBtn.addEventListener('click', (e) => {
        e.preventDefault();
        closeRuleDialog();
    });

    ruleForm.addEventListener('submit', async (e) => {
        e.preventDefault();
        const { data, index } = getRuleFormData();

        // 1. Update the local cache and re-render the table immediately for responsiveness
        saveRuleToCache(data, index);

        // 2. Assemble the complete routing settings payload
        const routingSettingsPayload = getRoutingSettingsData();

        // 3. Save the entire routing configuration to the backend
        try {
            updateStatusMessage('Saving rule to server...');
            await saveSettings('routing', routingSettingsPayload);
            updateStatusMessage('Rule saved successfully and applied.');
        } catch (error) {
            alert('Error saving rule to server: ' + error.message);
            updateStatusMessage('Failed to save rule.', 'failed');
            // On failure, we might want to reload settings from the server to ensure consistency
            loadSettings();
            return; // Don't close the dialog on error
        }

        // 4. Close the dialog on success
        closeRuleDialog();
    });

    // 应用变更按钮的事件监听器
    applyChangesBtn.addEventListener('click', async () => {
        applyChangesBtn.textContent = 'Applying...';
        applyChangesBtn.disabled = true;

        try {
            updateStatusMessage('Applying changes on the server...');
            await applyChanges(); // 只调用 POST /api/apply_changes

            hideApplyBanner();
            updateStatusMessage('Changes are being applied. Refreshing state...');

            // 稍等片刻后，重新获取状态，以确保与后端同步
            setTimeout(() => {
                // fetchServers(); // 不需要，因为配置没变
                fetchStatus();  // 只需要获取最新的运行时状态
            }, 1500); // 延长等待时间以确保后端有足够时间处理

        } catch (error) {
            alert('Error applying changes: ' + error.message);
            updateStatusMessage('Failed to apply changes.', 'failed');
        } finally {
            applyChangesBtn.textContent = 'Apply Changes';
            applyChangesBtn.disabled = false;
        }
    });

    // --- Initial Load ---
    initializeSettingsPage(); // 初始化设置页面的事件监听器
    initializeFirewallPage(); // 初始化防火墙页面的事件监听器
    initializeMonitorPage();
    initializeDashboardPage();
    showPage('dashboard'); // 默认显示服务器列表页面

    fetchServers();
    renderServers(currentFilterText, currentSortBy, currentSortDir); // Initial render with default state
    startStatusPolling();
});