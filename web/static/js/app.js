// Global State
let currentView = 'dashboard';
let maintenanceMap = null;
let maintenanceMarkers = {};
let selectedCameraOnMap = null;

// --- View Management ---
function switchView(viewName) {
    currentView = viewName;

    // Update URL without page reload
    if (viewName === 'commandcenter') {
        history.pushState(null, '', '/commandcenter');
    } else {
        history.pushState(null, '', '/admin');
    }

    // Hide all views
    document.querySelectorAll('#mainContainer > div').forEach(el => el.classList.add('hidden'));
    
    // Show requested view
    const target = document.getElementById(`view-${viewName}`);
    if (target) {
        target.classList.remove('hidden');
        if (viewName === 'commandcenter') {
            target.classList.add('flex');
        }
    }

    // Update Sidebar Styling
    document.querySelectorAll('.nav-link').forEach(el => {
        if (el.dataset.view === viewName) {
            el.classList.add('bg-brand-50', 'text-brand-700', 'dark:bg-brand-900/20', 'dark:text-brand-300');
            el.classList.remove('hover:bg-slate-100', 'dark:hover:bg-slate-800/50', 'text-slate-600', 'dark:text-slate-400');
        } else {
            el.classList.remove('bg-brand-50', 'text-brand-700', 'dark:bg-brand-900/20', 'dark:text-brand-300');
            el.classList.add('hover:bg-slate-100', 'dark:hover:bg-slate-800/50', 'text-slate-600', 'dark:text-slate-400');
        }
    });

    // Close sidebar on mobile if open
    const sidebar = document.getElementById('mainSidebar');
    const backdrop = document.getElementById('sidebarBackdrop');
    if (sidebar && sidebar.classList.contains('translate-x-0')) {
        sidebar.classList.remove('translate-x-0');
        sidebar.classList.add('-translate-x-full');
        if (backdrop) backdrop.classList.add('hidden');
    }

    // Update Breadcrumb if needed
    const breadcrumb = document.getElementById('headerBreadcrumb');
    if (breadcrumb) {
        breadcrumb.textContent = viewName === 'commandcenter' ? 'Command Center' : (viewName.charAt(0).toUpperCase() + viewName.slice(1));
    }

    // Handle view-specific initialization
    if (viewName === 'commandcenter') {
        if (allStreams.length === 0) {
            loadStreams().then(initCommandCenterMap);
        } else {
            initCommandCenterMap();
        }
        return; // Don't run other dashboard stuff
    }

    // Refresh Data for specific views
    if (viewName === 'dashboard') {
        fetchSysInfo();
        initMaintenanceMap();
    }
    if (viewName === 'cameras') loadStreams();
    if (viewName === 'users') loadUsers();
    if (viewName === 'timelapse') initTimelapseView();
}

// Ensure correct view on initial page load based on pathname
document.addEventListener('DOMContentLoaded', () => {
    if (window.location.pathname === '/commandcenter') {
        switchView('commandcenter');
    } else {
        switchView('dashboard');
    }
});

// Global theme toggle — called directly via onclick="toggleTheme()" from the button
function toggleTheme() {
    const html = document.documentElement;
    html.classList.toggle('dark');
    const nowDark = html.classList.contains('dark');
    localStorage.setItem('theme', nowDark ? 'dark' : 'light');
    // Sync Leaflet map tile layer if Command Center map is open
    if (typeof globalCameraMap !== 'undefined' && globalCameraMap && window.mapLayers) {
        changeGlobalMapLayer(nowDark ? 'Dark' : 'Light');
    }
}

// --- Stream Management ---
let allStreams = [];
let streamCurrentPage = 1;
let streamsPerPage = 10;

async function loadStreams() {
    try {
        const response = await fetch('/api/streams');
        allStreams = await response.json() || [];
        streamCurrentPage = 1;
        renderStreamsTable();
    } catch (e) {
        console.error("Failed to load streams", e);
    }
}

function renderStreamsTable() {
    const gridContainer = document.getElementById('streamsList');
    if (gridContainer) {
        gridContainer.innerHTML = '';
        allStreams.forEach(s => {
            if (s.enabled !== false) {
                gridContainer.appendChild(createStreamCard(s.name, s.url));
            }
        });
    }

    const tableBody = document.getElementById('cameraTableBody');
    if (!tableBody) return;
    tableBody.innerHTML = '';

    const query = (document.getElementById('cameraSearch')?.value || '').toLowerCase();
    let filtered = allStreams.filter(s => {
        if (!query) return true;
        const searchStr = `${s.name} ${s.url} ${s.backend || 'go2rtc'} ${s.online ? 'online' : 'offline'} ${s.enabled === false ? 'disabled' : 'enabled'} ${s.lat} ${s.lng}`.toLowerCase();
        return searchStr.includes(query);
    });

    const startIndex = (streamCurrentPage - 1) * streamsPerPage;
    const endIndex = startIndex + streamsPerPage;
    const pagedStreams = filtered.slice(startIndex, endIndex);

    pagedStreams.forEach((s, i) => {
        const index = startIndex + i;
        const tr = document.createElement('tr');
        tr.className = 'block md:table-row bg-white dark:bg-slate-900 md:bg-transparent rounded-2xl md:rounded-none border border-slate-200 dark:border-slate-800 md:border-none md:border-b mb-4 p-4 md:p-0 relative shadow-sm md:shadow-none hover:bg-slate-50 dark:hover:bg-slate-800/50 transition-colors';
        tr.innerHTML = `
            <td class="hidden md:table-cell px-3 md:px-4 py-4 text-center">
                <input type="checkbox" value="${escapeJS(s.name)}" class="camera-checkbox rounded border-slate-300 text-brand-600 focus:ring-brand-500 bg-white dark:bg-slate-800 dark:border-slate-600 cursor-pointer" onclick="updateBulkActions()">
            </td>
            <td class="hidden md:table-cell px-3 md:px-4 py-4 text-sm text-slate-400 font-medium">${index + 1}</td>
            <td class="hidden md:table-cell px-3 md:px-4 py-4 flex items-center h-full pt-[22px]">
                ${s.online ? '<span class="flex h-2 w-2 rounded-full bg-green-500 shadow-[0_0_8px_rgba(34,197,94,0.6)]" title="Online"></span>' : '<span class="flex h-2 w-2 rounded-full bg-slate-300 dark:bg-slate-700" title="Offline"></span>'}
                ${s.enabled === false ? '<span class="ml-2 text-[9px] font-bold px-1.5 py-0.5 rounded-full bg-yellow-100 text-yellow-600 dark:bg-yellow-900/30 dark:text-yellow-400 uppercase tracking-tighter" title="Transcoding Disabled">Disabled</span>' : ''}
            </td>

            <td class="block md:hidden pb-3 border-b border-slate-100 dark:border-slate-800 mb-3">
                <div class="flex justify-between items-center w-full">
                    <div class="flex items-center gap-2">
                        <input type="checkbox" value="${escapeJS(s.name)}" class="camera-checkbox rounded w-4 h-4 text-brand-500 border-slate-300 cursor-pointer" onclick="updateBulkActions()">
                        <span class="text-xs font-bold text-brand-500">#${index + 1}</span>
                        ${s.enabled === false ? '<span class="ml-2 text-[9px] font-bold px-1.5 py-0.5 rounded bg-yellow-100 text-yellow-600 dark:bg-yellow-900/30 uppercase">Disabled</span>' : ''}
                    </div>
                    ${s.online 
                        ? '<div class="p-1 px-[10px] bg-green-50 dark:bg-green-900/30 text-green-500 rounded text-xl"><svg class="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 10V3L4 14h7v7l9-11h-7z"/></svg></div>' 
                        : '<div class="p-1 px-[10px] bg-slate-50 dark:bg-slate-800 text-slate-400 rounded text-xl"><svg class="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M18.364 5.636a9 9 0 010 12.728M16.95 7.05a7 7 0 010 9.9m-2.829-2.829a3 3 0 11-4.242-4.242 3 3 0 014.242 4.242zM12 4v2m0 12v2m8-8h2M2 12h2"/></svg></div>'
                    }
                </div>
            </td>

            <td class="block md:table-cell md:px-6 py-2 md:py-4">
                <div class="text-base md:text-sm font-bold text-slate-900 dark:text-white uppercase mb-1 md:mb-0">${s.name}</div>
                <div class="text-[11px] md:text-[10px] text-slate-500 font-mono flex items-center gap-1 mb-4 md:mb-0 truncate max-w-full">
                    <span class="text-slate-400">ID:</span> ${s.name.replace(/[^a-zA-Z0-9]/g,'').substring(0,8) || s.name} <span class="text-slate-400">(${s.backend || 'HLS'})</span>
                    ${s.resolution ? `<span class="ml-2 font-bold text-indigo-500 dark:text-indigo-400 px-1.5 py-0.5 bg-indigo-50 dark:bg-indigo-900/30 rounded">${s.resolution}</span>` : ''}
                </div>
                
                <div class="md:hidden flex gap-2 w-full mt-3">
                    <div class="flex-1 bg-slate-50 dark:bg-slate-800 p-2.5 rounded border border-slate-100 dark:border-slate-700">
                        <p class="text-[9px] font-bold text-slate-400 mb-1 uppercase tracking-wider">Method</p>
                        <p class="text-xs font-bold text-slate-700 dark:text-slate-300 uppercase">${s.type === 'ffmpeg' ? 'FFmpeg' : 'Direct'}</p>
                    </div>
                    <div class="flex-1 bg-slate-50 dark:bg-slate-800 p-2.5 rounded border border-slate-100 dark:border-slate-700">
                        <p class="text-[9px] font-bold text-slate-400 mb-1 uppercase tracking-wider">Timelapse</p>
                        <p class="text-xs font-bold uppercase ${s.timelapse_enabled ? 'text-green-600 dark:text-green-400' : 'text-slate-500'}">${s.timelapse_enabled ? 'Enabled' : 'Disabled'}</p>
                    </div>
                </div>

                <div class="md:hidden mt-4 flex items-center gap-2 text-xs font-bold text-slate-400 uppercase tracking-wide truncate max-w-full">
                    <svg class="h-4 w-4 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M17.657 16.657L13.414 20.9a1.998 1.998 0 01-2.827 0l-4.244-4.243a8 8 0 1111.314 0z"/><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 11a3 3 0 11-6 0 3 3 0 016 0z"/></svg>
                    <span class="truncate">${s.url}</span>
                </div>
            </td>

            <td class="hidden md:table-cell px-6 py-4">
                <span class="text-[10px] font-bold px-2 py-0.5 rounded-full bg-slate-100 dark:bg-slate-800 text-slate-500 uppercase tracking-tighter">${s.type === 'ffmpeg' ? 'FFmpeg' : 'Direct'}</span>
            </td>
            <td class="hidden md:table-cell px-6 py-4">
                <span class="text-[10px] font-bold px-2 py-0.5 rounded-full ${s.timelapse_enabled ? 'bg-green-100 text-green-600 dark:bg-green-900/30 dark:text-green-400' : 'bg-slate-100 text-slate-500 dark:bg-slate-800'} uppercase tracking-tighter cursor-help" title="${s.timelapse_enabled ? 'Interval: ' + s.timelapse_interval + 's' : 'Off'}">${s.timelapse_enabled ? 'Enabled' : 'Disabled'}</span>
            </td>

            <td class="block md:table-cell md:px-6 py-3 mt-4 pt-4 md:mt-0 md:pt-3 border-t md:border-none border-slate-100 dark:border-slate-800">
                <div class="flex justify-end gap-2 md:gap-1 w-full flex-wrap">
                    <button onclick="openCameraPreviewModal('${escapeJS(s.name)}')" class="flex-1 md:flex-none justify-center flex items-center gap-1 md:p-1.5 p-2 bg-blue-50 dark:bg-slate-800 md:bg-transparent text-blue-600 md:text-slate-400 hover:text-blue-600 rounded-lg transition-colors" title="Live Preview">
                        <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z"/><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z"/></svg>
                        <span class="md:hidden text-[10px] font-bold uppercase">Preview</span>
                    </button>
                    <button onclick="openEditModal('${escapeJS(s.name)}', '${escapeJS(s.url)}')" class="flex-1 md:flex-none justify-center flex items-center gap-1 md:p-1.5 p-2 bg-indigo-50 dark:bg-slate-800 md:bg-transparent text-indigo-600 md:text-slate-400 hover:text-indigo-600 rounded-lg transition-colors" title="Edit">
                        <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z"/></svg>
                        <span class="md:hidden text-[10px] font-bold uppercase">Edit</span>
                    </button>
                    <button onclick="deleteStream('${escapeJS(s.name)}')" class="flex-1 md:flex-none justify-center flex items-center gap-1 md:p-1.5 p-2 bg-red-50 dark:bg-slate-800 md:bg-transparent text-red-600 md:text-slate-400 hover:text-red-500 rounded-lg transition-colors" title="Delete">
                        <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"/></svg>
                        <span class="md:hidden text-[10px] font-bold uppercase">Delete</span>
                    </button>
                </div>
            </td>
        `;
        tableBody.appendChild(tr);
    });

    initPlayers();

    const totalPages = Math.ceil(filtered.length / streamsPerPage) || 1;
    const pageInfo = document.getElementById('cameraPageInfo');
    if (pageInfo) pageInfo.textContent = `Showing ${(filtered.length === 0 ? 0 : startIndex + 1)} to ${Math.min(endIndex, filtered.length)} of ${filtered.length} entries`;

    const pagination = document.getElementById('cameraPagination');
    if (pagination) {
        pagination.innerHTML = `
            <button onclick="streamCurrentPage=Math.max(1, streamCurrentPage-1);renderStreamsTable()" class="px-2 py-1 border border-slate-200 dark:border-slate-700 rounded text-xs hover:bg-slate-100 dark:hover:bg-slate-700">&lt;</button>
            <button class="px-2 py-1 border border-brand-500 rounded text-xs bg-brand-500 text-white">${streamCurrentPage}</button>
            <button onclick="streamCurrentPage=Math.min(${totalPages}, streamCurrentPage+1);renderStreamsTable()" class="px-2 py-1 border border-slate-200 dark:border-slate-700 rounded text-xs hover:bg-slate-100 dark:hover:bg-slate-700">&gt;</button>
        `;
    }
}

