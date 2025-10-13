// This module holds the frontend's global state.

// Cache for the server list received from the backend.
export let serversCache = [];

// History of log messages for the log panel.
export let logMessages = [];

// The last message displayed, to avoid duplicates.
let lastLogMessage = '';
const MAX_LOGS = 50; // Keep a maximum of 50 log entries.

/**
 * Updates the serversCache with new data from the API.
 * @param {Array} newServers - The new list of servers.
 */
export function updateServersCache(newServers) {
    serversCache = newServers || [];
}

/**
 * Adds a new log message if it's different from the last one.
 * Manages the log history size.
 * @param {string} message - The new log message from globalStatus.
 * @returns {boolean} - True if a new message was added, false otherwise.
 */
export function addLogMessage(message) {
    if (message && message !== lastLogMessage) {
        lastLogMessage = message;
        const timestamp = new Date().toLocaleTimeString();
        logMessages.push(`[${timestamp}] ${message}`);

        // Trim the log array if it gets too long
        if (logMessages.length > MAX_LOGS) {
            logMessages.shift(); // Remove the oldest entry
        }
        return true;
    }
    return false;
}

/**
 * Clears all log messages from the state.
 */
export function clearLogMessages() {
    logMessages.length = 0; // Efficient way to clear an array
    lastLogMessage = ''; // Reset last message to allow re-logging
}

/**
 * Merges runtime data (health, metrics) into the serversCache.
 * @param {object} healthData - The health status and metrics data from the API.
 */
export function mergeRuntimeData(healthData) {
    const healthStatus = healthData.healthStatus || {};
    const metrics = healthData.metrics || {};
    const runtimeInfo = healthData.runtimeInfo || {};
    const exitIPs = healthData.exitIPs || {};

    serversCache.forEach(server => {
        server.health = healthStatus[server.id] || 0; // 0: Unknown, 1: Up, 2: Down
        server.exitIP = exitIPs[server.id] || '';
        const serverMetrics = metrics[server.id];

        if (serverMetrics) {
            // Store previous values before updating
            server.lastUplink = server.uplink;
            server.lastDownlink = server.downlink;
            server.lastUpdateTime = server.updateTime;

            // Update with new values from API
            server.connections = serverMetrics.activeConnections;
            server.latency = serverMetrics.latency;
            server.uplink = serverMetrics.uplink;
            server.downlink = serverMetrics.downlink;
            server.updateTime = Date.now();
        } else {
            // Reset metrics if server is not in the new data
            server.connections = -1;
            server.latency = -1;
            server.uplink = undefined;
            server.downlink = undefined;
        }

        const serverRuntimeInfo = runtimeInfo[server.id];
        if (serverRuntimeInfo && serverRuntimeInfo.Port > 0) {
            server.localPort = serverRuntimeInfo.Port;
        }
    });
}