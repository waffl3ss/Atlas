// Color cycle order
const colorCycle = ['grey', 'green', 'blue', 'yellow', 'orange', 'red'];

// ===== Tab Switching =====

function getActiveTab() {
    const active = document.querySelector('.tab.active');
    return active ? active.dataset.tab : 'services';
}

function updateNavLinks() {
    const tab = getActiveTab();
    const prev = document.getElementById('prevHostLink');
    const next = document.getElementById('nextHostLink');
    if (prev) prev.href = prev.href.split('?')[0] + '?tab=' + tab;
    if (next) next.href = next.href.split('?')[0] + '?tab=' + tab;
}

function switchTab(btn) {
    const tabName = btn.dataset.tab;

    // Update tab buttons
    document.querySelectorAll('.tab').forEach(t => t.classList.remove('active'));
    btn.classList.add('active');

    // Update tab panels
    document.querySelectorAll('.tab-panel').forEach(p => p.classList.remove('active'));
    document.getElementById('tab-' + tabName).classList.add('active');

    // Update nav arrow links to preserve tab
    updateNavLinks();
}

// On page load, restore tab from URL param
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
    markDefaultHostname();
})();

// Reload page while preserving the active tab
function reloadWithTab() {
    const tab = getActiveTab();
    const url = window.location.pathname + '?tab=' + tab;
    window.location.href = url;
}

// ===== Set Default Hostname =====

function markDefaultHostname() {
    var dataEl = document.getElementById('hostnamesTabData');
    if (!dataEl) return;
    var defaultHostname = dataEl.getAttribute('data-default-hostname');
    document.querySelectorAll('.btn-set-default').forEach(function(btn) {
        if (btn.getAttribute('data-hostname') === defaultHostname) {
            btn.classList.add('is-default');
        } else {
            btn.classList.remove('is-default');
        }
    });
}

function setDefaultHostname(id) {
    var btn = document.querySelector('.btn-set-default[data-hostname-id="' + id + '"]');
    if (!btn || btn.classList.contains('is-default')) return;

    var formData = new FormData();
    formData.append('hostname_id', id);

    fetch('/projects/' + projectId + '/hosts/' + hostId + '/hostnames/set-default', { method: 'POST', body: formData })
        .then(function(r) { return r.json(); })
        .then(function(data) {
            if (data.success) {
                // Update the data attribute so markDefaultHostname picks it up
                var dataEl = document.getElementById('hostnamesTabData');
                if (dataEl) dataEl.setAttribute('data-default-hostname', data.hostname);
                markDefaultHostname();
                // Update the info panel hostname display
                var hostnameDisplay = document.querySelector('.info-col-hostname .info-value');
                if (hostnameDisplay) hostnameDisplay.textContent = data.hostname;
            } else {
                showNotificationModal('Error', data.error || 'Failed to set default hostname');
            }
        })
        .catch(function() {
            showNotificationModal('Error', 'Failed to set default hostname');
        });
}

// ===== Color Cycling =====

function setAllColorDots(color) {
    document.querySelectorAll('.color-dot[data-host-id="' + hostId + '"]').forEach(d => {
        colorCycle.forEach(c => d.classList.remove('color-' + c));
        d.classList.add('color-' + color);
        d.dataset.color = color;
    });
}

function cycleColor(dot, event) {
    if (event) event.stopPropagation();
    const currentColor = dot.dataset.color;
    const currentIdx = colorCycle.indexOf(currentColor);
    const nextColor = colorCycle[(currentIdx + 1) % colorCycle.length];

    setAllColorDots(nextColor);

    const formData = new FormData();
    formData.append('host_id', hostId);
    formData.append('color', nextColor);

    fetch('/projects/' + projectId + '/hosts/color', { method: 'POST', body: formData })
        .then(r => r.json())
        .then(data => {
            if (!data.success) setAllColorDots(currentColor);
        })
        .catch(() => {
            setAllColorDots(currentColor);
        });
}

// ===== Edit OS Modal =====

function openEditOSModal() {
    // Sync tag input to current display value so saveOS sends the correct tag
    document.getElementById('editTagInput').value = document.getElementById('tagDisplay').textContent === '\u2014' ? '' : document.getElementById('tagDisplay').textContent;
    document.getElementById('editOSModal').style.display = 'flex';
    const input = document.getElementById('editOSInput');
    input.focus();
    input.select();
}

function closeEditOSModal() {
    document.getElementById('editOSModal').style.display = 'none';
}

function saveOS() {
    const os = document.getElementById('editOSInput').value.trim();
    const tag = document.getElementById('editTagInput').value.trim();

    const formData = new FormData();
    formData.append('os', os);
    formData.append('tag', tag);

    fetch('/projects/' + projectId + '/hosts/' + hostId + '/update', { method: 'POST', body: formData })
        .then(r => r.json())
        .then(data => {
            closeEditOSModal();
            if (data.success) {
                document.getElementById('osDisplay').textContent = os || '\u2014';
            }
        })
        .catch(() => {
            closeEditOSModal();
        });
}

