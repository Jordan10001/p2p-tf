// P2P File Transfer Application JavaScript Controller

let socket = null;
let currentSettings = {};
let selectedDevices = new Set();
let localQueue = [];
let activeTransfers = {};
let activeTargetDevices = [];
let sendModalReady = false;
let selectedPaths = [];

function openSendModal(deviceIds, deviceNamesStr, ipStr) {
    activeTargetDevices = deviceIds;
    sendModalReady = true;
    selectedPaths = [];
    
    // Reset selected items UI
    updateSelectedItemsUI([]);

    const modal = document.getElementById('send-modal');
    if (modal) {
        const nameEl = document.getElementById('send-modal-device-name');
        const detailsEl = document.getElementById('send-modal-device-details');
        if (nameEl) nameEl.textContent = deviceNamesStr || 'Device';
        if (detailsEl) detailsEl.textContent = ipStr || '';
        modal.classList.remove('hidden');
        modal.classList.add('flex');
    }
}

function closeSendModal() {
    activeTargetDevices = [];
    sendModalReady = false;
    const modal = document.getElementById('send-modal');
    if (modal) {
        modal.classList.add('hidden');
        modal.classList.remove('flex');
    }
    fetch('/api/clear-queue', { method: 'POST' }).catch(() => {});
}

function updateSelectedItemsUI(files) {
    const placeholder = document.getElementById('modal-selected-placeholder');
    const fileList = document.getElementById('modal-selected-files-list');
    const sizeSpan = document.getElementById('modal-selected-size');
    const sendBtn = document.getElementById('modal-send-btn');

    if (!fileList || !placeholder || !sizeSpan || !sendBtn) return;

    if (!files || files.length === 0) {
        placeholder.classList.remove('hidden');
        fileList.classList.add('hidden');
        fileList.innerHTML = '';
        sizeSpan.textContent = '0 Bytes';
        sendBtn.disabled = true;
        return;
    }

    placeholder.classList.add('hidden');
    fileList.classList.remove('hidden');
    fileList.innerHTML = '';

    let totalBytes = 0;
    files.forEach(file => {
        totalBytes += file.size;
        
        const row = document.createElement('div');
        row.className = 'flex items-center justify-between p-3.5 rounded-2xl bg-slate-950/60 border border-white/5';
        row.innerHTML = `
            <div class="flex items-center space-x-3 truncate">
                <div class="p-2 text-indigo-400 bg-indigo-500/10 rounded-xl flex-shrink-0">
                    <svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M7 21h10a2 2 0 002-2V9.414a1 1 0 00-.293-.707l-5.414-5.414A1 1 0 0012.586 3H7a2 2 0 00-2 2v14a2 2 0 002 2z"/>
                    </svg>
                </div>
                <div class="truncate text-left">
                    <p class="text-sm font-bold text-white truncate" title="${file.path}">${file.name}</p>
                    <p class="text-[11px] text-slate-500 mt-0.5">${formatBytes(file.size)}</p>
                </div>
            </div>
        `;
        fileList.appendChild(row);
    });

    sizeSpan.textContent = formatBytes(totalBytes);
    sendBtn.disabled = false;
}

// On Page Load
document.addEventListener('DOMContentLoaded', () => {
    // 1. Switch Sidebar Tabs
    initSidebar();

    // 3. Load Current Config
    loadSettings();

    // 4. Initialize WebSockets
    initWebSocket();

    // 5. Setup Click Listeners
    initClickHandlers();
});

// Sidebar Navigation
function initSidebar() {
    const menuItems = document.querySelectorAll('.sidebar-item');
    const panels = document.querySelectorAll('.content-panel');

    // Restore last active tab from localStorage if exists
    const activeTab = localStorage.getItem('activeTab') || 'dashboard-panel';
    menuItems.forEach(item => {
        const targetPanel = item.getAttribute('data-target');
        if (targetPanel === activeTab) {
            item.classList.add('active', 'bg-indigo-600', 'text-white', 'shadow-lg', 'shadow-indigo-600/15');
            item.classList.remove('text-slate-400', 'hover:text-slate-200', 'hover:bg-white/5');
        } else {
            item.classList.remove('active', 'bg-indigo-600', 'text-white', 'shadow-lg', 'shadow-indigo-600/15');
            item.classList.add('text-slate-400', 'hover:text-slate-200', 'hover:bg-white/5');
        }
    });
    panels.forEach(panel => {
        if (panel.id === activeTab) {
            panel.classList.remove('hidden');
        } else {
            panel.classList.add('hidden');
        }
    });

    menuItems.forEach(item => {
        item.addEventListener('click', () => {
            const targetPanel = item.getAttribute('data-target');
            localStorage.setItem('activeTab', targetPanel);
            
            menuItems.forEach(i => {
                i.classList.remove('active', 'bg-indigo-600', 'text-white', 'shadow-lg', 'shadow-indigo-600/15');
                i.classList.add('text-slate-400', 'hover:text-slate-200', 'hover:bg-white/5');
            });
            
            item.classList.add('active', 'bg-indigo-600', 'text-white', 'shadow-lg', 'shadow-indigo-600/15');
            item.classList.remove('text-slate-400', 'hover:text-slate-200', 'hover:bg-white/5');

            panels.forEach(panel => {
                if (panel.id === targetPanel) {
                    panel.classList.remove('hidden');
                } else {
                    panel.classList.add('hidden');
                }
            });
        });
    });
}

