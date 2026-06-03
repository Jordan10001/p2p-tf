package utils

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
)

// GetLocalIP retrieves the most suitable local IPv4 address, prioritizing physical adapters
// and filtering out common virtual interfaces like WSL, Docker, and Hyper-V.
// It first attempts to use a UDP dial method to find the primary outbound IP.
func GetLocalIP() (string, error) {
	// Try UDP dial first to find the active outbound IP
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err == nil {
		defer conn.Close()
		localAddr, ok := conn.LocalAddr().(*net.UDPAddr)
		if ok && localAddr.IP != nil && !localAddr.IP.IsLoopback() && localAddr.IP.To4() != nil {
			return localAddr.IP.String(), nil
		}
	}

	// Fallback to scanning interfaces if UDP dial fails (e.g. completely offline)
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}

	var candidates []string

	for _, iface := range ifaces {
		// Skip down, loopback, or virtual/tunnel interfaces
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		// Filter out common virtual interface names
		name := strings.ToLower(iface.Name)
		isVirtual := false
		virtualKeywords := []string{"wsl", "docker", "vbox", "virtualbox", "vmware", "hyper-v", "vbridge", "tun", "tap", "veth"}
		for _, kw := range virtualKeywords {
			if strings.Contains(name, kw) {
				isVirtual = true
				break
			}
		}
		if isVirtual {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			if ip == nil || ip.IsLoopback() || ip.To4() == nil {
				continue
			}

			ipStr := ip.String()
			
			// Prioritize standard LAN ranges
			if strings.HasPrefix(ipStr, "192.168.") {
				return ipStr, nil // Highest priority: most common home LAN
			}
			
			candidates = append(candidates, ipStr)
		}
	}

	// Fallback to any suitable candidate found (e.g., 10.x.x.x or 172.x.x.x if not virtual)
	for _, ip := range candidates {
		if strings.HasPrefix(ip, "10.") || strings.HasPrefix(ip, "172.") {
			return ip, nil
		}
	}

	if len(candidates) > 0 {
		return candidates[0], nil
	}

	return "", fmt.Errorf("no suitable local IP address found")
}

// GetUniqueFileName handles file name collisions by appending (1), (2), etc.
func GetUniqueFileName(dir, filename string) string {
	ext := filepath.Ext(filename)
	nameWithoutExt := filename[0 : len(filename)-len(ext)]
	targetPath := filepath.Join(dir, filename)

	if _, err := os.Stat(targetPath); os.IsNotExist(err) {
		return filename
	}

	counter := 1
	for {
		newFilename := fmt.Sprintf("%s (%d)%s", nameWithoutExt, counter, ext)
		newTargetPath := filepath.Join(dir, newFilename)
		if _, err := os.Stat(newTargetPath); os.IsNotExist(err) {
			return newFilename
		}
		counter++
	}
}

// SafeFileName removes path traversal components and replaces illegal characters.
func SafeFileName(name string) string {
	// Remove folder paths if any
	base := filepath.Base(name)
	
	// Replace standard Windows illegal characters
	illegals := []string{"<", ">", ":", "\"", "/", "\\", "|", "?", "*"}
	for _, char := range illegals {
		base = strings.ReplaceAll(base, char, "_")
	}
	
	return base
}

// SanitizePath prevents directory traversal attacks.
func SanitizePath(baseDir, relativePath string) (string, error) {
	// Clean relativePath to resolve references like ..
	cleanRel := filepath.Clean(relativePath)
	if strings.HasPrefix(cleanRel, "..") || filepath.IsAbs(cleanRel) {
		// Try to strip absolute indicator or root prefixes to keep it inside download dir
		cleanRel = strings.TrimPrefix(cleanRel, "/")
		cleanRel = strings.TrimPrefix(cleanRel, "\\")
		cleanRel = filepath.Clean(cleanRel)
		if strings.HasPrefix(cleanRel, "..") {
			return "", fmt.Errorf("directory traversal attempt detected: %s", relativePath)
		}
	}
	
	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return "", err
	}
	
	targetPath := filepath.Join(absBase, cleanRel)
	absTarget, err := filepath.Abs(targetPath)
	if err != nil {
		return "", err
	}
	
	// Ensure target path is actually within the base directory
	if !strings.HasPrefix(absTarget, absBase) {
		return "", fmt.Errorf("path is outside of base directory: %s", targetPath)
	}
	
	return absTarget, nil
}