// ===== Edit Tag Modal =====

function openEditTagModal() {
    // Sync OS input to current display value so saveTag sends the correct OS
    document.getElementById('editOSInput').value = document.getElementById('osDisplay').textContent === '\u2014' ? '' : document.getElementById('osDisplay').textContent;
    document.getElementById('editTagModal').style.display = 'flex';
    const input = document.getElementById('editTagInput');
    input.focus();
    input.select();
}

function closeEditTagModal() {
    document.getElementById('editTagModal').style.display = 'none';
}

function saveTag() {
    const os = document.getElementById('editOSInput').value.trim();
    const tag = document.getElementById('editTagInput').value.trim();

    const formData = new FormData();
    formData.append('os', os);
    formData.append('tag', tag);

    fetch('/projects/' + projectId + '/hosts/' + hostId + '/update', { method: 'POST', body: formData })
        .then(r => r.json())
        .then(data => {
            closeEditTagModal();
            if (data.success) {
                document.getElementById('tagDisplay').textContent = tag || '\u2014';
            }
        })
        .catch(() => {
            closeEditTagModal();
        });
}

// Enter key support in modals
document.getElementById('editOSInput')?.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') saveOS();
    if (e.key === 'Escape') closeEditOSModal();
});
document.getElementById('editTagInput')?.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') saveTag();
    if (e.key === 'Escape') closeEditTagModal();
});

// ===== Service Color Cycling =====

function cycleServiceColor(dot, event) {
    if (event) event.stopPropagation();
    const serviceId = dot.dataset.serviceId;
    const currentColor = dot.dataset.color;
    const currentIdx = colorCycle.indexOf(currentColor);
    const nextColor = colorCycle[(currentIdx + 1) % colorCycle.length];

    dot.className = 'color-dot color-' + nextColor;
    dot.dataset.color = nextColor;
    dot.closest('tr').dataset.color = nextColor;

    const formData = new FormData();
    formData.append('service_id', serviceId);
    formData.append('color', nextColor);

    fetch('/projects/' + projectId + '/hosts/' + hostId + '/services/color', { method: 'POST', body: formData })
        .then(r => r.json())
        .then(data => {
            if (!data.success) {
                dot.className = 'color-dot color-' + currentColor;
                dot.dataset.color = currentColor;
                dot.closest('tr').dataset.color = currentColor;
            }
        })
        .catch(() => {
            dot.className = 'color-dot color-' + currentColor;
            dot.dataset.color = currentColor;
            dot.closest('tr').dataset.color = currentColor;
        });
}

// ===== Service Checkbox Selection =====

function getSelectedServiceIds() {
    return Array.from(document.querySelectorAll('.svc-row-checkbox:checked')).map(cb => cb.value);
}

function updateSvcSelection() {
    const checkboxes = document.querySelectorAll('.svc-row-checkbox');
    const checked = document.querySelectorAll('.svc-row-checkbox:checked');
    const bar = document.getElementById('svcSelectionBar');
    const selectAll = document.getElementById('svcSelectAll');

    if (checked.length > 0) {
        bar.classList.add('active');
        document.getElementById('svcSelectionCount').textContent = checked.length + ' selected';
    } else {
        bar.classList.remove('active');
    }

    if (selectAll) {
        selectAll.checked = checkboxes.length > 0 && checked.length === checkboxes.length;
        selectAll.indeterminate = checked.length > 0 && checked.length < checkboxes.length;
    }
}

function toggleSvcSelectAll() {
    const selectAll = document.getElementById('svcSelectAll');
    document.querySelectorAll('.svc-row-checkbox').forEach(cb => {
        cb.checked = selectAll.checked;
    });
    updateSvcSelection();
}

// ===== Add Service Modal =====

function openAddServiceModal() {
    document.getElementById('newServicePort').value = '';
    document.getElementById('newServiceProtocol').value = 'TCP';
    document.getElementById('newServiceName').value = '';
    document.getElementById('addServiceModal').style.display = 'flex';
    document.getElementById('newServicePort').focus();
}

function closeAddServiceModal() {
    document.getElementById('addServiceModal').style.display = 'none';
}

function addService() {
    const port = document.getElementById('newServicePort').value.trim();
    const protocol = document.getElementById('newServiceProtocol').value;
    const serviceName = document.getElementById('newServiceName').value.trim();

    if (!port || !protocol) return;

    const formData = new FormData();
    formData.append('port', port);
    formData.append('protocol', protocol);
    formData.append('service_name', serviceName);

    fetch('/projects/' + projectId + '/hosts/' + hostId + '/services/add', { method: 'POST', body: formData })
        .then(r => r.json())
        .then(data => {
            closeAddServiceModal();
            if (data.success) {
                reloadWithTab();
            } else {
                showNotificationModal('Error', data.error || 'Failed to add service');
            }
        })
        .catch(() => {
            closeAddServiceModal();
            showNotificationModal('Error', 'Failed to add service');
        });
}

