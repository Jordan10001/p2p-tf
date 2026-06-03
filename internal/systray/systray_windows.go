//go:build windows

package systray

import (
	"os"
	"path/filepath"
	"syscall"
	"unsafe"

	"p2p-transfer/internal/logger"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	pRegisterClassEx    = user32.NewProc("RegisterClassExW")
	pCreateWindowEx     = user32.NewProc("CreateWindowExW")
	pDefWindowProc      = user32.NewProc("DefWindowProcW")
	pDestroyWindow      = user32.NewProc("DestroyWindow")
	pPostQuitMessage    = user32.NewProc("PostQuitMessage")
	pGetMessage         = user32.NewProc("GetMessageW")
	pTranslateMessage   = user32.NewProc("TranslateMessage")
	pDispatchMessage    = user32.NewProc("DispatchMessageW")
	pLoadIcon           = user32.NewProc("LoadIconW")
	pLoadImage          = user32.NewProc("LoadImageW")
	pCreatePopupMenu    = user32.NewProc("CreatePopupMenu")
	pAppendMenu         = user32.NewProc("AppendMenuW")
	pTrackPopupMenu     = user32.NewProc("TrackPopupMenu")
	pGetCursorPos       = user32.NewProc("GetCursorPos")
	pSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	pPostMessage        = user32.NewProc("PostMessageW")

	pShellNotifyIcon    = shell32.NewProc("Shell_NotifyIconW")
	pGetModuleHandle    = kernel32.NewProc("GetModuleHandleW")
)

const (
	WM_DESTROY      = 0x0002
	WM_COMMAND      = 0x0111
	WM_USER         = 0x0400
	WM_TRAY         = WM_USER + 1
	WM_RBUTTONUP    = 0x0205
	WM_LBUTTONDBLCLK = 0x0203

	NIM_ADD        = 0x00000000
	NIM_MODIFY     = 0x00000001
	NIM_DELETE     = 0x00000002

	NIF_MESSAGE    = 0x00000001
	NIF_ICON       = 0x00000002
	NIF_TIP        = 0x00000004
	NIF_INFO       = 0x00000010

	NIIF_INFO      = 0x00000001

	IDI_APPLICATION = 32512

	MF_STRING     = 0x00000000
	MF_SEPARATOR  = 0x00000800

	TPM_LEFTALIGN   = 0x0000
	TPM_RIGHTBUTTON = 0x0002
)

type POINT struct {
	X int32
	Y int32
}

type WNDCLASSEXW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     syscall.Handle
	HIcon         syscall.Handle
	HCursor       syscall.Handle
	HbrBackground syscall.Handle
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       syscall.Handle
}

type NOTIFYICONDATAW struct {
	CbSize           uint32
	HWnd             syscall.Handle
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            syscall.Handle
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	TimeoutOrVersion uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         [16]byte
	HBalloonIcon     syscall.Handle
}

var (
	globalHWnd           syscall.Handle
	onOpenDashboardCB    func()
	onSettingsCB         func()
	onRestartDiscoveryCB func()
	onExitCB             func()
)

func wndProc(hWnd syscall.Handle, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case WM_TRAY:
		if lParam == WM_RBUTTONUP {
			showContextMenu(hWnd)
		} else if lParam == WM_LBUTTONDBLCLK {
			if onOpenDashboardCB != nil {
				onOpenDashboardCB()
			}
		}
	case WM_COMMAND:
		switch wParam {
		case 1001:
			if onOpenDashboardCB != nil {
				onOpenDashboardCB()
			}
		case 1002:
			if onSettingsCB != nil {
				onSettingsCB()
			}
		case 1003:
			if onRestartDiscoveryCB != nil {
				onRestartDiscoveryCB()
			}
		case 1004:
			if onExitCB != nil {
				onExitCB()
			}
		}
	case WM_DESTROY:
		pPostQuitMessage.Call(0)
	default:
		r, _, _ := pDefWindowProc.Call(uintptr(hWnd), uintptr(msg), wParam, lParam)
		return r
	}
	return 0
}

func showContextMenu(hWnd syscall.Handle) {
	var pt POINT
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))

	hMenu, _, _ := pCreatePopupMenu.Call()
	if hMenu == 0 {
		return
	}

	pSetForegroundWindow.Call(uintptr(hWnd))

	appendMenu(hMenu, MF_STRING, 1001, "Open Dashboard")
	appendMenu(hMenu, MF_STRING, 1002, "Settings")
	appendMenu(hMenu, MF_STRING, 1003, "Restart Discovery")
	appendMenu(hMenu, MF_SEPARATOR, 0, "")
	appendMenu(hMenu, MF_STRING, 1004, "Exit")

	pTrackPopupMenu.Call(
		hMenu,
		TPM_LEFTALIGN|TPM_RIGHTBUTTON,
		uintptr(pt.X),
		uintptr(pt.Y),
		0,
		uintptr(hWnd),
		0,
	)

	pPostMessage.Call(uintptr(hWnd), 0, 0, 0)
}

