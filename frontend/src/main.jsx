import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { createRoot } from 'react-dom/client';
import {
  FiActivity, FiAlertCircle, FiArrowDown, FiArrowLeft, FiArrowUp, FiCheck,
  FiCheckCircle, FiChevronRight, FiClock, FiDownload, FiFile, FiFolder,
  FiHardDrive, FiInfo, FiMoon, FiMonitor,
  FiRefreshCw, FiSearch, FiSend, FiSettings, FiSun, FiTrash2,
  FiX, FiZap,
} from 'react-icons/fi';
import './style.css';

const NAV = [
  ['dashboard', 'Dashboard', FiActivity],
  ['transfers', 'Transfers', FiRefreshCw],
  ['history', 'History', FiClock],
  ['settings', 'Settings', FiSettings],
];
const THEME_KEY = 'p2p-tf-theme';

function preferredTheme() {
  const saved = localStorage.getItem(THEME_KEY);
  return saved === 'light' || saved === 'dark' ? saved : (matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light');
}

function savedPage() {
  const previous = localStorage.getItem('activeTab');
  const legacyPages = { 'dashboard-panel': 'dashboard', 'transfers-panel': 'transfers', 'history-panel': 'history', 'settings-panel': 'settings' };
  const page = legacyPages[previous] || previous;
  return NAV.some(([key]) => key === page) ? page : 'dashboard';
}

async function api(path, options = {}) {
  const response = await fetch(`/api${path}`, options);
  let data;
  try { data = await response.json(); } catch { data = {}; }
  if (!response.ok) throw new Error(data.error || `Request failed (${response.status})`);
  return data;
}

const post = (path, body) => api(path, {
  method: 'POST',
  ...(body === undefined ? {} : { headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }),
});

function formatBytes(value = 0, decimals = 1) {
  const bytes = Number(value) || 0;
  if (!bytes) return '0 B';
  const unit = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), 4);
  return `${(bytes / (1024 ** unit)).toFixed(decimals)} ${['B', 'KB', 'MB', 'GB', 'TB'][unit]}`;
}

function formatEta(value = 0) {
  if (value < 0 || value === 99999) return '--:--';
  const hours = Math.floor(value / 3600);
  const minutes = Math.floor(value % 3600 / 60);
  const seconds = Math.floor(value % 60);
  return `${hours ? `${String(hours).padStart(2, '0')}:` : ''}${String(minutes).padStart(2, '0')}:${String(seconds).padStart(2, '0')}`;
}

