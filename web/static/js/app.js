// Global State - v55 (NVR footer controls + sidebar auto-hide)
let currentView = 'dashboard';
let maintenanceMap = null;
let maintenanceMarkers = {};
let maintenanceFirstLoad = true;
let selectedCameraOnMap = null;
let markersLocked = true;

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
        const ccTabs = document.getElementById('ccTabControlsHeader');
        if (ccTabs) ccTabs.classList.remove('hidden');
        initCommandCenterMap();
    } else {
        const ccTabs = document.getElementById('ccTabControlsHeader');
        if (ccTabs) ccTabs.classList.add('hidden');
    }

    if (viewName === 'testlogs') {
        loadTestLogs();
    }

    if (viewName === 'waitinglist') {
        loadWaitingList();
    }

    if (viewName === 'nvr') {
        initNVRView();
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

function applySubscriptionRestrictions() {
    const plan = window.CURRENT_PLAN || 'Free';
    const role = window.CURRENT_ROLE || 'user';
    
    // Admins have no restrictions
    if (role === 'admin') return;

    // 1. Sidemenu Restrictions
    const dashboardLink = document.querySelector('.nav-link[data-view="dashboard"]');
    const ccLink = document.querySelector('.nav-link[data-view="commandcenter"]');
    const publicViewLink = document.querySelector('.nav-link[data-view="publicview"]');
    
    if (publicViewLink) {
        publicViewLink.classList.toggle('hidden', plan === 'Free' || plan === 'Basic' || plan === 'Premium');
    }

    if (plan === 'Free' || plan === 'Basic' || plan === 'Premium') {
        if (dashboardLink) dashboardLink.classList.add('hidden');
        if (ccLink) ccLink.classList.add('hidden');
        // If they are on a hidden view, move them to cameras
        if (currentView === 'dashboard' || currentView === 'commandcenter') {
            switchView('cameras');
        }
    } else {
        if (dashboardLink) dashboardLink.classList.remove('hidden');
        if (ccLink) ccLink.classList.remove('hidden');
    }

    // 2. Manage Camera Restrictions (Import/Export)
    const exportBtn = document.querySelector('button[onclick*="export"]');
    const importBtn = document.querySelector('button[onclick="openCSVImportModal()"]');
    if (plan !== 'Enterprise') {
        if (exportBtn) exportBtn.classList.add('hidden');
        if (importBtn) importBtn.classList.add('hidden');
    }

    // 3. Add Camera Button Visibility
    const addCamBtn = document.querySelector('button[onclick="openAddModal()"]');
    if (addCamBtn) {
        let limit = 2;
        if (plan === 'Basic') limit = 4;
        if (plan === 'Premium') limit = 8;
        if (plan === 'Advance') limit = 16;
        if (plan === 'Enterprise') limit = 9999;
        
        if (allStreams.length >= limit) {
            addCamBtn.classList.add('hidden');
        } else {
            addCamBtn.classList.remove('hidden');
        }
    }
}

// Ensure correct view on initial page load based on pathname
document.addEventListener('DOMContentLoaded', () => {
    applySubscriptionRestrictions();
    if (window.location.pathname === '/commandcenter') {
        switchView('commandcenter');
    } else {
        switchView('dashboard');
    }

    // Auto-collapse CC Map Sidebar when clicking outside
    document.addEventListener('mousedown', (e) => {
        const panel = document.getElementById('mapSidePanel');
        const toggleBtn = document.getElementById('ccSidePanelToggleBtn');
        if (panel && panel.classList.contains('translate-x-0')) {
            if (!panel.contains(e.target) && (!toggleBtn || !toggleBtn.contains(e.target))) {
                toggleSidePanel();
            }
        }

        // Auto-close search results dropdown
        const searchResults = document.getElementById('ccMapSearchResults');
        const searchInput = document.getElementById('mapSearchInput');
        if (searchResults && !searchResults.classList.contains('hidden')) {
            if (!searchResults.contains(e.target) && e.target !== searchInput) {
                searchResults.classList.add('hidden');
            }
        }
    });
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
        
        // Also refresh Command Center map markers if we are in that view
        if (currentView === 'commandcenter' && globalCameraMap) {
            renderCommandCenterMarkers();
        }
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
                gridContainer.appendChild(createStreamCard(s.name, s.url, s.display_name || s.name));
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
                    <!-- REPLACED LIGHTNING WITH COPY LINK -->
                    <button onclick="copyToClipboard('${window.location.origin}/rtc/stream.html?src=${encodeURIComponent(s.name)}&mode=mse,webrtc,hls,mp4,mjpeg')" class="p-2 bg-emerald-50 dark:bg-emerald-900/30 text-emerald-500 rounded-lg hover:bg-emerald-100 transition-colors shadow-sm" title="Copy Processed Stream URL">
                        <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 7v8a2 2 0 002 2h6M8 7V5a2 2 0 012-2h4.586a1 1 0 01.707.293l4.414 4.414a1 1 0 01.293.707V15a2 2 0 01-2 2h-2M8 7H6a2 2 0 00-2 2v10a2 2 0 002 2h8a2 2 0 002-2v-2" /></svg>
                    </button>
                </div>
            </td>

            <td class="block md:table-cell md:px-6 py-2 md:py-4">
                <div class="text-base md:text-sm font-bold text-slate-900 dark:text-white uppercase mb-1 md:mb-0">${s.display_name || s.name}</div>
                <div class="text-[11px] md:text-[10px] text-slate-500 font-mono flex items-center gap-1 mb-4 md:mb-0 truncate max-w-full">
                    <span class="text-slate-400">ID:</span> ${s.name.replace(/[^a-zA-Z0-9-]/g,'').substring(0,8) || s.name} <span class="text-slate-400">(${s.backend || 'HLS'})</span>
                    ${s.resolution ? `<span class="ml-2 font-bold text-indigo-500 dark:text-indigo-400 px-1.5 py-0.5 bg-indigo-50 dark:bg-indigo-900/30 rounded">${s.resolution}</span>` : ''}
                </div>
                
                <div class="md:hidden flex gap-2 w-full mt-3">
                    <div class="flex-1 bg-slate-50 dark:bg-slate-800 p-2.5 rounded border border-slate-100 dark:border-slate-700">
                        <p class="text-[9px] font-bold text-slate-400 mb-1 uppercase tracking-wider">Method</p>
                        <p class="text-xs font-bold text-slate-700 dark:text-slate-300 uppercase">${s.type === 'ffmpeg' ? 'FFmpeg' : 'Direct'}</p>
                    </div>
                    <div class="flex-1 bg-slate-50 dark:bg-slate-800 p-2.5 rounded border border-slate-100 dark:border-slate-700 overflow-hidden">
                        <p class="text-[9px] font-bold text-slate-400 mb-1 uppercase tracking-wider">Source</p>
                        <p class="text-xs font-bold text-slate-700 dark:text-slate-300 truncate" title="${s.url}">${s.url}</p>
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
            <td class="hidden md:table-cell px-6 py-4 max-w-[200px] col-source-url">
                <div class="text-[10px] font-mono text-slate-500 dark:text-slate-400 truncate bg-slate-50 dark:bg-slate-800/50 px-2 py-1 rounded inline-block w-full" title="${s.url}">${s.url}</div>
            </td>
            
            <td class="hidden md:table-cell px-6 py-4 text-center">
                <button onclick="copyToClipboard('${window.location.origin}/rtc/stream.html?src=${encodeURIComponent(s.name)}&mode=mse,webrtc,hls,mp4,mjpeg')" class="p-1.5 text-slate-400 hover:text-emerald-500 transition-colors" title="Copy Processed Stream URL">
                    <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 7v8a2 2 0 002 2h6M8 7V5a2 2 0 012-2h4.586a1 1 0 01.707.293l4.414 4.414a1 1 0 01.293.707V15a2 2 0 01-2 2h-2M8 7H6a2 2 0 00-2 2v10a2 2 0 002 2h8a2 2 0 002-2v-2" /></svg>
                </button>
            </td>

            <td class="block md:table-cell md:px-6 py-3 mt-4 pt-4 md:mt-0 md:pt-3 border-t md:border-none border-slate-100 dark:border-slate-800">
                <div class="flex justify-end gap-2 md:gap-1 w-full flex-wrap">
                    <button onclick="openCameraPreviewModal('${escapeJS(s.name)}', '${escapeJS(s.display_name || s.name)}')" class="flex-1 md:flex-none justify-center flex items-center gap-1 md:p-1.5 p-2 bg-blue-50 dark:bg-slate-800 md:bg-transparent text-blue-600 md:text-slate-400 hover:text-blue-600 rounded-lg transition-colors" title="Live Preview">
                        <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z"/><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z"/></svg>
                        <span class="md:hidden text-[10px] font-bold uppercase">Preview</span>
                    </button>
                    <button onclick="openEditModal('${escapeJS(s.name)}', '${escapeJS(s.url)}', '${escapeJS(s.display_name || s.name)}')" class="flex-1 md:flex-none justify-center flex items-center gap-1 md:p-1.5 p-2 bg-indigo-50 dark:bg-slate-800 md:bg-transparent text-indigo-600 md:text-slate-400 hover:text-indigo-600 rounded-lg transition-colors" title="Edit">
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

    // Reset selection state on page change to avoid count mismatches
    const selectAllCheck = document.getElementById('selectAllCameras');
    if (selectAllCheck) selectAllCheck.checked = false;
    updateBulkActions();
}

function createStreamCard(name, url, displayName = name) {
    const card = document.createElement('div');
    card.className = 'card bg-white dark:bg-slate-900 rounded-2xl overflow-hidden shadow-xl border border-slate-100 dark:border-slate-800 group';
    card.dataset.name = name;
    card.dataset.url = url;

    card.innerHTML = `
        <div class="p-4 flex justify-between items-center border-b border-slate-100 dark:border-slate-800">
            <h3 class="font-bold text-sm truncate text-slate-800 dark:text-white" title="${displayName}">${displayName}</h3>
            <button onclick="takeSnapshot('${name}', '${escapeJS(displayName)}')" class="p-1 rounded text-slate-400 hover:text-brand-600 transition-colors">
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
                .info { display: none !important; }
                .mode { display: none !important; }
                .retry { display: none !important; }
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
            style.textContent = `
                video { object-fit: fill !important; width:100%; height:100%; } 
                body { background: black; margin: 0; overflow: hidden; }
                .mode { display: none !important; }
                .retry { display: none !important; }
            `;
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
        tr.className = 'block md:table-row bg-white dark:bg-slate-900 md:bg-transparent rounded-2xl md:rounded-none border border-slate-200 dark:border-slate-800 md:border-none md:border-b mb-4 p-5 md:p-0 relative shadow-sm md:shadow-none transition-all';
        
        const nameInitial = (u.full_name || u.username).charAt(0).toUpperCase();
        const roleBadgeClass = u.role === 'admin'
            ? 'bg-brand-100 dark:bg-brand-900/30 text-brand-700 dark:text-brand-300'
            : 'bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400';

        const statusHtml = u.is_active 
            ? '<span class="px-2 py-0.5 rounded-full bg-green-100 dark:bg-green-900/30 text-green-600 text-[10px] font-bold">ACTIVE</span>'
            : '<span class="px-2 py-0.5 rounded-full bg-slate-100 dark:bg-slate-800 text-slate-400 text-[10px] font-bold">DISABLED</span>';

        // Check if user has public token
        const hasToken = !!u.public_token;

        tr.innerHTML = `
            <!-- DESKTOP COLUMNS -->
            <td class="hidden md:table-cell px-6 py-4 text-sm text-slate-400 font-medium">${index + 1}</td>
            
            <td class="hidden md:table-cell px-6 py-4">
                <div class="flex items-center gap-3">
                    <div class="shrink-0 w-9 h-9 rounded-full bg-gradient-to-br from-brand-500 to-brand-700 flex items-center justify-center text-white text-sm font-bold shadow">
                        ${nameInitial}
                    </div>
                    <div>
                        <div class="text-sm font-bold text-slate-800 dark:text-white uppercase truncate max-w-[150px]" title="${u.full_name || u.username}">${u.full_name || u.username}</div>
                        <div class="text-[11px] text-slate-400">@${u.username}</div>
                    </div>
                </div>
            </td>

            <td class="hidden 2xl:table-cell px-6 py-4 text-sm text-slate-500 dark:text-slate-400">
                ${u.email ? `<a href="mailto:${u.email}" class="hover:text-brand-500 transition-colors truncate block max-w-[180px]" title="${u.email}">${u.email}</a>` : '<span class="text-slate-300">—</span>'}
            </td>

            <td class="hidden 2xl:table-cell px-6 py-4 text-sm text-slate-500 dark:text-slate-400">
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
                <div class="flex items-center gap-2">
                    ${hasToken ? `
                        <button onclick="copyUserTokenLink(${u.id})" class="p-1.5 bg-emerald-50 dark:bg-emerald-900/20 text-emerald-600 dark:text-emerald-400 rounded-lg hover:bg-emerald-100 transition-all border border-emerald-100/50 dark:border-emerald-800/50" title="Copy Public Link">
                             <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 7v8a2 2 0 002 2h6M8 7V5a2 2 0 012-2h4.586a1 1 0 01.707.293l4.414 4.414a1 1 0 01.293.707V15a2 2 0 01-2 2h-2M8 7H6a2 2 0 00-2 2v10a2 2 0 002 2h8a2 2 0 002-2v-2" /></svg>
                        </button>
                        <button onclick="openUserHub(${u.id})" class="p-1.5 text-slate-400 hover:text-slate-600 dark:hover:text-slate-200" title="Open Public Link">
                            <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10 6H6a2 2 0 00-2 2v10a2 2 0 002 2h10a2 2 0 002-2v-4M14 4h6m0 0v6m0-6L10 14" /></svg>
                        </button>
                        <button onclick="generatePublicLink(${u.id})" class="p-1.5 text-slate-300 hover:text-amber-500 transition-colors" title="Regenerate Link">
                             <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" /></svg>
                        </button>
                    ` : `
                        <button onclick="generatePublicLink(${u.id})" class="p-1.5 bg-slate-100 dark:bg-slate-800 text-slate-500 rounded-lg hover:bg-brand-500 hover:text-white transition-all shadow-sm" title="Create Public Link">
                             <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13.828 10.172a4 4 0 00-5.656 0l-4 4a4 4 0 105.656 5.656l1.102-1.101m-.758-4.823a4 4 0 015.656 0l4 4a4 4 0 01-5.656 5.656l-1.102-1.101" /></svg>
                        </button>
                    `}
                </div>
            </td>

            <td class="hidden md:table-cell px-6 py-4">
                ${statusHtml}
            </td>

            <!-- DESKTOP ACTIONS -->
            <td class="hidden md:table-cell px-6 py-4">
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

            <!-- MOBILE CARD VIEW -->
            <div class="md:hidden">
                <div class="flex justify-between items-start mb-4">
                    <div>
                        <div class="text-[10px] font-bold text-brand-600 mb-1">#${index + 1}</div>
                        <h3 class="text-lg font-black text-slate-800 dark:text-white uppercase leading-tight">${u.full_name || u.username}</h3>
                        <p class="text-xs text-slate-400 font-medium">${u.email || '@' + u.username}</p>
                    </div>
                    <div class="w-10 h-10 rounded-xl bg-green-50 dark:bg-green-900/20 flex items-center justify-center text-green-500">
                        <svg xmlns="http://www.w3.org/2000/svg" class="h-6 w-6" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 12l2 2 4-4m5.618-4.016A11.955 11.955 0 0112 2.944a11.955 11.955 0 01-8.618 3.04A12.02 12.02 0 003 9c0 5.591 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.042-.133-2.052-.382-3.016z" /></svg>
                    </div>
                </div>

                <div class="space-y-3 mb-6">
                    <div class="bg-slate-50 dark:bg-slate-800/50 p-3 rounded-xl border border-slate-100 dark:border-slate-800">
                        <p class="text-[9px] font-black text-slate-400 uppercase tracking-widest mb-1">Role</p>
                        <p class="text-xs font-bold text-brand-600 uppercase">${u.role}</p>
                    </div>
                    <div class="bg-slate-50 dark:bg-slate-800/50 p-3 rounded-xl border border-slate-100 dark:border-slate-800">
                        <p class="text-[9px] font-black text-slate-400 uppercase tracking-widest mb-1">Cameras</p>
                        <div class="flex items-center gap-2 text-xs font-bold text-slate-700 dark:text-slate-200">
                            <svg class="w-4 h-4 text-brand-500" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2V6zM14 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2V6zM4 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2v-2zM14 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2v-2z" /></svg>
                            ${u.total_cameras || 0} Cameras
                        </div>
                    </div>
                </div>

                <div class="grid grid-cols-4 gap-2 pt-4 border-t border-slate-100 dark:border-slate-800">
                    <a href="/view/${u.public_token || ''}" target="_blank" class="flex flex-col items-center gap-1 p-2 bg-slate-50 dark:bg-slate-800 rounded-lg text-slate-500 ${!u.public_token ? 'opacity-20 pointer-events-none' : ''}">
                        <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10 6H6a2 2 0 00-2 2v10a2 2 0 002 2h10a2 2 0 002-2v-4M14 4h6m0 0v6m0-6L10 14" /></svg>
                        <span class="text-[9px] font-bold uppercase">Open</span>
                    </a>
                    <button onclick="copyToClipboard('${window.location.origin}/view/${u.public_token}')" class="flex flex-col items-center gap-1 p-2 bg-slate-50 dark:bg-slate-800 rounded-lg text-emerald-500 ${!u.public_token ? 'opacity-20 pointer-events-none' : ''}">
                        <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 7v8a2 2 0 002 2h6M8 7V5a2 2 0 012-2h4.586a1 1 0 01.707.293l4.414 4.414a1 1 0 01.293.707V15a2 2 0 01-2 2h-2M8 7H6a2 2 0 00-2 2v10a2 2 0 002 2h8a2 2 0 002-2v-2" /></svg>
                        <span class="text-[9px] font-bold uppercase">Copy</span>
                    </button>
                    <button onclick='openUserModal(${JSON.stringify(u)})' class="flex flex-col items-center gap-1 p-2 bg-slate-50 dark:bg-slate-800 rounded-lg text-blue-500">
                        <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15.232 5.232l3.536 3.536m-2.036-5.036a2.5 2.5 0 113.536 3.536L6.5 21.036H3v-3.572L16.732 3.732z" /></svg>
                        <span class="text-[9px] font-bold uppercase">Edit</span>
                    </button>
                    <button onclick="deleteUser(${u.id})" class="flex flex-col items-center gap-1 p-2 bg-slate-50 dark:bg-slate-800 rounded-lg text-red-500 ${u.username === 'admin' ? 'opacity-20 pointer-events-none' : ''}">
                        <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" /></svg>
                        <span class="text-[9px] font-bold uppercase">Delete</span>
                    </button>
                </div>
            </div>
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

async function generatePublicLink(userId) {
    if (!confirm("Generate a new public hub link? Any old link will stop working.")) return;
    try {
        const response = await fetch(`/api/users/token?id=${userId}`, { method: 'POST' });
        if (response.ok) {
            showToast("Public link generated successfully!", "success");
            const data = await response.json();
            if (parseInt(userId) === parseInt(window.USER_ID)) {
                window.USER_PUBLIC_TOKEN = data.public_token;
            }
            
            // Update local allUsers cache so UI updates instantly
            console.log(`Regenerating token for user ${userId}...`);
            const userIdx = allUsers.findIndex(u => parseInt(u.id) === parseInt(userId));
            if (userIdx !== -1) {
                console.log(`Old token: ${allUsers[userIdx].public_token}`);
                console.log(`New token: ${data.public_token}`);
                allUsers[userIdx].public_token = data.public_token;
            } else {
                console.warn(`User ${userId} not found in allUsers array`);
            }
            
            renderUsersTable(); // Re-render immediately
            console.log("UI Re-rendered with new token data.");
        } else {
            const err = await response.text();
            showToast(`Failed: ${err}`, "error");
        }
    } catch (e) {
        showToast("Network error generating token", "error");
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

    title.textContent = isEdit ? `Edit User: ${user.full_name || user.username}` : 'Add New User';
    submitBtnText.textContent = isEdit ? 'Update User' : 'Create User';
    
    selectRole(isEdit ? user.role : 'user');
    
    modal.classList.remove('hidden');
    checkModalPlanRestrictions();
}

function checkModalPlanRestrictions() {
    const plan = document.getElementById('userSubscription').value;
    const supportToggle = document.getElementById('userEnableSupport')?.closest('div.mt-4') || document.getElementById('userEnableSupport')?.closest('label')?.parentElement;

    if (supportToggle) {
        supportToggle.classList.toggle('hidden', plan === 'Free' || plan === 'Basic' || plan === 'Premium');
    }
}

// Add listener for plan changes in modal
document.getElementById('userSubscription')?.addEventListener('change', checkModalPlanRestrictions);

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
async function takeSnapshot(name, displayName = "") {
    try {
        const response = await fetch(`/api/snapshot?stream=${encodeURIComponent(name)}`);
        const blob = await response.blob();
        const url = window.URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        const filename = displayName ? `${displayName}-snapshot.jpg` : `${name}-snapshot.jpg`;
        a.download = filename;
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

let locationMapZoom = 13;

function initLocationMap(lat, lng) {
    setTimeout(() => {
        const DEFAULT_LAT = -7.2504; // Surabaya
        const DEFAULT_LNG = 112.7688;
        const initialLat = lat || DEFAULT_LAT;
        const initialLng = lng || DEFAULT_LNG;

        if (!locationMap) {
            locationMap = L.map('streamLocationMap').setView([initialLat, initialLng], locationMapZoom);
            L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
                maxZoom: 25,
                maxNativeZoom: 19,
                attribution: '© OpenStreetMap'
            }).addTo(locationMap);

            locationMap.on('zoomend', () => {
                locationMapZoom = locationMap.getZoom();
            });
            
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
            const currentZoom = locationMap.getZoom();
            locationMap.setView([initialLat, initialLng], currentZoom || 13);
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

async function openEditModal(name, url, displayName = "") {
    resetAdvancedOptions();
    document.getElementById("modalTitle").textContent = "Edit Stream";
    document.getElementById("editOriginalName").value = name;
    document.getElementById("streamName").value = displayName || name;
    document.getElementById("streamUrl").value = url;
    
    const streamInfo = allStreams.find(s => s.name === name);
    document.getElementById("streamEnabled").checked = streamInfo ? streamInfo.enabled !== false : true;
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
    const displayName = document.getElementById("streamName").value.trim();
    let url = document.getElementById("streamUrl").value.trim();
    const originalName = document.getElementById("editOriginalName").value.trim(); // This is the UUID
    const lat = parseFloat(document.getElementById("streamLat").value) || 0;
    const lng = parseFloat(document.getElementById("streamLng").value) || 0;
    const enabled = document.getElementById("streamEnabled").checked;

    if (!displayName || !url) { alert("Fields required"); return; }

    // Generate UUID if it's a new stream
    const name = isEdit ? originalName : (crypto.randomUUID ? crypto.randomUUID() : 'stream-' + Date.now() + Math.floor(Math.random()*1000));

    const method = isEdit ? 'PUT' : 'POST';
    const body = isEdit 
        ? JSON.stringify({ name, display_name: displayName, url, originalName, lat, lng, enabled }) 
        : JSON.stringify({ name, display_name: displayName, url, lat, lng, enabled });

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
    const checked = document.querySelectorAll('.camera-checkbox:checked');
    const uniqueSelected = new Set(Array.from(checked).map(cb => cb.value));
    const selected = uniqueSelected.size;
    
    const bulkDiv = document.getElementById('bulkActions');
    const bulkCount = document.getElementById('bulkCount');
    if (bulkCount) bulkCount.innerText = selected;
    
    if (selected > 0) {
        bulkDiv.classList.remove('hidden');
        bulkDiv.classList.add('flex');
    } else {
        bulkDiv.classList.add('hidden');
        bulkDiv.classList.remove('flex');
    }
}

async function executeBulkAction(action) {
    const checked = document.querySelectorAll('.camera-checkbox:checked');
    const selected = Array.from(new Set(Array.from(checked).map(cb => cb.value)));
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
        update('stat-active', data.activeStreams);
        update('stat-disabled', data.disabledStreams);
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

    // Reset local state before fetch
    tlFiles = [];
    tlPlayIndex = 0;
    if(gallery) gallery.innerHTML = '';
    document.getElementById('tlLoadingOverlay').classList.remove('hidden');

    try {
        const start = document.getElementById('tlStartDate').value;
        const end = document.getElementById('tlEndDate').value;
        // Add cache-buster to ensure we get latest file list after deletion
        const res = await fetch(`/api/timelapse/files?name=${encodeURIComponent(name)}&start=${start}&end=${end}&_=${Date.now()}`);
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
        console.error("Failed to load tl files", e);
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
            // Fix: Call the correct refresh function
            await loadTlFiles(tlCurrentStream);
            
            if (mode === 'all') {
                alert("All snapshots deleted successfully");
            }
        } else {
            const err = await res.text();
            alert("Delete failed: " + err);
        }
    } catch (e) {
        console.error("Delete failed", e);
        alert("Operation failed. Please try again.");
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
    if(playBtn) playBtn.innerHTML = '<svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10 9v6m4-6v6m7-3a9 9 0 11-18 0 9 9 0 0118 0z" /></svg> Pause';

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
    if(playBtn) playBtn.innerHTML = '<svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z" /></svg> Play';
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
        
        if (!res.ok) {
            const errorText = await res.text();
            throw new Error(errorText || "Export failed from server");
        }

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

function toggleMarkerLock() {
    markersLocked = !markersLocked;
    const btn = document.getElementById('btnLockMarkers');
    const icon = document.getElementById('lockIcon');
    const text = document.getElementById('lockStatusText');
    
    if (!btn || !icon || !text) return;

    if (markersLocked) {
        icon.innerHTML = `<svg xmlns="http://www.w3.org/2000/svg" class="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="3" d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z" /></svg>`;
        text.textContent = 'LOCKED';
        btn.classList.remove('bg-brand-500', 'text-white', 'border-brand-500');
        btn.classList.add('bg-white', 'dark:bg-slate-800', 'border-slate-200', 'dark:border-slate-700');
        icon.classList.remove('text-white');
        icon.classList.add('text-brand-500');
    } else {
        icon.innerHTML = `<svg xmlns="http://www.w3.org/2000/svg" class="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="3" d="M8 11V7a4 4 0 118 0m-4 8v2M5 21h14a2 2 0 002-2v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2z" /></svg>`;
        text.textContent = 'UNLOCKED';
        btn.classList.add('bg-brand-500', 'text-white', 'border-brand-500');
        btn.classList.remove('bg-white', 'dark:bg-slate-800', 'border-slate-200', 'dark:border-slate-700');
        icon.classList.add('text-white');
        icon.classList.remove('text-brand-500');
    }

    // Apply to all markers
    Object.values(maintenanceMarkers).forEach(marker => {
        if (markersLocked) marker.dragging.disable();
        else marker.dragging.enable();
    });
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
            maxZoom: 25,
            maxNativeZoom: 19,
            attribution: '&copy; OpenStreetMap'
        });
        
        const satellite = L.tileLayer('https://server.arcgisonline.com/ArcGIS/rest/services/World_Imagery/MapServer/tile/{z}/{y}/{x}', {
            maxZoom: 25,
            maxNativeZoom: 19,
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
                    
                    // Update local data
                    const stream = allStreams.find(s => s.name === name);
                    if (stream) {
                        stream.lat = lat;
                        stream.lng = lng;
                    }

                    // Add marker manually without reload
                    const roundIcon = L.divIcon({
                        className: 'round-marker',
                        iconSize: [14, 14],
                        iconAnchor: [7, 7]
                    });
                    const marker = L.marker([lat, lng], { icon: roundIcon }).addTo(maintenanceMap);
                    
                    const updatePopup = (lat, lng) => {
                        marker.bindPopup(`
                            <div class="p-1">
                                <div class="text-sm font-bold border-b border-slate-100 dark:border-slate-700 pb-1 mb-1">${stream ? (stream.display_name || stream.name) : name}</div>
                                <div class="text-[10px] font-mono bg-slate-100 dark:bg-slate-900/50 p-1.5 rounded border border-slate-200 dark:border-slate-700 leading-relaxed shadow-sm">
                                    <span class="text-indigo-600 dark:text-indigo-400 font-bold">LAT:</span> ${lat.toFixed(6)}<br>
                                    <span class="text-indigo-600 dark:text-indigo-400 font-bold">LNG:</span> ${lng.toFixed(6)}
                                </div>
                            </div>
                        `);
                    };

                    updatePopup(lat, lng);
                    
                    marker.on('click', () => {
                        selectCameraOnMap(name);
                    });

                    maintenanceMarkers[name] = marker;
                    
                    // Refresh the select dropdown text (remove "No Marker" status)
                    const opt = dashSelect.querySelector(`option[value="${name}"]`);
                    if (opt) opt.textContent = stream ? (stream.display_name || stream.name) : name;

                    setTimeout(() => selectDashboardCamera(name), 100);
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
            const isEnabled = s.enabled !== false;
            const markerColor = isEnabled ? '#6366f1' : '#94a3b8';
            
            const roundIcon = L.divIcon({
                className: 'round-marker-container',
                html: `
                    <div class="relative group">
                        <div class="w-4 h-4 rounded-full bg-white dark:bg-slate-900 border-2 shadow flex items-center justify-center transition-transform hover:scale-125" style="border-color: ${markerColor}">
                            <div class="w-1.5 h-1.5 rounded-full" style="background-color: ${markerColor}"></div>
                            ${!isEnabled ? '<div class="absolute -top-1 -right-1 w-2 h-2 bg-slate-400 border-2 border-white dark:border-slate-800 rounded-full"></div>' : ''}
                        </div>
                    </div>`,
                iconSize: [16, 16],
                iconAnchor: [8, 8],
                popupAnchor: [0, -8]
            });

            const marker = L.marker([s.lat, s.lng], {
                draggable: !markersLocked,
                title: s.display_name || s.name,
                icon: roundIcon
            }).addTo(maintenanceMap);

            const updatePopup = (lat, lng) => {
                const statusHtml = !isEnabled ? '<div class="text-[9px] font-black bg-yellow-100 text-yellow-600 dark:bg-yellow-900/40 dark:text-yellow-400 px-1.5 py-0.5 rounded uppercase tracking-tighter mb-1 inline-block">Disabled</div>' : '';
                marker.bindPopup(`
                    <div class="p-1">
                        <div class="text-sm font-bold border-b border-slate-100 dark:border-slate-700 pb-1 mb-1">${s.display_name || s.name}</div>
                        ${statusHtml}
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
            opt.textContent = `${s.display_name || s.name}${status}`;
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

    if (bounds.length > 0 && maintenanceFirstLoad) {
        maintenanceMap.fitBounds(bounds, { padding: [20, 20] });
        maintenanceFirstLoad = false;
    } else if (bounds.length === 0 && maintenanceFirstLoad) {
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
        const currentZoom = maintenanceMap.getZoom();
        maintenanceMap.setView(marker.getLatLng(), Math.max(currentZoom, 14), { animate: false });
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

    iframe.onload = () => {
        try {
            const style = document.createElement('style');
            style.textContent = `
                video { object-fit: fill !important; width: 100% !important; height: 100% !important; }
                body { background-color: black !important; margin: 0 !important; overflow: hidden !important; }
                .mode { display: none !important; }
                .retry { display: none !important; }
            `;
            iframe.contentDocument.head.appendChild(style);
        } catch (e) {}
    };

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
            toast.textContent = `Location updated for ${stream.display_name || stream.name}`;
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
let currentPreviewedCamera = null;

function takeSnapshotFromPreview() {
    if (currentPreviewedCamera) {
        takeSnapshot(currentPreviewedCamera);
    }
}

function openCameraPreviewModal(name) {
    currentPreviewedCamera = name;
    window.currentPreviewedCamera = name; // For share modal compatibility
    const modal = document.getElementById('cameraPreviewModal');
    const content = document.getElementById('cameraPreviewContent');
    const player = document.getElementById('cameraPreviewPlayer');
    const title = document.getElementById('cameraPreviewTitle');
    if (!modal || !player) return;

    const stream = allStreams.find(s => s.name === name);
    title.textContent = stream ? (stream.display_name || stream.name) : name;

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
    iframe.onload = () => {
        try {
            // Inject CSS to hide go2rtc status/info bar
            const style = document.createElement('style');
            style.textContent = `
                .info { display: none !important; }
                .mode { display: none !important; }
                .retry { display: none !important; }
            `; 
            iframe.contentDocument.head.appendChild(style);
        } catch (e) {}
    };
    player.appendChild(iframe);

    // Show modal with animation
    modal.classList.remove('hidden');
    requestAnimationFrame(() => {
        modal.classList.remove('opacity-0', 'pointer-events-none');
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
    setTimeout(() => { 
        if (player) player.innerHTML = ''; 
        modal.classList.add('hidden');
    }, 300);
    document.removeEventListener('keydown', _previewEscHandler);
}

// ===== Command Center Map Logic =====
let globalCameraMap = null;
let commandCenterMarkers = {};
let commandCenterFirstLoad = true;

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
    });

    // Use a small timeout to ensure container has size
    setTimeout(() => {
        globalCameraMap.setView([-2.5489, 118.0149], 5);
        globalCameraMap.invalidateSize();
        renderCommandCenterMarkers();
    }, 100);

    L.control.zoom({ position: 'bottomright' }).addTo(globalCameraMap);

    const darkLayer = L.tileLayer('https://{s}.basemaps.cartocdn.com/dark_all/{z}/{x}/{y}{r}.png', { maxZoom: 25, maxNativeZoom: 19, subdomains: 'abcd' });
    const lightLayer = L.tileLayer('https://{s}.basemaps.cartocdn.com/light_all/{z}/{x}/{y}{r}.png', { maxZoom: 25, maxNativeZoom: 19, subdomains: 'abcd' });
    const openMapsLayer = L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png', { maxZoom: 25, maxNativeZoom: 19 });
    const satelliteLayer = L.tileLayer('https://server.arcgisonline.com/ArcGIS/rest/services/World_Imagery/MapServer/tile/{z}/{y}/{x}', { maxZoom: 25, maxNativeZoom: 19 });

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
        if (s.lat && s.lng) {
            const isEnabled = s.enabled !== false;
            const markerColor = isEnabled ? '#6366f1' : '#94a3b8';

            const roundIcon = L.divIcon({
                className: 'round-marker-container',
                html: `
                    <div class="relative group">
                        <div class="w-4 h-4 rounded-full bg-white dark:bg-slate-900 border-2 shadow flex items-center justify-center transition-transform hover:scale-125" style="border-color: ${markerColor}">
                            <div class="w-1.5 h-1.5 rounded-full" style="background-color: ${markerColor}"></div>
                            ${!isEnabled ? '<div class="absolute -top-1 -right-1 w-2 h-2 bg-slate-400 border-2 border-white dark:border-slate-800 rounded-full"></div>' : ''}
                        </div>
                    </div>`,
                iconSize: [16, 16],
                iconAnchor: [8, 8],
                popupAnchor: [0, -8]
            });

            const marker = L.marker([s.lat, s.lng], { icon: roundIcon }).addTo(globalCameraMap);
            const popupContent = `
                <div class="relative w-[220px] h-[124px] bg-gray-950 group cursor-pointer overflow-hidden rounded-xl" onclick="openCameraPreviewModal('${escapeJS(s.name)}')">
                    <img src="/api/snapshot?name=${encodeURIComponent(s.name)}" class="w-full h-full object-cover opacity-90 group-hover:opacity-60" 
                         onload="window.globalCameraMap && this.parentElement.parentElement.parentElement.__popup && this.parentElement.parentElement.parentElement.__popup.update()"
                         onerror="this.src='data:image/svg+xml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIGZpbGw9Im5vbmUiIHZpZXdCb3g9IjAgMCAyNCAyNCIgc3Ryb2tlPSJncmF5Ij48cGF0aCBzdHJva2UtbGluZWNhcD0icm91bmQiIHN0cm9rZS1saW5lam9pbj0icm91bmQiIHN0cm9rZS13aWR0aD0iMiIgZD0iTTQgMTZsNC41ODYtNC41ODZhMiAyIDAgMDEyLjgyODAwTDE2IDE2bS0yLTJsMS41ODYtMS41ODZhMiAyIDAgMDEyLjgyODAwTDIwIDE0bS02LTZoLjAxTTYgMjBoMTJhMiAyIDAgMDAyLTJWNmEyIDIgMCAwMC0yLTJINmEyIDIgMDAwLTIgMTJoMiAwIDAwMiAyekkiLz48L3N2Zz4='">
                    
                    <div class="absolute bottom-0 left-0 right-0 p-2 pt-6 bg-gradient-to-t from-black/90 to-transparent pointer-events-none">
                        <h4 class="font-bold text-[11px] truncate text-white drop-shadow-md" title="${s.display_name || s.name}">${s.display_name || s.name}</h4>
                    </div>
                    ${isEnabled ? `
                    <div class="absolute inset-0 flex items-center justify-center pointer-events-none">
                        <div class="bg-black/40 backdrop-blur-sm w-11 h-11 rounded-full flex items-center justify-center shadow-2xl transform scale-90 opacity-0 group-hover:scale-100 group-hover:opacity-100 transition-all duration-300">
                            <svg class="w-6 h-6 text-white ml-0.5" fill="currentColor" viewBox="0 0 20 20"><path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zM9.555 7.168A1 1 0 008 8v4a1 1 0 001.555.832l3-2a1 1 0 000-1.664l-3-2z" clip-rule="evenodd" /></svg>
                        </div>
                    </div>` : ''}
                </div>`;

            const popup = L.popup({ 
                className: 'custom-popup',
                maxWidth: 220,
                minWidth: 220,
                autoPan: true
            }).setContent(popupContent);
            
            marker.bindPopup(popup);
            marker.__popup = popup; 

            commandCenterMarkers[s.name] = marker;
            bounds.push([s.lat, s.lng]);
        }
    });

    if (bounds.length > 0 && commandCenterFirstLoad) {
        globalCameraMap.fitBounds(bounds, { padding: [30, 30], maxZoom: 22 });
        commandCenterFirstLoad = false;
    }
}

function debounceMapSearch(query) {
    clearTimeout(window.mapSearchTimer);
    const resultsDropdown = document.getElementById('ccMapSearchResults');
    const resultsList = document.getElementById('ccSearchResultsList');
    const clearBtn = document.getElementById('ccClearSearchBtn');

    if (!query || query.trim() === '') {
        if (resultsDropdown) resultsDropdown.classList.add('hidden');
        if (clearBtn) clearBtn.classList.add('hidden');
        // Show all markers if query is empty
        Object.values(commandCenterMarkers).forEach(m => {
            if (globalCameraMap && !globalCameraMap.hasLayer(m)) {
                m.addTo(globalCameraMap);
            }
        });
        return;
    }

    if (clearBtn) clearBtn.classList.remove('hidden');

    window.mapSearchTimer = setTimeout(() => {
        const q = String(query).toLowerCase().trim();
        const matches = Object.keys(commandCenterMarkers).filter(name => {
            const title = commandCenterMarkers[name]?.options?.title || name;
            return name.toLowerCase().includes(q) || title.toLowerCase().includes(q);
        });

        // Filter markers on map
        Object.entries(commandCenterMarkers).forEach(([name, marker]) => {
            const title = marker.options?.title || name;
            if (name.toLowerCase().includes(q) || title.toLowerCase().includes(q)) {
                if (!globalCameraMap.hasLayer(marker)) marker.addTo(globalCameraMap);
            } else {
                if (globalCameraMap.hasLayer(marker)) marker.remove();
            }
        });

        // Populate dropdown
        if (resultsList) {
            resultsList.innerHTML = matches.slice(0, 10).map(name => {
                const title = commandCenterMarkers[name]?.options?.title || name;
                return `
                <button onclick="selectCCSearchResult('${name.replace(/'/g, "\\'")}')" class="w-full flex items-center px-4 py-3 hover:bg-slate-50 dark:hover:bg-slate-800 transition-colors border-b border-slate-100 dark:border-slate-800 last:border-none text-left">
                    <svg xmlns="http://www.w3.org/2000/svg" class="w-4 h-4 text-brand-500 mr-3 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M17.657 16.657L13.414 20.9a1.998 1.998 0 01-2.827 0l-4.244-4.243a8 8 0 1111.314 0z"/><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 11a3 3 0 11-6 0 3 3 0 016 0z"/></svg>
                    <span class="text-sm font-medium text-slate-700 dark:text-slate-200 truncate">${title}</span>
                </button>
            `}).join('');

            if (matches.length > 0) {
                resultsDropdown.classList.remove('hidden');
            } else {
                resultsList.innerHTML = '<div class="px-4 py-3 text-sm text-slate-400 italic">No cameras found</div>';
                resultsDropdown.classList.remove('hidden');
            }
        }
    }, 300);
}

function clearCCMapSearch() {
    const input = document.getElementById('mapSearchInput');
    if (input) {
        input.value = '';
        debounceMapSearch('');
    }
}

function selectCCSearchResult(name) {
    const input = document.getElementById('mapSearchInput');
    if (input) input.value = name;
    
    const resultsDropdown = document.getElementById('ccMapSearchResults');
    if (resultsDropdown) resultsDropdown.classList.add('hidden');
    
    focusCCCamera(name);
}

function focusCCCamera(name) {
    const marker = commandCenterMarkers[name];
    if (marker && globalCameraMap) {
        // Force add marker if it was filtered out
        if (!globalCameraMap.hasLayer(marker)) marker.addTo(globalCameraMap);
        
        const currentZoom = globalCameraMap.getZoom();
        const targetZoom = Math.max(currentZoom, 17);
        
        globalCameraMap.setView(marker.getLatLng(), targetZoom, {
            animate: true,
            duration: 1
        });
        
        marker.openPopup();
        
        const icon = marker.getElement();
        if (icon) {
            icon.classList.add('selected-marker');
            setTimeout(() => icon.classList.remove('selected-marker'), 3000);
        }
    }
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
    const mapBtnHeader = document.getElementById('btnCCMapHeader');
    const gridBtnHeader = document.getElementById('btnCCGridHeader');
    const toggleBtn = document.getElementById('ccSidePanelToggleBtn');
    
    const activeHeaderClass = ['bg-white', 'dark:bg-slate-600', 'shadow-sm', 'text-brand-600', 'dark:text-white'];
    const inactiveHeaderClass = ['text-slate-500', 'dark:text-slate-400', 'hover:text-slate-700', 'dark:hover:text-slate-200', 'bg-transparent', 'shadow-none'];

    if (tab === 'grid') {
        // Auto-collapse sidebar if open
        const panel = document.getElementById('mapSidePanel');
        if (panel && panel.classList.contains('translate-x-0')) {
            toggleSidePanel();
        }

        document.getElementById('commandcenter-map-container').classList.add('hidden');
        document.getElementById('commandcenter-grid-container').classList.remove('hidden');
        
        // Hide map specific UI components
        const mapActionBar = document.getElementById('ccMapActionBar');
        if (mapActionBar) mapActionBar.classList.add('hidden');

        if (toggleBtn) {
            toggleBtn.disabled = true;
            toggleBtn.classList.add('opacity-30', 'cursor-not-allowed');
            toggleBtn.classList.remove('hover:text-brand-600', 'hover:bg-slate-50', 'dark:hover:bg-slate-800');
        }

        if(mapBtnHeader) {
            mapBtnHeader.classList.remove(...activeHeaderClass);
            mapBtnHeader.classList.add(...inactiveHeaderClass);
        }
        if(gridBtnHeader) {
            gridBtnHeader.classList.remove(...inactiveHeaderClass);
            gridBtnHeader.classList.add(...activeHeaderClass);
        }
        
        renderCCGridPage(ccCurrentPage);
    } else {
        document.getElementById('commandcenter-grid-container').classList.add('hidden');
        document.getElementById('commandcenter-map-container').classList.remove('hidden');
        
        // Reshow map specific UI components
        const mapActionBar = document.getElementById('ccMapActionBar');
        if (mapActionBar) mapActionBar.classList.remove('hidden');

        if (toggleBtn) {
            toggleBtn.disabled = false;
            toggleBtn.classList.remove('opacity-30', 'cursor-not-allowed');
            toggleBtn.classList.add('hover:text-brand-600', 'hover:bg-slate-50', 'dark:hover:bg-slate-800');
        }

        if(mapBtnHeader) {
            mapBtnHeader.classList.remove(...inactiveHeaderClass);
            mapBtnHeader.classList.add(...activeHeaderClass);
        }
        if(gridBtnHeader) {
            gridBtnHeader.classList.remove(...activeHeaderClass);
            gridBtnHeader.classList.add(...inactiveHeaderClass);
        }
        
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
                    <h3 class="font-bold text-[15px] truncate text-slate-800 dark:text-white" title="${stream.display_name || stream.name}">${stream.display_name || stream.name}</h3>
                    <div class="flex items-center gap-1">
                        <button onclick="event.stopPropagation(); goToMapMarker('${stream.name.replace(/'/g, "\\'")}')" class="p-1.5 text-slate-400 hover:text-brand-600 rounded-lg transition-colors shrink-0 z-10" title="View location on map">
                            <svg class="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M17.657 16.657L13.414 20.9a1.998 1.998 0 01-2.827 0l-4.244-4.243a8 8 0 1111.314 0z"/><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 11a3 3 0 11-6 0 3 3 0 016 0z"/></svg>
                        </button>
                        <button onclick="event.stopPropagation(); openShareModal('${stream.name.replace(/'/g, "\\'")}')" class="p-1.5 text-slate-400 hover:text-emerald-500 rounded-lg transition-colors shrink-0 z-10" title="Share Camera">
                            <svg class="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8.684 13.342C8.886 12.938 9 12.482 9 12c0-.482-.114-.938-.316-1.342m0 2.684a3 3 0 110-2.684m0 2.684l6.632 3.316m-6.632-6l6.632-3.316m0 0a3 3 0 105.367-2.684 3 3 0 00-5.367 2.684zm0 9.316a3 3 0 105.368 2.684 3 3 0 00-5.368-2.684z" /></svg>
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
        const currentZoom = globalCameraMap.getZoom();
        globalCameraMap.setView(marker.getLatLng(), Math.max(currentZoom, 16), { animate: false });
        setTimeout(() => marker.openPopup(), 100);
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
    const rtspInput = document.getElementById('shareRtsp');
    const snapInput = document.getElementById('shareSnapshot');
    const iframeText = document.getElementById('shareIframe');
    
    if (!modal || !urlInput || !iframeText) return;
    
    const stream = allStreams.find(s => s.name === name);
    nameSpan.textContent = stream ? (stream.display_name || stream.name) : name;
    
    // Construct URLs
    const protocol = window.location.protocol;
    const hostname = window.location.hostname;
    const port = window.location.port ? `:${window.location.port}` : "";
    const baseUrl = `${protocol}//${hostname}${port}`;
    
    // Direct Player Link
    const streamUrl = `${baseUrl}/rtc/stream.html?src=${encodeURIComponent(name)}&mode=mse,webrtc`;
    // Embed Code
    const embedCode = `<iframe src="${streamUrl}" width="100%" height="450" frameborder="0" allowfullscreen allow="autoplay; fullscreen; picture-in-picture"></iframe>`;
    // RTSP Link (Proxy)
    const rtspUrl = `rtsp://${hostname}:8554/${encodeURIComponent(name)}`;
    // Snapshot Link
    const snapUrl = `${baseUrl}/api/snapshot?name=${encodeURIComponent(name)}`;
    
    urlInput.value = streamUrl;
    iframeText.value = embedCode;
    if (rtspInput) rtspInput.value = rtspUrl;
    if (snapInput) snapInput.value = snapUrl;
    
    modal.classList.remove('hidden');
}

function closeShareModal() {
    const modal = document.getElementById('shareModal');
    if (modal) modal.classList.add('hidden');
}
function renderMapSidebarCameraList(streams) {
    const listContainer = document.getElementById('mapSidebarCameraList');
    if (!listContainer) return;

    if (!streams || streams.length === 0) {
        listContainer.innerHTML = '<div class="text-center py-8 text-slate-400 italic text-xs">No cameras available</div>';
        return;
    }

    listContainer.innerHTML = streams.map(s => `
        <div onclick="focusCameraOnMap('${escapeJS(s.name)}')" class="group/item flex flex-col bg-white dark:bg-slate-800/40 rounded-xl border border-slate-100 dark:border-slate-800 overflow-hidden transition-all hover:shadow-lg hover:border-brand-500/50 cursor-pointer mb-2 shadow-sm">
            <div class="aspect-video w-full bg-slate-900 relative overflow-hidden">
                ${s.enabled !== false ? `
                <img src="/api/snapshot?name=${encodeURIComponent(s.name)}" 
                     class="w-full h-full object-cover opacity-90 group-hover/item:scale-110 transition-transform duration-700"
                     onerror="this.src='data:image/svg+xml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIGZpbGw9Im5vbmUiIHZpZXdCb3g9IjAgMCAyNCAyNCIgc3Ryb2tlPSJncmF5Ij48cGF0aCBzdHJva2UtbGluZWNhcD0icm91bmQiIHN0cm9rZS1saW5lam9pbj0icm91bmQiIHN0cm9rZS13aWR0aD0iMiIgZD0iTTQgMTZsNC41ODYtNC41ODZhMiAyIDAgMDEyLjgyODAwTDE2IDE2bS0yLTJsMS41ODYtMS41ODZhMiAyIDAgMDEyLjgyODAwTDIwIDE0bS02LTZoLjAxTTYgMjBoMTJhMiAyIDAgMDAyLTJWNmEyIDIgMCAwMC0yLTJINmEyIDIgMDAwLTIgMTJoMiAwIDAwMiAyekkiLz48L3N2Zz4='">
                ` : `
                <div class="w-full h-full flex flex-col items-center justify-center text-slate-600 italic">
                    <svg class="h-8 w-8 mb-2 opacity-20" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 10l4.553-2.276A1 1 0 0121 8.618v6.764a1 1 0 01-1.447.894L15 14M5 18h8a2 2 0 002-2V8a2 2 0 00-2-2H5a2 2 0 00-2 2v8a2 2 0 002 2z"/></svg>
                </div>
                `}
                
                <div class="absolute top-2 right-2">
                    ${s.online ? '<span class="flex h-2.5 w-2.5 rounded-full bg-green-500 shadow-[0_0_10px_rgba(34,197,94,0.8)] border-2 border-white dark:border-slate-900"></span>' : '<span class="flex h-2.5 w-2.5 rounded-full bg-slate-400 border-2 border-white dark:border-slate-900"></span>'}
                </div>
                
                ${s.enabled === false ? '<div class="absolute inset-0 bg-slate-900/40 flex items-center justify-center"><span class="px-2 py-1 bg-yellow-500/90 text-white text-[10px] font-black uppercase rounded tracking-widest">Disabled</span></div>' : ''}
            </div>
            <div class="p-2.5">
                <div class="text-[11px] font-black text-slate-800 dark:text-white uppercase truncate group-hover/item:text-brand-500 transition-colors mb-0.5">${s.display_name || s.name}</div>
                <div class="flex items-center justify-between">
                    <span class="text-[9px] font-bold text-slate-400 uppercase tracking-tighter">ID: ${s.name.substring(0,8)}...</span>
                    <div class="flex gap-1.5 items-center">
                        <svg class="w-3 h-3 text-brand-500" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M17.657 16.657L13.414 20.9a1.998 1.998 0 01-2.827 0l-4.244-4.243a8 8 0 1111.314 0z"/><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 11a3 3 0 11-6 0 3 3 0 016 0z"/></svg>
                    </div>
                </div>
            </div>
        </div>
    `).join('');
}

function focusCameraOnMap(name) {
    const stream = allStreams.find(s => s.name === name);
    if (!stream) return;

    if (currentView === 'commandcenter') {
        const gridContainer = document.getElementById('commandcenter-grid-container');
        const isGrid = gridContainer && !gridContainer.classList.contains('hidden');
        
        if (isGrid) {
            // Grid View behavior - open player and close sidebar
            openCameraPreviewModal(name);
            if (window.innerWidth < 1024 && typeof toggleSidePanel === 'function') {
                const panel = document.getElementById('mapSidePanel');
                if (panel && panel.classList.contains('translate-x-0')) toggleSidePanel();
            }
            return;
        }
    }

    const lat = parseFloat(stream.lat);
    const lng = parseFloat(stream.lng);
    if (isNaN(lat) || isNaN(lng) || (lat === 0 && lng === 0)) return;

    // Determine which map is active
    let mapToUse = null;
    let markersToSearch = [];

    if (currentView === 'dashboard') {
        mapToUse = maintenanceMap;
        markersToSearch = Object.values(maintenanceMarkers || {});
    } else if (currentView === 'commandcenter') {
        mapToUse = globalCameraMap;
        markersToSearch = Object.values(commandCenterMarkers || {});
    } else if (typeof publicMap !== 'undefined') {
        mapToUse = publicMap;
        markersToSearch = Object.values(publicMarkers || {});
    }

    if (mapToUse) {
        // Requirement: Ensure marker is centered in area
        mapToUse.setView([lat, lng], 18, { animate: true, duration: 1 });
        
        // Map View behavior - close sidebar and open popup
        if (window.innerWidth < 1024 && typeof toggleSidePanel === 'function') {
            const panel = document.getElementById('mapSidePanel');
            if (panel && panel.classList.contains('translate-x-0')) toggleSidePanel();
        }

        // Find marker and open popup
        setTimeout(() => {
            const marker = markersToSearch.find(m => {
                try {
                    const popup = m.getPopup();
                    if (popup && popup.getContent() && popup.getContent().includes(name)) return true;
                } catch(e) {}
                return false;
            });
            
            if (marker) marker.openPopup();
        }, 300); // 300ms to allow some movement
    }
}
let allTestLogs = [];
let testLogsCurrentPage = 1;
const testLogsPageSize = 10;

function loadTestLogs() {
    const tbody = document.getElementById('testLogTableBody');
    if (!tbody) return;

    tbody.innerHTML = '<tr><td colspan="3" class="px-6 py-12 text-center text-slate-400">Loading logs...</td></tr>';

    fetch('/api/admin/test-logs')
        .then(res => res.json())
        .then(logs => {
            allTestLogs = logs || [];
            testLogsCurrentPage = 1;
            renderTestLogsPage(1);
        })
        .catch(err => {
            console.error('Error loading logs:', err);
            tbody.innerHTML = '<tr><td colspan="3" class="px-6 py-12 text-center text-red-400">Error loading logs.</td></tr>';
        });
}

function renderTestLogsPage(page) {
    const tbody = document.getElementById('testLogTableBody');
    if (!tbody) return;

    const startIndex = (page - 1) * testLogsPageSize;
    const endIndex = startIndex + testLogsPageSize;
    const logsToShow = allTestLogs.slice(startIndex, endIndex);
    const totalPages = Math.ceil(allTestLogs.length / testLogsPageSize) || 1;

    if (logsToShow.length === 0) {
        tbody.innerHTML = '<tr><td colspan="3" class="px-6 py-12 text-center text-slate-400">No logs found.</td></tr>';
        return;
    }

    tbody.innerHTML = logsToShow.map(log => {
        const date = new Date(log.created_at);
        const dateStr = date.toLocaleString('id-ID', { 
            day: '2-digit', 
            month: 'short', 
            year: 'numeric', 
            hour: '2-digit', 
            minute: '2-digit' 
        });
        
        return `
            <tr class="hover:bg-slate-50 dark:hover:bg-slate-800/50 transition-colors group">
                <td class="px-6 py-4 whitespace-nowrap text-xs font-semibold text-slate-500 dark:text-slate-400">
                    ${dateStr}
                </td>
                <td class="px-6 py-4">
                    <div class="text-sm font-bold text-slate-800 dark:text-white break-all max-w-md">${log.url}</div>
                </td>
                <td class="px-6 py-4">
                    <div class="flex flex-col gap-1">
                        <div class="flex items-center gap-1.5">
                            <span class="w-1.5 h-1.5 rounded-full bg-blue-500"></span>
                            <span class="text-xs font-bold text-slate-700 dark:text-slate-300 select-all">${log.ip}</span>
                        </div>
                        <div class="text-[10px] text-slate-400 dark:text-slate-500 truncate max-w-xs" title="${log.user_agent}">
                            ${log.user_agent}
                        </div>
                    </div>
                </td>
            </tr>
        `;
    }).join('');

    // Update paging info
    const infoEl = document.getElementById('testLogPageInfo');
    if (infoEl) {
        infoEl.innerText = `Showing ${startIndex + 1} to ${Math.min(endIndex, allTestLogs.length)} of ${allTestLogs.length} entries`;
    }

    // Render pagination buttons
    renderTestLogPagination(totalPages, page);
}

function renderTestLogPagination(totalPages, currentPage) {
    const container = document.getElementById('testLogPagination');
    if (!container) return;

    container.innerHTML = '';
    
    const maxVisible = 5;
    let startPage = Math.max(1, currentPage - 2);
    let endPage = Math.min(totalPages, startPage + maxVisible - 1);
    
    if (endPage - startPage < maxVisible - 1) {
        startPage = Math.max(1, endPage - maxVisible + 1);
    }

    // Prev Button
    const prevBtn = document.createElement('button');
    prevBtn.className = `px-3 py-1 text-xs font-semibold rounded border transition-all ${currentPage === 1 ? 'opacity-50 cursor-not-allowed bg-slate-50 border-slate-200 text-slate-400' : 'bg-white border-slate-300 text-slate-600 hover:bg-slate-50'}`;
    prevBtn.disabled = (currentPage === 1);
    prevBtn.innerHTML = '&laquo;';
    prevBtn.onclick = () => { if(currentPage > 1) { testLogsCurrentPage--; renderTestLogsPage(testLogsCurrentPage); } };
    container.appendChild(prevBtn);

    for (let i = startPage; i <= endPage; i++) {
        const btn = document.createElement('button');
        btn.className = `px-3 py-1 text-xs font-bold rounded border transition-all ${i === currentPage ? 'bg-brand-600 border-brand-600 text-white shadow-md' : 'bg-white border-slate-300 text-slate-600 hover:bg-slate-50'}`;
        btn.innerText = i;
        btn.onclick = () => { testLogsCurrentPage = i; renderTestLogsPage(i); };
        container.appendChild(btn);
    }

    // Next Button
    const nextBtn = document.createElement('button');
    nextBtn.className = `px-3 py-1 text-xs font-semibold rounded border transition-all ${currentPage === totalPages ? 'opacity-50 cursor-not-allowed bg-slate-50 border-slate-200 text-slate-400' : 'bg-white border-slate-300 text-slate-600 hover:bg-slate-50'}`;
    nextBtn.disabled = (currentPage === totalPages);
    nextBtn.innerHTML = '&raquo;';
    nextBtn.onclick = () => { if(currentPage < totalPages) { testLogsCurrentPage++; renderTestLogsPage(testLogsCurrentPage); } };
    container.appendChild(nextBtn);
}

function exportTestLogsToCSV() {
    if (allTestLogs.length === 0) {
        alert("No logs to export.");
        return;
    }

    const headers = ["Timestamp", "RTSP URL", "IP Address", "User Agent"];
    const rows = allTestLogs.map(log => [
        new Date(log.created_at).toISOString(),
        `"${log.url}"`,
        log.ip,
        `"${log.user_agent.replace(/"/g, '""')}"`
    ]);

    const csvContent = [headers, ...rows].map(e => e.join(",")).join("\n");
    const blob = new Blob([csvContent], { type: 'text/csv;charset=utf-8;' });
    const link = document.createElement("a");
    const url = URL.createObjectURL(blob);
    link.setAttribute("href", url);
    link.setAttribute("download", `rtsp_test_logs_${new Date().toISOString().split('T')[0]}.csv`);
    link.style.visibility = 'hidden';
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
}

// --- Waiting List (Interests) Management ---
let allWaitingList = [];
let waitingListCurrentPage = 1;
const waitingListPerPage = 10;

async function loadWaitingList() {
    try {
        const res = await fetch('/api/admin/interests');
        if (!res.ok) throw new Error('API Error');
        allWaitingList = await res.json() || [];
        waitingListCurrentPage = 1;
        renderWaitingListPage(1);
    } catch (e) {
        console.error("Failed to load waiting list", e);
        const body = document.getElementById('waitingListTableBody');
        if (body) {
            body.innerHTML = `<tr><td colspan="4" class="px-6 py-10 text-center text-red-500 font-bold">Failed to load data from server</td></tr>`;
        }
    }
}

function renderWaitingListPage(page) {
    waitingListCurrentPage = page;
    const body = document.getElementById('waitingListTableBody');
    if (!body) return;
    
    body.innerHTML = '';
    const start = (page - 1) * waitingListPerPage;
    const end = start + waitingListPerPage;
    const pageData = allWaitingList.slice(start, end);

    if (pageData.length === 0) {
        body.innerHTML = `<tr><td colspan="4" class="px-6 py-16 text-center text-slate-400">
            <div class="flex flex-col items-center gap-3">
                <svg class="w-12 h-12 text-slate-200" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path d="M3 8l7.89 5.26a2 2 0 002.22 0L21 8M5 19h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z"/></svg>
                <p class="font-medium">No registrations yet</p>
            </div>
        </td></tr>`;
    } else {
        pageData.forEach((item, index) => {
            const date = new Date(item.created_at).toLocaleString();
            const row = document.createElement('tr');
            row.className = "hover:bg-slate-50 dark:hover:bg-slate-800/30 transition-colors";
            row.innerHTML = `
                <td class="px-6 py-4 text-center font-mono text-slate-400 text-xs">${start + index + 1}</td>
                <td class="px-6 py-4">
                    <div class="flex items-center gap-3">
                        <div class="w-8 h-8 rounded-full bg-indigo-50 dark:bg-indigo-900/30 flex items-center justify-center text-indigo-600 dark:text-indigo-400">
                            <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path d="M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z"/></svg>
                        </div>
                        <span class="font-semibold text-slate-700 dark:text-slate-200">${item.email}</span>
                    </div>
                </td>
                <td class="px-6 py-4 text-slate-500 dark:text-slate-400 font-medium">${date}</td>
                <td class="px-6 py-4 text-right">
                    <span class="inline-flex items-center px-2 py-0.5 rounded text-xs font-bold bg-amber-50 text-amber-600 dark:bg-amber-900/20 dark:text-amber-400">Pending</span>
                </td>
            `;
            body.appendChild(row);
        });
    }

    // Update paging UI
    const totalPages = Math.ceil(allWaitingList.length / waitingListPerPage) || 1;
    const info = document.getElementById('waitingListPageInfo');
    if (info) {
        info.innerText = `Showing ${Math.min(start + 1, allWaitingList.length)} - ${Math.min(end, allWaitingList.length)} of ${allWaitingList.length} entries`;
    }

    const pagin = document.getElementById('waitingListPagination');
    if (pagin) {
        pagin.innerHTML = '';
        if (totalPages > 1) {
            for (let i = 1; i <= totalPages; i++) {
                const btn = document.createElement('button');
                btn.className = `w-8 h-8 rounded-lg flex items-center justify-center text-xs font-bold transition-all ${i === page ? 'bg-indigo-600 text-white shadow-lg shadow-indigo-500/30' : 'bg-white dark:bg-slate-800 text-slate-600 hover:bg-slate-100 dark:hover:bg-slate-700 border border-slate-200 dark:border-slate-700'}`;
                btn.innerText = i;
                btn.onclick = () => renderWaitingListPage(i);
                pagin.appendChild(btn);
            }
        }
    }
}

function exportWaitingListToCSV() {
    if (allWaitingList.length === 0) {
        alert("No data to export");
        return;
    }
    
    let csv = "ID,Email,Date\n";
    allWaitingList.forEach((item, index) => {
        csv += `${index + 1},${item.email},${new Date(item.created_at).toLocaleString()}\n`;
    });
    
    const blob = new Blob([csv], { type: 'text/csv' });
    const url = window.URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.setAttribute('href', url);
    a.setAttribute('download', `waiting_list_${new Date().toISOString().split('T')[0]}.csv`);
    a.click();
}
function exportProcessedStreams() {
    if (!allStreams || allStreams.length === 0) {
        alert("No camera data to export");
        return;
    }
    
    let csv = "name,url,lat,lng\n";
    const origin = window.location.origin;
    
    allStreams.forEach(s => {
        // Generate the processed playback URL
        const playbackUrl = `${origin}/rtc/stream.html?src=${encodeURIComponent(s.name)}&mode=mse,webrtc,hls,mp4,mjpeg`;
        
        // Escape name if it contains commas
        const safeName = s.name.includes(',') ? `"${s.name}"` : s.name;
        
        csv += `${safeName},${playbackUrl},${s.lat || 0},${s.lng || 0}\n`;
    });
    
    const blob = new Blob([csv], { type: 'text/csv' });
    const url = window.URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.setAttribute('href', url);
    a.setAttribute('download', `camera_links_export_${new Date().toISOString().split('T')[0]}.csv`);
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
}
async function refreshMyPublicToken() {
    if (!confirm("Re-generate your Public Hub token? This will break old links.")) return;
    try {
        const res = await fetch(`/api/users/token?id=${window.USER_ID}`, { method: 'POST' });
        if (res.ok) {
            const data = await res.json();
            window.USER_PUBLIC_TOKEN = data.public_token;
            showToast("Public Hub token re-generated", "success");
            
            // Sync with allUsers array so Manage User view is updated instantly
            const userIdx = allUsers.findIndex(u => parseInt(u.id) === parseInt(window.USER_ID));
            if (userIdx !== -1) {
                allUsers[userIdx].public_token = data.public_token;
            }
            
            // Re-render table if we are currently looking at it
            if (currentView === 'users') {
                renderUsersTable();
            }
        } else {
            showToast("Failed to re-generate token", "error");
        }
    } catch (e) {
        showToast("Network error", "error");
    }
}
function copyUserTokenLink(userId) {
    const u = allUsers.find(x => parseInt(x.id) === parseInt(userId));
    if (u && u.public_token) {
        copyToClipboard(`${window.location.origin}/view/${u.public_token}`);
    } else {
        showToast("No public token available", "error");
    }
}

function openUserHub(userId) {
    const u = allUsers.find(x => parseInt(x.id) === parseInt(userId));
    if (u && u.public_token) {
        window.open(`/view/${u.public_token}`, '_blank');
    } else {
        showToast("No public token available", "error");
    }
}

// --- NVR / Multi-Stream View Logic ---
let nvrGridSize    = 2;    // columns x rows
let nvrGridPage    = 0;    // current grid page (0-indexed)
let nvrSelectedIndex = 0;  // active slot for manual assign
let nvrIsFullscreen  = false;  // NVR fullscreen state

// All enabled streams for NVR (populated from allStreams)
let nvrCameraPool  = [];   // full sorted list for the grid paging

function initNVRView() {
    nvrGridPage = 0;
    nvrSelectedIndex = 0;
    _buildCameraPool();
    renderNVRGrid();
    renderNVRCameraList();
    _updateNVRGridPager();
}

// Build sorted pool from allStreams
function _buildCameraPool(query) {
    const q = (query || '').toLowerCase();
    nvrCameraPool = [...allStreams]
        .filter(s => s.enabled !== false)
        .filter(s => !q || s.name.toLowerCase().includes(q) || (s.display_name||'').toLowerCase().includes(q))
        .sort((a, b) => (b.online ? 1 : 0) - (a.online ? 1 : 0));
}

// ── Sidebar: full camera list for manual assign ────────────────────────────
function filterNVRCameras() {
    renderNVRCameraList();
}

function renderNVRCameraList() {
    const list  = document.getElementById('nvrCameraList');
    if (!list) return;

    const query  = (document.getElementById('nvrSearch')?.value || '').toLowerCase();
    const isDark = document.documentElement.classList.contains('dark');
    const pools  = [...allStreams]
        .filter(s => s.enabled !== false)
        .filter(s => !query || s.name.toLowerCase().includes(query) || (s.display_name||'').toLowerCase().includes(query))
        .sort((a, b) => (b.online ? 1 : 0) - (a.online ? 1 : 0));

    list.innerHTML = '';
    if (pools.length === 0) {
        list.innerHTML = '<div class="text-center py-8 text-slate-400 italic text-xs">No cameras</div>';
        return;
    }
    const countEl = document.getElementById('nvrDeviceCount');
    if (countEl) countEl.textContent = `${pools.length} device${pools.length !== 1 ? 's' : ''}`;

    pools.forEach(s => {
        const div = document.createElement('div');
        div.className = `p-2.5 rounded-lg cursor-pointer transition-all border border-transparent flex items-center gap-2 group ${isDark ? 'hover:bg-slate-800 text-slate-300' : 'hover:bg-slate-100 text-slate-700'}`;
        div.onclick = () => selectCameraForNVR(s);
        div.innerHTML = `
            <div class="w-6 h-6 rounded flex-shrink-0 flex items-center justify-center ${s.online ? (isDark?'text-brand-400':'text-brand-600') : (isDark?'text-slate-600':'text-slate-400')}">
                <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 10l4.553-2.276A1 1 0 0121 8.618v6.764a1 1 0 01-1.447.894L15 14M5 18h8a2 2 0 002-2V8a2 2 0 00-2-2H5a2 2 0 00-2 2v8a2 2 0 002 2z" /></svg>
            </div>
            <div class="flex-1 min-w-0">
                <p class="text-[10px] font-bold truncate">${s.display_name || s.name}</p>
                <div class="flex items-center gap-1">
                    <span class="w-1.5 h-1.5 rounded-full flex-shrink-0 ${s.online ? 'bg-green-500' : 'bg-slate-400'}"></span>
                    <p class="text-[9px] text-slate-500 truncate">${s.online ? 'Online' : 'Offline'}</p>
                </div>
            </div>`;
        list.appendChild(div);
    });
}

// ── Grid SIZE change ───────────────────────────────────────────────────────
function setNVRGrid(size) {
    nvrGridSize  = size;
    nvrGridPage  = 0;       // reset to first page on grid size change
    nvrSelectedIndex = 0;
    _buildCameraPool();
    renderNVRGrid();
    _updateNVRGridPager();

    document.querySelectorAll('.grid-btn').forEach(btn => {
        const active = parseInt(btn.dataset.size) === size;
        btn.classList.toggle('bg-slate-800', active);
        btn.classList.toggle('text-brand-400', active);
        btn.classList.toggle('text-slate-500', !active);
    });
}

// ── Grid PAGING ────────────────────────────────────────────────────────────
function _totalGridPages() {
    const cap = nvrGridSize * nvrGridSize;
    return Math.max(1, Math.ceil(nvrCameraPool.length / cap));
}

function _updateNVRGridPager() {
    const total   = _totalGridPages();
    const pageEl  = document.getElementById('nvrGridPageText');
    const prevBtn = document.getElementById('nvrGridPrevBtn');
    const nextBtn = document.getElementById('nvrGridNextBtn');
    const status  = document.getElementById('nvrStatusText');
    const cap     = nvrGridSize * nvrGridSize;

    if (pageEl)  pageEl.textContent = `${nvrGridPage + 1} / ${total}`;
    if (prevBtn) prevBtn.disabled   = nvrGridPage === 0;
    if (nextBtn) nextBtn.disabled   = nvrGridPage >= total - 1;

    // Active slot count on this page
    const startIdx  = nvrGridPage * cap;
    const pageSlice = nvrCameraPool.slice(startIdx, startIdx + cap);
    const active    = pageSlice.length;
    if (status) status.textContent = `${active} / ${cap} SLOTS  |  PAGE ${nvrGridPage + 1} / ${total}`;
}

function nvrGridPrev() {
    if (nvrGridPage > 0) {
        nvrGridPage--;
        renderNVRGrid();
        _updateNVRGridPager();
    }
}

function nvrGridNext() {
    if (nvrGridPage < _totalGridPages() - 1) {
        nvrGridPage++;
        renderNVRGrid();
        _updateNVRGridPager();
    }
}

// ── Render grid from camera pool ────────────────────────────────────────────
function renderNVRGrid() {
    const area = document.getElementById('nvrGridArea');
    if (!area) return;

    const cap       = nvrGridSize * nvrGridSize;
    const startIdx  = nvrGridPage * cap;
    const pageSlice = nvrCameraPool.slice(startIdx, startIdx + cap);

    area.innerHTML = '';
    area.style.display = 'grid';
    area.style.gridTemplateColumns = `repeat(${nvrGridSize}, 1fr)`;
    area.style.gridTemplateRows    = `repeat(${nvrGridSize}, 1fr)`;
    area.style.gap    = '2px';
    area.style.height = '100%';

    for (let i = 0; i < cap; i++) {
        const s = pageSlice[i] || null;
        const slot = document.createElement('div');
        const isActive = nvrSelectedIndex === i;

        slot.className = [
            'nvr-cell relative group overflow-hidden flex items-center justify-center transition-all duration-150 cursor-crosshair',
            'bg-slate-300 dark:bg-slate-900',
            isActive ? 'ring-2 ring-inset ring-brand-500 z-10' : '',
        ].join(' ');
        slot.onclick = () => { nvrSelectedIndex = i; renderNVRGrid(); };

        if (s) {
            const globalIdx = startIdx + i;
            slot.innerHTML = `
                <div class="absolute inset-0 z-0" style="overflow:hidden;">
                    <iframe
                        src="/rtc/stream.html?src=${encodeURIComponent(s.name)}"
                        style="width:100%;height:calc(100% + 44px);margin-top:-44px;border:none;display:block;"
                        allow="autoplay; fullscreen"
                        scrolling="no"
                    ></iframe>
                </div>
                <div class="absolute bottom-0 left-0 right-0 z-10 px-2 py-1 bg-gradient-to-t from-black/80 to-transparent opacity-0 group-hover:opacity-100 transition-opacity pointer-events-none">
                    <p class="text-[9px] font-black text-white uppercase tracking-tighter truncate">${s.display_name || s.name}</p>
                </div>
                <div class="absolute top-1 right-1 z-10 flex gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
                    <button onclick="removeNVRSlot(${i}, event)" class="p-1 bg-red-500/90 hover:bg-red-500 text-white rounded">
                        <svg class="w-2.5 h-2.5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M6 18L18 6M6 6l12 12" /></svg>
                    </button>
                </div>
                ${isActive ? '<div class="absolute inset-0 ring-2 ring-inset ring-brand-500 pointer-events-none z-20"></div>' : ''}
            `;
        } else {
            slot.innerHTML = `
                <div class="flex flex-col items-center gap-1 pointer-events-none select-none ${'text-slate-400 dark:text-slate-700'} group-hover:text-slate-500 transition-colors">
                    <svg class="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M12 4v16m8-8H4" /></svg>
                    <span class="text-[8px] font-black uppercase tracking-widest">Empty</span>
                </div>
                ${isActive ? '<div class="absolute inset-0 ring-2 ring-inset ring-brand-500 pointer-events-none"></div>' : ''}
            `;
        }
        area.appendChild(slot);
    }
}

// Remove a camera from the pool at a specific page slot
function removeNVRSlot(slotIdx, e) {
    if (e) e.stopPropagation();
    const globalIdx = nvrGridPage * (nvrGridSize * nvrGridSize) + slotIdx;
    if (globalIdx < nvrCameraPool.length) {
        nvrCameraPool.splice(globalIdx, 1);
    }
    renderNVRGrid();
    _updateNVRGridPager();
}

// Manual assign: put camera into selected slot on this page
function selectCameraForNVR(camera) {
    const cap       = nvrGridSize * nvrGridSize;
    const globalIdx = nvrGridPage * cap + nvrSelectedIndex;

    // Replace or insert
    if (globalIdx < nvrCameraPool.length) {
        nvrCameraPool[globalIdx] = camera;
    } else {
        // fill gaps with nulls then insert
        while (nvrCameraPool.length < globalIdx) nvrCameraPool.push(null);
        nvrCameraPool[globalIdx] = camera;
    }

    // Move to next slot
    nvrSelectedIndex = (nvrSelectedIndex + 1) % cap;
    renderNVRGrid();
    _updateNVRGridPager();
}

function clearNVRGrid() {
    nvrCameraPool    = [];
    nvrGridPage      = 0;
    nvrSelectedIndex = 0;
    renderNVRGrid();
    _updateNVRGridPager();
}

// ── Fullscreen ─────────────────────────────────────────────────────────────
function toggleNVRFullscreen() {
    const view    = document.getElementById('view-nvr');
    const sidebar = view ? view.querySelector('.nvr-sidebar') : null;
    if (!view) return;

    if (!nvrIsFullscreen) {
        // Enter fullscreen
        const req = view.requestFullscreen || view.webkitRequestFullscreen || view.mozRequestFullscreen;
        if (req) req.call(view);
        view.classList.add('nvr-fullscreen');
        nvrIsFullscreen = true;
        _updateFullscreenBtn(true);
    } else {
        _exitNVRFullscreen();
    }
}

function _exitNVRFullscreen() {
    const view    = document.getElementById('view-nvr');
    const sidebar = view ? view.querySelector('.nvr-sidebar') : null;
    if (document.fullscreenElement || document.webkitFullscreenElement) {
        try { (document.exitFullscreen || document.webkitExitFullscreen).call(document); } catch(e) {}
    }
    if (view)    view.classList.remove('nvr-fullscreen');
    if (sidebar) sidebar.classList.remove('nvr-sidebar-peek');
    nvrIsFullscreen = false;
    _updateFullscreenBtn(false);
}

function _updateFullscreenBtn(isFs) {
    const btn   = document.getElementById('nvrFullscreenBtn');
    const iconIn  = document.getElementById('nvrFsIconIn');
    const iconOut = document.getElementById('nvrFsIconOut');
    if (!btn) return;
    if (isFs) {
        if (iconIn)  iconIn.classList.add('hidden');
        if (iconOut) iconOut.classList.remove('hidden');
        btn.title = 'Exit Fullscreen';
    } else {
        if (iconIn)  iconIn.classList.remove('hidden');
        if (iconOut) iconOut.classList.add('hidden');
        btn.title = 'Fullscreen';
    }
}

// Reveal sidebar on hover (always, even outside fullscreen)
function nvrSidebarPeek(show) {
    const sidebar = document.querySelector('#view-nvr .nvr-sidebar');
    if (!sidebar) return;
    if (show) {
        sidebar.classList.add('nvr-sidebar-peek');
    } else {
        sidebar.classList.remove('nvr-sidebar-peek');
    }
}

// Listen for native fullscreen exit (ESC key / browser UI)
document.addEventListener('fullscreenchange', () => {
    if (!document.fullscreenElement && nvrIsFullscreen) {
        _exitNVRFullscreen();
    }
});
document.addEventListener('webkitfullscreenchange', () => {
    if (!document.webkitFullscreenElement && nvrIsFullscreen) {
        _exitNVRFullscreen();
    }
});

async function autoPlayAllNVR() {
    const btn  = document.getElementById('nvrAutoPlayBtn');
    const orig = btn.textContent.trim();
    btn.textContent = '...';
    btn.disabled    = true;

    _buildCameraPool();   // rebuild from allStreams (online first)
    nvrGridPage = 0;
    nvrSelectedIndex = 0;

    renderNVRGrid();
    _updateNVRGridPager();

    setTimeout(() => {
        btn.textContent = orig;
        btn.disabled    = false;
        showToast(`Loaded ${nvrCameraPool.length} cameras across ${_totalGridPages()} pages`);
    }, 800);
}