func appendMenu(hMenu uintptr, flags uint32, id uintptr, text string) {
	uText, _ := syscall.UTF16PtrFromString(text)
	pAppendMenu.Call(hMenu, uintptr(flags), id, uintptr(unsafe.Pointer(uText)))
}

// Start launches the system tray icon and handles menu events in a background routine.
func Start(onOpenDashboard, onSettings, onRestartDiscovery, onExit func()) {
	onOpenDashboardCB = onOpenDashboard
	onSettingsCB = onSettings
	onRestartDiscoveryCB = onRestartDiscovery
	onExitCB = onExit

	go func() {
		hInstance, _, _ := pGetModuleHandle.Call(0)

		className, _ := syscall.UTF16PtrFromString("P2PTransferTrayClass")

		var wc WNDCLASSEXW
		wc.CbSize = uint32(unsafe.Sizeof(wc))
		wc.LpfnWndProc = syscall.NewCallback(wndProc)
		wc.HInstance = syscall.Handle(hInstance)
		wc.LpszClassName = className

		r, _, err := pRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))
		if r == 0 {
			logger.Error("Failed to register window class: %v", err)
			return
		}

		hWnd, _, err := pCreateWindowEx.Call(
			0,
			uintptr(unsafe.Pointer(className)),
			uintptr(unsafe.Pointer(className)),
			0,
			0, 0, 0, 0,
			0, // HWND_MESSAGE
			0,
			hInstance,
			0,
		)
		if hWnd == 0 {
			logger.Error("Failed to create tray window: %v", err)
			return
		}
		globalHWnd = syscall.Handle(hWnd)

		// Load custom icon if available in Temp directory (extracted in main.go)
		var hIcon uintptr
		icoPath := filepath.Join(os.TempDir(), "p2p-transfer-favicon.ico")
		if _, err := os.Stat(icoPath); err == nil {
			pathPtr, _ := syscall.UTF16PtrFromString(icoPath)
			res, _, _ := pLoadImage.Call(
				0,
				uintptr(unsafe.Pointer(pathPtr)),
				1,                      // IMAGE_ICON
				0,                      // cx
				0,                      // cy
				0x00000010|0x00000040, // LR_LOADFROMFILE | LR_DEFAULTSIZE
			)
			hIcon = res
		}
		if hIcon == 0 {
			res, _, _ := pLoadIcon.Call(0, uintptr(IDI_APPLICATION))
			hIcon = res
		}

		var nid NOTIFYICONDATAW
		nid.CbSize = uint32(unsafe.Sizeof(nid))
		nid.HWnd = globalHWnd
		nid.UID = 1
		nid.UFlags = NIF_MESSAGE | NIF_ICON | NIF_TIP
		nid.UCallbackMessage = WM_TRAY
		nid.HIcon = syscall.Handle(hIcon)

		tipBytes := []byte("P2P File Transfer")
		for i, b := range tipBytes {
			if i >= 127 {
				break
			}
			nid.SzTip[i] = uint16(b)
		}

		pShellNotifyIcon.Call(NIM_ADD, uintptr(unsafe.Pointer(&nid)))

		var msg struct {
			HWnd    syscall.Handle
			Message uint32
			WParam  uintptr
			LParam  uintptr
			Time    uint32
			Pt      POINT
		}

		for {
			r, _, _ := pGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
			if int32(r) <= 0 {
				break
			}
			pTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
			pDispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))
		}

		// Remove icon on exit
		pShellNotifyIcon.Call(NIM_DELETE, uintptr(unsafe.Pointer(&nid)))
	}()
}

// ShowNotification pops a native Windows balloon tooltip from the system tray.
func ShowNotification(title, message string) {
	if globalHWnd == 0 {
		return
	}

	var nid NOTIFYICONDATAW
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	nid.HWnd = globalHWnd
	nid.UID = 1
	nid.UFlags = NIF_INFO

	title16, _ := syscall.UTF16FromString(title)
	for i, v := range title16 {
		if i >= 63 {
			break
		}
		nid.SzInfoTitle[i] = v
	}

	msg16, _ := syscall.UTF16FromString(message)
	for i, v := range msg16 {
		if i >= 255 {
			break
		}
		nid.SzInfo[i] = v
	}

	nid.DwInfoFlags = NIIF_INFO

	pShellNotifyIcon.Call(NIM_MODIFY, uintptr(unsafe.Pointer(&nid)))
}