function App() {
  const [theme, setTheme] = useState(preferredTheme);
  const [page, setPage] = useState(savedPage);
  const [settings, setSettings] = useState({});
  const [devices, setDevices] = useState([]);
  const [transfers, setTransfers] = useState({});
  const [history, setHistory] = useState([]);
  const [toasts, setToasts] = useState([]);
  const [discovery, setDiscovery] = useState('Connecting…');
  const [connected, setConnected] = useState(false);
  const [sendTarget, setSendTarget] = useState(null);
  const [selectedFiles, setSelectedFiles] = useState([]);
  const [sendBrowseData, setSendBrowseData] = useState({ items: [], currentPath: 'drives', parentPath: '' });
  const [sendBrowseLoading, setSendBrowseLoading] = useState(false);
  const [sendSearch, setSendSearch] = useState('');
  const [incoming, setIncoming] = useState(null);
  const [confirmClear, setConfirmClear] = useState(false);

  useEffect(() => {
    document.documentElement.dataset.theme = theme;
    localStorage.setItem(THEME_KEY, theme);
  }, [theme]);

  const toast = useCallback((type, message) => {
    const id = `${Date.now()}-${Math.random()}`;
    setToasts((items) => [...items, { id, type, message }]);
    window.setTimeout(() => setToasts((items) => items.filter((item) => item.id !== id)), 4200);
  }, []);

  const loadSettings = useCallback(async () => {
    try { setSettings(await api('/settings')); } catch (error) { toast('error', error.message); }
  }, [toast]);

  const loadHistory = useCallback(async () => {
    try { setHistory(await api('/history')); } catch (error) { toast('error', error.message); }
  }, [toast]);

  const loadSendBrowse = useCallback(async (path) => {
    setSendBrowseLoading(true);
    try {
      const result = await api(`/browse?path=${encodeURIComponent(path)}`);
      setSendBrowseData(result);
    } catch (error) { toast('error', error.message); }
    finally { setSendBrowseLoading(false); }
  }, [toast]);

  useEffect(() => {
    loadSettings();
    loadHistory();
    api('/devices').then(setDevices).catch(() => {});
    api('/transfers').then((items) => setTransfers(Object.fromEntries((items || []).map((item) => [item.id, item])))).catch(() => {});
  }, [loadSettings, loadHistory]);

  useEffect(() => {
    let socket;
    let retry;
    let disposed = false;
    const connect = () => {
      if (disposed) return;
      const scheme = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
      socket = new WebSocket(`${scheme}//${window.location.hostname}:8081/ws`);
      socket.onopen = () => {
        setConnected(true);
      };
      socket.onclose = () => {
        setConnected(false);
        setDiscovery('Disconnected');
        retry = window.setTimeout(connect, 3000);
      };
      socket.onerror = () => socket.close();
      socket.onmessage = ({ data }) => {
        let message;
        try { message = JSON.parse(data); } catch { return; }
        switch (message.type) {
          case 'device_list': {
            const online = (message.payload || []).filter((device) => device.status !== 'offline');
            setDevices(online);
            setDiscovery(online.length ? `${online.length} device${online.length === 1 ? '' : 's'} found` : 'No devices found');
            break;
          }
          case 'history_update': setHistory(message.payload || []); break;
          case 'config_update': setSettings((current) => ({ ...current, ...(message.payload || {}), localIp: current.localIp })); break;
          case 'incoming_request': setIncoming(message.payload); break;
          case 'toast': toast(message.payload?.type || 'info', message.payload?.message || ''); break;
          case 'transfer_update': {
            const transfer = message.payload;
            if (!transfer?.id) break;
            setTransfers((current) => ({ ...current, [transfer.id]: transfer }));
            if (['completed', 'failed', 'cancelled'].includes(transfer.status)) {
              window.setTimeout(() => setTransfers((current) => {
                const next = { ...current };
                delete next[transfer.id];
                return next;
              }), 3000);
            }
            break;
          }
          default: break;
        }
      };
    };
    connect();
    return () => { disposed = true; clearTimeout(retry); socket?.close(); };
  }, [toast]);

  useEffect(() => { if (page === 'history') loadHistory(); }, [page, loadHistory]);
  const transferList = useMemo(() => Object.values(transfers), [transfers]);
  const onlineDevices = useMemo(() => devices.filter((device) => device.status !== 'offline'), [devices]);
  const navigate = (next) => { setPage(next); localStorage.setItem('activeTab', next); };

  const openSend = (device) => {
    setSendTarget(device);
    setSelectedFiles([]);
    setSendSearch('');
    loadSendBrowse('drives');
  };
  const sendBrowseItems = useMemo(() => (sendBrowseData.items || [])
    .filter((item) => item.name.toLowerCase().includes(sendSearch.toLowerCase()))
    .sort((a, b) => Number(b.isDir) - Number(a.isDir) || a.name.localeCompare(b.name)), [sendBrowseData.items, sendSearch]);
  const toggleSendFile = (item) => setSelectedFiles((current) => current.some((selected) => selected.path === item.path)
    ? current.filter((selected) => selected.path !== item.path)
    : [...current, item]);
  const sendDirect = async () => {
    if (!sendTarget || !selectedFiles.length) return;
    try {
      await post('/send-direct', { deviceIds: [sendTarget.id], paths: selectedFiles.map((item) => item.path) });
      toast('success', 'Transfer started successfully');
      setSendTarget(null);
    } catch (error) { toast('error', error.message); }
  };
  const scan = async () => {
    setDiscovery('Scanning LAN devices…');
    try { await post('/scan'); toast('info', 'Searching for active LAN devices'); }
    catch (error) { toast('error', error.message); }
  };
  const saveSettings = async (event) => {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    const value = (key) => form.get(key) === 'on';
    const update = {
      deviceId: settings.deviceId,
      deviceName: form.get('deviceName'), hostname: settings.hostname,
      transferPort: Number(form.get('transferPort')), downloadDir: form.get('downloadDir'),
      showNotifications: value('showNotifications'), showTransferCompleteNotification: value('showTransferCompleteNotification'),
      autoOpenFolder: value('autoOpenFolder'), autoAccept: value('autoAccept'), enableDiscovery: value('enableDiscovery'),
      autoScan: value('autoScan'), minimizeToTray: value('minimizeToTray'), startWithWindows: value('startWithWindows'),
    };
    try { await post('/settings', update); await loadSettings(); toast('success', 'Configuration updated successfully'); }
    catch (error) { toast('error', error.message); }
  };
  const resolveIncoming = async (accepted) => {
    if (!incoming) return;
    try {
      await post(accepted ? '/accept-transfer' : '/reject-transfer', { transferId: incoming.transferId });
      toast(accepted ? 'success' : 'info', accepted ? 'Transfer accepted' : 'Transfer rejected');
    } catch (error) { toast('error', error.message); }
    setIncoming(null);
  };
  const cancelTransfer = async (id) => {
    try { await post('/cancel-transfer', { transferId: id }); toast('info', 'Transfer cancellation requested'); }
    catch (error) { toast('error', error.message); }
  };
  const clearHistory = async () => {
    try { await post('/clear-history'); setHistory([]); setConfirmClear(false); toast('success', 'Transfer history cleared'); }
    catch (error) { toast('error', error.message); }
  };
  const openDownloads = async () => {
    try { await post('/open-folder'); } catch (error) { toast('error', error.message); }
  };
  return <div className="app-shell">
    <header className="topbar">
      <div className="brand"><img src="/static/icons/logo.svg" alt="P2P-TF logo" /><div><strong>P2P-TF</strong><small>LOCAL LAN FILE SHARING</small></div></div>
      <div className="top-actions"><span className="connection-pill"><i className={connected ? 'status-dot live' : 'status-dot'} />{connected ? `Discovery · ${discovery}` : discovery}</span><button className="icon-button" title="Scan network" onClick={scan}><FiRefreshCw /></button><button className="icon-button theme-toggle" title={`Switch to ${theme === 'light' ? 'dark' : 'light'} mode`} onClick={() => setTheme(theme === 'light' ? 'dark' : 'light')}>{theme === 'light' ? <FiMoon /> : <FiSun />}</button></div>
    </header>
    <div className="layout">
      <aside className="sidebar">
        <div className="device-summary"><div className="device-glyph"><FiMonitor /></div><div className="device-summary-text"><span>ACTIVE DEVICE</span><strong>{settings.deviceName || 'Loading device…'}</strong><small>{settings.localIp ? `${settings.localIp}:${settings.transferPort}` : settings.hostname || ''}</small></div></div>
        <nav className="nav-list">{NAV.map(([key, label, Icon]) => <button key={key} className={`nav-item ${page === key ? 'active' : ''}`} onClick={() => navigate(key)}><Icon /><span>{label}</span></button>)}</nav>
        <div className="sidebar-foot"><span className="foot-orb"><FiZap /></span><span><strong>Private & local</strong><small>Transfers stay on your network</small></span></div>
      </aside>
      <main className="main-content">
        {page === 'dashboard' && <Dashboard devices={onlineDevices} transfers={transferList} discovery={discovery} onScan={scan} onSend={openSend} />}
        {page === 'transfers' && <Transfers transfers={transferList} onCancel={cancelTransfer} onOpenDownloads={openDownloads} />}
        {page === 'history' && <History entries={history} settings={settings} onClear={() => setConfirmClear(true)} onOpenDownloads={openDownloads} />}
        {page === 'settings' && <Settings key={settings.deviceName || 'settings-loading'} settings={settings} onSave={saveSettings} />}
      </main>
    </div>
    <ToastList items={toasts} />
    {sendTarget && <SendDialog target={sendTarget} files={selectedFiles} browse={sendBrowseData} items={sendBrowseItems} loading={sendBrowseLoading} search={sendSearch} onSearch={setSendSearch} onBrowse={loadSendBrowse} onToggle={toggleSendFile} onClose={() => setSendTarget(null)} onSend={sendDirect} />}
    {incoming && <IncomingDialog request={incoming} onResolve={resolveIncoming} />}
    {confirmClear && <ConfirmDialog title="Clear transfer history?" message="This permanently removes all recorded transfer logs." onCancel={() => setConfirmClear(false)} onConfirm={clearHistory} />}
  </div>;
}