function createStreamCard(name, url) {
    const card = document.createElement('div');
    card.className = 'card bg-white dark:bg-slate-900 rounded-2xl overflow-hidden shadow-xl border border-slate-100 dark:border-slate-800 group';
    card.dataset.name = name;
    card.dataset.url = url;

    card.innerHTML = `
        <div class="p-4 flex justify-between items-center border-b border-slate-100 dark:border-slate-800">
            <h3 class="font-bold text-sm truncate text-slate-800 dark:text-white">${name}</h3>
            <button onclick="takeSnapshot('${name}')" class="p-1 rounded text-slate-400 hover:text-brand-600 transition-colors">
                <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" viewBox="0 0 20 20" fill="currentColor"><path fill-rule="evenodd" d="M4 5a2 2 0 00-2 2v8a2 2 0 002 2h12a2 2 0 002-2V7a2 2 0 00-2-2h-1.586a1 1 0 01-.707-.293l-1.121-1.121A2 2 0 0011.172 3H8.828a2 2 0 00-1.414.586L6.293 4.707A1 1 0 015.586 5H4zm6 9a3 3 0 100-6 3 3 0 000 6z" clip-rule="evenodd" /></svg>
            </button>
        </div>
        <div class="video-container relative w-full bg-black aspect-video" id="video-${name}"></div>
        <div class="p-3 bg-slate-50/50 dark:bg-slate-900/50 flex justify-between">
             <button onclick="reloadPlayer('${name}', 'webrtc')" class="text-[10px] font-bold px-2 py-1 rounded bg-blue-100 dark:bg-blue-900/30 text-blue-600">WebRTC</button>
             <button onclick="reloadPlayer('${name}', 'mse')" class="text-[10px] font-bold px-2 py-1 rounded bg-slate-200 dark:bg-slate-800 text-slate-600">MSE</button>
        </div>
    `;

    return card;
}

function initPlayers() {
    const cards = document.querySelectorAll('.card');
    cards.forEach(card => {
        const name = card.dataset.name;
        const container = card.querySelector('.video-container');
        if (container && container.innerHTML === '') {
            showSnapshotOverlay(name, container);
        }
    });
}

function showSnapshotOverlay(name, videoContainer) {
    const hostname = window.location.hostname;
    const port = window.location.port ? `:${window.location.port}` : "";
    const protocol = window.location.protocol;
    const go2rtcProxy = `${protocol}//${hostname}${port}/rtc`;
    const snapshotBase = `${protocol}//${hostname}${port}/api/snapshot`;

    const snapshotUrl = `${snapshotBase}?stream=${encodeURIComponent(name)}`;
    const iframeSrc = `${window.location.protocol}//${window.location.host}/rtc/stream.html?src=${encodeURIComponent(name)}&mode=webrtc,mse,hls,mp4,mjpeg`;

    videoContainer.innerHTML = `
        <div class="absolute inset-0 cursor-pointer flex items-center justify-center bg-gray-900 group" onclick="startLiveStream('${escapeJS(name)}', '${escapeJS(iframeSrc)}', this)">
            <img src="${snapshotUrl}" class="absolute inset-0 w-full h-full object-cover opacity-60 transition-opacity" onerror="this.src='data:image/svg+xml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIGZpbGw9Im5vbmUiIHZpZXdCb3g9IjAgMCAyNCAyNCIgc3Ryb2tlPSJncmF5Ij48cGF0aCBzdHJva2UtbGluZWNhcD0icm91bmQiIHN0cm9rZS1saW5lam9pbj0icm91bmQiIHN0cm9rZS13aWR0aD0iMiIgZD0iTTQgMTZsNC41ODYtNC41ODZhMiAyIDAgMDEyLjgyODAwTDE2IDE2bS0yLTJsMS41ODYtMS41ODZhMiAyIDAgMDEyLjgyODAwTDIwIDE0bS02LTZoLjAxTTYgMjBoMTJhMiAyIDAgMDAyLTJWNmEyIDIgMCAwMC0yLTJINmEyIDIgMCAwMC0yIDJ2MTJhMiAyIDAgMDAyIDJ6Ii8+PC9zdmc+'" alt="Snapshot Placeholder">
            <svg xmlns="http://www.w3.org/2000/svg" class="w-16 h-16 text-white opacity-80 group-hover:scale-110 transition-transform z-10 drop-shadow-lg" viewBox="0 0 20 20" fill="currentColor">
                <path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zM9.555 7.168A1 1 0 008 8v4a1 1 0 001.555.832l3-2a1 1 0 000-1.664l-3-2z" clip-rule="evenodd" />
            </svg>
        </div>
    `;
}

function startLiveStream(name, iframeSrc, overlayElement) {
    const container = overlayElement.parentElement;
    container.innerHTML = '';

    const loadingDiv = document.createElement('div');
    loadingDiv.className = 'absolute inset-0 flex flex-col items-center justify-center bg-gray-900';
    loadingDiv.innerHTML = `
        <div class="animate-spin rounded-full h-10 w-10 border-4 border-white border-t-transparent mb-3"></div>
        <span class="text-white text-sm opacity-70">Connecting...</span>
    `;
    container.appendChild(loadingDiv);

    const iframe = document.createElement('iframe');
    iframe.src = iframeSrc;
    iframe.style.cssText = 'width:100%;height:100%;border:none;position:absolute;top:0;left:0;z-index:10;';
    iframe.allow = 'autoplay; fullscreen; picture-in-picture';

    iframe.onload = function () {
        if (loadingDiv.parentElement) loadingDiv.remove();
        try {
            const style = document.createElement('style');
            style.textContent = `
                video { object-fit: fill !important; width: 100% !important; height: 100% !important; }
                body { background-color: black !important; margin: 0 !important; overflow: hidden !important; }
            `;
            iframe.contentDocument.head.appendChild(style);
        } catch (e) {}
    };

    setTimeout(() => { if (loadingDiv.parentElement) loadingDiv.remove(); }, 5000);
    container.appendChild(iframe);
}

function reloadPlayer(name, mode) {
    const card = document.querySelector(`.card[data-name="${name}"]`);
    if (!card) return;

    const videoContainer = card.querySelector('.video-container');
    videoContainer.innerHTML = '';

    const protocol = window.location.protocol;
    const hostname = window.location.hostname;
    const port = window.location.port ? `:${window.location.port}` : "";
    const go2rtcProxy = `${protocol}//${hostname}${port}/rtc`;
    const modeParam = (mode && mode !== 'auto') ? mode : 'webrtc';

    const iframe = document.createElement('iframe');
    iframe.src = `${go2rtcProxy}/stream.html?src=${encodeURIComponent(name)}&mode=${modeParam}`;
    iframe.style.width = "100%";
    iframe.style.height = "100%";
    iframe.style.border = "none";
    iframe.allow = "autoplay; fullscreen; picture-in-picture";

    iframe.onload = function () {
        try {
            const style = document.createElement('style');
            style.textContent = `video { object-fit: fill !important; width:100%; height:100%; } body { background: black; margin: 0; overflow: hidden; }`;
            iframe.contentDocument.head.appendChild(style);
        } catch (e) {}
    };

    videoContainer.appendChild(iframe);
}

// --- User Management ---
let allUsers = [];
let userCurrentPage = 1;
let usersPerPage = 10;

async function loadUsers() {
    try {
        const response = await fetch('/api/users');
        allUsers = await response.json() || [];
        userCurrentPage = 1;
        renderUsersTable();
    } catch (e) {
        console.error("Failed to load users", e);
    }
}

