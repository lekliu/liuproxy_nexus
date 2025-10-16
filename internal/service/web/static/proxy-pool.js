import { fetchProxyPoolStatus, validateProxies, deleteProxies } from './api.js';
import { updateStatusMessage } from './ui.js';

// --- UI Elements ---
const poolListBody = document.getElementById('proxy-pool-list-body');
const filterInput = document.getElementById('proxy-pool-filter-input');
const statusFilterSelect = document.getElementById('proxy-pool-status-filter');
const testSelectedBtn = document.getElementById('test-selected-btn');
const deleteSelectedBtn = document.getElementById('delete-selected-btn');
const selectAllCheckbox = document.getElementById('select-all-proxies');
const paginationControls = document.getElementById('proxy-pool-pagination');
const refreshBtn = document.getElementById('refresh-pool-btn');

// --- State ---
let allProxiesCache = [];
let viewState = {
    filterText: '',
    statusFilter: 'all',
    currentPage: 1,
    itemsPerPage: 20,
    selectedIds: new Set(),
};

// --- Helper Functions ---
function escapeHTML(str) {
    if (str === null || str === undefined) return '';
    return str.toString()
        .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;').replace(/'/g, '&#039;');
}

function formatTimestamp(ts) {
    if (!ts || ts.startsWith('0001')) return 'N/A';
    return new Date(ts).toLocaleString();
}

// --- Rendering Logic ---
function render() {
    // 1. Filter
    let filtered = allProxiesCache.filter(p => {
        const searchText = viewState.filterText.toLowerCase();
        const address = `${p.ip}:${p.port}`;
        const textMatch = address.includes(searchText) || (p.country && p.country.toLowerCase().includes(searchText));
        
        const isHealthy = p.verified_protocol !== "";
        const statusMatch = viewState.statusFilter === 'all' || 
                            (viewState.statusFilter === 'healthy' && isHealthy) ||
                            (viewState.statusFilter === 'unhealthy' && !isHealthy);
        
        return textMatch && statusMatch;
    });

    // 2. Paginate
    const startIndex = (viewState.currentPage - 1) * viewState.itemsPerPage;
    const endIndex = startIndex + viewState.itemsPerPage;
    const pageItems = filtered.slice(startIndex, endIndex);

    // 3. Render Table
    poolListBody.innerHTML = '';
    if (pageItems.length === 0) {
        const row = poolListBody.insertRow();
        row.insertCell().colSpan = 10;
        row.cells[0].textContent = 'No proxies match the current filters.';
        row.cells[0].style.textAlign = 'center';
    } else {
        pageItems.forEach(p => {
            const row = poolListBody.insertRow();
            row.dataset.id = p.id;
            // The checked state will be set by updateSelectionState() after rendering
            const isChecked = viewState.selectedIds.has(p.id);

            const statusClass = p.verified_protocol ? 'status-up' : 'status-down';
            const statusText = p.verified_protocol ? 'Healthy' : 'Unhealthy';

             row.innerHTML = `
                <td class="checkbox-col"><input type="checkbox" class="proxy-checkbox" data-id="${p.id}"></td>
                <td><span class="status-indicator ${statusClass}" title="${statusText}"></span></td>
                <td>${escapeHTML(p.verified_protocol || p.scraped_protocol)}</td>
                <td>${escapeHTML(p.ip)}:${p.port}</td>
                <td>${p.latency > 0 ? (p.latency / 1000000).toFixed(2) + 'ms' : '-'}</td>
                <td>${p.success_count}</td>
                <td>${p.failure_count}</td>
                <td>${escapeHTML(p.country) || 'N/A'}</td>
                <td>${escapeHTML(p.in_use_by) || '-'}</td>
                <td>${formatTimestamp(p.last_checked)}</td>
            `;
        });
    }

    // 4. Render Pagination
    renderPagination(filtered.length);

    // 5. Update Selection State (instead of full re-render)
    updateSelectionState();
}

function renderPagination(totalItems) {
    const totalPages = Math.ceil(totalItems / viewState.itemsPerPage);
    paginationControls.innerHTML = '';

    if (totalPages <= 1) return;

    // Previous Button
    const prevBtn = document.createElement('button');
    prevBtn.textContent = '«';
    prevBtn.disabled = viewState.currentPage === 1;
    prevBtn.addEventListener('click', () => {
        if (viewState.currentPage > 1) {
            viewState.currentPage--;
            render();
        }
    });
    paginationControls.appendChild(prevBtn);

    // Page numbers
    for (let i = 1; i <= totalPages; i++) {
        const pageBtn = document.createElement('button');
        pageBtn.textContent = i;
        if (i === viewState.currentPage) {
            pageBtn.classList.add('active');
        }
        pageBtn.addEventListener('click', () => {
            viewState.currentPage = i;
            render();
        });
        paginationControls.appendChild(pageBtn);
    }

    // Next Button
    const nextBtn = document.createElement('button');
    nextBtn.textContent = '»';
    nextBtn.disabled = viewState.currentPage === totalPages;
    nextBtn.addEventListener('click', () => {
        if (viewState.currentPage < totalPages) {
            viewState.currentPage++;
            render();
        }
    });
    paginationControls.appendChild(nextBtn);
}