function PageHeading({ eyebrow, title, description, action }) {
  return <div className="page-heading"><div><span className="eyebrow">{eyebrow}</span><h1>{title}</h1>{description && <p>{description}</p>}</div>{action}</div>;
}

function Dashboard({ devices, transfers, discovery, onScan, onSend }) {
  return <>
    <PageHeading eyebrow="YOUR NETWORK" title="Nearby devices" description="Discover devices on your local network and send files securely." action={<button className="button secondary" onClick={onScan}><FiRefreshCw /> Scan again</button>} />
    <section className="section-block"><div className="section-title"><div><h2>Available devices</h2><p>{devices.length ? `${devices.length} device${devices.length === 1 ? '' : 's'} ready to receive` : discovery}</p></div><span className="section-count">{devices.length.toString().padStart(2, '0')}</span></div>
      {devices.length ? <div className="device-grid">{devices.map((device) => <article className="surface device-card" key={device.id}><div className="card-top"><div className="round-icon"><FiMonitor /></div><div className="device-info"><h3>{device.name}</h3><p>{device.hostname}</p></div><span className="online-label"><i />{device.status || 'online'}</span></div><div className="device-actions"><code>{device.ip}:{device.port}</code><button className="button primary" onClick={() => onSend(device)}><FiSend /> Send files</button></div></article>)}</div>
      : <div className="surface empty-state"><div className="empty-illustration"><FiSearch /></div><h3>Looking for devices</h3><p>Make sure both devices are running P2P-TF and connected to the same network.</p><button className="button primary" onClick={onScan}><FiRefreshCw /> Scan network</button></div>}</section>
    <section className="section-block"><div className="section-title"><div><h2>Active transfers</h2><p>Live progress for current file transfers</p></div><span className="section-count"><FiActivity /></span></div>{transfers.length ? <div className="transfer-list">{transfers.slice(0, 3).map((item) => <TransferCard key={item.id} transfer={item} compact />)}</div> : <div className="surface quiet-empty"><FiActivity /><span>No active transfers right now</span></div>}</section>
  </>;
}