// Load Settings from API
async function loadSettings() {
    try {
        const res = await fetch('/api/settings');
        const settings = await res.json();
        currentSettings = settings;



        // Fill form fields in Settings Tab
        const devNameInput = document.getElementById('settings-device-name');
        const hostInput = document.getElementById('settings-hostname');
        const portInput = document.getElementById('settings-port');
        const downloadDirInput = document.getElementById('settings-download-dir');
        
        if (devNameInput) devNameInput.value = settings.deviceName || '';
        if (hostInput) hostInput.value = settings.hostname || '';
        if (portInput) portInput.value = settings.transferPort || 50005;
        if (downloadDirInput) downloadDirInput.value = settings.downloadDir || '';
        
        const showNotifInput = document.getElementById('settings-show-notifications');
        const showCompNotifInput = document.getElementById('settings-show-complete-notifications');
        const autoOpenFolderInput = document.getElementById('settings-auto-open-folder');
        const autoAcceptInput = document.getElementById('settings-auto-accept');
        const enableDiscInput = document.getElementById('settings-enable-discovery');
        const autoScanInput = document.getElementById('settings-auto-scan');
        const minimizeTrayInput = document.getElementById('settings-minimize-tray');
        const startWindowsInput = document.getElementById('settings-start-windows');

        if (showNotifInput) showNotifInput.checked = settings.showNotifications;
        if (showCompNotifInput) showCompNotifInput.checked = settings.showTransferCompleteNotification;
        if (autoOpenFolderInput) autoOpenFolderInput.checked = settings.autoOpenFolder;
        if (autoAcceptInput) autoAcceptInput.checked = settings.autoAccept;
        if (enableDiscInput) enableDiscInput.checked = settings.enableDiscovery;
        if (autoScanInput) autoScanInput.checked = settings.autoScan;
        if (minimizeTrayInput) minimizeTrayInput.checked = settings.minimizeToTray;
        if (startWindowsInput) startWindowsInput.checked = settings.startWithWindows;

        // Update My Device Name Card
        const localDeviceName = document.getElementById('local-device-name');
        if (localDeviceName) localDeviceName.textContent = settings.deviceName;
        const localDeviceIp = document.getElementById('local-device-ip');
        if (localDeviceIp) {
            localDeviceIp.textContent = settings.localIp ? `${settings.localIp}:${settings.transferPort}` : '';
        }
        
    } catch (err) {
        console.error('Failed to load settings:', err);
    }
}

// Save Settings to API
async function saveSettings(settingsObj) {
    try {
        const res = await fetch('/api/settings', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(settingsObj)
        });
        const data = await res.json();
        showToast('success', 'Configuration updated successfully');
        loadSettings();
    } catch (err) {
        showToast('error', 'Failed to save settings');
    }
}

let autoScanInterval = null;

function startAutoScanTimer() {
    if (autoScanInterval) clearInterval(autoScanInterval);
    autoScanInterval = setInterval(() => {
        if (currentSettings && currentSettings.autoScan) {
            const dashboardPanel = document.getElementById('dashboard-panel');
            if (dashboardPanel && !dashboardPanel.classList.contains('hidden')) {
                fetch('/api/scan', { method: 'POST' }).catch(() => {});
            }
        }
    }, 10000); // Auto-scan every 10 seconds if on dashboard
}

// WebSocket connection
function initWebSocket() {
    const host = window.location.hostname || 'localhost';
    const wsUrl = `ws://${host}:8081/ws`;

    socket = new WebSocket(wsUrl);

    socket.onopen = () => {
        console.log('WebSocket connection established');
        updateDiscoveryStatus(true, 0);
        // Automatically trigger a network scan when the frontend connects
        fetch('/api/scan', { method: 'POST' }).catch(() => {});
        startAutoScanTimer();
    };

    socket.onmessage = (event) => {
        const wsMsg = JSON.parse(event.data);
        handleWSMessage(wsMsg);
    };

    socket.onclose = () => {
        console.warn('WebSocket connection lost, reconnecting...');
        updateDiscoveryStatus(false, 0);
        setTimeout(initWebSocket, 3000);
    };

    socket.onerror = (err) => {
        console.error('WebSocket error:', err);
    };
}

function handleWSMessage(msg) {
    switch (msg.type) {
        case 'device_list':
            renderDevices(msg.payload);
            break;
        case 'queue_update':
            localQueue = msg.payload;
            renderQueue(msg.payload);
            break;
        case 'transfer_update':
            handleTransferUpdate(msg.payload);
            break;
        case 'incoming_request':
            showIncomingRequestModal(msg.payload);
            break;
        case 'history_update':
            renderHistory(msg.payload);
            break;
        case 'config_update':
            currentSettings = msg.payload;
            break;
        case 'toast':
            showToast(msg.payload.type, msg.payload.message);
            break;
    }
}

