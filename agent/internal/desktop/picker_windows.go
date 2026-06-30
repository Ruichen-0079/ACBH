//go:build windows

package desktop

import (
	"errors"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

var (
	modComdlg32  = syscall.NewLazyDLL("comdlg32.dll")
	modShell32   = syscall.NewLazyDLL("shell32.dll")
	modOle32     = syscall.NewLazyDLL("ole32.dll")
	procGetOFN   = modComdlg32.NewProc("GetOpenFileNameW")
	procSHBrowse   = modShell32.NewProc("SHBrowseForFolderW")
	procSHGetPath  = modShell32.NewProc("SHGetPathFromIDListW")
	procCoInit     = modOle32.NewProc("CoInitializeEx")
	procCoUninit   = modOle32.NewProc("CoUninitialize")
)

const (
	ofnFileMustExist  = 0x00001000
	ofnPathMustExist  = 0x00000800
	ofnAllowMultiSel  = 0x00000200
	ofnExplorer       = 0x00080000
	bifReturnOnlyFS   = 0x00000001
	bifDontGoBelowDomain = 0x00000002
	coInitApartmentThreaded = 0x2
	maxPickerFiles    = 64
)

type openFilenameW struct {
	StructSize      uint32
	Owner           uintptr
	Instance        uintptr
	Filter          uintptr
	CustomFilter    uintptr
	MaxCustomFilter uint32
	FilterIndex     uint32
	File            uintptr
	MaxFile         uint32
	FileTitle       uintptr
	MaxFileTitle    uint32
	InitialDir      uintptr
	Title           uintptr
	Flags           uint32
	FileOffset      uint16
	FileExtension   uint16
	DefExt          uintptr
	CustData        uintptr
	Hook            uintptr
	TemplateName    uintptr
	PvReserved      uintptr
	DwReserved      uint32
	FlagsEx         uint32
}

type browseInfoW struct {
	Owner         uintptr
	Root          uintptr
	DisplayName   uintptr
	Title         uintptr
	Flags         uint32
	Callback      uintptr
	Param         uintptr
	Image         int32
}

func pickFolderNative(title, initialDir string) (string, error) {
	_ = initialDir
	ensureCOM()
	defer releaseCOM()
	display := make([]uint16, syscall.MAX_PATH)
	titlePtr, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return "", err
	}
	bi := browseInfoW{
		DisplayName: uintptr(unsafe.Pointer(&display[0])),
		Title:       uintptr(unsafe.Pointer(titlePtr)),
		Flags:       bifReturnOnlyFS | bifDontGoBelowDomain,
	}
	ret, _, callErr := procSHBrowse.Call(uintptr(unsafe.Pointer(&bi)))
	if ret == 0 {
		if callErr != syscall.Errno(0) {
			return "", callErr
		}
		return "", errors.New("folder picker cancelled")
	}
	defer modOle32.NewProc("CoTaskMemFree").Call(ret)
	pathBuf := make([]uint16, syscall.MAX_PATH)
	ok, _, _ := procSHGetPath.Call(ret, uintptr(unsafe.Pointer(&pathBuf[0])))
	if ok == 0 {
		return "", errors.New("folder picker could not resolve selected path")
	}
	path := filepath.Clean(syscall.UTF16ToString(pathBuf))
	if strings.TrimSpace(path) == "" {
		return "", errors.New("folder picker returned empty path")
	}
	return path, nil
}

func pickFileNative(title, filter, initialDir string) (string, error) {
	paths, err := pickFilesNative(title, filter, initialDir, false)
	if err != nil {
		return "", err
	}
	if len(paths) == 0 {
		return "", errors.New("file picker returned no selection")
	}
	return paths[0], nil
}

func pickFilesNative(title, filter, initialDir string, multi bool) ([]string, error) {
	ensureCOM()
	defer releaseCOM()
	buf := make([]uint16, 32*1024)
	titlePtr, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return nil, err
	}
	filterPtr, err := syscall.UTF16PtrFromString(normalizePickerFilter(filter))
	if err != nil {
		return nil, err
	}
	flags := ofnExplorer | ofnFileMustExist | ofnPathMustExist
	if multi {
		flags |= ofnAllowMultiSel
	}
	ofn := openFilenameW{
		StructSize: uint32(unsafe.Sizeof(openFilenameW{})),
		Filter:     uintptr(unsafe.Pointer(filterPtr)),
		File:       uintptr(unsafe.Pointer(&buf[0])),
		MaxFile:    uint32(len(buf)),
		Title:      uintptr(unsafe.Pointer(titlePtr)),
		Flags:      uint32(flags),
	}
	if strings.TrimSpace(initialDir) != "" {
		if dirPtr, err := syscall.UTF16PtrFromString(initialDir); err == nil {
			ofn.InitialDir = uintptr(unsafe.Pointer(dirPtr))
		}
	}
	ok, _, callErr := procGetOFN.Call(uintptr(unsafe.Pointer(&ofn)))
	if ok == 0 {
		if callErr != syscall.Errno(0) {
			return nil, callErr
		}
		return nil, errors.New("file picker cancelled")
	}
	if !multi {
		return []string{filepath.Clean(syscall.UTF16ToString(buf))}, nil
	}
	return parseMultiSelectBuffer(buf), nil
}

func parseMultiSelectBuffer(buf []uint16) []string {
	dir := ""
	var files []string
	start := 0
	for i := 0; i < len(buf); i++ {
		if buf[i] != 0 {
			continue
		}
		part := syscall.UTF16ToString(buf[start:i])
		start = i + 1
		if part == "" {
			break
		}
		if dir == "" {
			dir = part
			continue
		}
		files = append(files, filepath.Clean(filepath.Join(dir, part)))
		if len(files) >= maxPickerFiles {
			break
		}
	}
	if dir != "" && len(files) == 0 {
		return []string{filepath.Clean(dir)}
	}
	return files
}

func normalizePickerFilter(filter string) string {
	if strings.TrimSpace(filter) == "" {
		return "All Files\000*.*\000"
	}
	parts := strings.Split(filter, "|")
	if len(parts) == 1 {
		return parts[0] + "\000" + parts[0] + "\000"
	}
	out := strings.Join(parts, "\000") + "\000"
	return out
}

func ensureCOM() {
	_, _, _ = procCoInit.Call(0, coInitApartmentThreaded)
}

func releaseCOM() {
	_, _, _ = procCoUninit.Call()
}

func PickFolder(title string) (string, error) {
	return PickFolderIn(title, "")
}

func PickFolderIn(title, initialDir string) (string, error) {
	return pickFolderNative(defaultPickerTitle(title, "选择文件夹"), initialDir)
}

func PickFile(title, filter string) (string, error) {
	return PickFileIn(title, filter, "")
}

func PickFileIn(title, filter, initialDir string) (string, error) {
	return pickFileNative(defaultPickerTitle(title, "选择文件"), filter, initialDir)
}

func PickFiles(title, filter string) ([]string, error) {
	return PickFilesIn(title, filter, "")
}

func PickFilesIn(title, filter, initialDir string) ([]string, error) {
	return pickFilesNative(defaultPickerTitle(title, "选择文件"), filter, initialDir, true)
}

func defaultPickerTitle(title, fallback string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return fallback
	}
	return title
}

func pickerAvailable() bool { return true }

func validatePickerPath(path string) error {
	return validateExistingPath(path, false)
}