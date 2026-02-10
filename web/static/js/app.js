document.addEventListener('DOMContentLoaded', () => {
    // Modal Interaction
    document.getElementById("addStreamBtn").onclick = openModal;
    document.getElementById("saveStreamBtn").onclick = handleSave;

    // Toggle Advanced
    document.getElementById("toggleAdvancedArgs").onclick = () => {
        const el = document.getElementById("advancedOptions");
        if (el.classList.contains("hidden")) {
            el.classList.remove("hidden");
            document.getElementById("toggleAdvancedArgs").querySelector("span").innerText = "▼";
        } else {
            el.classList.add("hidden");
            document.getElementById("toggleAdvancedArgs").querySelector("span").innerText = "▶";
        }
    };

    // Advanced Type Change
    document.getElementById("streamType").onchange = handleAdvancedTypeChange;

    // Theme Init
    initTheme();
    document.getElementById("themeToggleBtn").onclick = toggleTheme;

    // Init Players
    initPlayers();

    // Check Browser
    checkBrowserCompatibility();
});

function checkBrowserCompatibility() {
    const ua = navigator.userAgent;
    let browser = "Unknown";
    let version = 0;

    if (ua.indexOf("Chrome") > -1) {
        browser = "Chrome";
        const match = ua.match(/Chrome\/(\d+)/);
        if (match) version = parseInt(match[1]);
    } else if (ua.indexOf("Firefox") > -1) {
        browser = "Firefox";
        const match = ua.match(/Firefox\/(\d+)/);
        if (match) version = parseInt(match[1]);
    } else if (ua.indexOf("Safari") > -1) {
        browser = "Safari";
        const match = ua.match(/Version\/(\d+)/);
        if (match) version = parseInt(match[1]);
    } else if (ua.indexOf("Edg") > -1) {
        browser = "Edge";
        const match = ua.match(/Edg\/(\d+)/);
        if (match) version = parseInt(match[1]);
    }

    // MSE Support Check
    const mse = 'MediaSource' in window;

    // Logic for warning
    let outdated = false;
    let warningMsg = "";

    if (browser === "Chrome" && version < 90) outdated = true;
    if (browser === "Firefox" && version < 90) outdated = true;
    if (browser === "Safari" && version < 14) outdated = true;
    if (browser === "Edge" && version < 90) outdated = true;

    if (outdated || !mse) {
        warningMsg = `Your browser (${browser} ${version}) may be incompatible or outdated. `;
        if (!mse) warningMsg += "Media Source Extensions (MSE) are not supported. ";
        warningMsg += "Please update to the latest Chrome, Edge, Safari, or Firefox for the best streaming experience.";

        showToast(warningMsg, "error", 10000);
    } else {
        console.log(`Browser Compatible: ${browser} ${version} (MSE: ${mse})`);
    }
}

function showToast(msg, type = "info", duration = 3000) {
    const toast = document.createElement("div");
    toast.className = `fixed top-4 right-4 z-50 px-6 py-4 rounded-lg shadow-lg text-white transform transition-all duration-300 translate-y-0 opacity-100 ${type === "error" ? "bg-red-600" : "bg-blue-600"
        }`;
    toast.innerHTML = `
        <div class="flex items-center">
            <span class="mr-2 text-xl">${type === "error" ? "⚠️" : "ℹ️"}</span>
            <p>${msg}</p>
        </div>
    `;
    document.body.appendChild(toast);

    setTimeout(() => {
        toast.style.opacity = "0";
        toast.style.transform = "translateY(-20px)";
        setTimeout(() => toast.remove(), 300);
    }, duration);
}

// Settings Logic - REMOVED

function initTheme() {
    if (localStorage.theme === 'dark' || (!('theme' in localStorage) && window.matchMedia('(prefers-color-scheme: dark)').matches)) {
        document.documentElement.classList.add('dark');
    } else {
        document.documentElement.classList.remove('dark');
    }
}

function toggleTheme() {
    if (document.documentElement.classList.contains('dark')) {
        document.documentElement.classList.remove('dark');
        localStorage.theme = 'light';
    } else {
        document.documentElement.classList.add('dark');
        localStorage.theme = 'dark';
    }
}