function renderUsersTable() {
    const tableBody = document.getElementById('userTableBody');
    if (!tableBody) return;
    tableBody.innerHTML = '';
    
    const query = (document.getElementById('userSearch')?.value || '').toLowerCase();
    let filtered = allUsers.filter(u => {
        if (!query) return true;
        const statusStr = u.is_active ? 'active' : 'disabled';
        const searchStr = `${u.full_name||''} ${u.username||''} ${u.email||''} ${u.whatsapp||''} ${u.role||''} ${statusStr}`.toLowerCase();
        return searchStr.includes(query);
    });

    const startIndex = (userCurrentPage - 1) * usersPerPage;
    const endIndex = startIndex + usersPerPage;
    const pagedUsers = filtered.slice(startIndex, endIndex);

    pagedUsers.forEach((u, i) => {
        const index = startIndex + i;
        const tr = document.createElement('tr');
        tr.className = 'hover:bg-slate-50 dark:hover:bg-slate-800/30 transition-colors';
        
        const nameInitial = (u.full_name || u.username).charAt(0).toUpperCase();
        const roleBadgeClass = u.role === 'admin'
            ? 'bg-brand-100 dark:bg-brand-900/30 text-brand-700 dark:text-brand-300'
            : 'bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400';

        const statusHtml = u.is_active 
            ? '<span class="px-2 py-0.5 rounded-full bg-green-100 dark:bg-green-900/30 text-green-600 text-[10px] font-bold">ACTIVE</span>'
            : '<span class="px-2 py-0.5 rounded-full bg-slate-100 dark:bg-slate-800 text-slate-400 text-[10px] font-bold">DISABLED</span>';

        tr.innerHTML = `
            <td class="hidden md:table-cell px-6 py-4 text-sm text-slate-400 font-medium">${index + 1}</td>
            
            <td class="px-6 py-4">
                <div class="flex items-center gap-3">
                    <div class="shrink-0 w-9 h-9 rounded-full bg-gradient-to-br from-brand-500 to-brand-700 flex items-center justify-center text-white text-sm font-bold shadow">
                        ${nameInitial}
                    </div>
                    <div>
                        <div class="text-sm font-bold text-slate-800 dark:text-white">${u.full_name || '—'}</div>
                        <div class="text-[11px] text-slate-400">@${u.username}</div>
                    </div>
                </div>
            </td>

            <td class="hidden md:table-cell px-6 py-4 text-sm text-slate-500 dark:text-slate-400">
                ${u.email ? `<a href="mailto:${u.email}" class="hover:text-brand-500 transition-colors">${u.email}</a>` : '<span class="text-slate-300">—</span>'}
            </td>

            <td class="hidden md:table-cell px-6 py-4 text-sm text-slate-500 dark:text-slate-400">
                ${u.whatsapp || '<span class="text-slate-300">—</span>'}
            </td>

            <td class="hidden md:table-cell px-6 py-4">
                <span class="text-[10px] font-bold px-2.5 py-1 rounded-full uppercase tracking-wider ${roleBadgeClass}">${u.role}</span>
            </td>
            
            <td class="hidden md:table-cell px-6 py-4">
                <div class="flex items-center gap-1.5 whitespace-nowrap">
                    <span class="text-xs font-bold text-slate-700 dark:text-slate-200" title="Total Cameras">${u.total_cameras || 0}</span>
                    <span class="text-slate-300 dark:text-slate-600">/</span>
                    <span class="text-[10px] font-bold text-green-600 bg-green-50 dark:bg-green-900/30 px-1.5 py-0.5 rounded" title="Online">${u.online_cameras || 0}</span>
                    <span class="text-slate-300 dark:text-slate-600">/</span>
                    <span class="text-[10px] font-bold text-red-600 bg-red-50 dark:bg-red-900/30 px-1.5 py-0.5 rounded" title="Offline/Error">${u.offline_cameras || 0}</span>
                </div>
            </td>

            <td class="hidden md:table-cell px-6 py-4">
                ${statusHtml}
            </td>

            <td class="px-6 py-4">
                <div class="flex justify-end gap-1">
                    <button onclick='openUserModal(${JSON.stringify(u)})' class="p-1.5 text-slate-400 hover:text-brand-600 hover:bg-brand-50 dark:hover:bg-brand-900/20 rounded-lg transition-colors" title="Edit User">
                        <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" /></svg>
                    </button>
                    ${u.username === 'admin' ? '' : `
                    <button onclick="deleteUser(${u.id})" class="p-1.5 text-slate-400 hover:text-red-600 hover:bg-red-50 dark:hover:bg-red-900/20 rounded-lg transition-colors" title="Delete User">
                        <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" /></svg>
                    </button>`}
                </div>
            </td>
        `;
        tableBody.appendChild(tr);
    });

    const totalPages = Math.ceil(filtered.length / usersPerPage) || 1;
    const pageInfo = document.getElementById('userPageInfo');
    if (pageInfo) pageInfo.textContent = `Showing ${(filtered.length===0?0:startIndex + 1)} to ${Math.min(endIndex, filtered.length)} of ${filtered.length} entries`;

    const pagination = document.getElementById('userPagination');
    if (pagination) {
        pagination.innerHTML = `
            <button onclick="userCurrentPage=Math.max(1, userCurrentPage-1);renderUsersTable()" class="px-2 py-1 border border-slate-200 dark:border-slate-700 rounded text-xs hover:bg-slate-100 dark:hover:bg-slate-700">&lt;</button>
            <button class="px-2 py-1 border border-brand-500 rounded text-xs bg-brand-500 text-white">${userCurrentPage}</button>
            <button onclick="userCurrentPage=Math.min(${totalPages}, userCurrentPage+1);renderUsersTable()" class="px-2 py-1 border border-slate-200 dark:border-slate-700 rounded text-xs hover:bg-slate-100 dark:hover:bg-slate-700">&gt;</button>
        `;
    }
}

// --- User Modal Logic ---
let selectedRole = 'user';

function openUserModal(user = null) {
    const isEdit = !!user;
    const modal = document.getElementById('userModal');
    const title = document.getElementById('userModalTitle');
    const submitBtnText = document.getElementById('userSubmitBtnText');
    const editId = document.getElementById('editUserId');
    
    // Reset Form
    editId.value = isEdit ? user.id : '';
    document.getElementById('userUsername').value = isEdit ? user.username : '';
    document.getElementById('userEmail').value = isEdit ? (user.email || '') : '';
    document.getElementById('userFullName').value = isEdit ? (user.full_name || '') : '';
    document.getElementById('userWhatsapp').value = isEdit ? (user.whatsapp || '') : '';
    document.getElementById('userCurrentPassword').value = '';
    document.getElementById('userPassword').value = '';
    document.getElementById('updatePasswordToggle').checked = !isEdit; 
    document.getElementById('passwordField').classList.toggle('hidden', isEdit);
    
    // Hide current password field if adding new user
    const currentPassWrapper = document.getElementById('currentPasswordFieldWrapper');
    if (currentPassWrapper) {
        currentPassWrapper.classList.toggle('hidden', !isEdit);
    }
    document.getElementById('userIsActive').checked = isEdit ? user.is_active : true;
    
    // Multi-tenant fields
    document.getElementById('userSubscription').value = isEdit ? (user.subscription_plan || 'Free') : 'Free';
    document.getElementById('userEnableSupport').checked = isEdit ? !!user.enable_support : false;
    document.getElementById('userEnableVPN').checked = isEdit ? !!user.enable_vpn : false;

    title.textContent = isEdit ? `Edit User: ${user.full_name || user.username}` : 'Add New User';
    submitBtnText.textContent = isEdit ? 'Update User' : 'Create User';
    
    selectRole(isEdit ? user.role : 'user');
    
    modal.classList.remove('hidden');
}

function closeUserModal() {
    document.getElementById('userModal').classList.add('hidden');
}

function selectRole(role) {
    selectedRole = role;
    document.querySelectorAll('.role-btn').forEach(btn => {
        if (btn.dataset.role === role) {
            btn.classList.add('bg-brand-600', 'text-white', 'border-brand-600');
            btn.classList.remove('border-slate-200', 'text-slate-600', 'hover:bg-slate-50', 'dark:border-slate-700');
        } else {
            btn.classList.remove('bg-brand-600', 'text-white', 'border-brand-600');
            btn.classList.add('border-slate-200', 'text-slate-600', 'hover:bg-slate-50', 'dark:border-slate-700');
        }
    });
}

// Initializing Role Buttons
document.querySelectorAll('.role-btn').forEach(btn => {
    btn.addEventListener('click', () => selectRole(btn.dataset.role));
});

// Password Toggle Handler
document.getElementById('updatePasswordToggle')?.addEventListener('change', (e) => {
    document.getElementById('passwordField').classList.toggle('hidden', !e.target.checked);
});

function togglePasswordVisibility(id, btn) {
    const input = document.getElementById(id);
    const svg = btn.querySelector('svg');
    if (input.type === 'password') {
        input.type = 'text';
        // eye-off icon
        svg.innerHTML = '<path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24"></path><line x1="1" y1="1" x2="23" y2="23"></line>';
    } else {
        input.type = 'password';
        // eye icon
        svg.innerHTML = '<path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"></path><circle cx="12" cy="12" r="3"></circle>';
    }
}

async function submitUserForm() {
    const id = document.getElementById('editUserId').value;
    const isEdit = !!id;
    
    const payload = {
        username: document.getElementById('userUsername').value,
        full_name: document.getElementById('userFullName').value,
        email: document.getElementById('userEmail').value,
        whatsapp: document.getElementById('userWhatsapp').value,
        role: selectedRole,
        is_active: document.getElementById('userIsActive').checked,
        subscription_plan: document.getElementById('userSubscription').value,
        enable_support: document.getElementById('userEnableSupport').checked,
        enable_vpn: document.getElementById('userEnableVPN').checked,
        broadcast_notifications: false,
        notification_paid: false,
    };

    if (document.getElementById('updatePasswordToggle').checked) {
        const pass = document.getElementById('userPassword').value;
        const currentPass = document.getElementById('userCurrentPassword').value;
        
        if (!isEdit && !pass) {
            alert("Password is required for new users");
            return;
        }
        if (isEdit) {
            payload.newPassword = pass;
            payload.currentPassword = currentPass;
            payload.update_pass = true;
        } else {
            payload.password = pass;
        }
    }

    try {
        const method = isEdit ? 'PUT' : 'POST';
        const url = isEdit ? `/api/users?id=${id}` : '/api/users';
        
        const response = await fetch(url, {
            method: method,
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload)
        });

        if (response.ok) {
            closeUserModal();
            loadUsers();
        } else {
            const err = await response.text();
            alert("Error: " + err);
        }
    } catch (e) {
        console.error("Submission failed", e);
    }
}

async function deleteUser(id) {
    if (!confirm('Are you sure you want to delete this user?')) return;
    try {
        const response = await fetch(`/api/users?id=${id}`, { method: 'DELETE' });
        if (response.ok) {
            loadUsers();
        } else {
            const err = await response.text();
            alert("Error: " + err);
        }
    } catch (e) {
        console.error("Deletion failed", e);
    }
}

