// Color cycle order
const colorCycle = ['grey', 'green', 'blue', 'yellow', 'orange', 'red'];

// Delete modal state
let deleteHostId = null;

// ===== Filtering =====

function getActiveFilterColors() {
    return Array.from(document.querySelectorAll('.color-filter-dot.active')).map(d => d.dataset.filterColor);
}

function filterHosts() {
    const searchVal = document.getElementById('filterSearch').value.toLowerCase();
    const activeColors = getActiveFilterColors();

    document.querySelectorAll('.host-row').forEach(row => {
        const ip = (row.dataset.ip || '').toLowerCase();
        const hostname = (row.dataset.hostname || '').toLowerCase();
        const color = row.dataset.color || '';

        const matchesSearch = ip.includes(searchVal) || hostname.includes(searchVal);
        const matchesColor = activeColors.length === 6 || activeColors.includes(color);

        row.style.display = (matchesSearch && matchesColor) ? '' : 'none';
    });

    updateSelectionAfterFilter();
}

function toggleColorFilter(dot) {
    dot.classList.toggle('active');
    filterHosts();
}

function clearFilters() {
    document.getElementById('filterSearch').value = '';
    // Re-enable all color filter dots
    document.querySelectorAll('.color-filter-dot').forEach(d => d.classList.add('active'));
    filterHosts();
}

// Attach filter listener for search input
document.getElementById('filterSearch').addEventListener('input', filterHosts);

// ===== Color Cycling =====

function cycleColor(dot, event) {
    if (event) event.stopPropagation();
    const hostId = dot.dataset.hostId;
    const currentColor = dot.dataset.color;
    const currentIdx = colorCycle.indexOf(currentColor);
    const nextColor = colorCycle[(currentIdx + 1) % colorCycle.length];

    // Optimistic DOM update
    dot.className = 'color-dot color-' + nextColor;
    dot.dataset.color = nextColor;
    dot.closest('tr').dataset.color = nextColor;

    const formData = new FormData();
    formData.append('host_id', hostId);
    formData.append('color', nextColor);

    fetch('/projects/' + projectId + '/hosts/color', { method: 'POST', body: formData })
        .then(r => r.json())
        .then(data => {
            if (!data.success) {
                // Revert on failure
                dot.className = 'color-dot color-' + currentColor;
                dot.dataset.color = currentColor;
                dot.closest('tr').dataset.color = currentColor;
            }
        })
        .catch(() => {
            // Revert on failure
            dot.className = 'color-dot color-' + currentColor;
            dot.dataset.color = currentColor;
            dot.closest('tr').dataset.color = currentColor;
        });
}

// ===== Checkbox Selection =====

function getSelectedIds() {
    return Array.from(document.querySelectorAll('.row-checkbox:checked')).map(cb => cb.value);
}

function updateSelection() {
    const checkboxes = document.querySelectorAll('.row-checkbox');
    const checked = document.querySelectorAll('.row-checkbox:checked');
    const bar = document.getElementById('selectionBar');
    const selectAll = document.getElementById('selectAll');

    if (checked.length > 0) {
        bar.classList.add('active');
        document.getElementById('selectionCount').textContent = checked.length + ' selected';
    } else {
        bar.classList.remove('active');
    }

    if (selectAll) {
        const visibleCheckboxes = Array.from(checkboxes).filter(cb => cb.closest('tr').style.display !== 'none');
        const checkedVisible = visibleCheckboxes.filter(cb => cb.checked);
        selectAll.checked = visibleCheckboxes.length > 0 && checkedVisible.length === visibleCheckboxes.length;
        selectAll.indeterminate = checkedVisible.length > 0 && checkedVisible.length < visibleCheckboxes.length;
    }
}

function updateSelectionAfterFilter() {
    document.querySelectorAll('.host-row').forEach(row => {
        if (row.style.display === 'none') {
            const cb = row.querySelector('.row-checkbox');
            if (cb) cb.checked = false;
        }
    });
    updateSelection();
}

function toggleSelectAll() {
    const selectAll = document.getElementById('selectAll');
    document.querySelectorAll('.row-checkbox').forEach(cb => {
        if (cb.closest('tr').style.display !== 'none') {
            cb.checked = selectAll.checked;
        }
    });
    updateSelection();
}

// ===== Delete Single Host =====

function showDeleteModal(id) {
    deleteHostId = id;
    document.getElementById('deleteModal').style.display = 'flex';
}

function closeDeleteModal() {
    document.getElementById('deleteModal').style.display = 'none';
    deleteHostId = null;
}

function confirmDelete() {
    if (!deleteHostId) return;

    const formData = new FormData();
    formData.append('host_id', deleteHostId);

    fetch('/projects/' + projectId + '/hosts/delete', { method: 'POST', body: formData })
        .then(r => r.json())
        .then(data => {
            closeDeleteModal();
            if (data.success) {
                window.location.reload();
            } else {
                showNotificationModal('Error', data.error || 'Failed to delete host');
            }
        })
        .catch(() => {
            closeDeleteModal();
            showNotificationModal('Error', 'Failed to delete host');
        });
}

