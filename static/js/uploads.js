// projectID is injected by the template

// Dropzone functionality
const dropzone = document.getElementById('dropzone');
const fileInput = document.getElementById('fileInput');

dropzone.addEventListener('click', () => fileInput.click());

dropzone.addEventListener('dragover', (e) => {
    e.preventDefault();
    dropzone.classList.add('dragover');
});

dropzone.addEventListener('dragleave', () => {
    dropzone.classList.remove('dragover');
});

dropzone.addEventListener('drop', (e) => {
    e.preventDefault();
    dropzone.classList.remove('dragover');
    if (e.dataTransfer.files.length > 0) {
        uploadFiles(Array.from(e.dataTransfer.files));
    }
});

fileInput.addEventListener('change', () => {
    if (fileInput.files.length > 0) {
        uploadFiles(Array.from(fileInput.files));
    }
});

// Upload priority: host-creating tools first, then supplemental
// nmap/nessus create hosts+services, nuclei adds findings,
// bbot/httpx only enrich existing hosts
const toolPriority = { 'nmap': 0, 'nessus': 1, 'nuclei': 2, 'bbot': 3, 'httpx': 4, 'lair': 5, 'atlas_raw': 6 };

// Detect tool type from filename/extension (best-effort client-side guess for ordering)
function guessToolType(filename) {
    const lower = filename.toLowerCase();
    if (lower.endsWith('.nessus')) return 'nessus';
    if (lower.includes('nmap') && lower.endsWith('.xml')) return 'nmap';
    if (lower.endsWith('.xml')) return 'nmap';
    if (lower.includes('httpx')) return 'httpx';
    if (lower.includes('bbot')) return 'bbot';
    if (lower.includes('nuclei')) return 'nuclei';
    if (lower.includes('lair')) return 'lair';
    if (lower.includes('atlas_raw') || lower.includes('atlas-raw')) return 'atlas_raw';
    // Default — server will detect properly, place in middle priority
    return 'nuclei';
}

function sortFilesByPriority(files) {
    return files.slice().sort((a, b) => {
        const pa = toolPriority[guessToolType(a.name)] ?? 2;
        const pb = toolPriority[guessToolType(b.name)] ?? 2;
        return pa - pb;
    });
}

async function uploadFiles(files) {
    const sorted = sortFilesByPriority(files);
    const total = sorted.length;
    const textEl = document.querySelector('.dropzone-text');
    const origText = textEl.textContent;

    dropzone.classList.add('dragover');

    let errors = [];

    for (let i = 0; i < sorted.length; i++) {
        const file = sorted[i];
        textEl.textContent = 'Uploading ' + (i + 1) + ' of ' + total + ': ' + file.name;

        try {
            const result = await uploadSingleFile(file);
            if (result.error) {
                errors.push(file.name + ': ' + result.error);
            }
        } catch (e) {
            errors.push(file.name + ': Upload failed');
        }
    }

    dropzone.classList.remove('dragover');
    textEl.textContent = origText;
    fileInput.value = '';

    if (errors.length > 0) {
        showNotificationModal('Upload Errors', errors.join('\n'));
        // Still reload to show any successful uploads
        setTimeout(() => window.location.reload(), 1500);
    } else {
        window.location.reload();
    }
}

function uploadSingleFile(file) {
    const formData = new FormData();
    formData.append('file', file);

    return fetch('/projects/' + projectID + '/upload', {
        method: 'POST',
        body: formData
    }).then(res => res.json());
}

// Delete modal
let deleteUploadId = null;

function showDeleteModal(id, filename) {
    deleteUploadId = id;
    document.getElementById('deleteFilename').textContent = filename;
    document.getElementById('deleteUploadModal').style.display = 'flex';
}

function closeDeleteModal() {
    document.getElementById('deleteUploadModal').style.display = 'none';
    deleteUploadId = null;
}

function confirmDelete() {
    if (!deleteUploadId) return;
    const formData = new FormData();
    formData.append('upload_id', deleteUploadId);

    fetch('/projects/' + projectID + '/upload/delete', {
        method: 'POST',
        body: formData
    })
    .then(res => res.json())
    .then(data => {
        if (data.error) {
            closeDeleteModal();
            showNotificationModal('Upload Error', data.error);
        } else {
            window.location.reload();
        }
    })
    .catch(() => {
        closeDeleteModal();
        showNotificationModal('Error', 'Delete failed');
    });
}

// Format file sizes
document.querySelectorAll('.size-cell').forEach(cell => {
    const bytes = parseInt(cell.dataset.size);
    if (bytes < 1024) cell.textContent = bytes + ' B';
    else if (bytes < 1048576) cell.textContent = (bytes / 1024).toFixed(1) + ' KB';
    else cell.textContent = (bytes / 1048576).toFixed(1) + ' MB';
});