function Transfers({ transfers, onCancel, onOpenDownloads }) {
  return <><PageHeading eyebrow="LIVE ACTIVITY" title="Transfers" description="Monitor transfer progress and manage incoming or outgoing files." action={<button className="button secondary" onClick={onOpenDownloads}><FiFolder /> Open downloads</button>} />
    {transfers.length ? <div className="transfer-list">{transfers.map((item) => <TransferCard key={item.id} transfer={item} onCancel={onCancel} />)}</div> : <div className="surface empty-state"><div className="empty-illustration"><FiRefreshCw /></div><h3>No transfers yet</h3><p>Transfers will appear here when you send or receive files.</p></div>}</>;
}

function TransferCard({ transfer, compact = false, onCancel }) {
  const active = ['sending', 'receiving', 'waiting', 'preparing'].includes(transfer.status);
  const files = transfer.files || [];
  const firstName = files[0]?.name || 'Transfer';
  const fileLabel = files.length > 1 ? `${firstName} and ${files.length - 1} more` : firstName;
  return <article className={`surface transfer-card ${compact ? 'compact' : ''}`}><div className="transfer-head"><div className="round-icon transfer-direction">{transfer.direction === 'send' ? <FiArrowUp /> : <FiArrowDown />}</div><div className="transfer-description"><h3>{fileLabel}</h3><p>{transfer.direction === 'send' ? 'To' : 'From'} {transfer.peerName} <span>·</span> {transfer.peerIp} <span>·</span> {formatBytes(transfer.totalSize)}</p></div><span className={`status-badge ${transfer.status}`}>{transfer.status}</span>{active && onCancel && <button className="icon-button subtle" aria-label="Cancel transfer" onClick={() => onCancel(transfer.id)}><FiX /></button>}</div><div className="progress-meta"><span>{['sending', 'receiving'].includes(transfer.status) ? `${formatBytes(transfer.speed, 1)}/s · ETA ${formatEta(transfer.eta)}` : transfer.error || transfer.status}</span><strong>{Math.round(transfer.progress || 0)}%</strong></div><div className="progress-track"><div style={{ width: `${Math.min(100, Math.max(0, transfer.progress || 0))}%` }} /></div></article>;
}

