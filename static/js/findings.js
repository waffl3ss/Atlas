// Color cycle order
const colorCycle = ['grey', 'green', 'blue', 'yellow', 'orange', 'red'];

// Delete modal state
let deleteFindingId = null;

// ===== Filtering =====

function getActiveColorFilters() {
    return Array.from(document.querySelectorAll('.color-filter-dot.active')).map(d => d.dataset.filterColor);
}

function getActiveSevFilters() {
    return Array.from(document.querySelectorAll('.sev-filter-btn.active')).map(d => d.dataset.filterSev);
}

function filterFindings() {
    const searchVal = document.getElementById('filterSearch').value.toLowerCase();
    const activeColors = getActiveColorFilters();
    const activeSevs = getActiveSevFilters();

    document.querySelectorAll('.finding-row').forEach(row => {
        const title = (row.dataset.title || '').toLowerCase();
        const color = row.dataset.color || 'grey';
        const severity = (row.dataset.severity || '').toLowerCase();

        const matchesSearch = title.includes(searchVal);
        const matchesColor = activeColors.length === 6 || activeColors.includes(color);
        const matchesSev = activeSevs.length === 5 || activeSevs.includes(severity);

        row.style.display = (matchesSearch && matchesColor && matchesSev) ? '' : 'none';
    });

    updateSelectionAfterFilter();
}

function toggleColorFilter(dot) {
    dot.classList.toggle('active');
    filterFindings();
}

function toggleSevFilter(btn) {
    btn.classList.toggle('active');
    filterFindings();
}

function clearFilters() {
    document.getElementById('filterSearch').value = '';
    document.querySelectorAll('.color-filter-dot').forEach(d => d.classList.add('active'));
    document.querySelectorAll('.sev-filter-btn').forEach(d => d.classList.add('active'));
    filterFindings();
}

document.getElementById('filterSearch').addEventListener('input', filterFindings);

// ===== Color Cycling =====

