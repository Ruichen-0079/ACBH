package mcimport

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type ServerType string

const (
	Fabric   ServerType = "Fabric"
	Paper    ServerType = "Paper"
	Vanilla  ServerType = "Vanilla"
	Velocity ServerType = "Velocity"
	Unknown  ServerType = "Unknown"
)

type Report struct {
	ServerDir        string            `json:"serverDir"`
	ServerType       ServerType        `json:"serverType"`
	LaunchJar        string            `json:"launchJar,omitempty"`
	SuggestedCommand string            `json:"suggestedCommand,omitempty"`
	HasMods          bool              `json:"hasMods"`
	HasConfig        bool              `json:"hasConfig"`
	HasWorld         bool              `json:"hasWorld"`
	HasProperties    bool              `json:"hasProperties"`
	HasEULA          bool              `json:"hasEula"`
	Properties       map[string]string `json:"properties,omitempty"`
	RCON             RCONReport        `json:"rcon"`
	Warnings         []string          `json:"warnings,omitempty"`
}

type RCONReport struct {
	Enabled        bool   `json:"enabled"`
	Port           string `json:"port,omitempty"`
	PasswordSet    bool   `json:"passwordSet"`
	ChineseMessage string `json:"chineseMessage,omitempty"`
}

func Inspect(serverDir string) (Report, error) {
	serverDir = filepath.Clean(strings.TrimSpace(serverDir))
	if serverDir == "" {
		return Report{}, fmt.Errorf("请选择 Minecraft 服务端目录")
	}
	info, err := os.Stat(serverDir)
	if err != nil {
		return Report{}, fmt.Errorf("服务端目录不存在: %w", err)
	}
	if !info.IsDir() {
		return Report{}, fmt.Errorf("服务端路径不是目录: %s", serverDir)
	}

	report := Report{ServerDir: serverDir, ServerType: Unknown}
	report.HasMods = exists(filepath.Join(serverDir, "mods"))
	report.HasConfig = exists(filepath.Join(serverDir, "config"))
	report.HasWorld = exists(filepath.Join(serverDir, "world"))
	report.HasProperties = exists(filepath.Join(serverDir, "server.properties"))
	report.HasEULA = exists(filepath.Join(serverDir, "eula.txt"))

	report.ServerType, report.LaunchJar = detectType(serverDir)
	if report.LaunchJar != "" {
		report.SuggestedCommand = fmt.Sprintf("java -Xms2G -Xmx4G -jar %s nogui", report.LaunchJar)
	}
	if !report.HasProperties {
		report.Warnings = append(report.Warnings, "未找到 server.properties，请确认选择的是服务端根目录。")
	}
	if !report.HasEULA {
		report.Warnings = append(report.Warnings, "未找到 eula.txt。必须确认 Minecraft EULA 后才能写入 eula=true。")
	}
	if report.HasProperties {
		props, err := readProperties(filepath.Join(serverDir, "server.properties"))
		if err != nil {
			return Report{}, err
		}
		report.Properties = props
		report.RCON = inspectRCON(props)
	} else {
		report.RCON.ChineseMessage = "未找到 server.properties，无法检测 RCON。"
	}
	return report, nil
}

func detectType(serverDir string) (ServerType, string) {
	for _, candidate := range []struct {
		file string
		kind ServerType
	}{
		{"fabric-server-launch.jar", Fabric},
		{"paper.jar", Paper},
		{"velocity.jar", Velocity},
		{"server.jar", Vanilla},
	} {
		if exists(filepath.Join(serverDir, candidate.file)) {
			return candidate.kind, candidate.file
		}
	}
	return Unknown, ""
}

func inspectRCON(props map[string]string) RCONReport {
	enabled := strings.EqualFold(strings.TrimSpace(props["enable-rcon"]), "true")
	port := strings.TrimSpace(props["rcon.port"])
	passwordSet := strings.TrimSpace(props["rcon.password"]) != ""
	report := RCONReport{
		Enabled:     enabled,
		Port:        port,
		PasswordSet: passwordSet,
	}
	switch {
	case !enabled:
		report.ChineseMessage = "RCON 未开启。请在 server.properties 设置 enable-rcon=true，并设置 rcon.password。"
	case !passwordSet:
		report.ChineseMessage = "RCON 已开启，但 rcon.password 为空。请设置强密码。"
	default:
		report.ChineseMessage = "RCON 配置看起来可用。"
	}
	return report
}

func readProperties(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("读取 server.properties 失败: %w", err)
	}
	defer file.Close()

	props := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		props[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("读取 server.properties 失败: %w", err)
	}
	return props, nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
