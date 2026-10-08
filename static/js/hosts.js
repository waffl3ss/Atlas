// Color cycle order
const colorCycle = ['grey', 'green', 'blue', 'yellow', 'orange', 'red'];

// Delete modal state
let deleteHostId = null;

// ===== Filtering =====

function getActiveFilterColors() {
    return Array.from(document.querySelectorAll('.color-filter-dot.active')).map(d => d.dataset.filterColor);
}

function getRowTags(row) {
    return (row.dataset.tags || '').split(',').filter(t => t !== '').map(t => t.toLowerCase());
}

function filterHosts() {
    const searchVal = document.getElementById('filterSearch').value.toLowerCase();
    const tagFilter = document.getElementById('filterTag').value.toLowerCase();
    const activeColors = getActiveFilterColors();

    document.querySelectorAll('.host-row').forEach(row => {
        const ip = (row.dataset.ip || '').toLowerCase();
        const hostname = (row.dataset.hostname || '').toLowerCase();
        const color = row.dataset.color || '';
        const tags = getRowTags(row);

        const matchesSearch = ip.includes(searchVal) || hostname.includes(searchVal) || tags.some(t => t.includes(searchVal));
        const matchesColor = activeColors.length === 6 || activeColors.includes(color);
        let matchesTag = true;
        if (tagFilter === '__untagged__') matchesTag = tags.length === 0;
        else if (tagFilter !== '') matchesTag = tags.includes(tagFilter);

        row.style.display = (matchesSearch && matchesColor && matchesTag) ? '' : 'none';
    });

    updateSelectionAfterFilter();
}

function toggleColorFilter(dot) {
    dot.classList.toggle('active');
    filterHosts();
}

function clearFilters() {
    document.getElementById('filterSearch').value = '';
    document.getElementById('filterTag').value = '';
    // Re-enable all color filter dots
    document.querySelectorAll('.color-filter-dot').forEach(d => d.classList.add('active'));
    filterHosts();
}

// Attach filter listeners
document.getElementById('filterSearch').addEventListener('input', filterHosts);
document.getElementById('filterTag').addEventListener('change', filterHosts);

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
            } else {
                showNotificationModal('Error', data.error || 'Failed to update colors');
            }
        })
        .catch(err => showNotificationModal('Error', 'Failed to update colors: ' + err.message));
}

// ===== Bulk Tagging =====

let bulkTagAction = 'add';

function openBulkTagModal(action) {
    if (getSelectedIds().length === 0) return;
    bulkTagAction = action;
    const count = getSelectedIds().length;
    document.getElementById('bulkTagTitle').textContent = (action === 'add' ? 'Add Tags to ' : 'Remove Tags from ') + count + ' host' + (count === 1 ? '' : 's');
    document.getElementById('bulkTagSubmit').textContent = action === 'add' ? 'Add' : 'Remove';
    document.getElementById('bulkTagSubmit').className = action === 'add' ? 'btn-primary' : 'btn-danger';
    document.getElementById('bulkTagInput').value = '';
    document.getElementById('bulkTagModal').style.display = 'flex';
    document.getElementById('bulkTagInput').focus();
}

function closeBulkTagModal() {
    document.getElementById('bulkTagModal').style.display = 'none';
}

function submitBulkTag() {
    const ids = getSelectedIds();
    const tags = document.getElementById('bulkTagInput').value.trim();
    if (ids.length === 0 || !tags) return;

    const formData = new FormData();
    formData.append('ids', ids.join(','));
    formData.append('tags', tags);
    formData.append('action', bulkTagAction);

    fetch('/projects/' + projectId + '/hosts/bulk-tag', { method: 'POST', body: formData })
        .then(r => r.json())
        .then(data => {
            closeBulkTagModal();
            if (data.success) {
                window.location.reload();
            } else {
                showNotificationModal('Error', data.error || 'Failed to update tags');
            }
        })
        .catch(err => {
            closeBulkTagModal();
            showNotificationModal('Error', 'Failed to update tags: ' + err.message);
        });
}

document.getElementById('bulkTagInput')?.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') submitBulkTag();
    if (e.key === 'Escape') closeBulkTagModal();
});

// ===== Export Selected IPs =====

function exportSelectedIPs() {
    const rows = getSelectedIds()
        .map(id => document.querySelector('.host-row[data-id="' + id + '"]'))
        .filter(row => row);
    if (rows.length === 0) return;

    // Rows are already rendered in numeric IP order
    const ips = rows.map(row => row.dataset.ip);
    const tagFilter = document.getElementById('filterTag').value;
    let filename = 'hosts_selected.txt';
    if (tagFilter && tagFilter !== '__untagged__') {
        filename = 'hosts_tag_' + tagFilter.replace(/[^A-Za-z0-9._-]+/g, '_') + '.txt';
    }
    downloadText(filename, ips.join('\n') + '\n');
}

function downloadText(filename, text) {
    const blob = new Blob([text], { type: 'text/plain' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
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
