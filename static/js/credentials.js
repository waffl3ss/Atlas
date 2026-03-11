// Add Credential modal
function openAddModal() {
    document.getElementById('credUsername').value = '';
    document.getElementById('credPassword').value = '';
    document.getElementById('credHost').value = '';
    document.getElementById('credService').value = '';
    document.getElementById('credType').value = '';
    document.getElementById('credNotes').value = '';
    document.getElementById('addModal').style.display = 'flex';
    document.getElementById('credUsername').focus();
}

function closeAddModal() {
    document.getElementById('addModal').style.display = 'none';
}

function addCredential() {
    const username = document.getElementById('credUsername').value.trim();
    const password = document.getElementById('credPassword').value.trim();

    if (!username && !password) {
        showNotificationModal('Error', 'Username or password is required');
        return;
    }

    const formData = new FormData();
    formData.append('username', username);
    formData.append('password', password);
    formData.append('host', document.getElementById('credHost').value.trim());
    formData.append('service', document.getElementById('credService').value.trim());
    formData.append('credential_type', document.getElementById('credType').value.trim());
    formData.append('notes', document.getElementById('credNotes').value.trim());

    fetch(`/projects/${projectId}/credentials/add`, { method: 'POST', body: formData })
        .then(res => res.json())
        .then(data => {
            if (data.error) {
                closeAddModal();
                showNotificationModal('Error', data.error);
            } else {
                window.location.reload();
            }
        })
        .catch(() => {
            closeAddModal();
            showNotificationModal('Error', 'Failed to add credential');
        });
}

// Delete single credential
let deleteCredId = null;

function showDeleteModal(id) {
    deleteCredId = id;
    document.getElementById('deleteModal').style.display = 'flex';
}

function closeDeleteModal() {
    document.getElementById('deleteModal').style.display = 'none';
    deleteCredId = null;
}

function confirmDelete() {
    if (!deleteCredId) return;

    const formData = new FormData();
    formData.append('cred_id', deleteCredId);

    fetch(`/projects/${projectId}/credentials/delete`, { method: 'POST', body: formData })
        .then(res => res.json())
        .then(data => {
            if (data.error) {
                closeDeleteModal();
                showNotificationModal('Error', data.error);
            } else {
                window.location.reload();
            }
        })
        .catch(() => {
            closeDeleteModal();
            showNotificationModal('Error', 'Failed to delete credential');
        });
}

// Checkbox selection
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
        selectAll.checked = checkboxes.length > 0 && checked.length === checkboxes.length;
        selectAll.indeterminate = checked.length > 0 && checked.length < checkboxes.length;
    }
}

function toggleSelectAll() {
    const selectAll = document.getElementById('selectAll');
    document.querySelectorAll('.row-checkbox').forEach(cb => {
        cb.checked = selectAll.checked;
    });
    updateSelection();
}

// Bulk delete
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

    fetch(`/projects/${projectId}/credentials/bulk-delete`, { method: 'POST', body: formData })
        .then(res => res.json())
        .then(data => {
            if (data.error) {
                closeBulkDeleteModal();
                showNotificationModal('Error', data.error);
            } else {
                window.location.reload();
            }
        })
        .catch(() => {
            closeBulkDeleteModal();
            showNotificationModal('Error', 'Failed to delete credentials');
        });
}

// Reset checkboxes on page load
document.querySelectorAll('.cred-checkbox').forEach(cb => cb.checked = false);
document.getElementById('selectionBar').classList.remove('active');

// Escape key in notes textarea closes modal; Enter creates newline (default behavior)
document.getElementById('credNotes').addEventListener('keydown', (e) => {
    if (e.key === 'Escape') {
        closeAddModal();
    }
});
