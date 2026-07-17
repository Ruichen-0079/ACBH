package hobbyagent

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type PreflightResult struct {
	ServerDir    string `json:"server_dir"`
	JavaPath     string `json:"java_path"`
	JarPath      string `json:"jar_path"`
	EULAAccepted bool   `json:"eula_accepted"`
}

func PreflightServer(serverDir string) (PreflightResult, error) {
	absolute, err := filepath.Abs(strings.TrimSpace(serverDir))
	if err != nil {
		return PreflightResult{}, fmt.Errorf("resolve server directory: %w", err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return PreflightResult{}, fmt.Errorf("inspect server directory: %w", err)
	}
	if !info.IsDir() {
		return PreflightResult{}, errors.New("server_dir is not a directory")
	}
	javaPath, err := exec.LookPath("java")
	if err != nil {
		return PreflightResult{}, errors.New("java was not found on PATH")
	}
	eula, err := eulaAccepted(filepath.Join(absolute, "eula.txt"))
	if err != nil {
		return PreflightResult{}, err
	}
	if !eula {
		return PreflightResult{}, errors.New("Minecraft EULA is not accepted in eula.txt")
	}
	jarPath, err := findServerJar(absolute)
	if err != nil {
		return PreflightResult{}, err
	}
	return PreflightResult{ServerDir: absolute, JavaPath: javaPath, JarPath: jarPath, EULAAccepted: true}, nil
}

func eulaAccepted(path string) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, fmt.Errorf("read eula.txt: %w", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(strings.ToLower(scanner.Text()))
		if line == "eula=true" {
			return true, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return false, err
	}
	return false, nil
}

func findServerJar(directory string) (string, error) {
	preferred := filepath.Join(directory, "server.jar")
	if info, err := os.Stat(preferred); err == nil && !info.IsDir() {
		return preferred, nil
	}
	matches, err := filepath.Glob(filepath.Join(directory, "*.jar"))
	if err != nil || len(matches) == 0 {
		return "", errors.New("no Minecraft server jar was found")
	}
	return matches[0], nil
}

func probeAddress(ctx context.Context, address string, timeout time.Duration) error {
	dialer := net.Dialer{Timeout: timeout}
	connection, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return err
	}
	return connection.Close()
}