// ===== Delete Single Service =====

let deleteServiceId = null;

function showDeleteServiceModal(id) {
    deleteServiceId = id;
    document.getElementById('deleteServiceModal').style.display = 'flex';
}

function closeDeleteServiceModal() {
    document.getElementById('deleteServiceModal').style.display = 'none';
    deleteServiceId = null;
}

function confirmDeleteService() {
    if (!deleteServiceId) return;

    const formData = new FormData();
    formData.append('service_id', deleteServiceId);

    fetch('/projects/' + projectId + '/hosts/' + hostId + '/services/delete', { method: 'POST', body: formData })
        .then(r => r.json())
        .then(data => {
            closeDeleteServiceModal();
            if (data.success) {
                reloadWithTab();
            } else {
                showNotificationModal('Error', data.error || 'Failed to delete service');
            }
        })
        .catch(() => {
            closeDeleteServiceModal();
            showNotificationModal('Error', 'Failed to delete service');
        });
}

// ===== Bulk Delete Services =====

function showBulkDeleteServiceModal() {
    const ids = getSelectedServiceIds();
    if (ids.length === 0) return;
    document.getElementById('bulkDeleteSvcCount').textContent = ids.length;
    document.getElementById('bulkDeleteServiceModal').style.display = 'flex';
}

function closeBulkDeleteServiceModal() {
    document.getElementById('bulkDeleteServiceModal').style.display = 'none';
}

function confirmBulkDeleteServices() {
    const ids = getSelectedServiceIds();
    if (ids.length === 0) return;

    const formData = new FormData();
    formData.append('ids', ids.join(','));

    fetch('/projects/' + projectId + '/hosts/' + hostId + '/services/bulk-delete', { method: 'POST', body: formData })
        .then(r => r.json())
        .then(data => {
            closeBulkDeleteServiceModal();
            if (data.success) {
                reloadWithTab();
            } else {
                showNotificationModal('Error', data.error || 'Failed to delete services');
            }
        })
        .catch(() => {
            closeBulkDeleteServiceModal();
            showNotificationModal('Error', 'Failed to delete services');
        });
}

// ===== Service keyboard shortcuts =====

document.getElementById('newServicePort')?.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') addService();
    if (e.key === 'Escape') closeAddServiceModal();
});
document.getElementById('newServiceName')?.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') addService();
    if (e.key === 'Escape') closeAddServiceModal();
});

// ===== Delete Host =====

function showDeleteHostModal() {
    document.getElementById('deleteHostModal').style.display = 'flex';
}

function closeDeleteHostModal() {
    document.getElementById('deleteHostModal').style.display = 'none';
}

function confirmDeleteHost() {
    const formData = new FormData();
    formData.append('host_id', hostId);

    fetch('/projects/' + projectId + '/hosts/delete', { method: 'POST', body: formData })
        .then(r => r.json())
        .then(data => {
            if (data.success) {
                window.location.href = '/projects/' + projectId + '/hosts';
            } else {
                closeDeleteHostModal();
                showNotificationModal('Error', data.error || 'Failed to delete host');
            }
        })
        .catch(() => {
            closeDeleteHostModal();
            showNotificationModal('Error', 'Failed to delete host');
        });
}

// ===== Hostname Tab =====

// --- Add Hostname ---

function openAddHostnameModal() {
    document.getElementById('newHostnameInput').value = '';
    document.getElementById('addHostnameModal').style.display = 'flex';
    document.getElementById('newHostnameInput').focus();
}

function closeAddHostnameModal() {
    document.getElementById('addHostnameModal').style.display = 'none';
}

function addHostname() {
    const hostname = document.getElementById('newHostnameInput').value.trim();
    if (!hostname) return;

    const formData = new FormData();
    formData.append('hostname', hostname);

    fetch('/projects/' + projectId + '/hosts/' + hostId + '/hostnames/add', { method: 'POST', body: formData })
        .then(r => r.json())
        .then(data => {
            closeAddHostnameModal();
            if (data.success) {
                reloadWithTab();
            } else {
                showNotificationModal('Error', data.error || 'Failed to add hostname');
            }
        })
        .catch(() => {
            closeAddHostnameModal();
            showNotificationModal('Error', 'Failed to add hostname');
        });
}

// --- Delete Single Hostname ---

let deleteHostnameId = null;

function showDeleteHostnameModal(id) {
    deleteHostnameId = id;
    document.getElementById('deleteHostnameModal').style.display = 'flex';
}