// Render Devices list on Dashboard
function renderDevices(devices) {
    const devicesContainer = document.getElementById('devices-grid');
    const emptyState = document.getElementById('empty-devices-state');

    if (!devicesContainer) return;

    // Filter out offline devices
    const onlineDevices = devices.filter(d => d.status !== 'offline');

    if (onlineDevices.length === 0) {
        devicesContainer.classList.add('hidden');
        if (emptyState) emptyState.classList.remove('hidden');
        updateDiscoveryStatus(true, 0);
    } else {
        if (emptyState) emptyState.classList.add('hidden');
        devicesContainer.classList.remove('hidden');
        updateDiscoveryStatus(true, onlineDevices.length);

        devicesContainer.innerHTML = '';
        onlineDevices.forEach(device => {
            const card = document.createElement('div');
            card.className = `glass-panel rounded-3xl p-6 flex flex-col justify-between hover-scale relative transition-all border border-white/5`;

            card.innerHTML = `
                <div class="flex items-start justify-between">
                    <div class="flex items-center space-x-3.5">
                        <div class="p-3 bg-gradient-to-tr from-indigo-500/10 to-purple-500/10 text-indigo-400 rounded-2xl border border-indigo-500/10">
                            <svg class="w-5.5 h-5.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9.75 17L9 20l-1 1h8l-1-1-.75-3M3 13h18M5 17h14a2 2 0 002-2V5a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z"/>
                            </svg>
                        </div>
                        <div>
                            <h3 class="font-bold text-base text-white tracking-tight">${device.name}</h3>
                            <p class="text-xs text-slate-400 mt-0.5">${device.hostname}</p>
                            <p class="text-[10px] font-mono text-slate-500 mt-2 bg-slate-950/60 px-2.5 py-1 rounded-xl border border-white/5 inline-block">${device.ip}:${device.port}</p>
                        </div>
                    </div>
                </div>
                <div class="mt-6 flex items-center justify-between">
                    <div class="flex items-center space-x-2">
                        <span class="w-2 h-2 bg-green-500 rounded-full inline-block animate-ping"></span>
                        <span class="text-[10px] font-bold text-green-400 capitalize tracking-wider">${device.status}</span>
                    </div>
                    <button class="px-4 py-2 bg-indigo-600 hover:bg-indigo-500 text-white text-xs font-bold rounded-xl transition-all shadow-md shadow-indigo-600/10 hover:scale-105 active:scale-95 quick-send-btn" data-id="${device.id}">
                        Send Files
                    </button>
                </div>
            `;

            devicesContainer.appendChild(card);
        });

        // Add Quick Send button handler
        document.querySelectorAll('.quick-send-btn').forEach(btn => {
            btn.addEventListener('click', () => {
                const id = btn.getAttribute('data-id');
                const card = btn.closest('.glass-panel');
                const name = card && card.querySelector('h3') ? card.querySelector('h3').textContent : 'Device';
                const detailsEl = card ? card.querySelector('.font-mono') : null;
                const details = detailsEl ? detailsEl.textContent : '';
                openSendModal([id], name, details);
            });
        });
    }
}

function updateDiscoveryStatus(active, count) {
    const dot = document.getElementById('discovery-dot');
    const text = document.getElementById('discovery-status-text');

    if (!text) return;

    if (active) {
        if (dot) {
            dot.className = 'w-2.5 h-2.5 bg-green-500 rounded-full inline-block pulse-dot mr-2';
        }
        text.textContent = count > 0 ? `Discovery Active (${count} device${count > 1 ? 's' : ''} found)` : 'Discovery Active (No devices found)';
    } else {
        if (dot) {
            dot.className = 'w-2.5 h-2.5 bg-yellow-500 rounded-full inline-block pulse-dot-yellow mr-2';
        }
        text.textContent = 'Disconnected';
    }
}