function toggleMobileSidebar() {
    const sidebar = document.getElementById('mainSidebar');
    const backdrop = document.getElementById('sidebarBackdrop');
    if (!sidebar || !backdrop) return;
    
    if (sidebar.classList.contains('-translate-x-full')) {
        sidebar.classList.remove('-translate-x-full');
        sidebar.classList.add('translate-x-0');
        backdrop.classList.remove('hidden');
    } else {
        sidebar.classList.remove('translate-x-0');
        sidebar.classList.add('-translate-x-full');
        backdrop.classList.add('hidden');
    }
}

function openChangePwModal(id) {
    document.getElementById('changePwUserId').value = id;
    document.getElementById('changePwModal').classList.remove('hidden');
}

async function submitChangePw() {
    const id = document.getElementById('changePwUserId').value;
    const newPassword = document.getElementById('changePwInput').value;
    try {
        const res = await fetch(`/api/users?id=${id}`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ newPassword })
        });
        if (res.ok) { 
            document.getElementById('changePwModal').classList.add('hidden'); 
            alert("Updated"); 
        }
    } catch (e) { console.error(e); }
}

// --- Snapshot & Shared Functions ---
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
    } catch (error) { alert(`Failed to take snapshot: ${error.message}`); }
}

function escapeJS(str) {
    return str.replace(/'/g, "\\'").replace(/"/g, '\\"');
}

// --- Modals (Common) ---
let locationMap = null;
let locationMarker = null;

function initLocationMap(lat, lng) {
    setTimeout(() => {
        const DEFAULT_LAT = -7.2504; // Surabaya
        const DEFAULT_LNG = 112.7688;
        const initialLat = lat || DEFAULT_LAT;
        const initialLng = lng || DEFAULT_LNG;

        if (!locationMap) {
            locationMap = L.map('streamLocationMap').setView([initialLat, initialLng], 13);
            L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
                maxZoom: 19,
                attribution: '© OpenStreetMap'
            }).addTo(locationMap);
            
            locationMarker = L.marker([initialLat, initialLng], {draggable: true}).addTo(locationMap);
            
            locationMap.on('click', function(e) {
                const clickLat = e.latlng.lat;
                const clickLng = e.latlng.lng;
                document.getElementById('streamLat').value = clickLat.toFixed(6);
                document.getElementById('streamLng').value = clickLng.toFixed(6);
                locationMarker.setLatLng(e.latlng);
            });

            locationMarker.on('dragend', function(e) {
                const pos = locationMarker.getLatLng();
                document.getElementById('streamLat').value = pos.lat.toFixed(6);
                document.getElementById('streamLng').value = pos.lng.toFixed(6);
            });
            
            const updateMarker = () => {
                const mLat = parseFloat(document.getElementById('streamLat').value);
                const mLng = parseFloat(document.getElementById('streamLng').value);
                if(!isNaN(mLat) && !isNaN(mLng)) {
                    locationMarker.setLatLng([mLat, mLng]);
                    locationMap.panTo([mLat, mLng]);
                }
            };
            document.getElementById('streamLat').addEventListener('input', updateMarker);
            document.getElementById('streamLng').addEventListener('input', updateMarker);
        } else {
            locationMap.setView([initialLat, initialLng], 13);
            locationMarker.setLatLng([initialLat, initialLng]);
            locationMap.invalidateSize();
        }
        
        document.getElementById('streamLat').value = lat ? lat : '';
        document.getElementById('streamLng').value = lng ? lng : '';
    }, 150);
}

function openAddModal() {
    switchView('cameras'); // Helpful if coming from dashboard
    resetAdvancedOptions();
    document.getElementById("modalTitle").textContent = "Add Stream";
    document.getElementById("streamName").value = "";
    document.getElementById("streamUrl").value = "";
    document.getElementById("editOriginalName").value = "";
    document.getElementById("streamBuffer").value = "10";
    if (document.getElementById("timelapseEnabled")) {
        document.getElementById("timelapseEnabled").checked = false;
        document.getElementById("timelapsePresetSelect").value = "60";
        document.getElementById("timelapseCustomInput")?.classList.add("hidden");
    }
    const testRes = document.getElementById("testConnectionResult");
    if(testRes) testRes.textContent = "";

    initLocationMap(0, 0); // 0, 0 will be defaulted to Surabaya

    document.getElementById("streamModal").classList.remove("hidden");

    const submitBtn = document.getElementById("saveStreamBtn");
    submitBtn.onclick = async function () { await submitStreamForm(false); };
}

async function openEditModal(name, url) {
    resetAdvancedOptions();
    document.getElementById("modalTitle").textContent = "Edit Stream";
    document.getElementById("editOriginalName").value = name;
    document.getElementById("streamName").value = name;
    document.getElementById("streamUrl").value = url;
    
    const streamInfo = allStreams.find(s => s.name === name);
    initLocationMap(streamInfo?.lat || 0, streamInfo?.lng || 0);

    const testRes = document.getElementById("testConnectionResult");
    if(testRes) testRes.textContent = "";

    document.getElementById("streamModal").classList.remove("hidden");

    const submitBtn = document.getElementById("saveStreamBtn");
    submitBtn.onclick = async function () { await submitStreamForm(true); };
}

function closeModal() { document.getElementById("streamModal").classList.add("hidden"); }
function resetAdvancedOptions() { document.getElementById("advancedOptions")?.classList.add("hidden"); }

async function submitStreamForm(isEdit) {
    const name = document.getElementById("streamName").value.trim();
    let url = document.getElementById("streamUrl").value.trim();
    const originalName = document.getElementById("editOriginalName").value.trim();
    const lat = parseFloat(document.getElementById("streamLat").value) || 0;
    const lng = parseFloat(document.getElementById("streamLng").value) || 0;

    if (!name || !url) { alert("Fields required"); return; }

    const method = isEdit ? 'PUT' : 'POST';
    const body = isEdit ? JSON.stringify({ name, url, originalName, lat, lng }) : JSON.stringify({ name, url, lat, lng });

    try {
        const response = await fetch('/api/streams', {
            method,
            headers: { 'Content-Type': 'application/json' },
            body
        });
        if (response.ok) {
            closeModal(); 
            loadStreams(); 
        }
        else { alert(await response.text()); }
    } catch (e) { alert(e.message); }
}

async function testStreamConnection() {
    const urlInput = document.getElementById('streamUrl').value.trim();
    if (!urlInput) { alert("Please enter a URL first"); return; }
    
    const resultSpan = document.getElementById('testConnectionResult');
    const detailBtn = document.getElementById('showProbeDetailBtn');
    const detailOutput = document.getElementById('probeRawOutput');
    const detailContainer = document.getElementById('probeDetailContainer');
    
    if (!resultSpan) return;
    
    // Reset UI
    resultSpan.textContent = "Testing connection...";
    resultSpan.className = "text-xs font-medium text-slate-500 animate-pulse";
    detailBtn.classList.add('hidden');
    detailContainer.classList.add('hidden');
    detailOutput.textContent = '';
    
    const nameInput = document.getElementById('editStreamName')?.value || '';
    
    try {
        const res = await fetch('/api/probe', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ url: urlInput, name: nameInput })
        });
        
        const data = await res.json();
        
        // Always show raw output if present
        if (data.raw) {
            detailOutput.textContent = data.raw;
            detailBtn.classList.remove('hidden');
            detailBtn.textContent = 'Show Technical Details';
        }

        if (data.status === "success") {
            const resolution = data.resolution;
            resultSpan.textContent = "✓ Connection Successful" + (resolution ? ` (${resolution})` : "");
            resultSpan.className = "text-xs font-medium text-green-600";
        } else {
            resultSpan.textContent = "✗ Probe Failed: " + (data.error || "Unknown Error");
            resultSpan.className = "text-xs font-medium text-red-600";
        }
    } catch (e) {
        resultSpan.textContent = "✗ Network Error: " + e.message;
        resultSpan.className = "text-xs font-medium text-red-600";
    }
}

function toggleProbeDetail() {
    const container = document.getElementById('probeDetailContainer');
    const btn = document.getElementById('showProbeDetailBtn');
    if (container.classList.contains('hidden')) {
        container.classList.remove('hidden');
        btn.textContent = 'Hide Details';
    } else {
        container.classList.add('hidden');
        btn.textContent = 'Show Technical Details';
    }
}

function copyToClipboard(text) {
    if (!text) return;
    navigator.clipboard.writeText(text).then(() => {
        const btn = event.target;
        const originalText = btn.textContent;
        btn.textContent = 'Copied!';
        btn.classList.replace('text-slate-400', 'text-green-400');
        setTimeout(() => {
            btn.textContent = originalText;
            btn.classList.replace('text-green-400', 'text-slate-400');
        }, 2000);
    });
}

async function deleteStream(name) {
    if (!confirm(`Delete stream "${name}"?`)) return;
    try {
        const response = await fetch(`/api/streams?name=${encodeURIComponent(name)}`, { method: 'DELETE' });
        if (response.ok) loadStreams();
    } catch (e) { console.error(e); }
}

// --- Paging & Limits ---
function changePageSize() {
    const selector = document.getElementById('cameraPageSize');
    if (!selector) return;
    const val = selector.value;
    if (val === 'all') {
        streamsPerPage = allStreams.length > 0 ? allStreams.length : 9999;
    } else {
        streamsPerPage = parseInt(val, 10);
    }
    streamCurrentPage = 1;
    renderStreamsTable();
}

function changeUserPageSize() {
    const selector = document.getElementById('userPageSize');
    if (!selector) return;
    const val = selector.value;
    if (val === 'all') {
        usersPerPage = allUsers.length > 0 ? allUsers.length : 9999;
    } else {
        usersPerPage = parseInt(val, 10);
    }
    userCurrentPage = 1;
    renderUsersTable();
}

// --- Search / Filters ---
function filterCameraTable() {
    streamCurrentPage = 1;
    renderStreamsTable();
}

function filterUserTable() {
    userCurrentPage = 1;
    renderUsersTable();
}

// --- CSV Import ---
function openCSVImportModal() { document.getElementById('csvImportModal').classList.remove('hidden'); }
function closeCSVImportModal() { document.getElementById('csvImportModal').classList.add('hidden'); }
async function submitCSVImport() {
    const file = document.getElementById('csvFileInput').files[0];
    const text = document.getElementById('csvTextInput').value;
    
    if (!file && !text.trim()) {
        alert("Please provide a CSV file or paste the content.");
        return;
    }
    
    const formData = new FormData();
    if (file) formData.append('file', file);
    if (text.trim()) formData.append('raw_csv', text);
    
    try {
        const res = await fetch('/api/streams/import', { method: 'POST', body: formData });
        if (res.ok) { 
            closeCSVImportModal(); 
            document.getElementById('csvFileInput').value = '';
            document.getElementById('csvTextInput').value = '';
            loadStreams(); 
        } else {
            alert("Error importing CSV. Verify your format.");
        }
    } catch (e) {
        console.error(e);
        alert("Upload failed.");
    }
}