function History({ entries, settings, onClear, onOpenDownloads }) {
  return <><PageHeading eyebrow="TRANSFER LOG" title="History" description="Review completed, failed, and cancelled transfers." action={<div className="heading-actions"><button className="button secondary" onClick={onOpenDownloads}><FiFolder /> Downloads</button><button className="button danger-soft" onClick={onClear}><FiTrash2 /> Clear history</button></div>} />
    <div className="surface history-surface"><div className="table-scroll"><table><thead><tr><th>File / folder</th><th>Sender</th><th>Receiver</th><th>Size</th><th>Date</th><th>Status</th><th /></tr></thead><tbody>{entries.length ? entries.map((entry, index) => { const canOpen = entry.receiver === settings.deviceName && entry.status === 'completed'; return <tr key={entry.id || `${entry.timestamp}-${index}`}><td><strong className="file-cell"><FiFile />{entry.fileName}</strong></td><td>{entry.sender}</td><td>{entry.receiver}</td><td>{formatBytes(entry.size)}</td><td>{new Date(entry.timestamp).toLocaleString()}</td><td><span className={`status-badge ${entry.status}`}>{entry.status}</span></td><td>{canOpen && <button className="text-button" onClick={onOpenDownloads}>Open folder</button>}</td></tr>; }) : <tr><td colSpan="7"><div className="table-empty">No transfer history recorded</div></td></tr>}</tbody></table></div></div>
  </>;
}

const SETTINGS_FIELDS = [
  ['Notifications', [['showNotifications', 'Show toast notifications'], ['showTransferCompleteNotification', 'Show transfer complete notifications'], ['autoOpenFolder', 'Open folder after download'], ['autoAccept', 'Automatically accept incoming files']]],
  ['Startup & discovery', [['enableDiscovery', 'Enable device discovery'], ['autoScan', 'Automatically scan the network'], ['minimizeToTray', 'Minimize to system tray'], ['startWithWindows', 'Start with Windows']]],
];

function Settings({ settings, onSave }) {
  return <><PageHeading eyebrow="PREFERENCES" title="Settings" description="Configure your device identity, download location, and transfer behavior." />
    <form className="settings-form" onSubmit={onSave}><section className="surface settings-card"><div className="settings-card-title"><div className="round-icon"><FiMonitor /></div><div><h2>Device information</h2><p>How this computer appears to nearby devices</p></div></div><div className="form-grid"><label>Device name<input name="deviceName" defaultValue={settings.deviceName || ''} required /></label><label>Hostname<input value={settings.hostname || ''} readOnly /></label><label>Transfer port<input name="transferPort" type="number" defaultValue={settings.transferPort || 50005} min="1" max="65535" required /></label></div></section>
      <section className="surface settings-card"><div className="settings-card-title"><div className="round-icon"><FiDownload /></div><div><h2>Download storage</h2><p>Choose where received files are saved</p></div></div><label className="wide-field">Download directory<input name="downloadDir" defaultValue={settings.downloadDir || ''} placeholder="C:\Users\You\Downloads" /></label></section>
      <div className="settings-grid">{SETTINGS_FIELDS.map(([title, fields]) => <section className="surface settings-card" key={title}><h2>{title}</h2><div className="toggle-list">{fields.map(([name, label]) => <label className="toggle-row" key={name}><span><strong>{label}</strong><small>{settingHelp(name)}</small></span><input type="checkbox" name={name} defaultChecked={Boolean(settings[name])} /><i className="toggle-ui" /></label>)}</div></section>)}</div>
      <div className="form-footer"><button className="button primary" type="submit"><FiCheck /> Save settings</button></div>
    </form>
  </>;
}

function settingHelp(key) {
  return ({ showNotifications: 'Show in-app messages for transfer activity.', showTransferCompleteNotification: 'Notify when a transfer finishes.', autoOpenFolder: 'Open the download folder after a received transfer.', autoAccept: 'Accept incoming transfer requests automatically.', enableDiscovery: 'Allow this device to find peers on the LAN.', autoScan: 'Scan periodically while the dashboard is open.', minimizeToTray: 'Keep the app running in the system tray.', startWithWindows: 'Launch this app when Windows starts.' })[key];
}