// Render Queue List
function renderQueue(queue) {
    const queueList = document.getElementById('modal-queue-list');
    const queueContainer = document.getElementById('modal-queue-container');
    const totalFiles = document.getElementById('modal-total-files');
    const totalSize = document.getElementById('modal-total-size');
    const sendBtn = document.getElementById('modal-send-btn');

    if (!queue || !Array.isArray(queue)) {
        queue = [];
    }

    if (!queueList) return;

    // Update totals
    if (totalFiles) totalFiles.textContent = queue.length;
    if (totalSize) {
        const bytes = queue.reduce((acc, item) => acc + item.size, 0);
        totalSize.textContent = formatBytes(bytes);
    }
    if (sendBtn) {
        sendBtn.disabled = queue.length === 0;
    }

    if (queue.length === 0) {
        if (queueContainer) queueContainer.classList.add('hidden');
        queueList.innerHTML = '';
        return;
    }

    if (queueContainer) queueContainer.classList.remove('hidden');
    queueList.innerHTML = '';

    queue.forEach((item, index) => {
        const row = document.createElement('div');
        row.className = 'flex items-center justify-between p-3.5 rounded-2xl bg-slate-950/60 border border-white/5 hover:bg-slate-950/80 transition-all';
        row.innerHTML = `
            <div class="flex items-center space-x-3 truncate">
                <div class="p-2 text-indigo-400 bg-indigo-500/10 rounded-xl flex-shrink-0">
                    ${item.type === 'folder' ? 
                        `<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M3 7v10a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2h-6l-2-2H5a2 2 0 00-2 2z"/></svg>` :
                        `<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M7 21h10a2 2 0 002-2V9.414a1 1 0 00-.293-.707l-5.414-5.414A1 1 0 0012.586 3H7a2 2 0 00-2 2v14a2 2 0 002 2z"/></svg>`
                    }
                </div>
                <div class="truncate text-left">
                    <p class="text-sm font-bold text-white truncate" title="${item.path}">${item.name}</p>
                    <p class="text-[11px] text-slate-500 mt-0.5">${formatBytes(item.size)} • <span class="capitalize">${item.type}</span></p>
                </div>
            </div>
            <button class="p-1 text-gray-500 hover:text-red-500 transition-colors remove-queue-btn" data-index="${index}">
                <svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"/>
                </svg>
            </button>
        `;
        queueList.appendChild(row);
    });

    // Remove single item listener
    document.querySelectorAll('.remove-queue-btn').forEach(btn => {
        btn.addEventListener('click', (e) => {
            const index = parseInt(btn.getAttribute('data-index'));
            fetch('/api/remove-queue', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ index: index })
            })
            .then(res => res.json())
            .then(data => {
                if (data.queue) {
                    localQueue = data.queue;
                    renderQueue(data.queue);
                }
            });
        });
    });
}



// Handle real-time transfer updates from WebSocket
function handleTransferUpdate(transfer) {
    activeTransfers[transfer.id] = transfer;
    renderTransfers();

    // Check if finished and triggers alert
    if (transfer.status === 'completed' || transfer.status === 'failed' || transfer.status === 'cancelled') {
        delete activeTransfers[transfer.id];
        setTimeout(renderTransfers, 3000); // clear from list after 3s
    }
}

// Render Transfers
function renderTransfers() {
    const activeContainer = document.getElementById('active-transfers-list');
    const transfersPanelList = document.getElementById('transfers-panel-list');
    const noTransfersMsg = document.getElementById('no-transfers-msg');

    const transfersArray = Object.values(activeTransfers);

    if (transfersArray.length === 0) {
        if (activeContainer) activeContainer.innerHTML = '<p class="text-sm text-slate-555 italic">No active transfers running</p>';
        if (transfersPanelList) transfersPanelList.innerHTML = '';
        if (noTransfersMsg) noTransfersMsg.classList.remove('hidden');
        return;
    }

    if (noTransfersMsg) noTransfersMsg.classList.add('hidden');

    const buildHtml = (t) => {
        const isSend = t.direction === 'send';
        const progressPercent = Math.round(t.progress || 0);
        
        let directionIcon = '';
        if (isSend) {
            directionIcon = `<span class="p-2 text-indigo-400 bg-indigo-500/10 rounded-lg"><svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 10l7-7m0 0l7 7m-7-7v18"/></svg></span>`;
        } else {
            directionIcon = `<span class="p-2 text-green-400 bg-green-500/10 rounded-lg"><svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 14l-7 7m0 0l-7-7m7 7V3"/></svg></span>`;
        }

        let fileNameLabel = '';
        if (t.files.length === 1) {
            fileNameLabel = t.files[0].name;
        } else {
            fileNameLabel = `${t.files[0].name} and ${t.files.length - 1} other files`;
        }

        let statusBadge = '';
        let badgeColor = 'bg-slate-800/40 text-slate-400 border border-white/5';
        switch (t.status) {
            case 'sending': badgeColor = 'bg-indigo-500/10 text-indigo-400 border border-indigo-500/20'; break;
            case 'receiving': badgeColor = 'bg-green-500/10 text-green-400 border border-green-500/20'; break;
            case 'completed': badgeColor = 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/20'; break;
            case 'failed': badgeColor = 'bg-rose-500/10 text-rose-400 border border-rose-500/20'; break;
            case 'cancelled': badgeColor = 'bg-amber-500/10 text-amber-400 border border-amber-500/20'; break;
            case 'waiting': badgeColor = 'bg-yellow-500/10 text-yellow-400 border border-yellow-500/20 animate-pulse'; break;
            case 'preparing': badgeColor = 'bg-purple-500/10 text-purple-400 border border-purple-500/20 animate-pulse'; break;
        }
        statusBadge = `<span class="px-2.5 py-1 text-[10px] font-bold rounded-full ${badgeColor} capitalize tracking-wider">${t.status}</span>`;

        let speedText = '';
        if (t.status === 'sending' || t.status === 'receiving') {
            speedText = `${formatSpeed(t.speed)} • ETA: ${formatETA(t.eta)}`;
        } else if (t.error) {
            speedText = `<span class="text-rose-500 text-xs">${t.error}</span>`;
        } else {
            speedText = t.status;
        }

        const canCancel = t.status === 'sending' || t.status === 'receiving' || t.status === 'waiting' || t.status === 'preparing';

        return `
            <div class="p-5 rounded-3xl border border-white/5 glass-panel">
                <div class="flex items-center justify-between mb-4">
                    <div class="flex items-center space-x-3.5">
                        ${directionIcon}
                        <div>
                            <h4 class="text-sm font-bold text-white tracking-tight truncate max-w-xs md:max-w-md">${fileNameLabel}</h4>
                            <p class="text-xs text-slate-500">${isSend ? 'To' : 'From'} ${t.peerName} (${t.peerIp}) • ${formatBytes(t.totalSize)}</p>
                        </div>
                    </div>
                    <div class="flex items-center space-x-2">
                        ${statusBadge}
                        ${canCancel ? `
                            <button class="px-3.5 py-1.5 bg-rose-600/10 border border-rose-500/20 hover:bg-rose-600 hover:text-white text-rose-450 text-xs font-bold rounded-xl transition-all cancel-transfer-btn active:scale-95" data-id="${t.id}">
                                Cancel
                            </button>
                        ` : ''}
                    </div>
                </div>
                <div class="space-y-2">
                    <div class="flex items-center justify-between text-xs text-slate-400">
                        <span>${speedText}</span>
                        <span class="font-bold">${progressPercent}%</span>
                    </div>
                    <div class="w-full bg-slate-950 rounded-full h-2 border border-white/5">
                        <div class="bg-gradient-to-r from-indigo-500 via-purple-500 to-pink-500 h-2 rounded-full transition-all duration-300" style="width: ${progressPercent}%"></div>
                    </div>
                </div>
            </div>
        `;
    };

    // Render on Dashboard active list
    if (activeContainer) {
        activeContainer.innerHTML = '';
        transfersArray.slice(0, 3).forEach(t => {
            const tempDiv = document.createElement('div');
            tempDiv.innerHTML = buildHtml(t);
            activeContainer.appendChild(tempDiv.firstElementChild);
        });
    }

    // Render on dedicated Transfers Tab
    if (transfersPanelList) {
        transfersPanelList.innerHTML = '';
        transfersArray.forEach(t => {
            const tempDiv = document.createElement('div');
            tempDiv.innerHTML = buildHtml(t);
            transfersPanelList.appendChild(tempDiv.firstElementChild);
        });
    }

    // Register Cancel handlers
    document.querySelectorAll('.cancel-transfer-btn').forEach(btn => {
        btn.addEventListener('click', () => {
            const id = btn.getAttribute('data-id');
            fetch('/api/cancel-transfer', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ transferId: id })
            });
        });
    });
}