function openModal() {
    document.getElementById("modalTitle").innerText = "Add New Stream";
    document.getElementById("editOriginalName").value = "";
    document.getElementById("streamForm").reset();

    // Default to H.264 Native profile
    selectOptimization('h264_native', document.querySelector(".opt-card"));

    document.getElementById("scanNetworkBtn").classList.remove("hidden");
    document.getElementById("streamModal").classList.remove("hidden");
}

function openEditModal(name, url) {
    // Reset form FIRST to avoid clearing populated values
    document.getElementById("streamForm").reset();

    document.getElementById("modalTitle").innerText = "Edit Stream";
    document.getElementById("editOriginalName").value = name;
    document.getElementById("streamName").value = name;

    // Parse URL to detect settings
    let type = "direct";
    let realUrl = url;

    // Check if FFmpeg
    if (url.startsWith("ffmpeg:") || url.startsWith("exec:")) {
        type = "ffmpeg";
        let raw = url.startsWith("ffmpeg:") ? url.substring(7) : url.substring(5);
        let parts = raw.split("#");
        realUrl = parts[0];

        // Fill simple advanced fields from args
        parts.forEach(p => {
            if (p.startsWith("video=")) document.getElementById("videoCodec").value = p.split("=")[1];
            if (p.startsWith("audio=")) document.getElementById("audioCodec").value = p.split("=")[1];
            if (p.startsWith("hwaccel=")) document.getElementById("hwAccel").value = p.split("=")[1];
            if (p.includes("preset")) {
                // naive check
                if (p.includes("ultrafast")) document.getElementById("preset").value = "ultrafast";
                if (p.includes("superfast")) document.getElementById("preset").value = "superfast";
                if (p.includes("medium")) document.getElementById("preset").value = "medium";
            }
        });
    }

    document.getElementById("streamUrl").value = realUrl;
    document.getElementById("streamType").value = type;
    handleAdvancedTypeChange();

    // Auto-Select Profile based on detected settings
    let matchedProfile = 'manual';
    let matchedCard = document.getElementById("manualCard");

    if (type === 'direct') {
        // We can't easily distinguish H264 vs H265 native without metadata, 
        // but we can default to H264 Native as it's the "Direct" equivalent.
        matchedProfile = 'h264_native';
        matchedCard = document.querySelector(".opt-card[onclick*='h264_native']");
    } else if (type === 'ffmpeg') {
        const v = document.getElementById("videoCodec").value;
        const p = document.getElementById("preset").value;
        const hw = document.getElementById("hwAccel").value;

        if (v === 'h264' && p === 'ultrafast' && (hw === 'auto' || hw === '')) {
            matchedProfile = 'ultra_low';
            matchedCard = document.querySelector(".opt-card[onclick*='ultra_low']");
        }
    }

    selectOptimization(matchedProfile, matchedCard);

    // Expand advanced if manual or complex
    if (matchedProfile === 'manual') {
        document.getElementById("advancedOptions").classList.remove("hidden");
        document.getElementById("toggleAdvancedArgs").querySelector("span").innerText = "▼";
    } else {
        // Hide advanced for cleaner look if it matched a preset
        document.getElementById("advancedOptions").classList.add("hidden");
        document.getElementById("toggleAdvancedArgs").querySelector("span").innerText = "▶";
    }

    document.getElementById("scanNetworkBtn").classList.add("hidden");

    // Attempt to detect recording (Naïve check if we don't have dedicated field yet)
    // Ideally backend should return this status. 
    // For now, we leave it unchecked or we could check if URL array exists? 
    // We will implement specific field support later.
    // document.getElementById("enableRecording").checked = false;

    document.getElementById("streamModal").classList.remove("hidden");
}

function closeModal() {
    document.getElementById("streamModal").classList.add("hidden");
}

function openShareModal(name) {
    const hostname = window.location.hostname;
    const port = window.location.port ? ":" + window.location.port : "";
    const protocol = window.location.protocol;

    // We use the public share link which maps to /share?stream=...
    // Adjust logic if you want direct Go2RTC link. 
    // Here we use the app's share page for a cleaner player.
    const shareUrl = `${protocol}//${hostname}${port}/share?stream=${encodeURIComponent(name)}`;
    const embed = `<iframe src="${shareUrl}" width="100%" height="100%" frameborder="0" allowfullscreen></iframe>`;

    document.getElementById("shareLink").value = shareUrl;
    document.getElementById("embedCode").value = embed;
    document.getElementById("shareModal").classList.remove("hidden");
}