// --- Bulk Actions ---
function toggleAllCameras(checkbox) {
    document.querySelectorAll('.camera-checkbox').forEach(cb => cb.checked = checkbox.checked);
    updateBulkActions();
}

function updateBulkActions() {
    const selected = document.querySelectorAll('.camera-checkbox:checked').length;
    const bulkDiv = document.getElementById('bulkActions');
    document.getElementById('bulkCount').innerText = selected;
    if (selected > 0) {
        bulkDiv.classList.remove('hidden');
        bulkDiv.classList.add('flex');
    } else {
        bulkDiv.classList.add('hidden');
        bulkDiv.classList.remove('flex');
    }
}

async function executeBulkAction(action) {
    const selected = Array.from(document.querySelectorAll('.camera-checkbox:checked')).map(cb => cb.value);
    if (selected.length === 0) return;

    if (action === 'export') {
        window.location.href = `/api/streams/export?names=${encodeURIComponent(selected.join(','))}`;
        return;
    }

    if (!confirm(`Are you sure you want to ${action} ${selected.length} camera(s)?`)) return;

    try {
        const response = await fetch('/api/streams/bulk', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ action: action, names: selected })
        });
        if (response.ok) {
            document.getElementById('selectAllCameras').checked = false;
            updateBulkActions();
            loadStreams();
        } else {
            alert("Bulk action failed.");
        }
    } catch (e) {
        console.error(e);
        alert("Error executing bulk action.");
    }
}

// --- Theme & Metrics ---
function initTheme() {
    // Theme is already applied in <head> via inline script.
    // This function is kept for any future theme-related initialization.
    // The toggle is handled by toggleTheme() called via onclick on #themeToggleBtn.
}

async function fetchSysInfo() {
    try {
        const res = await fetch('/api/sysinfo');
        if (!res.ok) return;
        const data = await res.json();
        const update = (id, val) => { const el = document.getElementById(id); if(el) el.innerText = val; };
        update('stat-uptime', data.uptime);
        update('stat-appuptime', data.appUptime);
        update('stat-memused', Math.round(data.memUsedMB));
        update('stat-memtotal', Math.round(data.memTotalMB));
        update('stat-mempct', Math.round(data.memUsagePct) + '%');
        const mp = document.getElementById('stat-memprogress'); if(mp) mp.style.width = Math.round(data.memUsagePct) + '%';
        update('stat-cpupct', Math.round(data.cpuUsagePct) + '%');
        update('stat-cpuused', Math.round(data.cpuUsagePct) + '%');
        const cp = document.getElementById('stat-cpuprogress'); if(cp) cp.style.width = Math.round(data.cpuUsagePct) + '%';
        update('stat-streams', data.streamCount);
    } catch (e) {}
}

// --- Init ---
document.addEventListener('DOMContentLoaded', () => {
    initTheme();
    loadStreams();
    fetchSysInfo();
    initMaintenanceMap();
    setInterval(fetchSysInfo, 5000);
});

// --- Timelapse Logic ---
let tlIsInitialized = false;
let tlCurrentStream = '';
let tlFiles = [];
let tlIsPlaying = false;
let tlPlayIndex = 0;
let tlFps = 5;
let tlPlayInterval;
let tlDisplayedCount = 0;
const TL_BATCH_SIZE = 50;

async function initTimelapseView() {
    if (tlIsInitialized) return;

    // Set Default Dates
    const tlStartDateInput = document.getElementById('tlStartDate');
    const tlEndDateInput = document.getElementById('tlEndDate');
    const now = new Date();
    const yesterday = new Date(now);
    yesterday.setDate(now.getDate() - 1);
    
    const formatDateTime = (date) => {
        const pad = (num) => String(num).padStart(2, '0');
        return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
    };
    if (tlStartDateInput && !tlStartDateInput.value) tlStartDateInput.value = formatDateTime(yesterday);
    if (tlEndDateInput && !tlEndDateInput.value) tlEndDateInput.value = formatDateTime(now);

    [tlStartDateInput, tlEndDateInput].forEach(input => {
        if(input) input.addEventListener('change', () => {
            if (tlCurrentStream) loadTlFiles(tlCurrentStream);
        });
    });

    const streamSelect = document.getElementById('tlStreamSelect');
    if (streamSelect) {
        streamSelect.addEventListener('change', async (e) => {
            tlCurrentStream = e.target.value;
            loadTlFiles(tlCurrentStream);

            // Fetch specific timeline configuration seamlessly inline
            if (tlCurrentStream) {
                try {
                    const res = await fetch(`/api/timelapse/config?name=${encodeURIComponent(tlCurrentStream)}`);
                    if (res.ok) {
                        const config = await res.json();
                        const tlEnabledObj = document.getElementById("timelapseEnabled");
                        if (tlEnabledObj) {
                            tlEnabledObj.checked = config.enabled;
                            const presetSel = document.getElementById("timelapsePresetSelect");
                            
                            let matched = false;
                            Array.from(presetSel.options).forEach(opt => {
                                if (parseInt(opt.value) === config.interval) matched = true;
                            });
                            if (matched) {
                                presetSel.value = config.interval;
                                document.getElementById('timelapseCustomInput')?.classList.add('hidden');
                                document.getElementById('timelapseIntervalVal').value = 60;
                            } else {
                                presetSel.value = 'custom';
                                document.getElementById('timelapseCustomInput')?.classList.remove('hidden');
                                document.getElementById('timelapseCustomInput')?.classList.add('grid');
                                document.getElementById("timelapseIntervalVal").value = config.interval;
                                document.getElementById("timelapseIntervalUnit").value = "1";
                            }
                            if (config.width) document.getElementById("timelapseWidth").value = config.width;
                            if (config.height) document.getElementById("timelapseHeight").value = config.height;
                        }
                    }
                } catch(e) { console.error("Failed to load timelapse config", e); }
            }
        });
    }

    // Dynamic UI visibility for Custom Dropdown in Configuration Tool
    const tlPresetSel = document.getElementById("timelapsePresetSelect");
    if (tlPresetSel) {
        tlPresetSel.addEventListener('change', (e) => {
            const wrap = document.getElementById("timelapseCustomInput");
            if(e.target.value === 'custom') {
                wrap.classList.remove('hidden');
                wrap.classList.add('grid');
            } else {
                wrap.classList.add('hidden');
                wrap.classList.remove('grid');
            }
        });
    }

    // Save Setup Explicitly
    const configSaveBtn = document.getElementById("tlSaveConfigBtn");
    if (configSaveBtn) {
        configSaveBtn.addEventListener('click', async () => {
            if (!tlCurrentStream) {
                alert("Please select a valid monitor before saving.");
                return;
            }

            const btnOrgTxt = configSaveBtn.innerText;
            configSaveBtn.innerText = "Wait...";
            configSaveBtn.disabled = true;

            const tlEnabled = document.getElementById("timelapseEnabled").checked;
            let tlInterval = parseInt(document.getElementById("timelapsePresetSelect").value);
            if (isNaN(tlInterval) || document.getElementById("timelapsePresetSelect").value === 'custom') {
                const val = parseInt(document.getElementById("timelapseIntervalVal").value);
                const unit = parseInt(document.getElementById("timelapseIntervalUnit").value);
                if (!isNaN(val) && !isNaN(unit)) tlInterval = val * unit;
            }
            if (!tlInterval || tlInterval <= 0) tlInterval = 60;
            const tlWidth = parseInt(document.getElementById("timelapseWidth").value) || 1280;
            const tlHeight = parseInt(document.getElementById("timelapseHeight").value) || 720;

            try {
                const r = await fetch('/api/timelapse/config', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ name: tlCurrentStream, enabled: tlEnabled, interval: tlInterval, width: tlWidth, height: tlHeight })
                });

                if(r.ok) {
                    configSaveBtn.innerText = "Saved!";
                    setTimeout(() => { configSaveBtn.innerText = btnOrgTxt; configSaveBtn.disabled = false; }, 2000);
                } else {
                    throw new Error("Unable to parse config state");
                }
            } catch(e) { 
                console.error("Timelapse config explicit save failed", e); 
                alert("Failed saving Timelapse Configuration!");
                configSaveBtn.innerText = btnOrgTxt; 
                configSaveBtn.disabled = false; 
            }
        });
    }

    const exportBtn = document.getElementById('tlExportBtn');
    if (exportBtn) exportBtn.addEventListener('click', handleTlExport);

    const refreshBtn = document.getElementById('tlRefreshBtn');
    if (refreshBtn) refreshBtn.addEventListener('click', () => {
        if (tlCurrentStream) loadTlFiles(tlCurrentStream);
    });

    const fpsRange = document.getElementById('tlFpsRange');
    if (fpsRange) fpsRange.addEventListener('input', (e) => {
        tlFps = parseInt(e.target.value);
        document.getElementById('tlSpeedVal').textContent = `${tlFps} fps`;
        if (tlIsPlaying) {
            stopTlPlay();
            startTlPlay();
        }
    });

    const playBtn = document.getElementById('tlPlayBtn');
    if (playBtn) playBtn.addEventListener('click', () => {
        if (tlIsPlaying) stopTlPlay();
        else startTlPlay();
    });

    // Intersection Observer for Gallery
    const gallery = document.getElementById('tlGallery');
    if(gallery) {
        const observer = new IntersectionObserver((entries) => {
            if (entries[0].isIntersecting) {
                loadMoreTlSnapshots();
            }
        }, { root: gallery, threshold: 0.1 });
        const sentinel = document.createElement('div');
        sentinel.className = 'h-1 w-full col-span-3';
        gallery.appendChild(sentinel);
        observer.observe(sentinel);
    }

    try {
        const res = await fetch('/api/streams');
        const streams = await res.json();
        if (streamSelect) {
            streamSelect.innerHTML = '<option value="">Select Monitor</option>';
            streams.forEach(s => {
                const opt = document.createElement('option');
                opt.value = s.name;
                opt.textContent = s.name;
                streamSelect.appendChild(opt);
            });
        }
    } catch(e) {}

    tlIsInitialized = true;
}

