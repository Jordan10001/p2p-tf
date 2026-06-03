# Peer-to-Peer LAN File Transfer Application (Go & Windows)

A production-ready, decentralized, LAN-based file transfer application built in Go for Windows 10 and 11. It allows multiple computers on the same Local Area Network (LAN) to automatically discover each other and transfer files/directories directly without using any central server, relay server, cloud storage, or active internet connection.

It provides a modern user experience comparable to AirDrop or LocalSend, running fully standalone inside `p2p-transfer.exe` with zero external runtimes (no Node.js, Python, or Java required).

## Features

- **Zero-Config LAN Discovery**: Automatically registers and browses peers on the LAN via multicast DNS (mDNS/Zeroconf) over TCP.
- **Direct P2P Streaming**: Files are streamed directly from the sender's disk to the receiver's disk in chunked blocks (256KB), ensuring minimal memory utilization (never loads full files into RAM) and supporting massive files (>100GB).
- **Directory Structure Preservation**: Recursively scans shared folders on the sender's side and recreates identical folder structures on the receiver's side.
- **Resume Support**: Tracks incomplete files using `.part` buffers and automatically resumes file streaming from the last completed byte block if connections are interrupted.
- **Robust Security**: Prevents directory traversal attacks via path sanitization, employs UUIDs for transfer validation, and verifies sender addresses against the discovered LAN peer list.
- **Windows System Tray Integration**: Minimizes cleanly to the Windows system tray with a context menu (Open Dashboard, Settings, Restart Discovery, Exit) and sends native balloon toast notifications.
- **Glassmorphic Web Dashboard**: Runs a local Fiber web server (`http://localhost:8080`) that launches automatically on startup. Features a dark/light responsive layout styled with Tailwind CSS.
- **Native File Dialogs**: Leverages lightweight PowerShell scripts under the hood to trigger native Windows File and Directory pickers from the web dashboard.
- **Drag & Drop Support**: Allows dropping files directly onto the browser dashboard, copying them into a local buffer queue for sharing.

---

## Directory Structure

```text
/p2p-transfer
├── /cmd
│   └── /p2p-transfer
│       └── main.go                 # App Entry point (orchestration & signals)
├── /internal
│   ├── /config
│   │   └── config.go               # JSON Settings manager (%USERPROFILE%/.p2p-transfer/config.json)
│   ├── /discovery
│   │   └── discovery.go            # mDNS advertisement and peer sweep daemon
│   ├── /history
│   │   └── history.go              # JSON Transfer logs (%USERPROFILE%/.p2p-transfer/history.json)
│   ├── /logger
│   │   └── logger.go               # Console + File logs manager (logs/app.log)
│   ├── /models
│   │   └── models.go               # Shared Go structures (Device, Transfer, Settings, WSMessage)
│   ├── /systray
│   │   ├── systray_windows.go      # Native Windows Tray (Win32 APIs via syscalls - zero CGO)
│   │   └── systray_stub.go         # No-op stub for cross-platform compliance
│   ├── /transfer
│   │   ├── manager.go              # Send queue, active transfer logs, speed/ETA track
│   │   ├── protocol.go             # Length-prefixed JSON network framing over TCP
│   │   ├── receiver.go             # TCP receiver listener & write-append partial handler
│   │   └── sender.go               # TCP sender client & file seek streaming handler
│   ├── /utils
│   │   ├── dialogs.go              # Native File Explorer picker script wrapper
│   │   └── utils.go                # Traversal prevention, local IP lookup, auto-rename collisions
│   └── /web
│       └── web.go                  # Fiber Web Server REST endpoints
├── /web
│   ├── /static
│   │   ├── /css
│   │   │   └── style.css           # Custom scrollbars, glass styles, pulse animations
│   │   ├── /icons
│   │   │   └── .gitkeep            # Icon folder placeholder
│   │   └── /js
│   │       └── app.js              # Web Client logic (WebSocket, HTTP REST, DragDrop, UI states)
│   └── /templates
│       └── index.html              # SPA Dashboard HTML template
├── go.mod
├── go.sum
└── README.md
```

---

## Build Instructions

No external compilation tools (like GCC for CGO) are required. Build natively in pure Go:

