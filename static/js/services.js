// Delete modal state
let deletePort = null;
let deleteProtocol = null;
let deleteService = null;
let deleteBanner = null;

// Filtering
function filterServices() {
    const portFilter = document.getElementById('filterPort').value.toLowerCase();
    const protocolFilter = document.getElementById('filterProtocol').value.toLowerCase();
    const serviceFilter = document.getElementById('filterService').value.toLowerCase();
    const bannerFilter = document.getElementById('filterBanner').value.toLowerCase();

    document.querySelectorAll('.service-row').forEach(row => {
        const port = row.dataset.port.toLowerCase();
        const protocol = (row.dataset.protocol || '').toLowerCase();
        const service = (row.dataset.service || '').toLowerCase();
        const banner = (row.dataset.banner || '').toLowerCase();

        const matches =
            port.includes(portFilter) &&
            protocol.includes(protocolFilter) &&
            service.includes(serviceFilter) &&
            banner.includes(bannerFilter);

        row.style.display = matches ? '' : 'none';
    });

    updateSelectionAfterFilter();
}

function clearFilters() {
    document.getElementById('filterPort').value = '';
    document.getElementById('filterProtocol').value = '';
    document.getElementById('filterService').value = '';
    document.getElementById('filterBanner').value = '';
    filterServices();
}

// Attach filter listeners
['filterPort', 'filterProtocol', 'filterService', 'filterBanner'].forEach(id => {
    const el = document.getElementById(id);
    if (el) el.addEventListener('input', filterServices);
});

// Selection
function getSelectedServices() {
    return Array.from(document.querySelectorAll('.row-checkbox:checked')).map(cb => ({
        port: parseInt(cb.dataset.port),
        protocol: cb.dataset.protocol || '',
        service: cb.dataset.service || '',
        banner: cb.dataset.banner || ''
    }));
}

function updateSelection() {
    const selected = getSelectedServices();
    const bar = document.getElementById('selectionBar');
    const count = document.getElementById('selectionCount');

    if (selected.length > 0) {
        bar.classList.add('active');
        count.textContent = selected.length + ' selected';
    } else {
        bar.classList.remove('active');
    }

    // Update select all checkbox state
    const allCheckboxes = document.querySelectorAll('.row-checkbox');
    const visibleCheckboxes = Array.from(allCheckboxes).filter(cb => cb.closest('tr').style.display !== 'none');
    const checkedVisible = visibleCheckboxes.filter(cb => cb.checked);
    const selectAll = document.getElementById('selectAll');

    if (selectAll) {
        selectAll.checked = visibleCheckboxes.length > 0 && checkedVisible.length === visibleCheckboxes.length;
        selectAll.indeterminate = checkedVisible.length > 0 && checkedVisible.length < visibleCheckboxes.length;
    }
}

function updateSelectionAfterFilter() {
    // Uncheck hidden rows
    document.querySelectorAll('.service-row').forEach(row => {
        if (row.style.display === 'none') {
            const cb = row.querySelector('.row-checkbox');
            if (cb) cb.checked = false;
        }
    });
    updateSelection();
}

function toggleSelectAll() {
    const selectAll = document.getElementById('selectAll');
    const checkboxes = document.querySelectorAll('.row-checkbox');

    checkboxes.forEach(cb => {
        if (cb.closest('tr').style.display !== 'none') {
            cb.checked = selectAll.checked;
        }
    });
    updateSelection();
}

// Host Panel
function showHostPanel(row) {
    const port = row.dataset.port;
    const protocol = row.dataset.protocol || '';
    const service = row.dataset.service || '';
    const banner = row.dataset.banner || '';

    // Highlight active row
    document.querySelectorAll('.service-row').forEach(r => r.classList.remove('active'));
    row.classList.add('active');

    // Update panel info
    const infoEl = document.getElementById('panelServiceInfo');
    infoEl.innerHTML = `
        <div class="port">${escapeHtml(port)}</div>
        <div class="protocol-service">${escapeHtml(protocol)}${service ? ' / ' + escapeHtml(service) : ''}</div>
        ${banner ? '<div class="banner">' + escapeHtml(banner) + '</div>' : ''}
    `;

    // Fetch hosts
    const params = new URLSearchParams({
        port: port,
        protocol: protocol,
        service: service,
        banner: banner
    });

    fetch(`/projects/${projectId}/services/hosts?${params}`)
        .then(r => r.json())
        .then(data => {
            const listEl = document.getElementById('panelHostList');
            if (data.hosts && data.hosts.length > 0) {
                const ips = data.hosts.map(h => h.ip_address).join('\n');
                listEl.innerHTML = `
                    <div class="host-ip-card">
                        <div class="host-ip-header">
                            <span class="host-ip-count">${data.hosts.length} host${data.hosts.length > 1 ? 's' : ''}</span>
                            <button class="btn-copy" onclick="copyIPs(this)" title="Copy to clipboard">📋</button>
                        </div>
                        <pre class="host-ip-list">${escapeHtml(ips)}</pre>
                    </div>
                `;
            } else {
                listEl.innerHTML = '<div class="host-ip-card"><div class="host-ip-header">No hosts found</div></div>';
            }
        })
        .catch(() => {
            document.getElementById('panelHostList').innerHTML = '<div class="host-ip-card"><div class="host-ip-header">Failed to load hosts</div></div>';
        });

    // Show panel
    document.getElementById('hostPanel').classList.add('active');
    document.getElementById('hostPanelOverlay').classList.add('active');
}