function closeDeleteHostnameModal() {
    document.getElementById('deleteHostnameModal').style.display = 'none';
    deleteHostnameId = null;
}

function confirmDeleteHostname() {
    if (!deleteHostnameId) return;

    const formData = new FormData();
    formData.append('hostname_id', deleteHostnameId);

    fetch('/projects/' + projectId + '/hosts/' + hostId + '/hostnames/delete', { method: 'POST', body: formData })
        .then(r => r.json())
        .then(data => {
            closeDeleteHostnameModal();
            if (data.success) {
                reloadWithTab();
            } else {
                showNotificationModal('Error', data.error || 'Failed to delete hostname');
            }
        })
        .catch(() => {
            closeDeleteHostnameModal();
            showNotificationModal('Error', 'Failed to delete hostname');
        });
}

// --- Bulk Delete Hostnames ---

function showBulkDeleteHostnameModal() {
    const ids = getSelectedHostnameIds();
    if (ids.length === 0) return;
    document.getElementById('bulkDeleteHnCount').textContent = ids.length;
    document.getElementById('bulkDeleteHostnameModal').style.display = 'flex';
}

function closeBulkDeleteHostnameModal() {
    document.getElementById('bulkDeleteHostnameModal').style.display = 'none';
}

function confirmBulkDeleteHostnames() {
    const ids = getSelectedHostnameIds();
    if (ids.length === 0) return;

    const formData = new FormData();
    formData.append('ids', ids.join(','));

    fetch('/projects/' + projectId + '/hosts/' + hostId + '/hostnames/bulk-delete', { method: 'POST', body: formData })
        .then(r => r.json())
        .then(data => {
            closeBulkDeleteHostnameModal();
            if (data.success) {
                reloadWithTab();
            } else {
                showNotificationModal('Error', data.error || 'Failed to delete hostnames');
            }
        })
        .catch(() => {
            closeBulkDeleteHostnameModal();
            showNotificationModal('Error', 'Failed to delete hostnames');
        });
}

// --- Hostname Checkbox Selection ---

function getSelectedHostnameIds() {
    return Array.from(document.querySelectorAll('.hn-row-checkbox:checked')).map(cb => cb.value);
}

function updateHnSelection() {
    const checkboxes = document.querySelectorAll('.hn-row-checkbox');
    const checked = document.querySelectorAll('.hn-row-checkbox:checked');
    const bar = document.getElementById('hnSelectionBar');
    const selectAll = document.getElementById('hnSelectAll');

    if (checked.length > 0) {
        bar.classList.add('active');
        document.getElementById('hnSelectionCount').textContent = checked.length + ' selected';
    } else {
        bar.classList.remove('active');
    }

    if (selectAll) {
        selectAll.checked = checkboxes.length > 0 && checked.length === checkboxes.length;
        selectAll.indeterminate = checked.length > 0 && checked.length < checkboxes.length;
    }
}

function toggleHnSelectAll() {
    const selectAll = document.getElementById('hnSelectAll');
    document.querySelectorAll('.hn-row-checkbox').forEach(cb => {
        cb.checked = selectAll.checked;
    });
    updateHnSelection();
}

// --- Hostname keyboard shortcuts ---

document.getElementById('newHostnameInput')?.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') addHostname();
    if (e.key === 'Escape') closeAddHostnameModal();
});

// ===== Credentials Tab =====

// --- Add Credential ---

function openAddHostCredModal() {
    document.getElementById('hcUsername').value = '';
    document.getElementById('hcPassword').value = '';
    document.getElementById('hcHost').value = hostIp;
    document.getElementById('hcService').value = '';
    document.getElementById('hcType').value = '';
    document.getElementById('hcNotes').value = '';
    document.getElementById('addHostCredModal').style.display = 'flex';
    document.getElementById('hcUsername').focus();
}

function closeAddHostCredModal() {
    document.getElementById('addHostCredModal').style.display = 'none';
}

function addHostCred() {
    const username = document.getElementById('hcUsername').value.trim();
    const password = document.getElementById('hcPassword').value.trim();
    if (!username && !password) return;

    const formData = new FormData();
    formData.append('username', username);
    formData.append('password', password);
    formData.append('host', document.getElementById('hcHost').value.trim());
    formData.append('service', document.getElementById('hcService').value.trim());
    formData.append('credential_type', document.getElementById('hcType').value.trim());
    formData.append('notes', document.getElementById('hcNotes').value.trim());

    fetch('/projects/' + projectId + '/credentials/add', { method: 'POST', body: formData })
        .then(r => r.json())
        .then(data => {
            closeAddHostCredModal();
            if (data.success) {
                reloadWithTab();
            } else {
                showNotificationModal('Error', data.error || 'Failed to add credential');
            }
        })
        .catch(() => {
            closeAddHostCredModal();
            showNotificationModal('Error', 'Failed to add credential');
        });
}

