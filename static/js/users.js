// Add Users dropdown
function toggleAddDropdown() {
    document.getElementById('addDropdown').classList.toggle('active');
}

document.addEventListener('click', (e) => {
    if (!e.target.closest('.add-dropdown')) {
        document.getElementById('addDropdown').classList.remove('active');
    }
});

// New User modal
function openNewUserModal() {
    document.getElementById('addDropdown').classList.remove('active');
    document.getElementById('newUsername').value = '';
    document.getElementById('newUserModal').style.display = 'flex';
    document.getElementById('newUsername').focus();
}

function closeNewUserModal() {
    document.getElementById('newUserModal').style.display = 'none';
}

function addSingleUser() {
    const username = document.getElementById('newUsername').value.trim();
    if (!username) return;

    const formData = new FormData();
    formData.append('username', username);

    fetch(`/projects/${projectId}/users/add`, { method: 'POST', body: formData })
        .then(res => res.json())
        .then(data => {
            if (data.error) {
                closeNewUserModal();
                showNotificationModal('Error', data.error);
            } else {
                window.location.reload();
            }
        })
        .catch(() => {
            closeNewUserModal();
            showNotificationModal('Error', 'Failed to add user');
        });
}

// Bulk Users modal
function openBulkModal() {
    document.getElementById('addDropdown').classList.remove('active');
    document.getElementById('bulkUsernames').value = '';
    document.getElementById('bulkModal').style.display = 'flex';
    document.getElementById('bulkUsernames').focus();
}

function closeBulkModal() {
    document.getElementById('bulkModal').style.display = 'none';
}

function addBulkUsers() {
    const usernames = document.getElementById('bulkUsernames').value.trim();
    if (!usernames) return;

    const formData = new FormData();
    formData.append('usernames', usernames);

    fetch(`/projects/${projectId}/users/bulk`, { method: 'POST', body: formData })
        .then(res => res.json())
        .then(data => {
            closeBulkModal();
            if (data.error) {
                showNotificationModal('Error', data.error);
            } else {
                const msg = `Added: ${data.added}\nDuplicates skipped: ${data.skipped}`;
                showNotificationModal('Bulk Add Results', msg);
                if (data.added > 0) {
                    setTimeout(() => window.location.reload(), 1500);
                }
            }
        })
        .catch(() => {
            closeBulkModal();
            showNotificationModal('Error', 'Failed to add users');
        });
}

// Upload Users
function triggerUpload() {
    document.getElementById('addDropdown').classList.remove('active');
    document.getElementById('userFileInput').click();
}

document.getElementById('userFileInput').addEventListener('change', function () {
    if (!this.files.length) return;

    const formData = new FormData();
    formData.append('file', this.files[0]);

    fetch(`/projects/${projectId}/users/upload`, { method: 'POST', body: formData })
        .then(res => res.json())
        .then(data => {
            if (data.error) {
                showNotificationModal('Error', data.error);
            } else {
                const msg = `Added: ${data.added}\nDuplicates skipped: ${data.skipped}`;
                showNotificationModal('Upload Results', msg);
                if (data.added > 0) {
                    setTimeout(() => window.location.reload(), 1500);
                }
            }
        })
        .catch(() => {
            showNotificationModal('Error', 'Failed to upload users');
        });

    this.value = '';
});

// Delete single user modal
let deleteUserId = null;

function showDeleteModal(id, username) {
    deleteUserId = id;
    document.getElementById('deleteUsername').textContent = username;
    document.getElementById('deleteUserModal').style.display = 'flex';
}

function closeDeleteModal() {
    document.getElementById('deleteUserModal').style.display = 'none';
    deleteUserId = null;
}

function confirmDelete() {
    if (!deleteUserId) return;

    const formData = new FormData();
    formData.append('user_id', deleteUserId);

    fetch(`/projects/${projectId}/users/delete`, { method: 'POST', body: formData })
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
            showNotificationModal('Error', 'Failed to delete user');
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

// Bulk delete modal
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

    fetch(`/projects/${projectId}/users/bulk-delete`, { method: 'POST', body: formData })
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
            showNotificationModal('Error', 'Failed to delete users');
        });
}

// Reset checkboxes on page load (browser may restore state after reload)
document.querySelectorAll('.user-checkbox').forEach(cb => cb.checked = false);
document.getElementById('selectionBar').classList.remove('active');

// Enter key support for new user modal
document.getElementById('newUsername').addEventListener('keydown', (e) => {
    if (e.key === 'Enter') addSingleUser();
});
