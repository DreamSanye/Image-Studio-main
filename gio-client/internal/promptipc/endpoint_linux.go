//go:build linux

package promptipc

import (
	"os"
	"path/filepath"

	gioCompat "image-studio/gio-client/internal/compat"
)

func endpoint() (string, string, error) {
	root, err := gioCompat.StableDataRoot()
	if err != nil {
		return "", "", err
	}
	controlDir := filepath.Join(root, "control")
	if err := os.MkdirAll(controlDir, 0o700); err != nil {
		return "", "", err
	}
	return "unix", filepath.Join(controlDir, "prompt-import.sock"), nil
}
