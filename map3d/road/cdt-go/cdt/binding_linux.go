//go:build linux

package cdt

import (
	"fmt"

	"github.com/ebitengine/purego"
)

func load() (*api, error) {
	if globalAPI != nil {
		return globalAPI, nil
	}

	libPath, err := libraryPath()
	if err != nil {
		return nil, err
	}

	handle, err := purego.Dlopen(libPath, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil, fmt.Errorf("Dlopen failed: %w", err)
	}

	a := &api{handle: handle}
	purego.RegisterLibFunc(&a.triangulate, a.handle, "cdt_triangulate")
	purego.RegisterLibFunc(&a.freeResult, a.handle, "cdt_free_result")

	globalAPI = a
	return a, nil
}

func Close() error {
	if globalAPI == nil {
		return nil
	}
	err := purego.Dlclose(globalAPI.handle)
	globalAPI = nil
	return err
}