1. Clone or copy the repository.
2. Open PowerShell in the `/p2p-transfer` directory.
3. Run the standard compile command:
   ```powershell
   go build -o p2p-transfer.exe cmd/p2p-transfer/main.go
   ```
4. To compile as a background Windows GUI application (hiding the black command prompt window when launching `p2p-transfer.exe`), append the window flags:
   ```powershell
   go build -ldflags="-H windowsgui" -o p2p-transfer.exe cmd/p2p-transfer/main.go
   ```

---

## Running the Application

1. Double-click the compiled `p2p-transfer.exe`.
2. The application will:
   - Run in the background on your Windows machine.
   - Load/create settings and history JSON files in `C:\Users\<username>\.p2p-transfer\`.
   - Add a default tray icon in your taskbar.
   - Show a system notification balloon: *"P2P File Transfer Active"*.
   - Launch your default web browser to the dashboard: `http://localhost:8080`.
3. To share files with other computers:
   - Ensure the application is running on **both** machines connected to the same LAN (Wi-Fi or Ethernet).
   - If they don't find each other automatically, click **[Scan Again]** or check that the Windows Network Profile is set to **Private** (so multicast packets aren't blocked by the Windows Defender Firewall).
   - Select files/directories using the **[Select Files]** or **[Select Folder]** pickers, or drag and drop items into the dash.
   - Check the destinations you want to send to on the online devices grid.
   - Click **[Start P2P Transfer]** in the floating action bar.
   - The receiver will be prompted with a modal detailing the sender name, files list, and total size to click **[Accept]** or **[Reject]** (unless *Auto Accept* is checked in their settings panel).

---

## Technical Specifications

### 1. TCP Handshake & Framing Protocol (`internal/transfer/protocol.go`)
Network packets are prefixed with a **4-byte Big-Endian uint32 length** describing the size of the following JSON frame. 
- After connecting, the **Sender** sends a length-prefixed `HandshakeRequest` (containing Transfer UUID, Sender ID, total transfer size, and files catalog).
- The **Receiver** verifies authorization and replies with a length-prefixed `HandshakeResponse` containing status (`accepted` / `rejected`) and a map of resume byte offsets for each file if incomplete `.part` files are found on the receiver's disk.
- For each file, the Sender sends a length-prefixed `FileHeader` containing relative path, total file size, and target start seek offset. The receiver confirms with `FileHeaderResponse` (`ok` / `skip`). The sender then streams exactly `(FileSize - Offset)` raw bytes directly over the TCP socket.

### 2. Path Sanitization (`internal/utils/utils.go`)
Receiving nodes execute rigorous path checking on every file payload. In addition to discarding directory traversal indicators (`..` or absolute paths pointing outside the target directory), the system replaces illegal Windows file characters (`<`, `>`, `:`, `"`, `/`, `\`, `|`, `?`, `*`) with underscores.

### 3. File Collision Auto-Renaming (`internal/utils/utils.go`)
If the transfer completes and a file with the same name already exists in the target folder, the receiver triggers a unique filename loop. It checks if `file.pdf` is present, then tries `file (1).pdf`, `file (2).pdf`, and so on, until it resolves a non-conflicting path before renaming the completed `.part` buffer.

---

## Log Output

System events are appended in real-time to `logs/app.log`. The output tracks discovery notifications, TCP connections, start/stop states, and errors:
```text
[INFO] 2026/06/03 20:45:10 Starting P2P File Transfer Application...
[INFO] 2026/06/03 20:45:11 TCP Receiver started on :50005
[INFO] 2026/06/03 20:45:11 WebSocket server starting on :8081
[INFO] 2026/06/03 20:45:11 Advertising mDNS service: Name=DESKTOP-P2P, Port=50005, IP=192.168.1.12
[INFO] 2026/06/03 20:45:12 Fiber web server starting on http://localhost:8080
[INFO] 2026/06/03 20:45:12 Opening dashboard in browser...
[INFO] 2026/06/03 20:45:18 Device discovered/updated: LAPTOP-P2P (LAPTOP-01) at 192.168.1.48:50005, status: online
[INFO] 2026/06/03 20:45:32 Incoming connection from LAPTOP-P2P (192.168.1.48)
[INFO] 2026/06/03 20:45:34 Starting transfer receiver for ID: 9c34ef12-2309-4bfd-a129-873b879c938f
```
