//go:build windows

package cdt

import (
	"debug/pe"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ebitengine/purego"
	"golang.org/x/sys/windows"
)

func load() (*api, error) {
	if globalAPI != nil {
		return globalAPI, nil
	}

	libPath, err := libraryPath()
	if err != nil {
		return nil, err
	}

	absPath, err := filepath.Abs(libPath)
	if err != nil {
		return nil, fmt.Errorf("abs path failed: %w", err)
	}

	if _, err := os.Stat(absPath); err != nil {
		return nil, fmt.Errorf("dll not found: %s: %w", absPath, err)
	}

	// 同时搜索库所在目录及系统默认目录，不修改进程工作目录或 PATH。
	handle, err := windows.LoadLibraryEx(absPath, 0,
		windows.LOAD_LIBRARY_SEARCH_DLL_LOAD_DIR|windows.LOAD_LIBRARY_SEARCH_DEFAULT_DIRS)
	if err != nil {
		imports := "unknown"
		if file, openErr := pe.Open(absPath); openErr == nil {
			if names, importErr := file.ImportedLibraries(); importErr == nil {
				imports = strings.Join(names, ", ")
			}
			_ = file.Close()
		}
		return nil, fmt.Errorf("LoadLibraryEx failed for %s (DLL exists; direct dependencies: %s). Check matching x64 MinGW runtime DLLs, especially libwinpthread-1.dll; place them beside cdt_bridge.dll: %w", absPath, imports, err)
	}

	a := &api{handle: uintptr(handle)}
	purego.RegisterLibFunc(&a.triangulate, a.handle, "cdt_triangulate")
	purego.RegisterLibFunc(&a.freeResult, a.handle, "cdt_free_result")

	globalAPI = a
	return a, nil
}

func Close() error {
	if globalAPI == nil {
		return nil
	}
	err := windows.FreeLibrary(windows.Handle(globalAPI.handle))
	globalAPI = nil
	return err
}