function closeShareModal() {
    document.getElementById("shareModal").classList.add("hidden");
}

async function takeSnapshot(name) {
    // Open in new tab or trigger download
    // Using a simple window.open for now which will trigger browser to show/download image
    const url = `/api/snapshot?name=${encodeURIComponent(name)}`;
    window.open(url, '_blank');
}

function selectOptimization(profile, cardEl) {
    // UI selection
    document.querySelectorAll(".opt-card").forEach(c => c.classList.remove("selected"));
    if (cardEl) cardEl.classList.add("selected");

    // Apply logic
    const typeSelect = document.getElementById("streamType");
    const vCodec = document.getElementById("videoCodec");
    const aCodec = document.getElementById("audioCodec");
    const hwSelect = document.getElementById("hwAccel");
    const pSelect = document.getElementById("preset");

    switch (profile) {
        case 'h264_native':
            typeSelect.value = "direct";
            // Clears transcoder fields essentially
            break;
        case 'h265_native':
            typeSelect.value = "direct";
            break;
        case 'ultra_low':
            typeSelect.value = "ffmpeg";
            vCodec.value = "h264";
            aCodec.value = "aac";
            hwSelect.value = "auto";
            pSelect.value = "ultrafast";
            break;
        case 'manual':
            // Do not change values, just let user edit advanced
            // But ensure advanced is open
            document.getElementById("advancedOptions").classList.remove("hidden");
            document.getElementById("toggleAdvancedArgs").querySelector("span").innerText = "▼";
            break;
    }

    handleAdvancedTypeChange();
}

function handleAdvancedTypeChange() {
    const type = document.getElementById("streamType").value;
    const opts = document.getElementById("ffmpegOptions");
    if (type === "ffmpeg") {
        opts.classList.remove("hidden");
    } else {
        opts.classList.add("hidden");
    }
}

async function handleSave() {
    const originalName = document.getElementById("editOriginalName").value;
    const name = document.getElementById("streamName").value;
    let url = document.getElementById("streamUrl").value.trim();

    if (!name || !url) {
        alert("Please fill Name and URL");
        return;
    }

    const type = document.getElementById("streamType").value;

    if (type === "ffmpeg") {
        // Build FFmpeg string
        // Strip previous
        url = url.replace(/^ffmpeg:/, "").replace(/^exec:/, "").split("#")[0];

        let params = [];
        const vc = document.getElementById("videoCodec").value;
        const ac = document.getElementById("audioCodec").value;
        const hw = document.getElementById("hwAccel").value;
        const pre = document.getElementById("preset").value;

        if (vc) params.push("video=" + vc);
        if (ac) params.push("audio=" + ac);
        if (hw) params.push("hwaccel=" + hw);

        // Raw args for preset
        if (pre === "ultrafast") params.push("raw=-preset ultrafast");
        if (pre === "superfast") params.push("raw=-preset superfast");
        if (pre === "medium") params.push("raw=-preset medium");

        if (params.length > 0) {
            url = "ffmpeg:" + url + "#" + params.join("#");
        } else {
            // just ffmpeg prefix
            url = "ffmpeg:" + url;
        }
    }

    // If direct, keep raw url (or strip ffmpeg if user switched back)
    if (type === "direct") {
        url = url.replace(/^ffmpeg:/, "").replace(/^exec:/, "").split("#")[0];
    }

    const isEdit = !!originalName;
    const method = isEdit ? 'PUT' : 'POST';

    try {
        const res = await fetch('/api/streams', {
            method: method,
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name, url, originalName })
        });
        if (res.ok) window.location.reload();
        else alert(await res.text());
    } catch (e) {
        console.error(e);
        alert("Error");
    }
}

async function testStreamConnection() {
    const urlInput = document.getElementById("streamUrl");
    let url = urlInput.value.trim();
    const resultSpan = document.getElementById("testConnectionResult");
    const btn = document.getElementById("testStreamBtn");

    if (!url) {
        resultSpan.className = "ml-2 text-xs font-medium text-red-500";
        resultSpan.innerText = "URL required";
        return;
    }

    // Basic cleanup mimicking save logic to get the real source
    url = url.replace(/^ffmpeg:/, "").replace(/^exec:/, "").split("#")[0];

    resultSpan.className = "ml-2 text-xs font-medium text-blue-500";
    resultSpan.innerText = "Testing...";
    btn.disabled = true;

    try {
        const res = await fetch('/api/probe', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ url })
        });

        if (res.ok) {
            resultSpan.className = "ml-2 text-xs font-medium text-green-500";
            resultSpan.innerText = "✓ Connection Successful";
        } else {
            const err = await res.text();
            resultSpan.className = "ml-2 text-xs font-medium text-red-500";
            resultSpan.innerText = "✗ Failed: " + err;
        }
    } catch (e) {
        resultSpan.className = "ml-2 text-xs font-medium text-red-500";
        resultSpan.innerText = "✗ Error: " + e.message;
    } finally {
        btn.disabled = false;
    }
}

