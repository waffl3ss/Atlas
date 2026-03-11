// Projects page JavaScript

// Modal functions
function openModal() {
    document.getElementById('createModal').classList.add('active');
}

function closeModal() {
    document.getElementById('createModal').classList.remove('active');
    document.getElementById('createForm').reset();
}

document.getElementById('createForm').addEventListener('submit', async (e) => {
    e.preventDefault();

    const formData = new FormData(e.target);

    try {
        const response = await fetch('/projects/create', {
            method: 'POST',
            body: formData
        });

        const data = await response.json();

        if (data.success) {
            window.location.href = '/projects/' + data.project_id;
        } else {
            showNotificationModal('✗ Error', data.error);
        }
    } catch (error) {
        showNotificationModal('✗ Error', 'Failed to create project');
    }
});

// Close modal when clicking outside
document.getElementById('createModal').addEventListener('click', (e) => {
    if (e.target.id === 'createModal') {
        closeModal();
    }
});