// Incoming Request Modal popup handler
function showIncomingRequestModal(request) {
    const modal = document.getElementById('incoming-modal');
    const senderSpan = document.getElementById('incoming-sender');
    const sizeSpan = document.getElementById('incoming-size');
    const filesList = document.getElementById('incoming-files-list');

    if (!modal) return;

    if (senderSpan) senderSpan.textContent = `${request.peerName} (${request.peerIp})`;
    if (sizeSpan) sizeSpan.textContent = formatBytes(request.totalSize);

    if (filesList) {
        filesList.innerHTML = '';
        request.files.slice(0, 5).forEach(file => {
            const li = document.createElement('li');
            li.className = 'text-xs text-slate-400 py-1 border-b border-white/5 flex justify-between';
            li.innerHTML = `<span>${file.name}</span> <span class="text-slate-500">${formatBytes(file.size)}</span>`;
            filesList.appendChild(li);
        });

        if (request.files.length > 5) {
            const li = document.createElement('li');
            li.className = 'text-xs text-slate-500 italic py-1';
            li.textContent = `...and ${request.files.length - 5} more files`;
            filesList.appendChild(li);
        }
    }

    modal.classList.remove('hidden');
    modal.classList.add('flex');

    const acceptBtn = document.getElementById('incoming-accept-btn');
    const rejectBtn = document.getElementById('incoming-reject-btn');

    const handleChoice = (accepted) => {
        fetch(accepted ? '/api/accept-transfer' : '/api/reject-transfer', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ transferId: request.transferId })
        })
        .finally(() => {
            modal.classList.add('hidden');
            modal.classList.remove('flex');
        });
    };

    if (acceptBtn) acceptBtn.onclick = () => handleChoice(true);
    if (rejectBtn) rejectBtn.onclick = () => handleChoice(false);
}

// Render History Log List
async function loadHistory() {
    try {
        const res = await fetch('/api/history');
        const history = await res.json();
        renderHistory(history);
    } catch (err) {
        console.error('Failed to load history:', err);
    }
}

