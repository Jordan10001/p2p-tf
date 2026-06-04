//go:build !windows

package utils

// SelectLocalFiles is a no-op stub for non-windows platforms.
func SelectLocalFiles() ([]string, error) {
	return nil, nil
}

// SelectLocalFolder is a no-op stub for non-windows platforms.
func SelectLocalFolder() (string, error) {
	return "", nil
}