async function scanNetwork() {
    const btn = document.getElementById("scanNetworkBtn");
    const list = document.getElementById("scanList");
    const container = document.getElementById("scanResults");

    btn.disabled = true;
    btn.innerHTML = `<span class="animate-spin mr-1">↻</span> Scanning...`;

    // Clear previous
    list.innerHTML = "";
    container.classList.remove("hidden");
    list.innerHTML = `<div class="text-xs text-gray-400 italic">Scanning local subnet (1-25 sec)...</div>`;

    try {
        const res = await fetch('/api/discover', { method: 'POST' });
        const devices = await res.json();

        list.innerHTML = ""; // Clear loading msg

        if (devices && devices.length > 0) {
            devices.forEach(d => {
                const item = document.createElement("div");
                item.className = "flex justify-between items-center text-xs p-1 hover:bg-gray-200 dark:hover:bg-gray-700 rounded cursor-pointer";
                item.innerHTML = `
                    <span class="font-mono text-gray-700 dark:text-gray-300">${d.address}</span>
                    <span class="text-blue-500">Select</span>
                `;
                item.onclick = () => {
                    document.getElementById("streamUrl").value = d.url;
                    // Also suggest a name if empty
                    if (!document.getElementById("streamName").value) {
                        document.getElementById("streamName").value = "cam-" + d.address.split('.').pop();
                    }
                };
                list.appendChild(item);
            });
        } else {
            list.innerHTML = `<div class="text-xs text-gray-400">No devices found on port 554.</div>`;
        }

    } catch (e) {
        console.error(e);
        list.innerHTML = `<div class="text-xs text-red-500">Scan failed: ${e.message}</div>`;
    } finally {
        btn.disabled = false;
        btn.innerHTML = `Scan Local Network`;
    }
}

async function deleteStream(name) {
    if (!confirm(`Delete stream "${name}"?`)) return;
    await fetch(`/api/streams?name=${encodeURIComponent(name)}`, { method: 'DELETE' });
    window.location.reload();
}



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

    // If on HTTPS, we assume a Reverse Proxy setup (like /rtc/)
    if (window.location.protocol === 'https:') {
        // User confirmed https://stream.campod.my.id/rtc/ works
        go2rtcBase = '/rtc';
    }

    // Use WebRTC mode by default or respecting 'mode' for testing
    // But since we removed the mode buttons, we might default to webrtc
    // or keep the mode passed from UI (if buttons are still there)
    // For now, respect the argument, default 'webrtc'
    const playMode = mode || 'webrtc';

    iframe.src = `${go2rtcBase}/stream.html?src=${encodeURIComponent(name)}&mode=${playMode}`;
    iframe.style.width = "100%";
    iframe.style.height = "100%";
    iframe.style.border = "none";
    iframe.allow = "autoplay; fullscreen; picture-in-picture";

    container.appendChild(iframe);
}



function copyToClipboard(id) {
    const el = document.getElementById(id);
    el.select();
    document.execCommand("copy");
    // visual feedback could be added here
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

        // If on HTTPS, we assume a Reverse Proxy setup (like /rtc/)
        if (window.location.protocol === 'https:') {
            // User confirmed https://stream.campod.my.id/rtc/ works
            // So we use the relative path /rtc (which maps to the proxy)
            go2rtcBase = '/rtc';
        }

        const iframe = document.createElement('iframe');
        iframe.src = `${go2rtcBase}/stream.html?src=${encodeURIComponent(name)}&mode=webrtc`;
        iframe.style.width = "100%";
        iframe.style.height = "100%";
        iframe.style.border = "none";
        iframe.allow = "autoplay; fullscreen; picture-in-picture";
        container.appendChild(iframe);
    });
}


