//go:build windows

package cdt

import (
	"fmt"
	"os"
	"path/filepath"

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

	handle, err := windows.LoadLibrary(absPath)
	if err != nil {
		return nil, fmt.Errorf("LoadLibrary failed: %w", err)
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
