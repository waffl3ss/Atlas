// Color cycle order
const colorCycle = ['grey', 'green', 'blue', 'yellow', 'orange', 'red'];

// ===== Tab Switching =====

function getActiveTab() {
    const active = document.querySelector('.tab.active');
    return active ? active.dataset.tab : 'description';
}

function updateNavLinks() {
    const tab = getActiveTab();
    const prev = document.getElementById('prevFindingLink');
    const next = document.getElementById('nextFindingLink');
    if (prev) prev.href = prev.href.split('?')[0] + '?tab=' + tab;
    if (next) next.href = next.href.split('?')[0] + '?tab=' + tab;
}

function switchTab(btn) {
    const tabName = btn.dataset.tab;
    document.querySelectorAll('.tab').forEach(t => t.classList.remove('active'));
    btn.classList.add('active');
    document.querySelectorAll('.tab-panel').forEach(p => p.classList.remove('active'));
    document.getElementById('tab-' + tabName).classList.add('active');
    updateNavLinks();
}

(function() {
    const params = new URLSearchParams(window.location.search);
    const tab = params.get('tab');
    if (tab) {
        const btn = document.querySelector('.tab[data-tab="' + tab + '"]');
        if (btn) switchTab(btn);
    }
    // Remove the cloak style that prevented tab flash during load
    var cloak = document.getElementById('tab-cloak');
    if (cloak) cloak.remove();
    updateNavLinks();
})();

function reloadWithTab() {
    const tab = getActiveTab();
    const url = window.location.pathname + '?tab=' + tab;
    window.location.href = url;
}

// ===== Color Cycling =====

function cycleColor(dot, evt) {
    if (evt) evt.stopPropagation();
    const currentColor = dot.dataset.color;
    const currentIdx = colorCycle.indexOf(currentColor);
    const nextColor = colorCycle[(currentIdx + 1) % colorCycle.length];

    // Update all color dots on the page for this finding
    document.querySelectorAll('.color-dot[data-finding-id="' + findingId + '"]').forEach(d => {
        colorCycle.forEach(c => d.classList.remove('color-' + c));
        d.classList.add('color-' + nextColor);
        d.dataset.color = nextColor;
    });

    const formData = new FormData();
    formData.append('finding_id', findingId);
    formData.append('color', nextColor);

    fetch('/projects/' + projectId + '/findings/color', { method: 'POST', body: formData })
        .then(r => r.json())
        .then(data => {
            if (!data.success) {
                document.querySelectorAll('.color-dot[data-finding-id="' + findingId + '"]').forEach(d => {
                    colorCycle.forEach(c => d.classList.remove('color-' + c));
                    d.classList.add('color-' + currentColor);
                    d.dataset.color = currentColor;
                });
            }
        })
        .catch(() => {
            document.querySelectorAll('.color-dot[data-finding-id="' + findingId + '"]').forEach(d => {
                colorCycle.forEach(c => d.classList.remove('color-' + c));
                d.classList.add('color-' + currentColor);
                d.dataset.color = currentColor;
            });
        });
}

// ===== Edit Field Modal =====

let editField = null;

function openEditModal(field, currentValue) {
    editField = field;
    document.getElementById('editFieldLabel').textContent = field.charAt(0).toUpperCase() + field.slice(1);
    document.getElementById('editFieldValue').value = currentValue || '';
    document.getElementById('editFieldModal').style.display = 'flex';
    document.getElementById('editFieldValue').focus();
}

function closeEditModal() {
    document.getElementById('editFieldModal').style.display = 'none';
    editField = null;
}

function cvssToSeverity(score) {
    if (score >= 9.0) return 'critical';
    if (score >= 7.0) return 'high';
    if (score >= 4.0) return 'medium';
    if (score > 0) return 'low';
    return 'informational';
}

function saveField() {
    if (!editField) return;
    const value = document.getElementById('editFieldValue').value;

    const formData = new FormData();
    formData.append('field', editField);
    formData.append('value', value);

    fetch('/projects/' + projectId + '/findings/' + findingId + '/update', { method: 'POST', body: formData })
        .then(r => r.json())
        .then(data => {
            closeEditModal();
            if (data.success) {
                // If CVSS was updated, refresh severity badge inline
                if (editField === 'cvss_score' && data.severity) {
                    const cvssEl = document.getElementById('cvssDisplay');
                    const sevEl = document.getElementById('sevBadge');
                    if (cvssEl) {
                        const score = parseFloat(value) || 0;
                        cvssEl.textContent = score.toFixed(1);
                        cvssEl.className = 'cvss-score';
                        if (score >= 9.0) cvssEl.classList.add('cvss-critical');
                        else if (score >= 7.0) cvssEl.classList.add('cvss-high');
                        else if (score >= 4.0) cvssEl.classList.add('cvss-medium');
                        else if (score > 0) cvssEl.classList.add('cvss-low');
                        else cvssEl.classList.add('cvss-none');
                    }
                    if (sevEl) {
                        sevEl.className = 'sev-badge sev-' + data.severity;
                        sevEl.textContent = data.severity;
                    }
                } else {
                    reloadWithTab();
                }
            } else {
                showNotificationModal('Error', data.error || 'Failed to update');
            }
        })
        .catch(() => {
            closeEditModal();
            showNotificationModal('Error', 'Failed to update');
        });
}