// --- Delete Single Credential ---

let deleteCredId = null;

function showDeleteCredModal(id) {
    deleteCredId = id;
    document.getElementById('deleteCredModal').style.display = 'flex';
}

function closeDeleteCredModal() {
    document.getElementById('deleteCredModal').style.display = 'none';
    deleteCredId = null;
}

function confirmDeleteCred() {
    if (!deleteCredId) return;

    const formData = new FormData();
    formData.append('cred_id', deleteCredId);

    fetch('/projects/' + projectId + '/credentials/delete', { method: 'POST', body: formData })
        .then(r => r.json())
        .then(data => {
            closeDeleteCredModal();
            if (data.success) {
                reloadWithTab();
            } else {
                showNotificationModal('Error', data.error || 'Failed to delete credential');
            }
        })
        .catch(() => {
            closeDeleteCredModal();
            showNotificationModal('Error', 'Failed to delete credential');
        });
}

// --- Bulk Delete Credentials ---

function showBulkDeleteCredModal() {
    const ids = getSelectedCredIds();
    if (ids.length === 0) return;
    document.getElementById('bulkDeleteHcCount').textContent = ids.length;
    document.getElementById('bulkDeleteCredModal').style.display = 'flex';
}

function closeBulkDeleteCredModal() {
    document.getElementById('bulkDeleteCredModal').style.display = 'none';
}

function confirmBulkDeleteCreds() {
    const ids = getSelectedCredIds();
    if (ids.length === 0) return;

    const formData = new FormData();
    formData.append('ids', ids.join(','));

    fetch('/projects/' + projectId + '/credentials/bulk-delete', { method: 'POST', body: formData })
        .then(r => r.json())
        .then(data => {
            closeBulkDeleteCredModal();
            if (data.success) {
                reloadWithTab();
            } else {
                showNotificationModal('Error', data.error || 'Failed to delete credentials');
            }
        })
        .catch(() => {
            closeBulkDeleteCredModal();
            showNotificationModal('Error', 'Failed to delete credentials');
        });
}

// --- Credential Checkbox Selection ---

function getSelectedCredIds() {
    return Array.from(document.querySelectorAll('.hc-row-checkbox:checked')).map(cb => cb.value);
}

function updateHcSelection() {
    const checkboxes = document.querySelectorAll('.hc-row-checkbox');
    const checked = document.querySelectorAll('.hc-row-checkbox:checked');
    const bar = document.getElementById('hcSelectionBar');
    const selectAll = document.getElementById('hcSelectAll');

    if (checked.length > 0) {
        bar.classList.add('active');
        document.getElementById('hcSelectionCount').textContent = checked.length + ' selected';
    } else {
        bar.classList.remove('active');
    }

    if (selectAll) {
        selectAll.checked = checkboxes.length > 0 && checked.length === checkboxes.length;
        selectAll.indeterminate = checked.length > 0 && checked.length < checkboxes.length;
    }
}

function toggleHcSelectAll() {
    const selectAll = document.getElementById('hcSelectAll');
    document.querySelectorAll('.hc-row-checkbox').forEach(cb => {
        cb.checked = selectAll.checked;
    });
    updateHcSelection();
}

// --- Credential keyboard shortcuts ---

document.getElementById('hcUsername')?.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') closeAddHostCredModal();
});
document.getElementById('hcNotes')?.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') addHostCred();
    if (e.key === 'Escape') closeAddHostCredModal();
});

// ===== Web Directories Tab =====

// --- Add Web Directory ---

function openAddWebDirModal() {
    document.getElementById('wdPort').value = '';
    document.getElementById('wdBaseDomain').value = '';
    document.getElementById('wdPath').value = '';
    document.getElementById('addWebDirModal').style.display = 'flex';
    document.getElementById('wdPort').focus();
}

function closeAddWebDirModal() {
    document.getElementById('addWebDirModal').style.display = 'none';
}

function addWebDir() {
    const port = document.getElementById('wdPort').value.trim();
    const baseDomain = document.getElementById('wdBaseDomain').value.trim();
    const path = document.getElementById('wdPath').value.trim();
    if (!port || !path) return;

    const formData = new FormData();
    formData.append('port', port);
    formData.append('base_domain', baseDomain);
    formData.append('path', path);

    fetch('/projects/' + projectId + '/hosts/' + hostId + '/webdirs/add', { method: 'POST', body: formData })
        .then(r => r.json())
        .then(data => {
            closeAddWebDirModal();
            if (data.success) {
                reloadWithTab();
            } else {
                showNotificationModal('Error', data.error || 'Failed to add web directory');
            }
        })
        .catch(() => {
            closeAddWebDirModal();
            showNotificationModal('Error', 'Failed to add web directory');
        });
}

// --- Delete Single Web Directory ---

