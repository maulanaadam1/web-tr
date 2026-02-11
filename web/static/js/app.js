// Stream Management
async function loadStreams() {
    const response = await fetch('/api/streams');
    const streams = await response.json();

    const container = document.getElementById('streamsList');
    container.innerHTML = '';

    for (const s of streams) {
        const card = createStreamCard(s.name, s.url);
        container.appendChild(card);
    }

    initPlayers();
}

function createStreamCard(name, url) {
    const card = document.createElement('div');
    card.className = 'card bg-white dark:bg-gray-800 rounded-xl overflow-hidden shadow-lg border border-gray-200 dark:border-gray-700 hover:border-blue-300 dark:hover:border-gray-600 transition-all';
    card.dataset.name = name;
    card.dataset.url = url;

    card.innerHTML = `
        <div class="p-4 flex justify-between items-center bg-gray-50 dark:bg-gray-800/50 backdrop-blur-sm border-b border-gray-200 dark:border-gray-700/50">
            <h3 class="font-semibold text-lg truncate text-gray-800 dark:text-white" title="${name}">${name}</h3>
            <div class="flex gap-2">
                <!-- Edit Button -->
                <button class="text-gray-500 dark:text-gray-400 hover:text-blue-600 dark:hover:text-blue-400 p-1 rounded-md hover:bg-gray-200 dark:hover:bg-gray-700 transition-colors edit-btn" onclick="openEditModal('${name}', '${escapeJS(url)}')">
                    <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5" viewBox="0 0 20 20" fill="currentColor">
                        <path d="M13.586 3.586a2 2 0 112.828 2.828l-.793.793-2.828-2.828.793-.793zM11.379 5.793L3 14.172V17h2.828l8.38-8.379-2.83-2.828z"></path>
                    </svg>
                </button>
                <!-- Snapshot Button -->
                <button class="text-gray-500 dark:text-gray-400 hover:text-purple-600 dark:hover:text-purple-400 p-1 rounded-md hover:bg-gray-200 dark:hover:bg-gray-700 transition-colors snapshot-btn" onclick="takeSnapshot('${name}')" title="Take Snapshot">
                    <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5" viewBox="0 0 20 20" fill="currentColor">
                        <path fill-rule="evenodd" d="M4 5a2 2 0 00-2 2v8a2 2 0 002 2h12a2 2 0 002-2V7a2 2 0 00-2-2h-1.586a1 1 0 01-.707-.293l-1.121-1.121A2 2 0 0011.172 3H8.828a2 2 0 00-1.414.586L6.293 4.707A1 1 0 015.586 5H4zm6 9a3 3 0 100-6 3 3 0 000 6z" clip-rule="evenodd"></path>
                    </svg>
                </button>
                <!-- Delete Button -->
                <button class="text-gray-500 dark:text-gray-400 hover:text-red-600 dark:hover:text-red-400 p-1 rounded-md hover:bg-gray-200 dark:hover:bg-gray-700 transition-colors delete-btn" onclick="deleteStream('${name}')">
                    <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5" viewBox="0 0 20 20" fill="currentColor">
                        <path fill-rule="evenodd" d="M9 2a1 1 0 00-.894.553L7.382 4H4a1 1 0 000 2v10a2 2 0 002 2h8a2 2 0 002-2V6a1 1 0 100-2h-3.382l-.724-1.447A1 1 0 0011 2H9zM7 8a1 1 0 012 0v6a1 1 0 11-2 0V8zm5-1a1 1 0 00-1 1v6a1 1 0 102 0V8a1 1 0 00-1-1z" clip-rule="evenodd"></path>
                    </svg>
                </button>
                <!-- Share Button -->
                <button class="text-gray-500 dark:text-gray-400 hover:text-green-600 dark:hover:text-green-400 p-1 rounded-md hover:bg-gray-200 dark:hover:bg-gray-700 transition-colors share-btn" onclick="openShareModal('${name}')">
                    <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5" viewBox="0 0 20 20" fill="currentColor">
                        <path d="M15 8a3 3 0 10-2.977-2.63l-4.94 2.47a3 3 0 100 4.319l4.94 2.47a3 3 0 10.895-1.789l-4.94-2.47a3.027 3.027 0 000-.74l4.94-2.47C13.456 7.68 14.19 8 15 8z"></path>
                    </svg>
                </button>
            </div>
        </div>
        <div class="video-container relative w-full bg-black aspect-video group" id="video-${name}"></div>
        <div class="p-3 bg-gray-50 dark:bg-gray-800 border-t border-gray-200 dark:border-gray-700 flex justify-between items-center gap-2">
            <span class="text-xs text-gray-500 dark:text-gray-400 font-medium">Mode:</span>
            <div class="flex gap-2">
                <button onclick="reloadPlayer('${name}', 'webrtc')" class="px-2 py-1 text-xs font-medium rounded bg-blue-100 dark:bg-blue-900 text-blue-700 dark:text-blue-300 hover:bg-blue-200 dark:hover:bg-blue-800 transition-colors">
                    WebRTC
                </button>
                <button onclick="reloadPlayer('${name}', 'mse')" class="px-2 py-1 text-xs font-medium rounded bg-purple-100 dark:bg-purple-900 text-purple-700 dark:text-purple-300 hover:bg-purple-200 dark:hover:bg-purple-800 transition-colors">
                    MSE
                </button>
                <!-- Optional: Add HLS button
                <button onclick="reloadPlayer('${name}', 'hls')" class="px-2 py-1 text-xs font-medium rounded bg-indigo-100 dark:bg-indigo-900 text-indigo-700 dark:text-indigo-300 hover:bg-indigo-200 dark:hover:bg-indigo-800 transition-colors">
                    HLS
                </button>
                -->
            </div>
        </div>
    `;

    return card;
}