function closeHostPanel() {
    document.getElementById('hostPanel').classList.remove('active');
    document.getElementById('hostPanelOverlay').classList.remove('active');
    document.querySelectorAll('.service-row').forEach(r => r.classList.remove('active'));
}

function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

function copyIPs(btn) {
    const pre = btn.closest('.host-ip-card').querySelector('.host-ip-list');
    const text = pre.textContent;

    navigator.clipboard.writeText(text).then(() => {
        // Visual feedback
        const originalText = btn.textContent;
        btn.textContent = '✓';
        setTimeout(() => {
            btn.textContent = originalText;
        }, 1500);
    }).catch(() => {
        // Fallback for older browsers
        const textarea = document.createElement('textarea');
        textarea.value = text;
        document.body.appendChild(textarea);
        textarea.select();
        document.execCommand('copy');
        document.body.removeChild(textarea);

        const originalText = btn.textContent;
        btn.textContent = '✓';
        setTimeout(() => {
            btn.textContent = originalText;
        }, 1500);
    });
}

// Delete Single Service
function showDeleteModal(port, protocol, service, banner) {
    deletePort = port;
    deleteProtocol = protocol;
    deleteService = service;
    deleteBanner = banner;
    document.getElementById('deleteModal').style.display = 'flex';
}

function closeDeleteModal() {
    document.getElementById('deleteModal').style.display = 'none';
    deletePort = null;
    deleteProtocol = null;
    deleteService = null;
    deleteBanner = null;
}

function confirmDelete() {
    if (deletePort === null) return;

    const formData = new FormData();
    formData.append('port', deletePort);
    formData.append('protocol', deleteProtocol);
    formData.append('service', deleteService);
    formData.append('banner', deleteBanner);

    fetch(`/projects/${projectId}/services/delete`, {
        method: 'POST',
        body: formData
    })
    .then(r => r.json())
    .then(data => {
        closeDeleteModal();
        if (data.success) {
            window.location.reload();
        } else {
            showNotificationModal('Error', data.error || 'Failed to delete service');
        }
    })
    .catch(() => {
        closeDeleteModal();
        showNotificationModal('Error', 'Failed to delete service');
    });
}

// Bulk Delete
function showBulkDeleteModal() {
    const selected = getSelectedServices();
    if (selected.length === 0) return;

    document.getElementById('bulkDeleteCount').textContent = selected.length;
    document.getElementById('bulkDeleteModal').style.display = 'flex';
}

function closeBulkDeleteModal() {
    document.getElementById('bulkDeleteModal').style.display = 'none';
}

function confirmBulkDelete() {
    const selected = getSelectedServices();
    if (selected.length === 0) return;

    fetch(`/projects/${projectId}/services/bulk-delete`, {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json'
        },
        body: JSON.stringify(selected)
    })
    .then(r => r.json())
    .then(data => {
        closeBulkDeleteModal();
        if (data.success) {
            window.location.reload();
        } else {
            showNotificationModal('Error', data.error || 'Failed to delete services');
        }
    })
    .catch(() => {
        closeBulkDeleteModal();
        showNotificationModal('Error', 'Failed to delete services');
    });
}

// Merge Services
let mergeSelection = [];

function showMergeModal() {
    const selected = getSelectedServices();
    if (selected.length < 2) {
        showNotificationModal('Error', 'Select at least 2 services to merge');
        return;
    }

    mergeSelection = selected;
    document.getElementById('mergeCount').textContent = selected.length;

    // Build options from selected services
    const optionsEl = document.getElementById('mergeOptions');
    optionsEl.innerHTML = selected.map((svc, idx) => `
        <label class="merge-option ${idx === 0 ? 'selected' : ''}" onclick="selectMergeOption(this, ${idx})">
            <input type="radio" name="mergeTarget" value="${idx}" ${idx === 0 ? 'checked' : ''}>
            <div class="merge-option-details">
                <div class="merge-option-port">${escapeHtml(svc.port)}/${escapeHtml(svc.protocol)}</div>
                <div class="merge-option-service">${escapeHtml(svc.service) || '(no service name)'}</div>
                ${svc.banner ? `<div class="merge-option-banner">${escapeHtml(svc.banner)}</div>` : ''}
            </div>
        </label>
    `).join('');

    document.getElementById('mergeModal').style.display = 'flex';
}

function selectMergeOption(label, idx) {
    document.querySelectorAll('.merge-option').forEach(el => el.classList.remove('selected'));
    label.classList.add('selected');
    label.querySelector('input').checked = true;
}

function closeMergeModal() {
    document.getElementById('mergeModal').style.display = 'none';
    mergeSelection = [];
}

function confirmMerge() {
    if (mergeSelection.length < 2) return;

    const selectedIdx = document.querySelector('input[name="mergeTarget"]:checked').value;
    const target = mergeSelection[parseInt(selectedIdx)];

    fetch(`/projects/${projectId}/services/merge`, {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json'
        },
        body: JSON.stringify({
            services: mergeSelection,
            target_service: target.service,
            target_banner: target.banner
        })
    })
    .then(r => r.json())
    .then(data => {
        closeMergeModal();
        if (data.success) {
            window.location.reload();
        } else {
            showNotificationModal('Error', data.error || 'Failed to merge services');
        }
    })
    .catch(() => {
        closeMergeModal();
        showNotificationModal('Error', 'Failed to merge services');
    });
}

// Reset checkboxes on page load (browser may restore form state)
document.querySelectorAll('.svc-checkbox').forEach(cb => cb.checked = false);
