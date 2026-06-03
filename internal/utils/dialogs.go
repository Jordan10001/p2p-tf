package utils

import (
	"bytes"
	"os/exec"
	"strings"
	"syscall"
	"p2p-transfer/internal/logger"
)

// SelectLocalFiles opens a native Windows multi-file selection dialog.
func SelectLocalFiles() ([]string, error) {
	psScript := `
Add-Type -AssemblyName System.Windows.Forms
$dialog = New-Object System.Windows.Forms.OpenFileDialog
$dialog.Multiselect = $true
$dialog.Title = "Select Files for P2P Transfer"
$dialog.Filter = "All Files (*.*)|*.*"
$res = $dialog.ShowDialog()
if ($res -eq [System.Windows.Forms.DialogResult]::OK) {
    $dialog.FileNames
}
`
	cmd := exec.Command("powershell", "-STA", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", psScript)
	
	// Hide the powershell console window from popping up
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}

	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		logger.Error("SelectLocalFiles powershell failed: %v, stderr: %s", err, stderr.String())
		return nil, err
	}

	lines := strings.Split(out.String(), "\r\n")
	var files []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

// SelectLocalFolder opens a native Windows folder selection dialog.
func SelectLocalFolder() (string, error) {
	psScript := `
Add-Type -AssemblyName System.Windows.Forms
$dialog = New-Object System.Windows.Forms.FolderBrowserDialog
$dialog.Description = "Select Folder for P2P Transfer"
$res = $dialog.ShowDialog()
if ($res -eq [System.Windows.Forms.DialogResult]::OK) {
    $dialog.SelectedPath
}
`
	cmd := exec.Command("powershell", "-STA", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", psScript)
	
	// Hide the powershell console window from popping up
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}

	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		logger.Error("SelectLocalFolder powershell failed: %v, stderr: %s", err, stderr.String())
		return "", err
	}

	folder := strings.TrimSpace(out.String())
	return folder, nil
}