// ===== Edit Title (inline from info panel) =====

function openEditTitleModal() {
    const current = document.getElementById('titleDisplay').textContent.trim();
    openEditModal('title', current === '\u2014' ? '' : current);
}

function openEditCvssModal() {
    const current = document.getElementById('cvssDisplay').textContent.trim();
    openEditModal('cvss_score', current === '\u2014' ? '' : current);
}

// ===== CVE CRUD =====

function openAddCveModal() {
    document.getElementById('newCveInput').value = '';
    document.getElementById('addCveModal').style.display = 'flex';
    document.getElementById('newCveInput').focus();
}

function closeAddCveModal() {
    document.getElementById('addCveModal').style.display = 'none';
}

function addCve() {
    const cve = document.getElementById('newCveInput').value.trim();
    if (!cve) return;

    const formData = new FormData();
    formData.append('cve', cve);

    fetch('/projects/' + projectId + '/findings/' + findingId + '/cves/add', { method: 'POST', body: formData })
        .then(r => r.json())
        .then(data => {
            closeAddCveModal();
            if (data.success) {
                reloadWithTab();
            } else {
                showNotificationModal('Error', data.error || 'Failed to add CVE');
            }
        })
        .catch(() => {
            closeAddCveModal();
            showNotificationModal('Error', 'Failed to add CVE');
        });
}

let deleteCveId = null;

function showDeleteCveModal(id) {
    deleteCveId = id;
    document.getElementById('deleteCveModal').style.display = 'flex';
}

function closeDeleteCveModal() {
    document.getElementById('deleteCveModal').style.display = 'none';
    deleteCveId = null;
}

function confirmDeleteCve() {
    if (!deleteCveId) return;

    const formData = new FormData();
    formData.append('cve_id', deleteCveId);

    fetch('/projects/' + projectId + '/findings/' + findingId + '/cves/delete', { method: 'POST', body: formData })
        .then(r => r.json())
        .then(data => {
            closeDeleteCveModal();
            if (data.success) {
                reloadWithTab();
            } else {
                showNotificationModal('Error', data.error || 'Failed to delete CVE');
            }
        })
        .catch(() => {
            closeDeleteCveModal();
            showNotificationModal('Error', 'Failed to delete CVE');
        });
}

// CVE checkbox selection
function getSelectedCveIds() {
    return Array.from(document.querySelectorAll('.cve-row-checkbox:checked')).map(cb => cb.value);
}

function updateCveSelection() {
    const checkboxes = document.querySelectorAll('.cve-row-checkbox');
    const checked = document.querySelectorAll('.cve-row-checkbox:checked');
    const bar = document.getElementById('cveSelectionBar');
    const selectAll = document.getElementById('cveSelectAll');

    if (checked.length > 0) {
        bar.classList.add('active');
        document.getElementById('cveSelectionCount').textContent = checked.length + ' selected';
    } else {
        bar.classList.remove('active');
    }

    if (selectAll) {
        selectAll.checked = checkboxes.length > 0 && checked.length === checkboxes.length;
        selectAll.indeterminate = checked.length > 0 && checked.length < checkboxes.length;
    }
}

function toggleCveSelectAll() {
    const selectAll = document.getElementById('cveSelectAll');
    document.querySelectorAll('.cve-row-checkbox').forEach(cb => {
        cb.checked = selectAll.checked;
    });
    updateCveSelection();
}

function showBulkDeleteCveModal() {
    const ids = getSelectedCveIds();
    if (ids.length === 0) return;
    document.getElementById('bulkDeleteCveCount').textContent = ids.length;
    document.getElementById('bulkDeleteCveModal').style.display = 'flex';
}

function closeBulkDeleteCveModal() {
    document.getElementById('bulkDeleteCveModal').style.display = 'none';
}

