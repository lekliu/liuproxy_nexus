import { serversCache, clearLogMessages } from './state.js';
import { fetchServers, fetchStatus, saveSettings, applyChanges, importProxies } from './api.js';
import { initializeSettingsPage, loadSettings, saveRuleToCache, getRoutingSettingsData, initializeFirewallPage } from './settings.js';
import { initializeMonitorPage } from './monitor.js';
import { initializeDashboardPage } from './dashboard.js';
import { initializeProxyPoolPage } from './proxy-pool.js';
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
    const mainProxyPool = document.getElementById('main-proxy-pool');
    const mainGateway = document.getElementById('main-gateway');
    const mainRouting = document.getElementById('main-routing');
    const mainFirewall = document.getElementById('main-firewall');
    const mainMonitor = document.getElementById('main-monitor');

    const navDashboardLink = document.getElementById('nav-dashboard');
    const navServersLink = document.getElementById('nav-servers');
    const navProxyPoolLink = document.getElementById('nav-proxy-pool');
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

    // --- 代理池导入对话框的引用 ---
    const showImportDialogBtn = document.getElementById('show-import-dialog-btn');
    const importProxyDialog = document.getElementById('import-proxy-dialog');
    const importProxyForm = document.getElementById('import-proxy-form');
    const cancelImportBtn = document.getElementById('cancel-import-btn');

    // --- References for filtering and sorting ---
    const serverFilterInput = document.getElementById('server-filter-input');
    const remarksHeader = document.getElementById('remarks-header');
    const latencyHeader = document.getElementById('latency-header');

    // --- State Management ---
    let statusInterval = null;
    let proxyPoolInterval = null;
    let currentFilterText = '';
    let currentSortBy = 'remarks';
    let currentSortDir = 'asc';

// --- Core Functions (恢复原始逻辑) ---
    async function startStatusPolling() {
        if (statusInterval) clearInterval(statusInterval);

        const poll = async () => {
            await fetchStatus();
            // ALWAYS use the persisted state for rendering
            renderServers(currentFilterText, currentSortBy, currentSortDir);
        };

        poll(); // Fetch immediately
        statusInterval = setInterval(poll, 3000);
    }