let deleteWebDirId = null;

function showDeleteWebDirModal(id) {
    deleteWebDirId = id;
    document.getElementById('deleteWebDirModal').style.display = 'flex';
}

function closeDeleteWebDirModal() {
    document.getElementById('deleteWebDirModal').style.display = 'none';
    deleteWebDirId = null;
}

function confirmDeleteWebDir() {
    if (!deleteWebDirId) return;

    const formData = new FormData();
    formData.append('webdir_id', deleteWebDirId);

    fetch('/projects/' + projectId + '/hosts/' + hostId + '/webdirs/delete', { method: 'POST', body: formData })
        .then(r => r.json())
        .then(data => {
            closeDeleteWebDirModal();
            if (data.success) {
                reloadWithTab();
            } else {
                showNotificationModal('Error', data.error || 'Failed to delete web directory');
            }
        })
        .catch(() => {
            closeDeleteWebDirModal();
            showNotificationModal('Error', 'Failed to delete web directory');
        });
}

// --- Bulk Delete Web Directories ---

function showBulkDeleteWebDirModal() {
    const ids = getSelectedWebDirIds();
    if (ids.length === 0) return;
    document.getElementById('bulkDeleteWdCount').textContent = ids.length;
    document.getElementById('bulkDeleteWebDirModal').style.display = 'flex';
}

function closeBulkDeleteWebDirModal() {
    document.getElementById('bulkDeleteWebDirModal').style.display = 'none';
}

function confirmBulkDeleteWebDirs() {
    const ids = getSelectedWebDirIds();
    if (ids.length === 0) return;

    const formData = new FormData();
    formData.append('ids', ids.join(','));

    fetch('/projects/' + projectId + '/hosts/' + hostId + '/webdirs/bulk-delete', { method: 'POST', body: formData })
        .then(r => r.json())
        .then(data => {
            closeBulkDeleteWebDirModal();
            if (data.success) {
                reloadWithTab();
            } else {
                showNotificationModal('Error', data.error || 'Failed to delete web directories');
            }
        })
        .catch(() => {
            closeBulkDeleteWebDirModal();
            showNotificationModal('Error', 'Failed to delete web directories');
        });
}

// --- Web Directory Checkbox Selection ---

function getSelectedWebDirIds() {
    return Array.from(document.querySelectorAll('.wd-row-checkbox:checked')).map(cb => cb.value);
}

function updateWdSelection() {
    const checkboxes = document.querySelectorAll('.wd-row-checkbox');
    const checked = document.querySelectorAll('.wd-row-checkbox:checked');
    const bar = document.getElementById('wdSelectionBar');
    const selectAll = document.getElementById('wdSelectAll');

    if (checked.length > 0) {
        bar.classList.add('active');
        document.getElementById('wdSelectionCount').textContent = checked.length + ' selected';
    } else {
        bar.classList.remove('active');
    }

    if (selectAll) {
        selectAll.checked = checkboxes.length > 0 && checked.length === checkboxes.length;
        selectAll.indeterminate = checked.length > 0 && checked.length < checkboxes.length;
    }
}

function toggleWdSelectAll() {
    const selectAll = document.getElementById('wdSelectAll');
    document.querySelectorAll('.wd-row-checkbox').forEach(cb => {
        cb.checked = selectAll.checked;
    });
    updateWdSelection();
}

// --- Web Directory keyboard shortcuts ---

document.getElementById('wdPort')?.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') closeAddWebDirModal();
});
document.getElementById('wdBaseDomain')?.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') closeAddWebDirModal();
});
document.getElementById('wdPath')?.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') addWebDir();
    if (e.key === 'Escape') closeAddWebDirModal();
});

// ===== Web Stats Tab =====

// --- Add Web Probe ---

function openAddWpModal() {
    var m = document.getElementById('addWpModal');
    if (!m) return;
    document.getElementById('addWpPort').value = '';
    document.getElementById('addWpScheme').value = 'https';
    document.getElementById('addWpStatus').value = '0';
    document.getElementById('addWpTitle').value = '';
    document.getElementById('addWpServer').value = '';
    document.getElementById('addWpTech').value = '';
    document.getElementById('addWpLocation').value = '';
    m.style.display = 'flex';
    document.getElementById('addWpPort').focus();
}

function closeAddWpModal() {
    var m = document.getElementById('addWpModal');
    if (m) m.style.display = 'none';
}