// --- NEW FUNCTION: Fine-grained update for selection ---
function updateSelectionState() {
    // Update all visible checkboxes
    poolListBody.querySelectorAll('.proxy-checkbox').forEach(checkbox => {
        const id = checkbox.dataset.id;
        checkbox.checked = viewState.selectedIds.has(id);
    });

    // Update bulk action buttons
    testSelectedBtn.disabled = deleteSelectedBtn.disabled = viewState.selectedIds.size === 0;

    // Update "Select All" checkbox
    const visibleCheckboxes = Array.from(poolListBody.querySelectorAll('.proxy-checkbox'));
    const allVisibleSelected = visibleCheckboxes.length > 0 && visibleCheckboxes.every(cb => cb.checked);

    selectAllCheckbox.checked = allVisibleSelected;
    selectAllCheckbox.indeterminate = !allVisibleSelected && visibleCheckboxes.some(cb => cb.checked);
}

// --- Data Fetching ---
async function fetchDataAndRender() {
    try {
        allProxiesCache = await fetchProxyPoolStatus();
        render();
    } catch (error) {
        console.error("Failed to fetch proxy pool status:", error);
        poolListBody.innerHTML = '<tr><td colspan="10" class="error-message">Failed to load proxy pool data.</td></tr>';
    }
}

// --- Event Handlers ---
function handleSelection(e) {
    const target = e.target;
    if (target.id === 'select-all-proxies') {
        const isChecked = target.checked;
        const visibleIds = Array.from(poolListBody.querySelectorAll('.proxy-checkbox')).map(cb => cb.dataset.id);
        visibleIds.forEach(id => {
            if (isChecked) {
                viewState.selectedIds.add(id);
            } else {
                viewState.selectedIds.delete(id);
            }
        });
    } else if (target.classList.contains('proxy-checkbox')) {
        const id = target.dataset.id;
        if (target.checked) {
            viewState.selectedIds.add(id);
        } else {
            viewState.selectedIds.delete(id);
        }
    }
    // DO NOT call render() here. Call the fine-grained update instead.
    updateSelectionState();
}

async function handleTestSelected() {
    const idsToTest = Array.from(viewState.selectedIds);
    if (idsToTest.length === 0) return;

    testSelectedBtn.disabled = true;
    testSelectedBtn.textContent = 'Testing...';
    try {
        await validateProxies(idsToTest);
        updateStatusMessage(`Triggered validation for ${idsToTest.length} proxies. List will refresh.`);
        setTimeout(fetchDataAndRender, 3000); // Give backend time to process
    } catch (error) {
        alert('Error triggering validation: ' + error.message);
    } finally {
        testSelectedBtn.textContent = 'Test Selected';
        viewState.selectedIds.clear();
        render(); // Full re-render is appropriate here to reset everything.
    }
}

async function handleDeleteSelected() {
    const idsToDelete = Array.from(viewState.selectedIds);
    if (idsToDelete.length === 0 || !confirm(`Are you sure you want to delete ${idsToDelete.length} proxies?`)) {
        return;
    }

    deleteSelectedBtn.disabled = true;
    deleteSelectedBtn.textContent = 'Deleting...';
    try {
        await deleteProxies(idsToDelete);
        updateStatusMessage(`Successfully deleted ${idsToDelete.length} proxies.`);
        // Optimistic UI update
        allProxiesCache = allProxiesCache.filter(p => !idsToDelete.includes(p.id));
        viewState.selectedIds.clear();
        render();
    } catch (error) {
        alert('Error deleting proxies: ' + error.message);
        await fetchDataAndRender(); // Re-sync with backend on error
    } finally {
        deleteSelectedBtn.textContent = 'Delete Selected';
        // Re-render will handle button state
    }
}

// --- Initialization ---
export function initializeProxyPoolPage() {
    // Filter listeners
    filterInput.addEventListener('input', () => {
        viewState.filterText = filterInput.value;
        viewState.currentPage = 1;
        render();
    });
    statusFilterSelect.addEventListener('change', () => {
        viewState.statusFilter = statusFilterSelect.value;
        viewState.currentPage = 1;
        render();
    });

    // --- NEW: Refresh button listener ---
    refreshBtn.addEventListener('click', async () => {
        refreshBtn.textContent = 'Refreshing...';
        refreshBtn.disabled = true;
        await fetchDataAndRender();
        refreshBtn.textContent = 'Refresh';
        refreshBtn.disabled = false;
    });
    
    // Selection listeners
    poolListBody.addEventListener('click', handleSelection);
    selectAllCheckbox.addEventListener('click', handleSelection);

    // Bulk action listeners
    testSelectedBtn.addEventListener('click', handleTestSelected);
    deleteSelectedBtn.addEventListener('click', handleDeleteSelected);

    // Initial fetch
    fetchDataAndRender();
    
    // Return the polling function
    return fetchDataAndRender;
}