function cycleColor(dot, event) {
    if (event) event.stopPropagation();
    const findingId = dot.dataset.findingId;
    const currentColor = dot.dataset.color;
    const currentIdx = colorCycle.indexOf(currentColor);
    const nextColor = colorCycle[(currentIdx + 1) % colorCycle.length];

    dot.className = 'color-dot color-' + nextColor;
    dot.dataset.color = nextColor;

    // Update the row's data-color so filtering stays in sync
    const row = dot.closest('.finding-row');
    if (row) row.dataset.color = nextColor;

    const formData = new FormData();
    formData.append('finding_id', findingId);
    formData.append('color', nextColor);

    fetch('/projects/' + projectId + '/findings/color', { method: 'POST', body: formData })
        .then(r => r.json())
        .then(data => {
            if (!data.success) {
                dot.className = 'color-dot color-' + currentColor;
                dot.dataset.color = currentColor;
                if (row) row.dataset.color = currentColor;
            }
        })
        .catch(() => {
            dot.className = 'color-dot color-' + currentColor;
            dot.dataset.color = currentColor;
            if (row) row.dataset.color = currentColor;
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
    document.querySelectorAll('.finding-row').forEach(row => {
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

// ===== Delete Single Finding =====

function showDeleteModal(id) {
    deleteFindingId = id;
    document.getElementById('deleteModal').style.display = 'flex';
}

function closeDeleteModal() {
    document.getElementById('deleteModal').style.display = 'none';
    deleteFindingId = null;
}

function confirmDelete() {
    if (!deleteFindingId) return;

    const formData = new FormData();
    formData.append('finding_id', deleteFindingId);

    fetch('/projects/' + projectId + '/findings/delete', { method: 'POST', body: formData })
        .then(r => r.json())
        .then(data => {
            closeDeleteModal();
            if (data.success) {
                window.location.reload();
            } else {
                showNotificationModal('Error', data.error || 'Failed to delete finding');
            }
        })
        .catch(() => {
            closeDeleteModal();
            showNotificationModal('Error', 'Failed to delete finding');
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

    fetch('/projects/' + projectId + '/findings/bulk-delete', { method: 'POST', body: formData })
        .then(r => r.json())
        .then(data => {
            closeBulkDeleteModal();
            if (data.success) {
                window.location.reload();
            } else {
                showNotificationModal('Error', data.error || 'Failed to delete findings');
            }
        })
        .catch(() => {
            closeBulkDeleteModal();
            showNotificationModal('Error', 'Failed to delete findings');
        });
}

// ===== Bulk Color Change =====

function bulkSetColor(color) {
    const ids = getSelectedIds();
    if (ids.length === 0) return;

    const formData = new FormData();
    formData.append('ids', ids.join(','));
    formData.append('color', color);

    fetch('/projects/' + projectId + '/findings/bulk-color', { method: 'POST', body: formData })
        .then(r => r.json())
        .then(data => {
            if (data.success) {
                ids.forEach(id => {
                    const row = document.querySelector('.finding-row[data-id="' + id + '"]');
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

// ===== Add Finding Modal =====

function cvssToSeverityLabel(score) {
    if (score >= 9.0) return 'Critical';
    if (score >= 7.0) return 'High';
    if (score >= 4.0) return 'Medium';
    if (score > 0) return 'Low';
    return 'Informational';
}

function updateCvssHint() {
    const val = parseFloat(document.getElementById('newFindingCvss').value);
    const hint = document.getElementById('cvssSeverityHint');
    if (isNaN(val)) {
        hint.textContent = '';
    } else {
        hint.textContent = 'Severity: ' + cvssToSeverityLabel(val);
    }
}

function openAddFindingModal() {
    document.getElementById('newFindingTitle').value = '';
    document.getElementById('newFindingCvss').value = '';
    document.getElementById('cvssSeverityHint').textContent = '';
    document.getElementById('newFindingSynopsis').value = '';
    document.getElementById('newFindingDescription').value = '';
    document.getElementById('newFindingSolution').value = '';
    document.getElementById('addFindingModal').style.display = 'flex';
    document.getElementById('newFindingTitle').focus();
}

function closeAddFindingModal() {
    document.getElementById('addFindingModal').style.display = 'none';
}

function addFinding() {
    const title = document.getElementById('newFindingTitle').value.trim();
    const cvss = document.getElementById('newFindingCvss').value.trim();
    if (!title || cvss === '') {
        showNotificationModal('Error', 'Title and CVSS score are required');
        return;
    }

    const formData = new FormData();
    formData.append('title', title);
    formData.append('cvss_score', cvss);
    formData.append('synopsis', document.getElementById('newFindingSynopsis').value.trim());
    formData.append('description', document.getElementById('newFindingDescription').value.trim());
    formData.append('solution', document.getElementById('newFindingSolution').value.trim());

    fetch('/projects/' + projectId + '/findings/add', { method: 'POST', body: formData })
        .then(r => r.json())
        .then(data => {
            closeAddFindingModal();
            if (data.success) {
                window.location.reload();
            } else {
                showNotificationModal('Error', data.error || 'Failed to add finding');
            }
        })
        .catch(() => {
            closeAddFindingModal();
            showNotificationModal('Error', 'Failed to add finding');
        });
}

// ===== CVSS Score Colorization =====

function colorizeCvss() {
    document.querySelectorAll('.cvss-score').forEach(el => {
        const score = parseFloat(el.textContent) || 0;
        if (score >= 9.0) el.classList.add('cvss-critical');
        else if (score >= 7.0) el.classList.add('cvss-high');
        else if (score >= 4.0) el.classList.add('cvss-medium');
        else if (score > 0) el.classList.add('cvss-low');
        else el.classList.add('cvss-none');
    });
}

// ===== Init =====

document.querySelectorAll('.finding-checkbox').forEach(cb => cb.checked = false);
if (document.getElementById('selectionBar')) {
    document.getElementById('selectionBar').classList.remove('active');
}
colorizeCvss();

document.getElementById('newFindingTitle')?.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') closeAddFindingModal();
});
document.getElementById('newFindingCvss')?.addEventListener('input', updateCvssHint);