function renderHistory(historyList) {
    const historyBody = document.getElementById('history-table-body');
    const dashboardHistory = document.getElementById('recent-history-list');

    if (historyBody) {
        if (historyList.length === 0) {
            historyBody.innerHTML = `
                <tr>
                    <td colspan="7" class="text-center py-8 text-sm text-slate-500 italic bg-slate-900/10">No transfer history recorded</td>
                </tr>
            `;
        } else {
            historyBody.innerHTML = '';
            historyList.forEach(entry => {
                const tr = document.createElement('tr');
                tr.className = 'border-b border-white/5 hover:bg-white/5 transition-all';
                
                let statusColor = 'text-emerald-400 bg-emerald-500/10 border border-emerald-500/20';
                if (entry.status === 'failed') statusColor = 'text-rose-400 bg-rose-500/10 border border-rose-500/20';
                if (entry.status === 'cancelled') statusColor = 'text-amber-400 bg-amber-500/10 border border-amber-500/20';

                // Determine if received successfully
                const isReceiver = entry.receiver === currentSettings.deviceName;
                const canOpen = isReceiver && entry.status === 'completed';

                tr.innerHTML = `
                    <td class="py-4 px-6 text-sm font-medium text-white truncate max-w-xs" title="${entry.fileName}">${entry.fileName}</td>
                    <td class="py-4 px-6 text-sm text-slate-400">${entry.sender}</td>
                    <td class="py-4 px-6 text-sm text-slate-400">${entry.receiver}</td>
                    <td class="py-4 px-6 text-sm text-slate-400 font-mono">${formatBytes(entry.size)}</td>
                    <td class="py-4 px-6 text-sm text-slate-400">${new Date(entry.timestamp).toLocaleString()}</td>
                    <td class="py-4 px-6">
                        <span class="px-2.5 py-1 text-[10px] font-bold rounded-full ${statusColor} capitalize tracking-wider">${entry.status}</span>
                    </td>
                    <td class="py-4 px-6 text-right">
                        ${canOpen ? `
                            <button class="px-3.5 py-1.5 bg-indigo-600/10 border border-indigo-500/20 hover:bg-indigo-600 hover:text-white text-indigo-400 text-xs font-bold rounded-xl transition-all open-folder-inline-btn active:scale-95">
                                Open Folder
                            </button>
                        ` : '-'}
                    </td>
                `;

                const btn = tr.querySelector('.open-folder-inline-btn');
                if (btn) {
                    btn.addEventListener('click', () => {
                        fetch('/api/open-folder', { method: 'POST' });
                    });
                }

                historyBody.appendChild(tr);
            });
        }
    }
}

// Click Listeners
function initClickHandlers() {
    const closeBtn = document.getElementById('send-modal-close-btn');
    if (closeBtn) closeBtn.addEventListener('click', closeSendModal);
    
    const cancelBtn = document.getElementById('modal-cancel-btn');
    if (cancelBtn) cancelBtn.addEventListener('click', closeSendModal);

    // Select & Send Files (Native Dialog)
    const selectFilesBtn = document.getElementById('modal-select-files-btn');
    if (selectFilesBtn) {
        selectFilesBtn.addEventListener('click', () => {
            fetch('/api/select-files', { method: 'POST' })
            .then(res => res.json())
            .then(data => {
                if (data.error) {
                    showToast('error', data.error);
                } else if (data.files && data.files.length > 0) {
                    selectedPaths = data.files.map(f => f.path);
                    updateSelectedItemsUI(data.files);
                }
            })
            .catch(() => {
                showToast('error', 'Failed to open file picker');
            });
        });
    }

    // Select & Send Folder (Native Dialog)
    const selectFolderBtn = document.getElementById('modal-select-folder-btn');
    if (selectFolderBtn) {
        selectFolderBtn.addEventListener('click', () => {
            fetch('/api/select-folder', { method: 'POST' })
            .then(res => res.json())
            .then(data => {
                if (data.error) {
                    showToast('error', data.error);
                } else if (data.files && data.files.length > 0) {
                    selectedPaths = data.files.map(f => f.path);
                    updateSelectedItemsUI(data.files);
                }
            })
            .catch(() => {
                showToast('error', 'Failed to open folder picker');
            });
        });
    }

    // Send Files Button in Modal Footer
    const modalSendBtn = document.getElementById('modal-send-btn');
    if (modalSendBtn) {
        modalSendBtn.addEventListener('click', () => {
            if (selectedPaths.length === 0) return;
            
            showToast('info', 'Starting direct transfer...');
            
            fetch('/api/send-direct', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    deviceIds: activeTargetDevices,
                    paths: selectedPaths
                })
            })
            .then(res => res.json())
            .then(sendRes => {
                if (sendRes.status === 'ok') {
                    showToast('success', 'Transfer started successfully!');
                    closeSendModal();
                } else {
                    showToast('error', 'Failed to start direct transfer');
                }
            })
            .catch(() => {
                showToast('error', 'Network error while sending');
            });
        });
    }

    // Explorer Back Button
    const explorerBackBtn = document.getElementById('explorer-back-btn');
    if (explorerBackBtn) {
        explorerBackBtn.addEventListener('click', () => {
            if (explorerParentPath) {
                loadDirectory(explorerParentPath);
            }
        });
    }

    // Explorer Drives Button
    const explorerDrivesBtn = document.getElementById('explorer-drives-btn');
    if (explorerDrivesBtn) {
        explorerDrivesBtn.addEventListener('click', () => {
            loadDirectory('drives');
        });
    }

    // Clear Queue Button (inside Modal)
    const clearQueueBtn = document.getElementById('modal-clear-queue-btn');
    if (clearQueueBtn) {
        clearQueueBtn.addEventListener('click', () => {
            fetch('/api/clear-queue', { method: 'POST' })
            .then(res => res.json())
            .then(data => {
                if (data.queue) {
                    localQueue = data.queue;
                    renderQueue(data.queue);
                }
            });
        });
    }

    // Manual Scan Buttons
    const manualScanHandler = () => {
        const scanText = document.getElementById('discovery-status-text');
        if (scanText) scanText.textContent = "Scanning LAN devices...";
        
        fetch('/api/scan', { method: 'POST' })
        .then(() => {
            showToast('info', 'Searching for active LAN devices...');
        });
    };

    const headerScanBtn = document.getElementById('header-scan-btn');
    if (headerScanBtn) headerScanBtn.addEventListener('click', manualScanHandler);

    const emptyScanBtn = document.getElementById('empty-scan-btn');
    if (emptyScanBtn) emptyScanBtn.addEventListener('click', manualScanHandler);

    // Settings Save Form
    const settingsForm = document.getElementById('settings-form');
    if (settingsForm) {
        settingsForm.addEventListener('submit', (e) => {
            e.preventDefault();
            const updated = {
                deviceId: currentSettings.deviceId, // preserve ID
                deviceName: document.getElementById('settings-device-name').value,
                hostname: document.getElementById('settings-hostname').value,
                transferPort: parseInt(document.getElementById('settings-port').value),
                downloadDir: document.getElementById('settings-download-dir').value,
                
                showNotifications: document.getElementById('settings-show-notifications').checked,
                showTransferCompleteNotification: document.getElementById('settings-show-complete-notifications').checked,
                autoOpenFolder: document.getElementById('settings-auto-open-folder').checked,
                autoAccept: document.getElementById('settings-auto-accept').checked,
                enableDiscovery: document.getElementById('settings-enable-discovery').checked,
                autoScan: document.getElementById('settings-auto-scan').checked,
                minimizeToTray: document.getElementById('settings-minimize-tray').checked,
                startWithWindows: document.getElementById('settings-start-windows').checked
            };
            saveSettings(updated);
        });
    }

    // Browse Folder on Settings
    const browseDirBtn = document.getElementById('settings-browse-btn');
    if (browseDirBtn) {
        browseDirBtn.addEventListener('click', () => {
            fetch('/api/select-folder', { method: 'POST' })
            .then(res => res.json())
            .then(data => {
                if (data.files && data.files.length > 0) {
                    const selectedFolder = data.files[0].path;
                    const downloadDirInput = document.getElementById('settings-download-dir');
                    if (downloadDirInput) {
                        downloadDirInput.value = selectedFolder;
                    }
                }
            })
            .catch(err => {
                console.error('Failed to select folder:', err);
                showToast('error', 'Failed to browse folder');
            });
        });
    }

    // Open Download Folder
    const openDownloadsBtn = document.getElementById('open-downloads-btn');
    if (openDownloadsBtn) {
        openDownloadsBtn.addEventListener('click', () => {
            fetch('/api/open-folder', { method: 'POST' });
        });
    }

    // Clear History Button
    const clearHistoryBtn = document.getElementById('clear-history-btn');
    if (clearHistoryBtn) {
        clearHistoryBtn.addEventListener('click', () => {
            if (confirm("Are you sure you want to delete all transfer logs?")) {
                fetch('/api/clear-history', { method: 'POST' });
            }
        });
    }

    const historyTab = document.querySelector('[data-target="history-panel"]');
    if (historyTab) {
        historyTab.addEventListener('click', loadHistory);
    }
}