function escapeJS(str) {
    return str.replace(/'/g, "\\'").replace(/"/g, '\\"');
}

function openShareModal(name) {
    const hostname = window.location.hostname;
    const port = window.location.port ? `:${window.location.port}` : "";
    const protocol = window.location.protocol;

    // Determine Base URL
    let shareUrl = `${protocol}//${hostname}${port}/share?stream=${encodeURIComponent(name)}`;

    // If not localhost, we use the direct Go2RTC player via Reverse Proxy
    if (hostname !== 'localhost' && hostname !== '127.0.0.1') {
        // Assuming reverse proxy is at /rtc/
        shareUrl = `${protocol}//${hostname}/rtc/stream.html?src=${encodeURIComponent(name)}`;
    }

    const embed = `<iframe src="${shareUrl}" width="100%" height="100%" frameborder="0" allowfullscreen></iframe>`;

    document.getElementById("shareLink").value = shareUrl;
    document.getElementById("embedCode").value = embed;
    document.getElementById("shareModal").classList.remove("hidden");
}

function closeShareModal() {
    document.getElementById("shareModal").classList.add("hidden");
}

function copyToClipboard(elementId) {
    const input = document.getElementById(elementId);
    input.select();
    document.execCommand('copy');
    alert('Copied to clipboard!');
}

// === Add/Edit/Delete Stream Functions ===

function openAddModal() {
    resetAdvancedOptions();

    document.getElementById("modalTitle").textContent = "Add Stream";
    document.getElementById("streamName").value = "";
    document.getElementById("streamUrl").value = "";
    document.getElementById("editOriginalName").value = "";
    document.getElementById("streamModal").classList.remove("hidden");

    // Reset advanced options to hidden
    document.getElementById("advancedOptions").classList.add("hidden");
    document.getElementById("toggleAdvancedArgs").innerHTML = '<span class="mr-1">▶</span> Advanced Stream Tuning (Manual)';

    // Update button text and action
    const submitBtn = document.getElementById("saveStreamBtn");
    submitBtn.textContent = "Add Stream";
    submitBtn.onclick = async function () {
        await submitStreamForm(false); // False = Add mode
    };
}

function resetAdvancedOptions() {
    document.getElementById("streamType").value = "direct";
    document.getElementById("videoCodec").value = "";
    document.getElementById("audioCodec").value = "";
    document.getElementById("hwAccel").value = "";
    document.getElementById("preset").value = "";
    document.getElementById("ffmpegOptions").classList.add("hidden");
}

function openEditModal(name, url) {
    resetAdvancedOptions();

    document.getElementById("modalTitle").textContent = "Edit Stream";
    document.getElementById("streamName").value = name;
    document.getElementById("streamUrl").value = url;
    document.getElementById("editOriginalName").value = name;
    document.getElementById("streamModal").classList.remove("hidden");

    // Update button text
    const submitBtn = document.getElementById("saveStreamBtn");
    submitBtn.textContent = "Save Changes";
    submitBtn.onclick = async function () {
        await submitStreamForm(true); // True = Edit mode
    };
}

async function submitStreamForm(isEdit) {
    const name = document.getElementById("streamName").value.trim();
    const url = document.getElementById("streamUrl").value.trim();
    const originalName = document.getElementById("editOriginalName").value.trim();

    if (!name || !url) {
        alert("Please fill in all required fields");
        return;
    }

    const method = isEdit ? 'PUT' : 'POST';
    const body = isEdit ? JSON.stringify({ name, url, originalName }) : JSON.stringify({ name, url });

    try {
        const response = await fetch('/api/streams', {
            method,
            headers: { 'Content-Type': 'application/json' },
            body
        });

        if (response.ok) {
            closeModal();
            location.reload();
        } else {
            const errorText = await response.text();
            alert(`Failed: ${errorText}`);
        }
    } catch (error) {
        alert(`Error: ${error.message}`);
    }
}