// ===== Bulk Delete =====

function showBulkDeleteModal() {
    const ids = getSelectedIds();
    if (ids.length === 0) return;
    document.getElementById('bulkDeleteCount').textContent = ids.length;
    document.getElementById('bulkDeleteModal').style.display = 'flex';
}

function closeBulkDeleteModal() {
    document.getElementById('bulkDeleteModal').style.display = 'none';
}

function confirmBulkDelete() {
    const ids = getSelectedIds();
    if (ids.length === 0) return;

    const formData = new FormData();
    formData.append('ids', ids.join(','));

    fetch('/projects/' + projectId + '/hosts/bulk-delete', { method: 'POST', body: formData })
        .then(r => r.json())
        .then(data => {
            closeBulkDeleteModal();
            if (data.success) {
                window.location.reload();
            } else {
                showNotificationModal('Error', data.error || 'Failed to delete hosts');
            }
        })
        .catch(() => {
            closeBulkDeleteModal();
            showNotificationModal('Error', 'Failed to delete hosts');
        });
}

// ===== Bulk Color Change =====

function bulkSetColor(color) {
    const ids = getSelectedIds();
    if (ids.length === 0) return;

    const formData = new FormData();
    formData.append('ids', ids.join(','));
    formData.append('color', color);

    fetch('/projects/' + projectId + '/hosts/bulk-color', { method: 'POST', body: formData })
        .then(r => r.json())
        .then(data => {
            if (data.success) {
                // Update all selected rows in the DOM
                ids.forEach(id => {
                    const row = document.querySelector('.host-row[data-id="' + id + '"]');
                    if (row) {
                        row.dataset.color = color;
                        const dot = row.querySelector('.color-dot');
                        if (dot) {
                            dot.className = 'color-dot color-' + color;
                            dot.dataset.color = color;
                        }
                    }
                });
            }
        })
        .catch(() => {});
}

// ===== Add Dropdown =====

function toggleAddDropdown() {
    document.getElementById('addDropdown').classList.toggle('active');
}

// Close add dropdown when clicking outside
document.addEventListener('click', (e) => {
    if (!e.target.closest('.add-dropdown')) {
        const dd = document.getElementById('addDropdown');
        if (dd) dd.classList.remove('active');
    }
});

// ===== Add Host Modal =====

function openAddHostModal() {
    document.getElementById('addDropdown').classList.remove('active');
    document.getElementById('newHostIP').value = '';
    document.getElementById('addHostModal').style.display = 'flex';
    document.getElementById('newHostIP').focus();
}

function closeAddHostModal() {
    document.getElementById('addHostModal').style.display = 'none';
}

function addHost() {
    const ip = document.getElementById('newHostIP').value.trim();
    if (!ip) return;

    const formData = new FormData();
    formData.append('ip_address', ip);

    fetch('/projects/' + projectId + '/hosts/add', { method: 'POST', body: formData })
        .then(r => r.json())
        .then(data => {
            closeAddHostModal();
            if (data.success) {
                window.location.reload();
            } else {
                showNotificationModal('Error', data.error || 'Failed to add host');
            }
        })
        .catch(() => {
            closeAddHostModal();
            showNotificationModal('Error', 'Failed to add host');
        });
}

// ===== Bulk Add Modal =====

function openBulkAddModal() {
    document.getElementById('addDropdown').classList.remove('active');
    document.getElementById('bulkHostIPs').value = '';
    document.getElementById('bulkAddModal').style.display = 'flex';
    document.getElementById('bulkHostIPs').focus();
}

function closeBulkAddModal() {
    document.getElementById('bulkAddModal').style.display = 'none';
}

function addBulkHosts() {
    const text = document.getElementById('bulkHostIPs').value.trim();
    if (!text) return;

    const formData = new FormData();
    formData.append('ip_addresses', text);

    fetch('/projects/' + projectId + '/hosts/bulk-add', { method: 'POST', body: formData })
        .then(r => r.json())
        .then(data => {
            closeBulkAddModal();
            if (data.success) {
                window.location.reload();
            } else {
                showNotificationModal('Error', data.error || 'Failed to add hosts');
            }
        })
        .catch(() => {
            closeBulkAddModal();
            showNotificationModal('Error', 'Failed to add hosts');
        });
}

// ===== Init =====

// Reset checkboxes on page load (browser may restore form state)
document.querySelectorAll('.host-checkbox').forEach(cb => cb.checked = false);
if (document.getElementById('selectionBar')) {
    document.getElementById('selectionBar').classList.remove('active');
}

// Allow Enter key in add host modal
document.getElementById('newHostIP')?.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') addHost();
});