async function loadTlFiles(name) {
    const mainImg = document.getElementById('tlMainImage');
    const emptyState = document.getElementById('tlEmptyState');
    const gallery = document.getElementById('tlGallery');

    if (!name) {
        if(gallery) gallery.innerHTML = '<div class="col-span-3 text-center text-slate-500 py-10 text-sm italic">Select a monitor</div>';
        if(mainImg) { mainImg.src = ''; mainImg.classList.add('hidden'); }
        if(emptyState) emptyState.classList.remove('hidden');
        document.getElementById('tlFileCount').textContent = '0';
        return;
    }

    document.getElementById('tlLoadingOverlay').classList.remove('hidden');
    try {
        const start = document.getElementById('tlStartDate').value;
        const end = document.getElementById('tlEndDate').value;
        const res = await fetch(`/api/timelapse/files?name=${encodeURIComponent(name)}&start=${start}&end=${end}`);
        if (!res.ok) throw new Error('Failed');

        tlFiles = await res.json() || [];
        tlFiles.sort();

        renderTlGallery();

        if (tlFiles.length > 0) {
            showTlImage(tlFiles.length - 1);
            if(mainImg) mainImg.classList.remove('hidden');
            if(emptyState) emptyState.classList.add('hidden');
        } else {
            if(mainImg) { mainImg.src = ''; mainImg.classList.add('hidden'); }
            if(emptyState) {
                emptyState.classList.remove('hidden');
                emptyState.querySelector('p').textContent = "No snapshots found for this monitor";
            }
        }
        document.getElementById('tlFileCount').textContent = tlFiles.length;
        const delAllBtn = document.getElementById('tlDeleteAllBtn');
        if (delAllBtn) {
            if (tlFiles.length > 0) delAllBtn.classList.remove('hidden');
            else delAllBtn.classList.add('hidden');
        }
    } catch (e) {
        if(gallery) gallery.innerHTML = '<div class="col-span-3 text-center text-red-500 py-10 text-sm">Failed to load files</div>';
    } finally {
        document.getElementById('tlLoadingOverlay').classList.add('hidden');
    }
}

function renderTlGallery() {
    const gallery = document.getElementById('tlGallery');
    if(!gallery) return;
    gallery.innerHTML = '';
    tlDisplayedCount = 0;
    loadMoreTlSnapshots();
}

function loadMoreTlSnapshots() {
    if (tlDisplayedCount >= tlFiles.length) return;
    const gallery = document.getElementById('tlGallery');
    const nextBatch = tlFiles.slice(tlDisplayedCount, tlDisplayedCount + TL_BATCH_SIZE);
    const fragment = document.createDocumentFragment();

    nextBatch.forEach((file, i) => {
        const index = tlDisplayedCount + i;
        const div = document.createElement('div');
        div.className = 'relative w-full h-0 pb-[56.25%] bg-slate-200 dark:bg-slate-700 rounded-lg overflow-hidden cursor-pointer group border-2 border-transparent hover:border-brand-500 transition-all';
        div.onclick = () => showTlImage(index);

        const parts = file.split('_');
        const timePart = parts[1]?.replace('.jpg', '').replace(/-/g, ':') || '';

        div.innerHTML = `
            <img src="/data/timelapse/${tlCurrentStream}/${file}" loading="lazy" class="absolute inset-0 w-full h-full object-cover opacity-90 group-hover:opacity-100 transition-opacity">
            <div class="absolute bottom-0 left-0 right-0 bg-black/60 p-1 text-[10px] text-white truncate font-mono z-10">
                ${timePart}
            </div>
            <button onclick="event.stopPropagation(); deleteTimelapse('single', '${file}')" class="absolute top-1 right-1 bg-red-600/80 hover:bg-red-600 text-white rounded p-1 opacity-0 group-hover:opacity-100 transition-opacity z-20" title="Delete Snapshot">
                <svg class="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" /></svg>
            </button>
        `;

        if (index === tlPlayIndex) {
            div.classList.add('border-brand-500', 'ring-2', 'ring-brand-500/30');
            div.classList.remove('border-transparent');
        }

        div.id = `tlThumb-${index}`;
        fragment.appendChild(div);
    });

    if(gallery) gallery.appendChild(fragment);
    tlDisplayedCount += nextBatch.length;
}

async function deleteTimelapse(mode, filename = '') {
    if (!tlCurrentStream) return;
    const msg = mode === 'all' 
        ? 'Are you sure you want to delete ALL snapshots for this monitor?' 
        : 'Are you sure you want to delete this snapshot?';
    if (!confirm(msg)) return;

    try {
        const url = `/api/timelapse/files/delete?name=${encodeURIComponent(tlCurrentStream)}&filename=${encodeURIComponent(mode === 'all' ? 'all' : filename)}`;
        const res = await fetch(url, { method: 'DELETE' });
        if (res.ok) {
            loadTimelapseFiles(tlCurrentStream);
        } else {
            const err = await res.text();
            alert("Delete failed: " + err);
        }
    } catch (e) {
        console.error("Delete failed", e);
    }
}

function showTlImage(index) {
    if (index < 0 || index >= tlFiles.length) return;

    const prevThumb = document.getElementById(`tlThumb-${tlPlayIndex}`);
    if (prevThumb) {
        prevThumb.classList.remove('border-brand-500', 'ring-2', 'ring-brand-500/30');
        prevThumb.classList.add('border-transparent');
    }

    tlPlayIndex = index;
    const file = tlFiles[index];
    const mainImg = document.getElementById('tlMainImage');
    if(mainImg) mainImg.src = `/data/timelapse/${tlCurrentStream}/${file}`;

    const parts = file.split('_');
    if (parts.length >= 2) {
        const datePart = parts[0];
        const timePart = parts[1].replace('.jpg', '').replace(/-/g, ':');
        document.getElementById('tlTimestampOverlay').textContent = `${datePart} ${timePart}`;
    }

    const newThumb = document.getElementById(`tlThumb-${tlPlayIndex}`);
    if (newThumb) {
        newThumb.classList.add('border-brand-500', 'ring-2', 'ring-brand-500/30');
        newThumb.classList.remove('border-transparent');
        newThumb.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
    }
}

function startTlPlay() {
    if (!tlFiles || tlFiles.length === 0) return;
    tlIsPlaying = true;
    const playBtn = document.getElementById('tlPlayBtn');
    if(playBtn) playBtn.innerHTML = '<svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10 9v6m4-6v6m7-3a9 9 0 11-18 0 9 9 0 0118 0z" /></svg> Pause';

    if (tlPlayIndex >= tlFiles.length - 1) tlPlayIndex = 0;

    tlPlayInterval = setInterval(() => {
        tlPlayIndex++;
        if (tlPlayIndex >= tlFiles.length) tlPlayIndex = 0;
        showTlImage(tlPlayIndex);
    }, 1000 / tlFps);
}

function stopTlPlay() {
    tlIsPlaying = false;
    clearInterval(tlPlayInterval);
    const playBtn = document.getElementById('tlPlayBtn');
    if(playBtn) playBtn.innerHTML = '<svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z" /></svg> Play';
}

async function handleTlExport() {
    if (!tlCurrentStream) return;
    const btn = document.getElementById('tlExportBtn');
    const originalText = btn.innerHTML;
    btn.disabled = true;
    btn.innerHTML = '<svg class="animate-spin h-4 w-4 text-white" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24"><circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle><path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path></svg> Processing...';

    try {
        const start = document.getElementById('tlStartDate').value;
        const end = document.getElementById('tlEndDate').value;
        const res = await fetch(`/api/timelapse/export?name=${encodeURIComponent(tlCurrentStream)}&start=${start}&end=${end}`);

        if (!res.ok) throw new Error("Export failed");

        const blob = await res.blob();
        const url = window.URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.style.display = 'none';
        a.href = url;
        a.download = `${tlCurrentStream}_timelapse.mp4`;
        document.body.appendChild(a);
        a.click();
        window.URL.revokeObjectURL(url);
    } catch (e) {
        alert("Export failed: " + e.message);
    } finally {
        btn.disabled = false;
        btn.innerHTML = originalText;
    }
}

// --- Maintenance Map logic ---
async function initMaintenanceMap() {
    const mapContainer = document.getElementById('maintenanceMap');
    if (!mapContainer) return;

    // Ensure we have streams data
    if (allStreams.length === 0) {
        await loadStreams();
    }

    if (!maintenanceMap) {
        // Base Layers
        const osm = L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
            attribution: '&copy; OpenStreetMap'
        });
        
        const satellite = L.tileLayer('https://server.arcgisonline.com/ArcGIS/rest/services/World_Imagery/MapServer/tile/{z}/{y}/{x}', {
            attribution: 'Tiles &copy; Esri &mdash; Source: Esri, i-cubed, USDA, USGS, AEX, GeoEye, Getmapping, Aerogrid, IGN, IGP, UPR-EBP, and the GIS User Community'
        });

        maintenanceMap = L.map('maintenanceMap', {
            center: [-7.250445, 112.768845],
            zoom: 11,
            layers: [osm] // Default
        });

        const baseMaps = {
            "Road": osm,
            "Satellite": satellite
        };

        L.control.layers(baseMaps).addTo(maintenanceMap);

        // Map Click: If a camera is selected from dropdown but doesn't have a marker, set it here
        maintenanceMap.on('click', async (e) => {
            const dashSelect = document.getElementById('dashboardCameraSelect');
            if (dashSelect && dashSelect.value) {
                const name = dashSelect.value;
                // If marker doesn't exist, we can create it
                if (!maintenanceMarkers[name]) {
                    const { lat, lng } = e.latlng;
                    await updateCameraLocation(name, lat, lng);
                    initMaintenanceMap(); // Re-init to show new marker
                    setTimeout(() => selectDashboardCamera(name), 500);
                }
            }
        });
    }

    // Clear existing markers
    for (let id in maintenanceMarkers) {
        maintenanceMap.removeLayer(maintenanceMarkers[id]);
    }
    maintenanceMarkers = {};

    const dashSelect = document.getElementById('dashboardCameraSelect');
    if (dashSelect) {
        dashSelect.innerHTML = '<option value="">-- Select Camera --</option>';
    }

    const bounds = [];
    allStreams.forEach(s => {
        if (s.lat && s.lng) {
            const roundIcon = L.divIcon({
                className: 'round-marker',
                iconSize: [14, 14],
                iconAnchor: [7, 7]
            });

            const marker = L.marker([s.lat, s.lng], {
                draggable: true,
                title: s.name,
                icon: roundIcon
            }).addTo(maintenanceMap);

            const updatePopup = (lat, lng) => {
                marker.bindPopup(`
                    <div class="p-1">
                        <div class="text-sm font-bold border-b border-slate-100 dark:border-slate-700 pb-1 mb-1">${s.name}</div>
                        <div class="text-[10px] text-slate-500 dark:text-slate-400 mb-1 font-medium italic">Coordinate updated via drag</div>
                        <div class="text-[10px] font-mono bg-slate-100 dark:bg-slate-900/50 p-1.5 rounded border border-slate-200 dark:border-slate-700 leading-relaxed shadow-sm">
                            <span class="text-indigo-600 dark:text-indigo-400 font-bold">LAT:</span> ${lat.toFixed(6)}<br>
                            <span class="text-indigo-600 dark:text-indigo-400 font-bold">LNG:</span> ${lng.toFixed(6)}
                        </div>
                    </div>
                `);
            };

            updatePopup(s.lat, s.lng);

            // Marker Click - Show Preview
            marker.on('click', () => {
                selectCameraOnMap(s.name);
            });

            marker.on('dragend', async (event) => {
                const position = marker.getLatLng();
                updatePopup(position.lat, position.lng);
                marker.openPopup();
                await updateCameraLocation(s.name, position.lat, position.lng);
            });

            maintenanceMarkers[s.name] = marker;
            bounds.push([s.lat, s.lng]);
        }
        
        // Add to dropdown even if no coordinates
        if (dashSelect) {
            const opt = document.createElement('option');
            opt.value = s.name;
            const status = !s.lat || !s.lng ? ' (No Marker - Click map to place)' : '';
            opt.textContent = `${s.name}${status}`;
            dashSelect.appendChild(opt);
        }
    });

    // Handle Popup Close - Reset Preview
    // We listen on the map itself to catch all ways a popup can close (X button, background click, etc)
    maintenanceMap.off('popupclose'); // Prevent multiple listeners
    maintenanceMap.on('popupclose', () => {
        // Small delay to check if another popup is about to open (e.g. switching markers)
        // Actually, just deselecting is fine as selectCameraOnMap will run immediately after if a marker was clicked
        setTimeout(() => {
            if (!maintenanceMap.hasLayer(maintenanceMap._popup)) {
                deselectCameraOnMap();
            }
        }, 50);
    });

    if (bounds.length > 0) {
        maintenanceMap.fitBounds(bounds, { padding: [20, 20] });
    } else {
        // Fallback to Current Location if no markers
        maintenanceMap.locate({ setView: true, maxZoom: 14 });
        maintenanceMap.on('locationerror', () => {
            maintenanceMap.setView([-6.2000, 106.8166], 12); // Fallback to Jakarta
        });
    }

    // Force resize to fix gray tile issue
    setTimeout(() => maintenanceMap.invalidateSize(), 300);
}