function submitAddWp() {
    var port = document.getElementById('addWpPort').value.trim();
    var scheme = document.getElementById('addWpScheme').value;
    if (!port || !scheme) return;

    var fd = new FormData();
    fd.append('port', port);
    fd.append('scheme', scheme);
    fd.append('status_code', document.getElementById('addWpStatus').value || '0');
    fd.append('title', document.getElementById('addWpTitle').value.trim());
    fd.append('webserver', document.getElementById('addWpServer').value.trim());
    fd.append('tech', document.getElementById('addWpTech').value.trim());
    fd.append('location', document.getElementById('addWpLocation').value.trim());

    fetch('/projects/' + projectId + '/hosts/' + hostId + '/webprobes/add', { method: 'POST', body: fd })
        .then(function(r) { return r.json(); })
        .then(function(data) {
            closeAddWpModal();
            if (data.success) {
                reloadWithTab();
            } else {
                showNotificationModal('Error', data.error || 'Failed to add web probe');
            }
        })
        .catch(function() {
            closeAddWpModal();
            showNotificationModal('Error', 'Failed to add web probe');
        });
}

// --- Delete Single Web Probe ---

var pendingDeleteWpId = null;

function showDeleteWpModal(id) {
    pendingDeleteWpId = id;
    var m = document.getElementById('deleteWpModal');
    if (m) m.style.display = 'flex';
}

function closeDeleteWpModal() {
    var m = document.getElementById('deleteWpModal');
    if (m) m.style.display = 'none';
    pendingDeleteWpId = null;
}

function confirmDeleteWp() {
    if (!pendingDeleteWpId) return;

    var fd = new FormData();
    fd.append('probe_id', pendingDeleteWpId);

    fetch('/projects/' + projectId + '/hosts/' + hostId + '/webprobes/delete', { method: 'POST', body: fd })
        .then(function(r) { return r.json(); })
        .then(function(data) {
            closeDeleteWpModal();
            if (data.success) {
                reloadWithTab();
            } else {
                showNotificationModal('Error', data.error || 'Failed to delete web probe');
            }
        })
        .catch(function() {
            closeDeleteWpModal();
            showNotificationModal('Error', 'Failed to delete web probe');
        });
}

// --- Bulk Delete Web Probes ---

function showBulkDeleteWpModal() {
    var ids = getSelectedWpIds();
    if (ids.length === 0) return;
    document.getElementById('bulkDeleteWpCount').textContent = ids.length;
    var m = document.getElementById('bulkDeleteWpModal');
    if (m) m.style.display = 'flex';
}

function closeBulkDeleteWpModal() {
    var m = document.getElementById('bulkDeleteWpModal');
    if (m) m.style.display = 'none';
}

function confirmBulkDeleteWp() {
    var ids = getSelectedWpIds();
    if (ids.length === 0) return;

    var fd = new FormData();
    fd.append('ids', ids.join(','));

    fetch('/projects/' + projectId + '/hosts/' + hostId + '/webprobes/bulk-delete', { method: 'POST', body: fd })
        .then(function(r) { return r.json(); })
        .then(function(data) {
            closeBulkDeleteWpModal();
            if (data.success) {
                reloadWithTab();
            } else {
                showNotificationModal('Error', data.error || 'Failed to delete web probes');
            }
        })
        .catch(function() {
            closeBulkDeleteWpModal();
            showNotificationModal('Error', 'Failed to delete web probes');
        });
}

// --- Web Probe Checkbox Selection ---

function getSelectedWpIds() {
    return Array.from(document.querySelectorAll('.wp-row-checkbox:checked')).map(function(cb) { return cb.value; });
}

function updateWpSelection() {
    var checkboxes = document.querySelectorAll('.wp-row-checkbox');
    var checked = document.querySelectorAll('.wp-row-checkbox:checked');
    var bar = document.getElementById('wpSelectionBar');
    var selectAll = document.getElementById('wpSelectAll');

    if (checked.length > 0) {
        bar.classList.add('active');
        document.getElementById('wpSelectionCount').textContent = checked.length + ' selected';
    } else {
        bar.classList.remove('active');
    }

    if (selectAll) {
        selectAll.checked = checkboxes.length > 0 && checked.length === checkboxes.length;
        selectAll.indeterminate = checked.length > 0 && checked.length < checkboxes.length;
    }
}

function toggleWpSelectAll() {
    var selectAll = document.getElementById('wpSelectAll');
    document.querySelectorAll('.wp-row-checkbox').forEach(function(cb) {
        cb.checked = selectAll.checked;
    });
    updateWpSelection();
}

// --- Web Probe keyboard shortcuts ---

(function() {
    var wpPortEl = document.getElementById('addWpPort');
    var wpTechEl = document.getElementById('addWpTech');
    if (wpPortEl) wpPortEl.addEventListener('keydown', function(e) {
        if (e.key === 'Escape') closeAddWpModal();
    });
    if (wpTechEl) wpTechEl.addEventListener('keydown', function(e) {
        if (e.key === 'Enter') submitAddWp();
        if (e.key === 'Escape') closeAddWpModal();
    });
})();

// --- Status code badge coloring ---

