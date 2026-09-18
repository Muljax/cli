//go:build !windows

package cmd

func addDirToWindowsUserPath(targetDir string) (bool, error) {
	return false, nil
}