function selectCameraOnMap(name) {
    const stream = allStreams.find(s => s.name === name);
    if (!stream) return;

    // Remove highlight from previous
    if (selectedCameraOnMap && maintenanceMarkers[selectedCameraOnMap]) {
        maintenanceMarkers[selectedCameraOnMap].getElement()?.classList.remove('selected-marker');
    }

    selectedCameraOnMap = name;
    
    // Update select dropdown
    const dashSelect = document.getElementById('dashboardCameraSelect');
    if (dashSelect) {
        dashSelect.value = name;
    }
    
    // Highlight current
    if (maintenanceMarkers[name]) {
        maintenanceMarkers[name].getElement()?.classList.add('selected-marker');
    }
    
    // UI Updates
    document.getElementById('noCameraSelected').classList.add('hidden');
    document.getElementById('previewPlayerArea').classList.remove('hidden');
    document.getElementById('previewStatus').classList.remove('hidden');
    document.getElementById('previewCamName').textContent = name;

    reloadDashboardPreview('mse');
}

function selectDashboardCamera(name) {
    if (!name) {
        deselectCameraOnMap();
        return;
    }
    
    // Check if the marker exists
    if (maintenanceMarkers[name]) {
        const marker = maintenanceMarkers[name];
        maintenanceMap.flyTo(marker.getLatLng(), 14, { duration: 1.5 });
        marker.openPopup();
    }
    
    selectCameraOnMap(name);
}

function deselectCameraOnMap() {
    if (selectedCameraOnMap && maintenanceMarkers[selectedCameraOnMap]) {
        maintenanceMarkers[selectedCameraOnMap].getElement()?.classList.remove('selected-marker');
    }
    selectedCameraOnMap = null;
    
    // UI Reset
    document.getElementById('noCameraSelected').classList.remove('hidden');
    document.getElementById('previewPlayerArea').classList.add('hidden');
    document.getElementById('previewStatus').classList.add('hidden');
    const player = document.getElementById('dashboardPlayer');
    if (player) player.innerHTML = '';
}

function reloadDashboardPreview(mode) {
    if (!selectedCameraOnMap) return;
    const name = selectedCameraOnMap;
    
    const container = document.getElementById('dashboardPlayer');
    if (!container) return;
    container.innerHTML = '';

    const protocol = window.location.protocol;
    const hostname = window.location.hostname;
    const port = window.location.port ? `:${window.location.port}` : "";
    const go2rtcProxy = `${protocol}//${hostname}${port}/rtc`;
    const modeParam = mode || 'mse,webrtc,hls,mp4,mjpeg';

    const iframe = document.createElement('iframe');
    iframe.src = `${go2rtcProxy}/stream.html?src=${encodeURIComponent(name)}&mode=${modeParam}`;
    iframe.className = "w-full h-full border-none";
    iframe.allow = "autoplay; fullscreen; picture-in-picture";

    container.appendChild(iframe);
}

async function updateCameraLocation(name, lat, lng) {
    const stream = allStreams.find(s => s.name === name);
    if (!stream) return;

    const payload = {
        name: stream.name,
        originalName: name, // Required by backend to identify the row
        url: stream.url,
        backend: stream.backend || 'go2rtc',
        lat: lat,
        lng: lng,
        enabled: stream.enabled !== false
    };

    try {
        const response = await fetch(`/api/streams?name=${encodeURIComponent(name)}`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload)
        });

        if (response.ok) {
            // Update local state
            stream.lat = lat;
            stream.lng = lng;
            
            // Show subtle feedback
            const toast = document.createElement('div');
            toast.className = 'fixed bottom-10 left-1/2 -translate-x-1/2 z-[100] bg-green-600 text-white px-6 py-3 rounded-2xl shadow-2xl text-sm font-bold animate-bounce';
            toast.textContent = `Location updated for ${name}`;
            document.body.appendChild(toast);
            setTimeout(() => toast.remove(), 3000);
            
            // Refresh table if in cameras view
            if (currentView === 'cameras') renderStreamsTable();
        } else {
            alert("Failed to update location");
        }
    } catch (e) {
        console.error("Error updating location", e);
    }
}

// ===== Camera Live Preview Modal (Manage Camera) =====
function openCameraPreviewModal(name) {
    const modal = document.getElementById('cameraPreviewModal');
    const content = document.getElementById('cameraPreviewContent');
    const player = document.getElementById('cameraPreviewPlayer');
    const title = document.getElementById('cameraPreviewTitle');
    if (!modal || !player) return;

    title.textContent = name;

    // Loading spinner
    player.innerHTML = `
        <div class="absolute inset-0 flex flex-col items-center justify-center bg-gray-950 text-white">
            <div class="animate-spin rounded-full h-12 w-12 border-4 border-brand-500 border-t-transparent mb-4 shadow-[0_0_15px_rgba(99,102,241,0.5)]"></div>
            <span class="font-medium text-sm animate-pulse tracking-wide text-brand-200">Connecting to feed...</span>
        </div>`;

    // Inject stream iframe
    // Default to MSE for better compatibility with H.265
    const iframeSrc = `${window.location.protocol}//${window.location.host}/rtc/stream.html?src=${encodeURIComponent(name)}&mode=mse,webrtc,hls,mp4,mjpeg`;
    const iframe = document.createElement('iframe');
    iframe.src = iframeSrc;
    iframe.style.cssText = 'width:100%;height:100%;border:none;position:absolute;top:0;left:0;z-index:20;';
    iframe.allow = 'autoplay; fullscreen; picture-in-picture';
    player.appendChild(iframe);

    // Show modal with animation
    modal.classList.remove('opacity-0', 'pointer-events-none');
    requestAnimationFrame(() => {
        content.classList.remove('scale-95');
        content.classList.add('scale-100');
    });

    document.addEventListener('keydown', _previewEscHandler);
}

function _previewEscHandler(e) {
    if (e.key === 'Escape') closeCameraPreviewModal();
}

function closeCameraPreviewModal() {
    const modal = document.getElementById('cameraPreviewModal');
    const content = document.getElementById('cameraPreviewContent');
    const player = document.getElementById('cameraPreviewPlayer');
    if (!modal) return;

    content.classList.remove('scale-100');
    content.classList.add('scale-95');
    modal.classList.add('opacity-0', 'pointer-events-none');

    // Clear iframe to stop the stream
    setTimeout(() => { if (player) player.innerHTML = ''; }, 300);
    document.removeEventListener('keydown', _previewEscHandler);
}

// ===== Command Center Map Logic =====
let globalCameraMap = null;
let commandCenterMarkers = {};

function initCommandCenterMap() {
    if (globalCameraMap) {
        setTimeout(() => {
            globalCameraMap.invalidateSize();
            renderCommandCenterMarkers();
        }, 300);
        return;
    }
    
    // Default coordinates (Indonesia center)
    globalCameraMap = L.map('globalCameraMap', {
        zoomControl: false,
        attributionControl: false
    }).setView([-2.5489, 118.0149], 5);

    L.control.zoom({ position: 'bottomright' }).addTo(globalCameraMap);

    const darkLayer = L.tileLayer('https://{s}.basemaps.cartocdn.com/dark_all/{z}/{x}/{y}{r}.png', { maxZoom: 19, subdomains: 'abcd' });
    const lightLayer = L.tileLayer('https://{s}.basemaps.cartocdn.com/light_all/{z}/{x}/{y}{r}.png', { maxZoom: 19, subdomains: 'abcd' });
    const openMapsLayer = L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png', { maxZoom: 19 });
    const satelliteLayer = L.tileLayer('https://server.arcgisonline.com/ArcGIS/rest/services/World_Imagery/MapServer/tile/{z}/{y}/{x}', { maxZoom: 19 });

    window.mapLayers = {
        'Dark': darkLayer,
        'Light': lightLayer,
        'OpenMaps': openMapsLayer,
        'Satellite': satelliteLayer
    };

    const isDarkMode = document.documentElement.classList.contains('dark');
    if (isDarkMode) {
        darkLayer.addTo(globalCameraMap);
        document.getElementById('layeritem-Dark')?.classList.add('active-layer');
    } else {
        lightLayer.addTo(globalCameraMap);
        document.getElementById('layeritem-Light')?.classList.add('active-layer');
    }
    
    window.addEventListener('resize', () => {
        if (globalCameraMap) globalCameraMap.invalidateSize();
    });

    setTimeout(() => {
        globalCameraMap.invalidateSize();
        renderCommandCenterMarkers();
    }, 300);
}