(function() {
    document.querySelectorAll('.status-badge').forEach(function(el) {
        var code = parseInt(el.getAttribute('data-code')) || 0;
        if (code >= 200 && code <= 299) el.classList.add('status-2xx');
        else if (code >= 300 && code <= 399) el.classList.add('status-3xx');
        else if (code >= 400 && code <= 499) el.classList.add('status-4xx');
        else if (code >= 500) el.classList.add('status-5xx');
    });
})();

// ===== Host Findings (Issues Tab) =====

// --- Delete Single Finding Association ---

let deleteHfId = null;

function showDeleteHfModal(id) {
    deleteHfId = id;
    document.getElementById('deleteHfModal').style.display = 'flex';
}

function closeDeleteHfModal() {
    document.getElementById('deleteHfModal').style.display = 'none';
    deleteHfId = null;
}

function confirmDeleteHf() {
    if (!deleteHfId) return;

    const formData = new FormData();
    formData.append('finding_host_id', deleteHfId);

    fetch('/projects/' + projectId + '/hosts/' + hostId + '/findings/remove', { method: 'POST', body: formData })
        .then(r => r.json())
        .then(data => {
            closeDeleteHfModal();
            if (data.success) {
                reloadWithTab();
            } else {
                showNotificationModal('Error', data.error || 'Failed to remove finding');
            }
        })
        .catch(() => {
            closeDeleteHfModal();
            showNotificationModal('Error', 'Failed to remove finding');
        });
}

// --- Bulk Remove Finding Associations ---

function showBulkDeleteHfModal() {
    const ids = getSelectedHfIds();
    if (ids.length === 0) return;
    document.getElementById('bulkDeleteHfCount').textContent = ids.length;
    document.getElementById('bulkDeleteHfModal').style.display = 'flex';
}

function closeBulkDeleteHfModal() {
    document.getElementById('bulkDeleteHfModal').style.display = 'none';
}

function confirmBulkDeleteHf() {
    const ids = getSelectedHfIds();
    if (ids.length === 0) return;

    const formData = new FormData();
    formData.append('ids', ids.join(','));

    fetch('/projects/' + projectId + '/hosts/' + hostId + '/findings/bulk-remove', { method: 'POST', body: formData })
        .then(r => r.json())
        .then(data => {
            closeBulkDeleteHfModal();
            if (data.success) {
                reloadWithTab();
            } else {
                showNotificationModal('Error', data.error || 'Failed to remove findings');
            }
        })
        .catch(() => {
            closeBulkDeleteHfModal();
            showNotificationModal('Error', 'Failed to remove findings');
        });
}

// --- Host Finding Checkbox Selection ---

function getSelectedHfIds() {
    return Array.from(document.querySelectorAll('.hf-row-checkbox:checked')).map(cb => cb.value);
}

function updateHfSelection() {
    const checkboxes = document.querySelectorAll('.hf-row-checkbox');
    const checked = document.querySelectorAll('.hf-row-checkbox:checked');
    const bar = document.getElementById('hfSelectionBar');
    const selectAll = document.getElementById('hfSelectAll');

    if (checked.length > 0) {
        bar.classList.add('active');
        document.getElementById('hfSelectionCount').textContent = checked.length + ' selected';
    } else {
        bar.classList.remove('active');
    }

    if (selectAll) {
        selectAll.checked = checkboxes.length > 0 && checked.length === checkboxes.length;
        selectAll.indeterminate = checked.length > 0 && checked.length < checkboxes.length;
    }
}

function toggleHfSelectAll() {
    const selectAll = document.getElementById('hfSelectAll');
    document.querySelectorAll('.hf-row-checkbox').forEach(cb => {
        cb.checked = selectAll.checked;
    });
    updateHfSelection();
}

// --- CVSS score colorization (Issues tab) ---

(function() {
    document.querySelectorAll('.cvss-score').forEach(function(el) {
        var score = parseFloat(el.textContent) || 0;
        if (score >= 9.0) el.classList.add('cvss-critical');
        else if (score >= 7.0) el.classList.add('cvss-high');
        else if (score >= 4.0) el.classList.add('cvss-medium');
        else if (score > 0) el.classList.add('cvss-low');
        else el.classList.add('cvss-none');
    });
})();

// Reset checkboxes on page load
document.querySelectorAll('.svc-row-checkbox').forEach(function(cb) { cb.checked = false; });
document.querySelectorAll('.hn-row-checkbox').forEach(function(cb) { cb.checked = false; });
document.querySelectorAll('.hc-row-checkbox').forEach(function(cb) { cb.checked = false; });
document.querySelectorAll('.wd-row-checkbox').forEach(function(cb) { cb.checked = false; });
document.querySelectorAll('.wp-row-checkbox').forEach(function(cb) { cb.checked = false; });
document.querySelectorAll('.hf-row-checkbox').forEach(function(cb) { cb.checked = false; });