function showPage(pageName) {
        // Hide all main sections
        mainDashboard.style.display = 'none';
        mainServers.style.display = 'none';
        mainProxyPool.style.display = 'none';
        mainGateway.style.display = 'none';
        mainRouting.style.display = 'none';
        mainFirewall.style.display = 'none';
        mainMonitor.style.display = 'none';

        // Deactivate all nav links
        navDashboardLink.classList.remove('active');
        navServersLink.classList.remove('active');
        navProxyPoolLink.classList.remove('active');
        navGatewayLink.classList.remove('active');
        navRoutingLink.classList.remove('active');
        navFirewallLink.classList.remove('active');
        navMonitorLink.classList.remove('active');

        if (proxyPoolInterval) {
            clearInterval(proxyPoolInterval);
            proxyPoolInterval = null;
        }

        // Show the selected page and activate the corresponding link
        switch (pageName) {
            case 'gateway':
                mainGateway.style.display = 'block';
                navGatewayLink.classList.add('active');
                loadSettings();
                break;
            case 'routing':
                mainRouting.style.display = 'block';
                navRoutingLink.classList.add('active');
                loadSettings();
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
            case 'proxy-pool':
                mainProxyPool.style.display = 'block';
                navProxyPoolLink.classList.add('active');
                initializeProxyPoolPage();
                // const pollFunc = initializeProxyPoolPage();
                // proxyPoolInterval = setInterval(pollFunc, 5000);
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
    navProxyPoolLink.addEventListener('click', (e) => { e.preventDefault(); showPage('proxy-pool'); });
    navGatewayLink.addEventListener('click', (e) => { e.preventDefault(); showPage('gateway'); });
    navRoutingLink.addEventListener('click', (e) => { e.preventDefault(); showPage('routing'); });
    navFirewallLink.addEventListener('click', (e) => { e.preventDefault(); showPage('firewall'); });
    navMonitorLink.addEventListener('click', (e) => { e.preventDefault(); showPage('monitor'); });

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
        const actionTarget = target.closest('button');
        if (!actionTarget) return;

        const id = actionTarget.dataset.id;
        if (!id) return;

        if (actionTarget.classList.contains('delete-btn')) {
            if (confirm('Are you sure you want to delete this server?')) {
                try {
                    updateStatusMessage(`Deleting server...`);
                    const response = await fetch(`/api/servers?id=${id}`, { method: 'DELETE' });
                    if (!response.ok) throw new Error('Failed to delete');
                    await fetchServers();
                    renderServers(currentFilterText, currentSortBy, currentSortDir);
                    showApplyBanner();
                    updateStatusMessage('Server deletion saved. Click "Apply Changes".');
                } catch (error) {
                    alert('Error deleting server: ' + error.message);
                }
            }
        } else if (actionTarget.classList.contains('edit-btn')) {
            const serverToEdit = serversCache.find(s => s.id === id);
            if (serverToEdit) {
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
                await fetchServers();
                renderServers(currentFilterText, currentSortBy, currentSortDir);
                showApplyBanner();
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
                if (!response.ok) throw new Error(`Failed to set active state`);
                await fetchServers();
                renderServers(currentFilterText, currentSortBy, currentSortDir);
                showApplyBanner();
                updateStatusMessage(`Server ${actionText.toLowerCase()} saved. Click "Apply Changes".`);
            } catch (error) {
                alert(`Error: ${error.message}`);
                await fetchServers();
                await fetchStatus();
                renderServers(currentFilterText, currentSortBy, currentSortDir);
            }
        }
    });

    cancelRuleBtn.addEventListener('click', (e) => {
        e.preventDefault();
        closeRuleDialog();
    });

    ruleForm.addEventListener('submit', async (e) => {
        e.preventDefault();
        const { data, index } = getRuleFormData();
        saveRuleToCache(data, index);
        const routingSettingsPayload = getRoutingSettingsData();
        try {
            updateStatusMessage('Saving rule to server...');
            await saveSettings('routing', routingSettingsPayload);
            updateStatusMessage('Rule saved successfully and applied.');
        } catch (error) {
            alert('Error saving rule to server: ' + error.message);
            updateStatusMessage('Failed to save rule.', 'failed');
            loadSettings();
            return;
        }
        closeRuleDialog();
    });

     applyChangesBtn.addEventListener('click', async () => {
        applyChangesBtn.textContent = 'Applying...';
        applyChangesBtn.disabled = true;
        try {
            updateStatusMessage('Applying changes on the server...');
            await applyChanges();
            hideApplyBanner();
            updateStatusMessage('Changes are being applied. Refreshing state...');
            setTimeout(() => {
                fetchStatus();
            }, 1500);
        } catch (error) {
            alert('Error applying changes: ' + error.message);
            updateStatusMessage('Failed to apply changes.', 'failed');
        } finally {
            applyChangesBtn.textContent = 'Apply Changes';
            applyChangesBtn.disabled = false;
        }
    });

         // --- 新增: 代理池导入对话框的事件监听器 ---
    showImportDialogBtn.addEventListener('click', () => {
        importProxyForm.reset();
        importProxyDialog.showModal();
    });

    cancelImportBtn.addEventListener('click', () => {
        importProxyDialog.close();
    });

    importProxyForm.addEventListener('submit', async (e) => {
        e.preventDefault();
        const importBtn = importProxyForm.querySelector('button[type="submit"]');
        const importListTextarea = document.getElementById('import-proxy-list');
        const importProtocolSelect = document.getElementById('import-proxy-protocol');

        const proxyListRaw = importListTextarea.value.trim();
        if (!proxyListRaw) {
            alert('Proxy list cannot be empty.');
            return;
        }

        const proxyList = proxyListRaw.split('\n').map(p => p.trim()).filter(Boolean);
        const protocol = importProtocolSelect.value;

        importBtn.textContent = 'Importing...';
        importBtn.disabled = true;

        try {
            await importProxies(protocol, proxyList);
            updateStatusMessage(`Successfully submitted ${proxyList.length} proxies for validation. The list will refresh shortly.`);
            importProxyDialog.close();
        } catch (error) {
            alert('Error importing proxies: ' + error.message);
            updateStatusMessage('Failed to import proxies.', 'failed');
        } finally {
            importBtn.textContent = 'Import and Validate';
            importBtn.disabled = false;
        }
    });

    // --- Initial Load ---
    initializeSettingsPage(); // 初始化设置页面的事件监听器
    initializeFirewallPage(); // 初始化防火墙页面的事件监听器
    initializeMonitorPage();
    initializeDashboardPage();
    initializeProxyPoolPage();
    showPage('dashboard'); // 默认显示服务器列表页面

    fetchServers();
    renderServers(currentFilterText, currentSortBy, currentSortDir); // Initial render with default state
    startStatusPolling();
});