function SendDialog({ target, files, browse, items, loading, search, onSearch, onBrowse, onToggle, onClose, onSend }) {
  const total = files.reduce((sum, item) => sum + (item.size || 0), 0);
  return <div className="modal-backdrop" onMouseDown={(event) => event.target === event.currentTarget && onClose()}><section className="surface modal-card send-browser-modal"><div className="modal-title"><div><span className="eyebrow">DIRECT TRANSFER</span><h2>Send files to {target.name}</h2><p>{target.ip}:{target.port}</p></div><button className="icon-button" onClick={onClose}><FiX /></button></div>
    <div className="send-browser-heading"><div><h3>Browse this computer</h3><p>Select files or folders to send</p></div><button className="icon-button subtle" title="Refresh folder" onClick={() => onBrowse(browse.currentPath)}><FiRefreshCw /></button></div>
    <div className="browser-toolbar"><button className="icon-button tiny" disabled={!browse.parentPath} title="Go back" onClick={() => onBrowse(browse.parentPath)}><FiArrowLeft /></button><button className="crumb" onClick={() => onBrowse('drives')}><FiHardDrive /> This PC</button><FiChevronRight className="crumb-separator" /><code title={browse.currentPath}>{browse.currentPath === 'drives' ? 'Drives' : browse.currentPath}</code><div className="browser-search"><FiSearch /><input value={search} onChange={(event) => onSearch(event.target.value)} placeholder="Filter items" /></div></div>
    <div className="browser-items send-browser-items">{loading ? <div className="browser-empty">Loading folders…</div> : items.length ? items.map((item) => { const selected = files.some((file) => file.path === item.path); return <div className={`browser-item ${selected ? 'selected' : ''}`} key={item.path}><button className="browse-open" onClick={() => item.isDir && onBrowse(item.path)} disabled={!item.isDir} title={item.isDir ? 'Open folder' : item.name}><span className={`browse-icon ${item.isDir ? 'folder' : ''}`}>{item.isDir ? <FiFolder /> : <FiFile />}</span><span className="browse-name"><strong>{item.name}</strong><small>{item.isDir ? 'Folder' : formatBytes(item.size)}</small></span>{item.isDir && <FiChevronRight className="browse-chevron" />}</button><button className={`send-select ${selected ? 'selected' : ''}`} title={selected ? 'Remove from selection' : 'Select item'} aria-label={`${selected ? 'Deselect' : 'Select'} ${item.name}`} aria-pressed={selected} onClick={() => onToggle(item)}>{selected && <FiCheck />}</button></div>; }) : <div className="browser-empty">{search ? 'No matching items' : 'This folder is empty'}</div>}</div>
    <div className="modal-footer"><span>{files.length} item{files.length === 1 ? '' : 's'} · {formatBytes(total)} selected</span><div><button className="button secondary" onClick={onClose}>Cancel</button><button className="button primary" disabled={!files.length} onClick={onSend}><FiSend /> Send files</button></div></div></section></div>;
}
function IncomingDialog({ request, onResolve }) {
  const files = request.files || [];
  return <div className="modal-backdrop"><section className="surface modal-card incoming-card"><div className="incoming-mark"><FiDownload /></div><span className="eyebrow">INCOMING TRANSFER</span><h2>Accept files from {request.peerName}?</h2><p className="incoming-source">{request.peerIp} wants to send {files.length} item{files.length === 1 ? '' : 's'}</p><div className="incoming-files">{files.slice(0, 5).map((file, index) => <div key={`${file.name}-${index}`}><span><FiFile />{file.name}</span><small>{formatBytes(file.size)}</small></div>)}{files.length > 5 && <small>…and {files.length - 5} more files</small>}</div><div className="incoming-total"><span>Total payload</span><strong>{formatBytes(request.totalSize)}</strong></div><div className="modal-footer"><button className="button secondary" onClick={() => onResolve(false)}>Reject</button><button className="button primary" onClick={() => onResolve(true)}><FiCheck /> Accept transfer</button></div></section></div>;
}

function ConfirmDialog({ title, message, onCancel, onConfirm }) {
  return <div className="modal-backdrop"><section className="surface modal-card confirm-card"><div className="round-icon warning-icon"><FiAlertCircle /></div><h2>{title}</h2><p>{message}</p><div className="modal-footer"><button className="button secondary" onClick={onCancel}>Cancel</button><button className="button danger" onClick={onConfirm}><FiTrash2 /> Clear</button></div></section></div>;
}

function ToastList({ items }) {
  const icons = { success: FiCheckCircle, error: FiAlertCircle, warning: FiAlertCircle, info: FiInfo };
  return <div className="toast-stack" aria-live="polite">{items.map((item) => { const Icon = icons[item.type] || FiInfo; return <div className={`toast surface ${item.type}`} key={item.id}><Icon /><span>{item.message}</span></div>; })}</div>;
}

createRoot(document.getElementById('root')).render(<React.StrictMode><App /></React.StrictMode>);
