//go:build windows

package nativecapture

import (
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func supported() bool           { return true }
func hideProcess(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} }

type rect struct{ Left, Top, Right, Bottom int32 }

// Windows callback thunks cannot be freed. Allocate them once and serialize
// enumeration instead of retaining a new closure and its results on every refresh.
var (
	sourceMu        sync.Mutex
	result          []Source
	user            = windows.NewLazySystemDLL("user32.dll")
	visible         = user.NewProc("IsWindowVisible")
	title           = user.NewProc("GetWindowTextW")
	getRect         = user.NewProc("GetWindowRect")
	getPID          = user.NewProc("GetWindowThreadProcessId")
	monitorCallback = syscall.NewCallback(func(handle, _ uintptr, area *rect, _ uintptr) uintptr {
		result = append(result, Source{ID: "monitor:" + strconv.FormatUint(uint64(handle), 10), Name: fmt.Sprintf("Экран %d · %d×%d", len(result)+1, area.Right-area.Left, area.Bottom-area.Top), Kind: "monitor", Width: int(area.Right - area.Left), Height: int(area.Bottom - area.Top)})
		return 1
	})
	windowCallback = syscall.NewCallback(func(handle, _ uintptr) uintptr {
		shown, _, _ := visible.Call(handle)
		if shown == 0 {
			return 1
		}
		var text [512]uint16
		length, _, _ := title.Call(handle, uintptr(unsafe.Pointer(&text[0])), uintptr(len(text)))
		if length == 0 {
			return 1
		}
		var area rect
		valid, _, _ := getRect.Call(handle, uintptr(unsafe.Pointer(&area)))
		if valid == 0 || area.Right <= area.Left || area.Bottom <= area.Top {
			return 1
		}
		var pid uint32
		getPID.Call(handle, uintptr(unsafe.Pointer(&pid)))
		result = append(result, Source{ID: "window:" + strconv.FormatUint(uint64(handle), 10), Name: windows.UTF16ToString(text[:]), Kind: "window", Width: int(area.Right - area.Left), Height: int(area.Bottom - area.Top), ProcessID: pid})
		return 1
	})
)

func sources() ([]Source, error) {
	sourceMu.Lock()
	defer sourceMu.Unlock()
	result = []Source{}
	defer func() { result = nil }()
	ok, _, err := user.NewProc("EnumDisplayMonitors").Call(0, 0, monitorCallback, 0)
	if ok == 0 {
		return nil, err
	}
	ok, _, err = user.NewProc("EnumWindows").Call(windowCallback, 0)
	if ok == 0 {
		return nil, err
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Kind != result[j].Kind {
			return result[i].Kind == "monitor"
		}
		return result[i].Name < result[j].Name
	})
	return result, nil
}
