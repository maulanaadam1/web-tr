// CSV Import Functions - Add to app.js

function openCSVImportModal() {
    document.getElementById('csvImportModal').classList.remove('hidden');
    document.getElementById('csvFileInput').value = '';
    document.getElementById('importResult').classList.add('hidden');
    document.getElementById('importProgress').classList.add('hidden');
}

function closeCSVImportModal() {
    document.getElementById('csvImportModal').classList.add('hidden');
}

function downloadCSVTemplate() {
    const csv = 'name,url\nWorkshop,rtsp://admin:password@192.168.1.100:554/stream\nCamera2,rtsp://admin:password@192.168.1.101:554/stream\n';
    const blob = new Blob([csv], { type: 'text/csv' });
    const url = window.URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'streams_template.csv';
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    window.URL.revokeObjectURL(url);
}

async function submitCSVImport() {
    const fileInput = document.getElementById('csvFileInput');
    const file = fileInput.files[0];

    if (!file) {
        alert('Please select a CSV file');
        return;
    }

    // Show progress
    document.getElementById('importProgress').classList.remove('hidden');
    document.getElementById('importResult').classList.add('hidden');
    document.getElementById('importCSVSubmitBtn').disabled = true;

    try {
        const formData = new FormData();
        formData.append('file', file);

        const response = await fetch('/api/streams/import', {
            method: 'POST',
            body: formData
        });

        const result = await response.json();

        // Hide progress
        document.getElementById('importProgress').classList.add('hidden');
        document.getElementById('importCSVSubmitBtn').disabled = false;

        // Show result
        const resultDiv = document.getElementById('importResult');
        resultDiv.classList.remove('hidden');

        if (result.success > 0) {
            resultDiv.innerHTML = `
                <div class="bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800 rounded p-3 mb-2">
                    <p class="text-green-800 dark:text-green-200 font-medium">✓ Successfully imported ${result.success} stream(s)</p>
                </div>
            `;
        }

        if (result.failed > 0) {
            resultDiv.innerHTML += `
                <div class="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded p-3">
                    <p class="text-red-800 dark:text-red-200 font-medium mb-2">✗ Failed to import ${result.failed} stream(s):</p>
                    <ul class="text-xs text-red-700 dark:text-red-300 list-disc list-inside">
                        ${result.errors.slice(0, 5).map(err => `<li>${err}</li>`).join('')}
                        ${result.errors.length > 5 ? `<li>... and ${result.errors.length - 5} more errors</li>` : ''}
                    </ul>
                </div>
            `;
        }

        // Refresh streams list
        if (result.success > 0) {
            setTimeout(() => {
                location.reload();
            }, 2000);
        }

    } catch (error) {
        document.getElementById('importProgress').classList.add('hidden');
        document.getElementById('importCSVSubmitBtn').disabled = false;

        const resultDiv = document.getElementById('importResult');
        resultDiv.classList.remove('hidden');
        resultDiv.innerHTML = `
            <div class="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded p-3">
                <p class="text-red-800 dark:text-red-200">Error: ${error.message}</p>
            </div>
        `;
    }
}

// Add event listener for Import CSV button
document.getElementById('importCSVBtn').addEventListener('click', openCSVImportModal);
