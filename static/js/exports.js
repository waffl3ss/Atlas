// projectID is injected by the template
let deleteExportId = null;

// ===== PlexTrac Generation (with tag modal) =====

function generatePlextrac() {
    document.getElementById('plextracTagModal').style.display = 'flex';
}

function closePlextracTagModal() {
    document.getElementById('plextracTagModal').style.display = 'none';
}

function confirmGeneratePlextrac() {
    const tag = document.getElementById('plextracTagSelect').value;
    closePlextracTagModal();

    const btn = document.querySelector('.btn-plextrac');
    const origText = btn.textContent;
    btn.textContent = 'Generating...';
    btn.disabled = true;

    const errors = [];
    const assetForm = new FormData();
    assetForm.append('tag', tag);

    fetch(`/projects/${projectID}/exports/generate-plextrac-assets`, { method: 'POST', body: assetForm })
        .then(r => r.json())
        .then(assets => {
            if (!assets.success) errors.push('Assets: ' + (assets.error || 'Failed'));
            const findingsForm = new FormData();
            findingsForm.append('tag', tag);
            return fetch(`/projects/${projectID}/exports/generate-plextrac-findings`, { method: 'POST', body: findingsForm }).then(r => r.json());
        })
        .then(findings => {
            if (!findings.success) errors.push('Findings: ' + (findings.error || 'Failed'));
            if (errors.length > 0) {
                btn.textContent = origText;
                btn.disabled = false;
                showNotificationModal('Export Errors', errors.join('\n'));
                if (errors.length < 2) setTimeout(() => location.reload(), 1500);
            } else {
                location.reload();
            }
        })
        .catch(() => {
            btn.textContent = origText;
            btn.disabled = false;
            showNotificationModal('Error', 'Failed to generate PlexTrac exports');
        });
}

// ===== Other Generators =====

function generateLair() {
    fetch(`/projects/${projectID}/exports/generate-lair`, { method: 'POST' })
        .then(r => r.json())
        .then(data => {
            if (data.success) location.reload();
            else showNotificationModal('Error', data.error || 'Failed to generate Lair export');
        })
        .catch(() => showNotificationModal('Error', 'Failed to generate Lair export'));
}

function generateRaw() {
    fetch(`/projects/${projectID}/exports/generate-raw`, { method: 'POST' })
        .then(r => r.json())
        .then(data => {
            if (data.success) {
                location.reload();
            } else {
                showNotificationModal('Error', data.error || 'Failed to generate raw export');
            }
        })
        .catch(() => {
            showNotificationModal('Error', 'Failed to generate raw export');
        });
}

// ===== Export Delete =====

function showDeleteModal(id, filename) {
    deleteExportId = id;
    document.getElementById('deleteFilename').textContent = filename;
    document.getElementById('deleteExportModal').style.display = 'flex';
}

function closeDeleteModal() {
    document.getElementById('deleteExportModal').style.display = 'none';
    deleteExportId = null;
}

function confirmDelete() {
    if (!deleteExportId) return;
    const form = new FormData();
    form.append('export_id', deleteExportId);

    fetch(`/projects/${projectID}/exports/delete`, {
        method: 'POST',
        body: form
    })
    .then(r => r.json())
    .then(data => {
        if (data.success) {
            location.reload();
        } else {
            closeDeleteModal();
            showNotificationModal('Error', data.error || 'Failed to delete export');
        }
    })
    .catch(() => {
        closeDeleteModal();
        showNotificationModal('Error', 'Failed to delete export');
    });
}

// ===== Tags =====

function openAddTagModal() {
    document.getElementById('newTagName').value = '';
    document.getElementById('addTagModal').style.display = 'flex';
    document.getElementById('newTagName').focus();
}

function closeAddTagModal() {
    document.getElementById('addTagModal').style.display = 'none';
}

function addTag() {
    const name = document.getElementById('newTagName').value.trim();
    if (!name) return;

    const form = new FormData();
    form.append('name', name);

    fetch(`/projects/${projectID}/exports/tags/add`, { method: 'POST', body: form })
        .then(r => r.json())
        .then(data => {
            closeAddTagModal();
            if (data.success) {
                location.reload();
            } else {
                showNotificationModal('Error', data.error || 'Failed to add tag');
            }
        })
        .catch(() => {
            closeAddTagModal();
            showNotificationModal('Error', 'Failed to add tag');
        });
}

function openBulkTagModal() {
    document.getElementById('bulkTagInput').value = '';
    document.getElementById('bulkTagModal').style.display = 'flex';
    document.getElementById('bulkTagInput').focus();
}

function closeBulkTagModal() {
    document.getElementById('bulkTagModal').style.display = 'none';
}

function bulkAddTags() {
    const input = document.getElementById('bulkTagInput').value.trim();
    if (!input) return;

    const form = new FormData();
    form.append('name', input);

    fetch(`/projects/${projectID}/exports/tags/add`, { method: 'POST', body: form })
        .then(r => r.json())
        .then(data => {
            closeBulkTagModal();
            if (data.success) {
                location.reload();
            } else {
                showNotificationModal('Error', data.error || 'Failed to add tags');
            }
        })
        .catch(() => {
            closeBulkTagModal();
            showNotificationModal('Error', 'Failed to add tags');
        });
}

function deleteTag(tagId, el) {
    const form = new FormData();
    form.append('tag_id', tagId);

    fetch(`/projects/${projectID}/exports/tags/delete`, { method: 'POST', body: form })
        .then(r => r.json())
        .then(data => {
            if (data.success) {
                const row = el.closest('.tag-row');
                const tagName = row ? row.querySelector('.tag-name-cell').textContent : '';
                if (row) row.remove();
                // Remove from the PlexTrac modal dropdown too
                const select = document.getElementById('plextracTagSelect');
                if (select && tagName) {
                    const opt = Array.from(select.options).find(o => o.value === tagName);
                    if (opt) opt.remove();
                }
                // Show empty message if no tags left
                const list = document.getElementById('tagsList');
                if (list && !list.querySelector('.tag-row')) {
                    list.innerHTML = '<div class="tags-empty">No tags defined. Add tags to include them in PlexTrac exports.</div>';
                }
            }
        })
        .catch(() => {});
}

// ===== Keyboard shortcuts =====

document.getElementById('newTagName')?.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') addTag();
    if (e.key === 'Escape') closeAddTagModal();
});

