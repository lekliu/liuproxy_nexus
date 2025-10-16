import { updateServersCache, addLogMessage, mergeRuntimeData } from './state.js';
import { renderServers, renderLogPanel, updateStatusMessage } from './ui.js';

/**
 * Fetches the list of servers from the backend and updates the state.
 */
export async function fetchServers() {
    try {
        const response = await fetch('/api/servers');
        if (!response.ok) throw new Error(`Failed to fetch servers: ${response.statusText}`);
        const servers = await response.json();
        updateServersCache(servers);
        renderServers(); // Initial render without health data
    } catch (error) {
        console.error(error);
        updateStatusMessage('Error fetching server list', 'failed');
    }
}

/**
 * Fetches the current system status, health, and metrics.
 * Updates the state and triggers UI re-renders.
 */
export async function fetchStatus() {
    try {
        const response = await fetch('/api/status');
        if (!response.ok) {
            updateStatusMessage('Failed to get status', 'failed');
            return;
        }
        const data = await response.json();

        // Update global status message and log panel
        const status = data.globalStatus || "Unknown";
        if (addLogMessage(status)) {
            renderLogPanel();
        }

        // Merge runtime data into the server cache and re-render the server list
        mergeRuntimeData(data);
        renderServers();

    } catch (error) {
        console.error(error);
        updateStatusMessage('Error polling status', 'failed');

        // On error, reset health data in the cache and re-render
        mergeRuntimeData({}); // Pass empty data to reset
        renderServers();
    }
}

/**
 * Fetches the entire runtime settings object from the backend.
 * @returns {Promise<object>} The full settings object.
 */
export async function fetchAllSettings() {
    const response = await fetch('/api/settings');
    if (!response.ok) {
        throw new Error(`Failed to fetch settings: ${response.status} ${response.statusText}`);
    }
    return response.json();
}

/**
 * Saves a specific module's settings to the backend.
 * @param {string} moduleKey - The key of the module to update (e.g., 'gateway').
 * @param {object} settingsData - The settings object for that module.
 */
export async function saveSettings(moduleKey, settingsData) {
    const response = await fetch(`/api/settings/${moduleKey}`, {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify(settingsData),
    });

    if (!response.ok) {
        const errorText = await response.text();
        throw new Error(`Failed to save settings: ${errorText}`);
    }
}
/**
 * Tells the backend to apply all staged configuration changes.
 */
export async function applyChanges() {
    const response = await fetch(`/api/apply_changes`, {
        method: 'POST',
    });

    if (!response.ok) {
        const errorText = await response.text();
        throw new Error(`Failed to apply changes: ${errorText}`);
    }
}

/**
 * Fetches the list of recently accessed targets from the backend.
 * @returns {Promise<string[]>} A list of target addresses (e.g., "domain:port").
 */
export async function fetchRecentTargets() {
    const response = await fetch('/api/recent_targets');
    if (!response.ok) {
        const errorText = await response.text();
        throw new Error(`Failed to fetch recent targets: ${errorText}`);
    }
    return response.json();
}

/**
 * Fetches the list of available (unconfigured) client IPs from the backend.
 * @returns {Promise<string[]>} A list of IP addresses.
 */
export async function fetchAvailableClientIPs() {
    const response = await fetch('/api/clients');
    if (!response.ok) {
        const errorText = await response.text();
        throw new Error(`Failed to fetch client IPs: ${errorText}`);
    }
    return response.json();
}

/**
 * Fetches a single available proxy from the backend proxy pool.
 * @returns {Promise<object|null>} A single proxy object or null if none are available.
 */
export async function fetchAvailableProxy(protocol = 'http') {
    const response = await fetch(`/api/proxypool/available?protocol=${protocol}`);
    if (!response.ok) {
        const errorText = await response.text();
        throw new Error(`Failed to fetch available proxy: ${errorText}`);
    }
    const proxies = await response.json();
    // The backend returns an array, we just need the first element.
    return proxies.length > 0 ? proxies[0] : null;
}

/**
 * Fetches the status of all healthy proxies in the pool.
 * @returns {Promise<Array>} A list of proxy status items.
 */
export async function fetchProxyPoolStatus() {
    const response = await fetch('/api/proxypool/all');
    if (!response.ok) {
        const errorText = await response.text();
        throw new Error(`Failed to fetch proxy pool status: ${errorText}`);
    }
    return response.json();
}

/**
 * Imports a list of proxies to the backend for validation.
 * @param {string} protocol - The protocol of the proxies ('http' or 'socks5').
 * @param {string[]} proxyList - An array of 'IP:Port' strings.
 */
export async function importProxies(protocol, proxyList) {
    const response = await fetch('/api/proxypool/import', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify({
            protocol: protocol,
            proxies: proxyList,
        }),
    });

    if (!response.ok) {
        const errorText = await response.text();
        throw new Error(`Failed to import proxies: ${errorText}`);
    }
    return response.json();
}

/**
 * Sends a request to the backend to validate a list of proxies by their IDs.
 * @param {string[]} ids - An array of proxy IDs to validate.
 */
export async function validateProxies(ids) {
    const response = await fetch('/api/proxypool/validate', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(ids),
    });
    if (!response.ok) {
        const errorText = await response.text();
        throw new Error(`Failed to trigger validation: ${errorText}`);
    }
    return response.json();
}

/**
 * Sends a request to the backend to delete a list of proxies by their IDs.
 * @param {string[]} ids - An array of proxy IDs to delete.
 */
export async function deleteProxies(ids) {
    const response = await fetch('/api/proxypool/delete', {
        method: 'DELETE',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(ids),
    });
    if (!response.ok) {
        const errorText = await response.text();
        throw new Error(`Failed to delete proxies: ${errorText}`);
    }
    return response.json();
}