function renderCommandCenterMarkers() {
    if (!globalCameraMap) return;

    // Clear existing
    Object.values(commandCenterMarkers).forEach(m => globalCameraMap.removeLayer(m));
    commandCenterMarkers = {};
    const bounds = [];

    allStreams.forEach(s => {
        if (s.enabled !== false && s.lat && s.lng) {
            const roundIcon = L.divIcon({
                className: 'round-marker',
                iconSize: [14, 14],
                iconAnchor: [7, 7]
            });

            const marker = L.marker([s.lat, s.lng], { icon: roundIcon }).addTo(globalCameraMap);
            
            const host = window.location.host;
            const iframeSrc = `${window.location.protocol}//${host}/rtc/stream.html?src=${encodeURIComponent(s.name)}&mode=mse,webrtc,hls,mp4,mjpeg`;
            
            const popupContent = `
                <div class="w-72 sm:w-80 -m-4 overflow-hidden rounded-xl bg-white dark:bg-slate-900 shadow-2xl flex flex-col">
                    <div class="flex justify-between items-center px-4 py-3 bg-slate-50 dark:bg-slate-800/80 border-b border-slate-100 dark:border-slate-800">
                        <h3 class="font-bold text-slate-800 dark:text-white truncate pr-2 text-sm max-w-[80%]">${s.name}</h3>
                        ${s.online ? '<span class="flex h-2 w-2 rounded-full bg-green-500 shadow-[0_0_8px_rgba(34,197,94,0.6)]"></span>' : '<span class="flex h-2 w-2 rounded-full bg-slate-400 dark:bg-slate-500"></span>'}
                    </div>
                    <div class="bg-black w-full aspect-video relative group">
                        <iframe src="${iframeSrc}" class="w-full h-full border-none pointer-events-auto" allow="autoplay; fullscreen; picture-in-picture"></iframe>
                        <div class="absolute inset-0 border border-white/10 rounded-b-xl pointer-events-none"></div>
                    </div>
                </div>`;

            marker.bindPopup(popupContent, {
                maxWidth: 400,
                minWidth: 280,
                className: 'custom-popup-container',
                closeButton: true,
                autoPanPadding: [50, 50]
            });

            commandCenterMarkers[s.name] = marker;
            bounds.push([s.lat, s.lng]);
        }
    });

    if (bounds.length > 0) {
        globalCameraMap.fitBounds(bounds, { padding: [30, 30], maxZoom: 16 });
    }
}

function debounceMapSearch(query) {
    clearTimeout(window.mapSearchTimer);
    window.mapSearchTimer = setTimeout(() => {
        const q = String(query).toLowerCase().trim();
        let found = false;
        
        Object.keys(commandCenterMarkers).forEach(name => {
            const marker = commandCenterMarkers[name];
            
            // Close all popups first
            marker.closePopup();

            if (q !== '' && name.toLowerCase().includes(q)) {
                if (!found) {
                    globalCameraMap.flyTo(marker.getLatLng(), 15, { duration: 1.5 });
                    setTimeout(() => marker.openPopup(), 1500);
                    found = true;
                }
            }
        });
    }, 500);
}

function changeGlobalMapLayer(layerName) {
    if (!globalCameraMap || !window.mapLayers) return;
    
    // Remove all
    Object.values(window.mapLayers).forEach(layer => {
        if (globalCameraMap.hasLayer(layer)) {
            globalCameraMap.removeLayer(layer);
        }
    });
    
    // Add selected
    window.mapLayers[layerName].addTo(globalCameraMap);
    
    // Update UI
    document.querySelectorAll('#mapStyleButtons .layer-item').forEach(btn => {
        btn.classList.remove('active-layer');
    });
    document.getElementById(`layeritem-${layerName}`)?.classList.add('active-layer');
}

function toggleSidePanel() {
    const panel = document.getElementById('mapSidePanel');
    if (panel) {
        if (panel.classList.contains('-translate-x-full')) {
            panel.classList.remove('-translate-x-full');
            panel.classList.add('translate-x-0');
        } else {
            panel.classList.remove('translate-x-0');
            panel.classList.add('-translate-x-full');
        }
    }
}

// Command Center Grid View Logic
let ccCurrentPage = 1;
const ccItemsPerPage = 12;

function switchCCTab(tab) {
    const mapBtn = document.getElementById('btnCCMap');
    const gridBtn = document.getElementById('btnCCGrid');
    
    if (tab === 'grid') {
        document.getElementById('commandcenter-map-container').classList.add('hidden');
        document.getElementById('commandcenter-grid-container').classList.remove('hidden');
        
        // Update styling
        mapBtn.className = "px-3 py-1.5 rounded-lg text-sm font-semibold text-slate-500 hover:text-brand-600 hover:bg-slate-50 dark:hover:bg-slate-800 transition-colors";
        gridBtn.className = "px-3 py-1.5 rounded-lg text-sm font-semibold bg-brand-50 dark:bg-brand-900/20 text-brand-600 dark:text-brand-400 transition-colors";
        
        renderCCGridPage(ccCurrentPage);
    } else {
        document.getElementById('commandcenter-grid-container').classList.add('hidden');
        document.getElementById('commandcenter-map-container').classList.remove('hidden');
        
        // Update styling
        gridBtn.className = "px-3 py-1.5 rounded-lg text-sm font-semibold text-slate-500 hover:text-brand-600 hover:bg-slate-50 dark:hover:bg-slate-800 transition-colors";
        mapBtn.className = "px-3 py-1.5 rounded-lg text-sm font-semibold bg-brand-50 dark:bg-brand-900/20 text-brand-600 dark:text-brand-400 transition-colors";
        
        if (globalCameraMap) globalCameraMap.invalidateSize();
    }
}

function renderCCGridPage(page) {
    const container = document.getElementById('ccGridList');
    if(!container) return;
    
    const startIndex = (page - 1) * ccItemsPerPage;
    const endIndex = startIndex + ccItemsPerPage;
    const streamsToShow = allStreams.slice(startIndex, endIndex);
    const totalPages = Math.ceil(allStreams.length / ccItemsPerPage) || 1;

    container.innerHTML = '';

    if (streamsToShow.length === 0) {
        container.innerHTML = `
            <div class="col-span-full py-16 flex flex-col items-center justify-center text-center bg-white/50 dark:bg-slate-900/50 rounded-3xl border border-dashed border-slate-300 dark:border-slate-700">
                <p class="text-slate-500">No cameras available</p>
            </div>`;
    } else {
        streamsToShow.forEach(stream => {
            const card = document.createElement('div');
            card.className = "bg-white dark:bg-slate-900 rounded-2xl overflow-hidden shadow-lg border border-slate-100 dark:border-slate-800 transition-all duration-300 group cursor-pointer";
            card.onclick = () => openCameraPreviewModal(stream.name);
            card.innerHTML = `
                <div class="p-4 flex justify-between items-center border-b border-slate-100 dark:border-slate-800">
                    <h3 class="font-bold text-[15px] truncate text-slate-800 dark:text-white" title="${stream.name}">${stream.name}</h3>
                    <div class="flex items-center gap-1">
                        <button onclick="event.stopPropagation(); openShareModal('${stream.name.replace(/'/g, "\\'")}')" class="p-1.5 text-slate-400 hover:text-emerald-500 rounded-lg transition-colors shrink-0 z-10" title="Share Camera">
                            <svg class="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8.684 13.342C8.886 12.938 9 12.482 9 12c0-.482-.114-.938-.316-1.342m0 2.684a3 3 0 110-2.684m0 2.684l6.632 3.316m-6.632-6l6.632-3.316m0 0a3 3 0 105.367-2.684 3 3 0 00-5.367 2.684zm0 9.316a3 3 0 105.368 2.684 3 3 0 00-5.368-2.684z" /></svg>
                        </button>
                        <button onclick="event.stopPropagation(); goToMapMarker('${stream.name.replace(/'/g, "\\'")}')" class="p-1.5 text-slate-400 hover:text-brand-600 rounded-lg transition-colors shrink-0 z-10" title="View location on map">
                            <svg class="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M17.657 16.657L13.414 20.9a1.998 1.998 0 01-2.827 0l-4.244-4.243a8 8 0 1111.314 0z"/><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 11a3 3 0 11-6 0 3 3 0 016 0z"/></svg>
                        </button>
                    </div>
                </div>
                <div class="relative w-full bg-black aspect-video group/video">
                    <div class="absolute inset-0 w-full h-full flex items-center justify-center bg-cover bg-center cursor-pointer group-hover/video:scale-[1.02] transition-transform" style="background-image: url('/api/snapshot?name=${encodeURIComponent(stream.name)}');">
                        <div class="bg-black/40 absolute inset-0 text-white/10 flex items-center justify-center">
                            <svg class="w-16 h-16 opacity-10" fill="currentColor" viewBox="0 0 20 20"><path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zM9.555 7.168A1 1 0 008 8v4a1 1 0 001.555.832l3-2a1 1 0 000-1.664l-3-2z" clip-rule="evenodd" /></svg>
                        </div>
                    </div>
                </div>`;
            container.appendChild(card);
        });
    }

    document.getElementById('ccPageIndicator').innerText = `Page ${page} of ${totalPages}`;
    document.getElementById('btnCCPrev').disabled = (page === 1);
    document.getElementById('btnCCNext').disabled = (page === totalPages || totalPages === 0);
}

function changeCCPage(delta) {
    const totalPages = Math.ceil(allStreams.length / ccItemsPerPage) || 1;
    ccCurrentPage += delta;
    if (ccCurrentPage < 1) ccCurrentPage = 1;
    if (ccCurrentPage > totalPages) ccCurrentPage = totalPages;
    renderCCGridPage(ccCurrentPage);
    document.getElementById('commandcenter-grid-container').scrollTo({ top: 0, behavior: 'smooth' });
}

function goToMapMarker(name) {
    switchCCTab('map');
    const marker = commandCenterMarkers[name];
    if (marker) {
        globalCameraMap.flyTo(marker.getLatLng(), 16, { duration: 1.5 });
        setTimeout(() => marker.openPopup(), 1600);
    } else {
        alert("Location data not available for this camera.");
    }
}

function openShareModal(manualName) {
    // Check which view we are in to get the correct selected camera
    let name = manualName || "";
    
    if (!name) {
        if (selectedCameraOnMap) {
            name = selectedCameraOnMap;
        } else {
            const previewTitle = document.getElementById('cameraPreviewTitle');
            if (previewTitle && previewTitle.textContent !== "Camera Name") {
                name = previewTitle.textContent.trim();
            }
        }
    }

    if (!name || name === "--" || name === "Camera Name") {
        alert("Please select a camera first.");
        return;
    }
    
    const modal = document.getElementById('shareModal');
    const nameSpan = document.getElementById('shareStreamName');
    const urlInput = document.getElementById('shareUrl');
    const iframeText = document.getElementById('shareIframe');
    
    if (!modal || !urlInput || !iframeText) return;
    
    nameSpan.textContent = name;
    
    // Construct URLs
    const protocol = window.location.protocol;
    const hostname = window.location.hostname;
    const port = window.location.port ? `:${window.location.port}` : "";
    const baseUrl = `${protocol}//${hostname}${port}`;
    
    // We point to the proxied stream.html
    const streamUrl = `${baseUrl}/rtc/stream.html?src=${encodeURIComponent(name)}&mode=webrtc,mse`;
    const embedCode = `<iframe src="${streamUrl}" width="100%" height="450" frameborder="0" allowfullscreen allow="autoplay; fullscreen; picture-in-picture"></iframe>`;
    
    urlInput.value = streamUrl;
    iframeText.value = embedCode;
    
    modal.classList.remove('hidden');
}