// Send local queue to target devices
function sendQueueToDevices(deviceIds) {
    if (deviceIds.length === 0) {
        showToast('error', 'Select at least one destination device');
        return;
    }

    showToast('info', `Starting transfer to ${deviceIds.length} device(s)...`);

    fetch('/api/send', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ deviceIds: deviceIds })
    })
    .then(res => res.json())
    .then(data => {
        showToast('success', 'Files sent to transfer manager.');
        selectedDevices.clear();
    })
    .catch(err => {
        showToast('error', 'Failed to initiate transfer');
    });
}

// Toast Notifications System
function showToast(type, message) {
    const container = document.getElementById('toast-container');
    if (!container) return;

    const toast = document.createElement('div');
    toast.className = 'transform translate-x-24 opacity-0 transition-all duration-300 ease-out flex items-center space-x-3 p-4 rounded-xl shadow-2xl glass-panel max-w-sm pointer-events-auto border';
    
    let colorClasses = 'border-indigo-500 bg-indigo-500/10 text-indigo-400';
    let icon = `<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"/></svg>`;

    if (type === 'success') {
        colorClasses = 'border-emerald-500 bg-emerald-500/10 text-emerald-400';
        icon = `<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z"/></svg>`;
    } else if (type === 'error') {
        colorClasses = 'border-rose-500 bg-rose-500/10 text-rose-400';
        icon = `<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z"/></svg>`;
    } else if (type === 'warning') {
        colorClasses = 'border-amber-500 bg-amber-500/10 text-amber-400';
        icon = `<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z"/></svg>`;
    }

    toast.className += ` ${colorClasses}`;
    toast.innerHTML = `
        <div class="flex-shrink-0">${icon}</div>
        <div class="text-sm font-medium text-white">${message}</div>
    `;

    container.appendChild(toast);
    
    // Animate In
    setTimeout(() => {
        toast.classList.remove('translate-x-24', 'opacity-0');
    }, 10);

    // Animate Out
    setTimeout(() => {
        toast.classList.add('translate-x-24', 'opacity-0');
        setTimeout(() => toast.remove(), 300);
    }, 4000);
}

// Helpers
function formatBytes(bytes, decimals = 2) {
    if (bytes === 0) return '0 Bytes';
    const k = 1024;
    const dm = decimals < 0 ? 0 : decimals;
    const sizes = ['Bytes', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(dm)) + ' ' + sizes[i];
}

function formatSpeed(bytesPerSec) {
    return `${formatBytes(bytesPerSec, 1)}/s`;
}

