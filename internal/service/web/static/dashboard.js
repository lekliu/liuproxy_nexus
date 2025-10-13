const CHART_MAX_DATA_POINTS = 60; // Show last 60 seconds

const statActiveConnections = document.getElementById('stat-active-connections');
const statUplinkRate = document.getElementById('stat-uplink-rate');
const statDownlinkRate = document.getElementById('stat-downlink-rate');
const ctx = document.getElementById('traffic-chart').getContext('2d');

let trafficChart = null;

function formatBytesPerSecond(bytes) {
    if (bytes < 1024) return `${bytes} B/s`;
    const kb = bytes / 1024;
    if (kb < 1024) return `${kb.toFixed(1)} KB/s`;
    const mb = kb / 1024;
    return `${mb.toFixed(2)} MB/s`;
}

export function initializeDashboardPage() {
    if (trafficChart) return;

    trafficChart = new Chart(ctx, {
        type: 'line',
        data: {
            labels: [],
            datasets: [
                {
                    label: 'Upload',
                    data: [],
                    borderColor: 'rgba(255, 99, 132, 1)',
                    backgroundColor: 'rgba(255, 99, 132, 0.2)',
                    fill: true,
                    tension: 0.3,
                    pointRadius: 0,
                },
                {
                    label: 'Download',
                    data: [],
                    borderColor: 'rgba(54, 162, 235, 1)',
                    backgroundColor: 'rgba(54, 162, 235, 0.2)',
                    fill: true,
                    tension: 0.3,
                    pointRadius: 0,
                }
            ]
        },
        options: {
            responsive: true,
            maintainAspectRatio: false,
            scales: {
                x: {
                    type: 'time',
                    time: {
                        unit: 'second',
                        displayFormats: {
                            second: 'HH:mm:ss'
                        }
                    },
                    ticks: {
                        maxRotation: 0,
                        minRotation: 0,
                        autoSkip: true,
                        maxTicksLimit: 10
                    }
                },
                y: {
                    beginAtZero: true,
                    ticks: {
                        callback: function(value) {
                            return formatBytesPerSecond(value).replace('/s', '');
                        }
                    }
                }
            },
            plugins: {
                legend: {
                    position: 'top',
                },
                tooltip: {
                    mode: 'index',
                    intersect: false,
                }
            },
            animation: {
                duration: 200 // Faster animation
            }
        }
    });
}

export function updateDashboard(stats) {
    if (!trafficChart || !stats) return;

    const timestamp = new Date(stats.timestamp);

    // Update stat cards
    statActiveConnections.textContent = stats.active_connections;
    statUplinkRate.textContent = formatBytesPerSecond(stats.uplink_rate);
    statDownlinkRate.textContent = formatBytesPerSecond(stats.downlink_rate);

    // Update chart data
    const chartData = trafficChart.data;
    chartData.labels.push(timestamp);
    chartData.datasets[0].data.push(stats.uplink_rate);
    chartData.datasets[1].data.push(stats.downlink_rate);

    // Keep the chart from growing indefinitely
    if (chartData.labels.length > CHART_MAX_DATA_POINTS) {
        chartData.labels.shift();
        chartData.datasets.forEach(dataset => {
            dataset.data.shift();
        });
    }

    trafficChart.update('none'); // 'none' for no animation to keep it smooth
}