async function deleteStream(name) {
    if (!confirm(`Delete stream "${name}"?`)) return;

    try {
        const response = await fetch(`/api/streams?name=${encodeURIComponent(name)}`, {
            method: 'DELETE'
        });

        if (response.ok) {
            location.reload();
        } else {
            const errorText = await response.text();
            alert(`Failed to delete: ${errorText}`);
        }
    } catch (error) {
        alert(`Error: ${error.message}`);
    }
}

function closeModal() {
    document.getElementById("streamModal").classList.add("hidden");
}

// === Test Connection ===
async function testStreamConnection() {
    const url = document.getElementById("streamUrl").value.trim();
    const resultSpan = document.getElementById("testConnectionResult");
    const testBtn = document.getElementById("testStreamBtn");

    if (!url) {
        resultSpan.textContent = "Please enter a URL first";
        resultSpan.className = "block text-right mt-1 text-xs font-medium text-red-600 dark:text-red-400";
        return;
    }

    testBtn.disabled = true;
    testBtn.textContent = "Testing...";
    resultSpan.textContent = "Connecting...";
    resultSpan.className = "block text-right mt-1 text-xs font-medium text-gray-600 dark:text-gray-400";

    try {
        const response = await fetch('/api/probe', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ url })
        });

        if (response.ok) {
            resultSpan.textContent = "✓ Connection Successful";
            resultSpan.className = "block text-right mt-1 text-xs font-medium text-green-600 dark:text-green-400";
        } else {
            const errorText = await response.text();
            resultSpan.textContent = `✗ ${errorText}`;
            resultSpan.className = "block text-right mt-1 text-xs font-medium text-red-600 dark:text-red-400";
        }
    } catch (error) {
        resultSpan.textContent = `✗ Error: ${error.message}`;
        resultSpan.className = "block text-right mt-1 text-xs font-medium text-red-600 dark:text-red-400";
    } finally {
        testBtn.disabled = false;
        testBtn.textContent = "Test Connection";
    }
}

// === Network Scanner ===
async function scanNetwork() {
    const btn = document.getElementById("scanNetworkBtn");
    const resultsDiv = document.getElementById("scanResults");
    const listDiv = document.getElementById("scanList");

    btn.disabled = true;
    btn.innerHTML = `
        <svg class="animate-spin h-3 w-3 inline-block" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
            <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
            <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
        </svg>
        Scanning...
    `;

    try {
        const response = await fetch('/api/discover');
        const devices = await response.json();

        if (devices && devices.length > 0) {
            resultsDiv.classList.remove("hidden");
            listDiv.innerHTML = devices.map(d => `
                <div class="text-xs text-blue-600 dark:text-blue-400 hover:underline cursor-pointer" onclick="fillStreamUrl('rtsp://${d.address}:554/stream')">
                    ${d.address}
                </div>
            `).join('');
        } else {
            resultsDiv.classList.remove("hidden");
            listDiv.innerHTML = '<div class="text-xs text-gray-500 dark:text-gray-400">No devices found</div>';
        }
    } catch (error) {
        alert(`Scan failed: ${error.message}`);
    } finally {
        btn.disabled = false;
        btn.innerHTML = `
            <svg xmlns="http://www.w3.org/2000/svg" class="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 12a9 9 0 01-9 9m9-9a9 9 0 00-9-9m9 9H3m9 9a9 9 0 01-9-9m9 9c1.657 0 3-4.03 3-9s-1.343-9-3-9m0 18c-1.657 0-3-4.03-3-9s1.343-9 3-9m-9 9a9 9 0 019-9"></path>
            </svg>
            or Scan Local Network
        `;
    }
}

function fillStreamUrl(url) {
    document.getElementById("streamUrl").value = url;
}

// === Snapshot Function ===
async function takeSnapshot(name) {
    try {
        const response = await fetch(`/api/snapshot?stream=${encodeURIComponent(name)}`);
        const blob = await response.blob();
        const url = window.URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = `${name}-snapshot.jpg`;
        document.body.appendChild(a);
        a.click();
        document.body.removeChild(a);
        window.URL.revokeObjectURL(url);
    } catch (error) {
        alert(`Failed to take snapshot: ${error.message}`);
    }
}

