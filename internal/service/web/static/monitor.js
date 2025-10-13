// FILE: internal/service/web/static/monitor.js
import { addLogMessage } from './state.js';
import { renderLogPanel } from './ui.js';
import { updateDashboard } from './dashboard.js';

const logBody = document.getElementById('monitor-log-body');
const pauseBtn = document.getElementById('monitor-pause-btn');
const clearBtn = document.getElementById('monitor-clear-btn');
const MAX_MONITOR_LOGS = 100;
let isPaused = false;
let socket = null;

function connectWebSocket() {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const url = `${protocol}//${window.location.host}/ws`;

    socket = new WebSocket(url);

    socket.onopen = () => {
        addLogMessage('Monitor WebSocket connected.');
        renderLogPanel();
    };

    socket.onmessage = (event) => {
        if (isPaused) return;

        try {
            const message = JSON.parse(event.data);
            if (message.type === 'traffic_log') {
                addTrafficLogRow(message.data);
            }else if (message.type === 'dashboard_update') {
                updateDashboard(message.data);
            }
        } catch (e) {
            console.error('Failed to parse WebSocket message:', e);
        }
    };

    socket.onclose = () => {
        addLogMessage('Monitor WebSocket disconnected. Reconnecting in 3s...');
        renderLogPanel();
        setTimeout(connectWebSocket, 3000);
    };

    socket.onerror = (error) => {
        console.error('WebSocket Error:', error);
    };
}

function addTrafficLogRow(logEntry) {
    const row = document.createElement('tr');

    const timestamp = new Date(logEntry.timestamp).toLocaleTimeString();

    row.innerHTML = `
        <td>${timestamp}</td>
        <td>${escapeHTML(logEntry.client_ip)}</td>
        <td>${escapeHTML(logEntry.protocol)}</td>
        <td>${escapeHTML(logEntry.destination)}</td>
        <td>${escapeHTML(logEntry.action)}</td>
        <td>${escapeHTML(logEntry.target || '-')}</td>
    `;

    logBody.prepend(row); // 在顶部插入新行

    // 限制日志行数
    if (logBody.rows.length > MAX_MONITOR_LOGS) {
        logBody.deleteRow(-1); // 删除最后一行
    }
}

function escapeHTML(str) {
    if (str === null || str === undefined) return '';
    return str.toString()
        .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;').replace(/'/g, '&#039;');
}

export function initializeMonitorPage() {
    pauseBtn.addEventListener('click', () => {
        isPaused = !isPaused;
        pauseBtn.textContent = isPaused ? 'Resume' : 'Pause';
        pauseBtn.classList.toggle('active', isPaused);
    });

    clearBtn.addEventListener('click', () => {
        logBody.innerHTML = '';
    });

    // 仅在首次初始化时连接
    if (!socket) {
        connectWebSocket();
    }
}
