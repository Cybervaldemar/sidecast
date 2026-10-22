package desktop

import (
	"context"
	_ "embed"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32              = windows.NewLazySystemDLL("user32.dll")
	shell32             = windows.NewLazySystemDLL("shell32.dll")
	registerClass       = user32.NewProc("RegisterClassExW")
	unregisterClass     = user32.NewProc("UnregisterClassW")
	createWindow        = user32.NewProc("CreateWindowExW")
	destroyWindow       = user32.NewProc("DestroyWindow")
	defWindowProc       = user32.NewProc("DefWindowProcW")
	getMessage          = user32.NewProc("GetMessageW")
	translateMessage    = user32.NewProc("TranslateMessage")
	dispatchMessage     = user32.NewProc("DispatchMessageW")
	postMessage         = user32.NewProc("PostMessageW")
	postQuitMessage     = user32.NewProc("PostQuitMessage")
	registerMessage     = user32.NewProc("RegisterWindowMessageW")
	createPopupMenu     = user32.NewProc("CreatePopupMenu")
	appendMenu          = user32.NewProc("AppendMenuW")
	destroyMenu         = user32.NewProc("DestroyMenu")
	trackPopupMenu      = user32.NewProc("TrackPopupMenu")
	setForegroundWindow = user32.NewProc("SetForegroundWindow")
	getCursorPos        = user32.NewProc("GetCursorPos")
	createIcon          = user32.NewProc("CreateIconFromResourceEx")
	destroyIcon         = user32.NewProc("DestroyIcon")
	messageBox          = user32.NewProc("MessageBoxW")
	notifyIcon          = shell32.NewProc("Shell_NotifyIconW")
	shellExecute        = shell32.NewProc("ShellExecuteW")
)

const (
	wmClose   = 0x0010
	wmDestroy = 0x0002
	wmTray    = 0x8001
	wmCommand = 0x0111
	menuOpen  = 1
	menuExit  = 2
)

// These layouts follow the Windows SDK WNDCLASSEXW, MSG and NOTIFYICONDATAW.
// uintptr fields preserve the ABI on both Windows amd64 and arm64.
type windowClass struct {
	Size, Style                        uint32
	Proc                               uintptr
	ClassExtra, WindowExtra            int32
	Instance, Icon, Cursor, Background uintptr
	MenuName, ClassName                *uint16
	SmallIcon                          uintptr
}

type point struct{ X, Y int32 }
type message struct {
	Window         uintptr
	ID             uint32
	WParam, LParam uintptr
	Time           uint32
	Point          point
	Private        uint32
}

type notificationIcon struct {
	Size                uint32
	Window              uintptr
	ID, Flags, Callback uint32
	Icon                uintptr
	Tip                 [128]uint16
	State, StateMask    uint32
	Info                [256]uint16
	Version             uint32
	InfoTitle           [64]uint16
	InfoFlags           uint32
	GUID                windows.GUID
	BalloonIcon         uintptr
}

func wide(s string) *uint16 { return windows.StringToUTF16Ptr(s) }

func DataDirectory() string {
	executable, err := os.Executable()
	if err != nil {
		return "data"
	}
	directory := filepath.Dir(executable)
	// Repository builds share the existing project data, outside build/.
	if filepath.Base(filepath.Dir(directory)) == "build" {
		return filepath.Join(directory, "..", "..", "data")
	}
	return filepath.Join(directory, "data")
}

func ShowError(err error) {
	messageBox.Call(0, uintptr(unsafe.Pointer(wide(err.Error()))), uintptr(unsafe.Pointer(wide("Sidecast — ошибка"))), 0x10)
}

func openBrowser(address string) {
	result, _, _ := shellExecute.Call(0, uintptr(unsafe.Pointer(wide("open"))), uintptr(unsafe.Pointer(wide(address))), 0, 0, 1)
	if result <= 32 {
		ShowError(fmt.Errorf("Не удалось открыть браузер. Откройте %s вручную.", address))
	}
}

//go:embed app.ico
var applicationIcon []byte

// makeIcon uses the same ICO that is linked into the executable's resources.
func makeIcon() (uintptr, error) {
	const size = 32
	for i := 0; i < int(binary.LittleEndian.Uint16(applicationIcon[4:6])); i++ {
		entry := applicationIcon[6+i*16 : 6+(i+1)*16]
		if entry[0] != size || entry[1] != size {
			continue
		}
		length := binary.LittleEndian.Uint32(entry[8:12])
		offset := binary.LittleEndian.Uint32(entry[12:16])
		pixels := applicationIcon[offset : offset+length]
		icon, _, err := createIcon.Call(uintptr(unsafe.Pointer(&pixels[0])), uintptr(length), 1, 0x00030000, size, size, 0)
		if icon == 0 {
			return 0, fmt.Errorf("create tray icon: %w", err)
		}
		return icon, nil
	}
	return 0, fmt.Errorf("application icon has no %dpx image", size)
}