function confirmBulkDeleteCves() {
    const ids = getSelectedCveIds();
    if (ids.length === 0) return;

    const formData = new FormData();
    formData.append('ids', ids.join(','));

    fetch('/projects/' + projectId + '/findings/' + findingId + '/cves/bulk-delete', { method: 'POST', body: formData })
        .then(r => r.json())
        .then(data => {
            closeBulkDeleteCveModal();
            if (data.success) {
                reloadWithTab();
            } else {
                showNotificationModal('Error', data.error || 'Failed to delete CVEs');
            }
        })
        .catch(() => {
            closeBulkDeleteCveModal();
            showNotificationModal('Error', 'Failed to delete CVEs');
        });
}

// ===== Host Add Dropdown =====

function toggleHostAddDropdown(evt) {
    evt.stopPropagation();
    const menu = document.getElementById('hostAddDropdownMenu');
    menu.classList.toggle('active');
}

document.addEventListener('click', function() {
    const menu = document.getElementById('hostAddDropdownMenu');
    if (menu) menu.classList.remove('active');
});

// ===== Affected Host CRUD =====

let selectedHostId = null;

function openAddHostModal() {
    clearSelectedHost();
    document.getElementById('addHostModal').style.display = 'flex';
    document.getElementById('hostSearchInput').focus();
}

function closeAddHostModal() {
    document.getElementById('addHostModal').style.display = 'none';
    clearSelectedHost();
    document.getElementById('hostSearchInput').value = '';
    document.getElementById('hostSearchResults').classList.remove('active');
    document.getElementById('hostSearchResults').innerHTML = '';
}

// Searchable host input
document.getElementById('hostSearchInput')?.addEventListener('input', function() {
    const query = this.value.trim().toLowerCase();
    const resultsEl = document.getElementById('hostSearchResults');

    if (!query) {
        resultsEl.classList.remove('active');
        resultsEl.innerHTML = '';
        return;
    }

    const matches = allHosts.filter(h =>
        h.ip.toLowerCase().includes(query) ||
        (h.hostname && h.hostname.toLowerCase().includes(query))
    );

    if (matches.length === 0) {
        resultsEl.innerHTML = '<div class="host-search-item" style="color: var(--text-secondary); cursor: default;">No matches</div>';
        resultsEl.classList.add('active');
        return;
    }

    resultsEl.innerHTML = matches.map(h =>
        '<div class="host-search-item" data-hid="' + h.id + '" data-ip="' + escapeHtml(h.ip) + '" data-hostname="' + escapeHtml(h.hostname || '') + '" onclick="selectHost(+this.dataset.hid, this.dataset.ip, this.dataset.hostname)">' +
            escapeHtml(h.ip) +
            (h.hostname ? '<span class="host-search-hostname">(' + escapeHtml(h.hostname) + ')</span>' : '') +
        '</div>'
    ).join('');
    resultsEl.classList.add('active');
});

document.getElementById('hostSearchInput')?.addEventListener('keydown', function(e) {
    if (e.key === 'Escape') {
        document.getElementById('hostSearchResults').classList.remove('active');
    }
});

function selectHost(id, ip, hostname) {
    selectedHostId = id;
    document.getElementById('addHostId').value = id;

    // Hide search, show chip
    document.getElementById('hostSearchWrapper').style.display = 'none';
    const chip = document.getElementById('selectedHostDisplay');
    document.getElementById('selectedHostLabel').textContent = ip + (hostname ? ' (' + hostname + ')' : '');
    chip.style.display = 'flex';

    // Hide search results
    document.getElementById('hostSearchResults').classList.remove('active');

    // Load services for this host
    loadHostServices(id);
}

function clearSelectedHost() {
    selectedHostId = null;
    document.getElementById('addHostId').value = '';

    // Show search, hide chip
    document.getElementById('hostSearchWrapper').style.display = 'block';
    document.getElementById('selectedHostDisplay').style.display = 'none';
    document.getElementById('hostSearchInput').value = '';

    // Reset port dropdown
    const portSelect = document.getElementById('addHostPortSelect');
    portSelect.innerHTML = '<option value="">-- Select host first --</option>';
    portSelect.disabled = true;
}