function formatETA(seconds) {
    if (seconds === 99999 || seconds < 0) return '--:--';
    const hrs = Math.floor(seconds / 3600);
    const mins = Math.floor((seconds % 3600) / 60);
    const secs = seconds % 60;
    
    let res = "";
    if (hrs > 0) res += (hrs < 10 ? "0" + hrs : hrs) + ":";
    res += (mins < 10 ? "0" + mins : mins) + ":";
    res += (secs < 10 ? "0" + secs : secs);
    return res;
}

// Built-in Web File Explorer JS Controller
let explorerCurrentPath = 'drives';
let explorerParentPath = '';

async function loadDirectory(path) {
    const listContainer = document.getElementById('explorer-items-list');
    const currentPathSpan = document.getElementById('explorer-current-path');
    const backBtn = document.getElementById('explorer-back-btn');

    if (!listContainer) return;

    listContainer.innerHTML = '<div class="p-4 text-xs text-slate-500 italic text-center">Loading files...</div>';
    if (currentPathSpan) currentPathSpan.textContent = path;

    try {
        const url = `/api/browse?path=${encodeURIComponent(path)}`;
        const res = await fetch(url);
        const data = await res.json();

        if (data.error) {
            listContainer.innerHTML = `<div class="p-4 text-xs text-rose-500 text-center">Error: ${data.error}</div>`;
            return;
        }

        explorerCurrentPath = data.currentPath;
        explorerParentPath = data.parentPath;

        if (currentPathSpan) currentPathSpan.textContent = explorerCurrentPath;
        if (backBtn) {
            backBtn.disabled = !explorerParentPath;
        }

        renderExplorerItems(data.items);
    } catch (err) {
        listContainer.innerHTML = `<div class="p-4 text-xs text-rose-500 text-center font-bold">Failed to load directory.</div>`;
    }
}

function renderExplorerItems(items) {
    const listContainer = document.getElementById('explorer-items-list');
    if (!listContainer) return;

    listContainer.innerHTML = '';

    if (!items || items.length === 0) {
        listContainer.innerHTML = '<div class="p-4 text-xs text-slate-500 italic text-center">Empty directory</div>';
        return;
    }

    // Sort items: folders first, then files (alphabetical)
    items.sort((a, b) => {
        if (a.isDir && !b.isDir) return -1;
        if (!a.isDir && b.isDir) return 1;
        return a.name.localeCompare(b.name);
    });

    items.forEach(item => {
        const row = document.createElement('div');
        row.className = 'flex items-center justify-between px-4 py-2.5 hover:bg-white/5 transition-all group';

        const sizeLabel = item.isDir ? 'Folder' : formatBytes(item.size);
        
        let icon = '';
        if (item.isDir) {
            icon = `<svg class="w-4 h-4 text-amber-500 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M3 7v10a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2h-6l-2-2H5a2 2 0 00-2 2z"/></svg>`;
        } else {
            icon = `<svg class="w-4 h-4 text-slate-400 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M7 21h10a2 2 0 002-2V9.414a1 1 0 00-.293-.707l-5.414-5.414A1 1 0 0012.586 3H7a2 2 0 00-2 2v14a2 2 0 002 2z"/></svg>`;
        }

        row.innerHTML = `
            <div class="flex items-center space-x-3 overflow-hidden flex-grow cursor-pointer explorer-item-click-target pr-2">
                ${icon}
                <div class="truncate text-left flex-grow">
                    <p class="text-xs font-semibold text-slate-200 group-hover:text-indigo-300 truncate" title="${item.name}">${item.name}</p>
                    <p class="text-[10px] text-slate-500 font-mono">${sizeLabel}</p>
                </div>
            </div>
            <button class="p-1.5 text-slate-400 hover:text-indigo-400 border border-white/5 hover:border-indigo-500/30 bg-slate-900/50 hover:bg-indigo-500/10 rounded-lg transition-all explorer-send-btn flex-shrink-0" data-path="${item.path}" title="Send Immediately">
                <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 19l9 2-9-18-9 18 9-2zm0 0v-8"/>
                </svg>
            </button>
        `;

        if (item.isDir) {
            row.querySelector('.explorer-item-click-target').addEventListener('click', () => {
                loadDirectory(item.path);
            });
        }

        row.querySelector('.explorer-send-btn').addEventListener('click', () => {
            sendFileFromExplorer(item.path);
        });

        listContainer.appendChild(row);
    });
}

function sendFileFromExplorer(path) {
    const filename = path.split('\\').pop().split('/').pop();
    showToast('info', 'Preparing to send: ' + filename);
    
    // 1. Clear any existing queue first
    fetch('/api/clear-queue', { method: 'POST' })
    .then(() => {
        // 2. Add the selected item to the queue
        return fetch('/api/add-to-queue', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ paths: [path] })
        });
    })
    .then(res => res.json())
    .then(data => {
        if (data.error) {
            showToast('error', data.error);
        } else if (data.added && data.added.length > 0) {
            // 3. Send immediately
            sendQueueToDevices(activeTargetDevices);
            // 4. Close the modal
            closeSendModal();
        } else {
            showToast('error', `Failed to send "${filename}" (file/folder might be empty, locked, or invalid).`);
        }
    })
    .catch(err => {
        showToast('error', 'Network error: Failed to send item.');
    });
}