func Run(ctx context.Context, address string, stop context.CancelFunc) error {
	return run(ctx, address, stop, openBrowser)
}

func run(ctx context.Context, address string, stop context.CancelFunc, open func(string)) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	icon, err := makeIcon()
	if err != nil {
		return err
	}
	defer destroyIcon.Call(icon)
	menu, _, err := createPopupMenu.Call()
	if menu == 0 {
		return fmt.Errorf("create tray menu: %w", err)
	}
	defer destroyMenu.Call(menu)
	for _, item := range []struct {
		id    uintptr
		title string
	}{{menuOpen, "Открыть Sidecast"}, {menuExit, "Выход"}} {
		if ok, _, err := appendMenu.Call(menu, 0, item.id, uintptr(unsafe.Pointer(wide(item.title)))); ok == 0 {
			return fmt.Errorf("add tray menu item: %w", err)
		}
	}
	taskbarCreated, _, _ := registerMessage.Call(uintptr(unsafe.Pointer(wide("TaskbarCreated"))))
	var data notificationIcon
	var trayErr error
	callback := windows.NewCallback(func(window uintptr, msg uint32, wParam, lParam uintptr) uintptr {
		switch {
		case taskbarCreated != 0 && msg == uint32(taskbarCreated):
			// Explorer recreates its notification area after a restart.
			if ok, _, err := notifyIcon.Call(0, uintptr(unsafe.Pointer(&data))); ok == 0 {
				trayErr = fmt.Errorf("restore tray icon: %w", err)
				stop()
			}
			return 0
		case msg == wmTray:
			switch lParam {
			case 0x0203: // WM_LBUTTONDBLCLK
				open(address)
			case 0x0205, 0x007b: // WM_RBUTTONUP / WM_CONTEXTMENU
				var cursor point
				getCursorPos.Call(uintptr(unsafe.Pointer(&cursor)))
				setForegroundWindow.Call(window)
				command, _, _ := trackPopupMenu.Call(menu, 0x0102, uintptr(cursor.X), uintptr(cursor.Y), 0, window, 0)
				postMessage.Call(window, 0, 0, 0)
				if command != 0 {
					postMessage.Call(window, wmCommand, command, 0)
				}
			}
			return 0
		case msg == wmCommand:
			switch wParam & 0xffff {
			case menuOpen:
				open(address)
			case menuExit:
				stop()
			}
			return 0
		case msg == wmClose:
			stop()
			destroyWindow.Call(window)
			return 0
		case msg == wmDestroy:
			postQuitMessage.Call(0)
			return 0
		}
		result, _, _ := defWindowProc.Call(window, uintptr(msg), wParam, lParam)
		return result
	})
	className := wide("Sidecast.Tray")
	var instance windows.Handle
	if err := windows.GetModuleHandleEx(2, nil, &instance); err != nil {
		return err
	}
	class := windowClass{Proc: callback, Instance: uintptr(instance), ClassName: className, Icon: icon, SmallIcon: icon}
	class.Size = uint32(unsafe.Sizeof(class))
	if atom, _, err := registerClass.Call(uintptr(unsafe.Pointer(&class))); atom == 0 {
		return fmt.Errorf("register tray window: %w", err)
	}
	defer unregisterClass.Call(uintptr(unsafe.Pointer(className)), uintptr(instance))
	// A hidden top-level window receives TaskbarCreated broadcasts; a
	// message-only window would not receive them.
	window, _, err := createWindow.Call(0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(wide("Sidecast"))), 0, 0, 0, 0, 0, 0, 0, uintptr(instance), 0)
	if window == 0 {
		return fmt.Errorf("create tray window: %w", err)
	}
	defer destroyWindow.Call(window)
	data = notificationIcon{Window: window, ID: 1, Flags: 1 | 2 | 4, Callback: wmTray, Icon: icon}
	data.Size = uint32(unsafe.Sizeof(data))
	copy(data.Tip[:], windows.StringToUTF16("Sidecast · "+address))
	if ok, _, err := notifyIcon.Call(0, uintptr(unsafe.Pointer(&data))); ok == 0 {
		return fmt.Errorf("show tray icon: %w", err)
	}
	defer notifyIcon.Call(2, uintptr(unsafe.Pointer(&data)))
	finished := make(chan struct{})
	defer close(finished)
	go func() {
		select {
		case <-ctx.Done():
			postMessage.Call(window, wmClose, 0, 0)
		case <-finished:
		}
	}()
	if ctx.Err() == nil {
		open(address)
	}
	var event message
	for {
		result, _, err := getMessage.Call(uintptr(unsafe.Pointer(&event)), 0, 0, 0)
		if int32(result) == -1 {
			return fmt.Errorf("tray message loop: %w", err)
		}
		if result == 0 {
			return trayErr
		}
		translateMessage.Call(uintptr(unsafe.Pointer(&event)))
		dispatchMessage.Call(uintptr(unsafe.Pointer(&event)))
	}
}