function loadHostServices(hostId) {
    const portSelect = document.getElementById('addHostPortSelect');
    portSelect.innerHTML = '<option value="">Loading...</option>';
    portSelect.disabled = true;

    fetch('/projects/' + projectId + '/hosts/' + hostId + '/services/json')
        .then(r => r.json())
        .then(data => {
            portSelect.innerHTML = '<option value="">-- No service --</option>';
            if (data.services && data.services.length > 0) {
                data.services.forEach(svc => {
                    const opt = document.createElement('option');
                    opt.value = svc.port + '|' + (svc.protocol || '');
                    let label = svc.port.toString();
                    if (svc.service_name) label += ' - ' + svc.service_name.toUpperCase();
                    if (svc.protocol) label += ' (' + svc.protocol + ')';
                    opt.textContent = label;
                    portSelect.appendChild(opt);
                });
            }
            portSelect.disabled = false;
        })
        .catch(() => {
            portSelect.innerHTML = '<option value="">-- No service --</option>';
            portSelect.disabled = false;
        });
}

function onPortSelectionChange() {
    // No additional logic needed — port and protocol are derived from the selected service
}

function addAffectedHost() {
    const hostId = document.getElementById('addHostId').value;
    if (!hostId) {
        showNotificationModal('Error', 'Please select a host');
        return;
    }

    let port = '';
    let protocol = '';
    const portSelectVal = document.getElementById('addHostPortSelect').value;

    if (portSelectVal && portSelectVal !== '') {
        const parts = portSelectVal.split('|');
        port = parts[0];
        protocol = parts[1] || '';
    }

    const formData = new FormData();
    formData.append('host_id', hostId);
    formData.append('port', port);
    formData.append('protocol', protocol);

    fetch('/projects/' + projectId + '/findings/' + findingId + '/hosts/add', { method: 'POST', body: formData })
        .then(r => r.json())
        .then(data => {
            closeAddHostModal();
            if (data.success) {
                reloadWithTab();
            } else {
                showNotificationModal('Error', data.error || 'Failed to add host');
            }
        })
        .catch(() => {
            closeAddHostModal();
            showNotificationModal('Error', 'Failed to add host');
        });
}

// ===== Bulk Add Hosts =====

function openBulkAddHostModal() {
    document.getElementById('bulkAddHostEntries').value = '';
    document.getElementById('bulkAddResults').style.display = 'none';
    document.getElementById('bulkAddResults').innerHTML = '';
    document.getElementById('bulkAddSubmitBtn').textContent = 'Add All';
    document.getElementById('bulkAddSubmitBtn').onclick = submitBulkAddHosts;
    document.getElementById('bulkAddHostModal').style.display = 'flex';
    document.getElementById('bulkAddHostEntries').focus();
}

function closeBulkAddHostModal() {
    document.getElementById('bulkAddHostModal').style.display = 'none';
}

function submitBulkAddHosts() {
    const entries = document.getElementById('bulkAddHostEntries').value.trim();
    if (!entries) return;

    const btn = document.getElementById('bulkAddSubmitBtn');
    btn.textContent = 'Adding...';
    btn.disabled = true;

    const formData = new FormData();
    formData.append('entries', entries);

    fetch('/projects/' + projectId + '/findings/' + findingId + '/hosts/bulk-add', { method: 'POST', body: formData })
        .then(r => r.json())
        .then(data => {
            const added = data.added || 0;

            if (data.results) {
                const resultsEl = document.getElementById('bulkAddResults');
                resultsEl.innerHTML = data.results.map(r =>
                    '<div class="bulk-result-line ' + (r.success ? 'bulk-result-success' : 'bulk-result-fail') + '">' +
                        '<span class="result-icon">' + (r.success ? '&#10003;' : '&#10007;') + '</span>' +
                        '<span class="result-text">' + escapeHtml(r.line) + '</span>' +
                        (r.reason ? '<span class="result-reason">' + escapeHtml(r.reason) + '</span>' : '') +
                    '</div>'
                ).join('');
                resultsEl.style.display = 'block';
            }

            // Check if there were any failures
            const failedLines = data.results ? data.results.filter(r => !r.success) : [];

            if (failedLines.length > 0) {
                // Replace textarea content with only the failed lines so user can fix and retry
                document.getElementById('bulkAddHostEntries').value = failedLines.map(r => r.line).join('\n');
                btn.textContent = 'Retry';
                btn.disabled = false;
                btn.onclick = submitBulkAddHosts;
            } else {
                // All succeeded — close and reload immediately
                closeBulkAddHostModal();
                reloadWithTab();
            }
        })
        .catch(() => {
            btn.textContent = 'Add All';
            btn.disabled = false;
            showNotificationModal('Error', 'Failed to bulk add hosts');
        });
}

function escapeHtml(str) {
    if (!str) return '';
    const div = document.createElement('div');
    div.textContent = str;
    return div.innerHTML;
}

let deleteHostAssocId = null;

function showDeleteHostAssocModal(id) {
    deleteHostAssocId = id;
    document.getElementById('deleteHostAssocModal').style.display = 'flex';
}

