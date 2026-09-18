//go:build !windows

package cmd

import "os"

func addDirToWindowsUserPath(targetDir string) (bool, error) {
	return false, nil
}

func replaceBinary(tmpPath, destPath string) error {
	_ = os.Remove(destPath)
	return os.Rename(tmpPath, destPath)
}

func cleanupOldBinaries(destDir, binName string) {
	// No-op on non-Windows
}