// === Quick Optimization Selection ===
function selectOptimization(type, card) {
    // Remove selected class from all cards
    document.querySelectorAll('.opt-card').forEach(c => c.classList.remove('selected'));
    card.classList.add('selected');

    const streamType = document.getElementById('streamType');
    const videoCodec = document.getElementById('videoCodec');
    const audioCodec = document.getElementById('audioCodec');
    const hwAccel = document.getElementById('hwAccel');
    const preset = document.getElementById('preset');

    // Reset first
    streamType.value = 'direct';
    videoCodec.value = '';
    audioCodec.value = '';
    hwAccel.value = '';
    preset.value = '';

    if (type === 'h264_native' || type === 'h265_native') {
        streamType.value = 'direct';
    } else if (type === 'ultra_low') {
        streamType.value = 'ffmpeg';
        videoCodec.value = 'h264';
        preset.value = 'ultrafast';
    } else if (type === 'manual') {
        streamType.value = 'ffmpeg';
        // Allow user to configure manually
    }

    // Trigger stream type change to show/hide FFmpeg options
    streamType.dispatchEvent(new Event('change'));
}

// === Advanced Options Toggle ===
document.getElementById("toggleAdvancedArgs")?.addEventListener("click", function () {
    const advOptions = document.getElementById("advancedOptions");
    if (advOptions.classList.contains("hidden")) {
        advOptions.classList.remove("hidden");
        this.innerHTML = '<span class="mr-1">▼</span> Advanced Stream Tuning (Manual)';
    } else {
        advOptions.classList.add("hidden");
        this.innerHTML = '<span class="mr-1">▶</span> Advanced Stream Tuning (Manual)';
    }
});

document.getElementById("streamType")?.addEventListener("change", function () {
    const ffmpegOpts = document.getElementById("ffmpegOptions");
    if (this.value === 'ffmpeg') {
        ffmpegOpts.classList.remove("hidden");
    } else {
        ffmpegOpts.classList.add("hidden");
    }
});

// === Theme Toggle ===
const themeToggleBtn = document.getElementById('themeToggleBtn');
const htmlElement = document.documentElement;

themeToggleBtn?.addEventListener('click', () => {
    if (htmlElement.classList.contains('dark')) {
        htmlElement.classList.remove('dark');
        localStorage.setItem('theme', 'light');
    } else {
        htmlElement.classList.add('dark');
        localStorage.setItem('theme', 'dark');
    }
});

// Load theme on page load
if (localStorage.getItem('theme') === 'dark' || !localStorage.getItem('theme')) {
    htmlElement.classList.add('dark');
}

// === Player Functions ===

function reloadPlayer(name, mode) {
    // Find the card for this stream
    const card = document.querySelector(`.card[data-name="${name}"]`);
    if (!card) return;

    const container = card.querySelector('.video-container');
    container.innerHTML = ''; // Clear existing

    console.log(`Reloading ${name} in ${mode} mode`);

    const iframe = document.createElement('iframe');
    const hostname = window.location.hostname;

    // Determine Go2RTC Base URL
    let go2rtcBase = `http://${hostname}:1984`;

    // If not localhost, we assume a Reverse Proxy setup (like /rtc/)
    if (hostname !== 'localhost' && hostname !== '127.0.0.1') {
        go2rtcBase = '/rtc';
    }

    // Direct Go2RTC player
    iframe.src = `${go2rtcBase}/stream.html?src=${encodeURIComponent(name)}`;
    iframe.style.width = "100%";
    iframe.style.height = "100%";
    iframe.style.border = "none";
    iframe.allow = "autoplay; fullscreen; picture-in-picture";

    container.appendChild(iframe);
}

function initPlayers() {
    const cards = document.querySelectorAll('.card');
    cards.forEach(card => {
        const name = card.dataset.name;
        const container = card.querySelector('.video-container');

        // Clear container
        container.innerHTML = '';

        // Determine Go2RTC Base URL
        const hostname = window.location.hostname;
        let go2rtcBase = `http://${hostname}:1984`;

        // If not localhost, we assume a Reverse Proxy setup (like /rtc/)
        // This covers both HTTPS and HTTP (if port 1984 is blocked externally)
        if (hostname !== 'localhost' && hostname !== '127.0.0.1') {
            go2rtcBase = '/rtc';
        }

        const iframe = document.createElement('iframe');
        // Direct Go2RTC player
        iframe.src = `${go2rtcBase}/stream.html?src=${encodeURIComponent(name)}`;

        //https://stream.campod.my.id/rtc/stream.html?src=Workshop

        iframe.style.width = "100%";
        iframe.style.height = "100%";
        iframe.style.border = "none";
        iframe.allow = "autoplay; fullscreen; picture-in-picture";
        container.appendChild(iframe);
    });
}


// ===== CSV Import Functions =====

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

        document.getElementById('importProgress').classList.add('hidden');
        document.getElementById('importCSVSubmitBtn').disabled = false;

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

// === Event Listeners ===
document.getElementById('addStreamBtn')?.addEventListener('click', openAddModal);
document.getElementById('importCSVBtn')?.addEventListener('click', openCSVImportModal);
document.addEventListener('DOMContentLoaded', loadStreams);