function closeDeleteHostAssocModal() {
    document.getElementById('deleteHostAssocModal').style.display = 'none';
    deleteHostAssocId = null;
}

function confirmDeleteHostAssoc() {
    if (!deleteHostAssocId) return;

    const formData = new FormData();
    formData.append('finding_host_id', deleteHostAssocId);

    fetch('/projects/' + projectId + '/findings/' + findingId + '/hosts/delete', { method: 'POST', body: formData })
        .then(r => r.json())
        .then(data => {
            closeDeleteHostAssocModal();
            if (data.success) {
                reloadWithTab();
            } else {
                showNotificationModal('Error', data.error || 'Failed to remove host');
            }
        })
        .catch(() => {
            closeDeleteHostAssocModal();
            showNotificationModal('Error', 'Failed to remove host');
        });
}

// Host checkbox selection
function getSelectedHostAssocIds() {
    return Array.from(document.querySelectorAll('.host-assoc-row-checkbox:checked')).map(cb => cb.value);
}

function updateHostAssocSelection() {
    const checkboxes = document.querySelectorAll('.host-assoc-row-checkbox');
    const checked = document.querySelectorAll('.host-assoc-row-checkbox:checked');
    const bar = document.getElementById('hostAssocSelectionBar');
    const selectAll = document.getElementById('hostAssocSelectAll');

    if (checked.length > 0) {
        bar.classList.add('active');
        document.getElementById('hostAssocSelectionCount').textContent = checked.length + ' selected';
    } else {
        bar.classList.remove('active');
    }

    if (selectAll) {
        selectAll.checked = checkboxes.length > 0 && checked.length === checkboxes.length;
        selectAll.indeterminate = checked.length > 0 && checked.length < checkboxes.length;
    }
}

function toggleHostAssocSelectAll() {
    const selectAll = document.getElementById('hostAssocSelectAll');
    document.querySelectorAll('.host-assoc-row-checkbox').forEach(cb => {
        cb.checked = selectAll.checked;
    });
    updateHostAssocSelection();
}

function showBulkDeleteHostAssocModal() {
    const ids = getSelectedHostAssocIds();
    if (ids.length === 0) return;
    document.getElementById('bulkDeleteHostAssocCount').textContent = ids.length;
    document.getElementById('bulkDeleteHostAssocModal').style.display = 'flex';
}

function closeBulkDeleteHostAssocModal() {
    document.getElementById('bulkDeleteHostAssocModal').style.display = 'none';
}

function confirmBulkDeleteHostAssocs() {
    const ids = getSelectedHostAssocIds();
    if (ids.length === 0) return;

    const formData = new FormData();
    formData.append('ids', ids.join(','));

    fetch('/projects/' + projectId + '/findings/' + findingId + '/hosts/bulk-delete', { method: 'POST', body: formData })
        .then(r => r.json())
        .then(data => {
            closeBulkDeleteHostAssocModal();
            if (data.success) {
                reloadWithTab();
            } else {
                showNotificationModal('Error', data.error || 'Failed to remove hosts');
            }
        })
        .catch(() => {
            closeBulkDeleteHostAssocModal();
            showNotificationModal('Error', 'Failed to remove hosts');
        });
}

// ===== Delete Finding =====

function showDeleteFindingModal() {
    document.getElementById('deleteFindingModal').style.display = 'flex';
}

function closeDeleteFindingModal() {
    document.getElementById('deleteFindingModal').style.display = 'none';
}

function confirmDeleteFinding() {
    fetch('/projects/' + projectId + '/findings/' + findingId + '/delete', { method: 'POST' })
        .then(r => r.json())
        .then(data => {
            if (data.success) {
                window.location.href = '/projects/' + projectId + '/findings';
            } else {
                closeDeleteFindingModal();
                showNotificationModal('Error', data.error || 'Failed to delete finding');
            }
        })
        .catch(() => {
            closeDeleteFindingModal();
            showNotificationModal('Error', 'Failed to delete finding');
        });
}

// ===== CVSS Colorization =====

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

// ===== Keyboard Shortcuts =====

document.getElementById('newCveInput')?.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') addCve();
    if (e.key === 'Escape') closeAddCveModal();
});

document.getElementById('editFieldValue')?.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') closeEditModal();
});

// ===== Init =====

colorizeCvss();

// Reset checkboxes on page load
document.querySelectorAll('.cve-row-checkbox').forEach(cb => { cb.checked = false; });
document.querySelectorAll('.host-assoc-row-checkbox').forEach(cb => { cb.checked = false; });
