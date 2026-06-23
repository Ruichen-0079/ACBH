package mcimport

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

type ServerType string

const (
	Fabric    ServerType = "Fabric"
	Paper     ServerType = "Paper"
	Purpur    ServerType = "Purpur"
	Forge     ServerType = "Forge"
	NeoForge  ServerType = "NeoForge"
	Cleanroom ServerType = "Cleanroom"
	Vanilla   ServerType = "Vanilla"
	Velocity  ServerType = "Velocity"
	Unknown   ServerType = "Unknown"
)

type Report struct {
	ServerDir        string            `json:"serverDir"`
	ServerType       ServerType        `json:"serverType"`
	LaunchJar        string            `json:"launchJar,omitempty"`
	LaunchEntry      string            `json:"launchEntry,omitempty"`
	LaunchCandidates []string          `json:"launchCandidates,omitempty"`
	SuggestedCommand string            `json:"suggestedCommand,omitempty"`
	JavaRequirement  string            `json:"javaRequirement,omitempty"`
	ServerPort       string            `json:"serverPort,omitempty"`
	WorldDir         string            `json:"worldDir,omitempty"`
	HasMods          bool              `json:"hasMods"`
	HasConfig        bool              `json:"hasConfig"`
	HasWorld         bool              `json:"hasWorld"`
	HasProperties    bool              `json:"hasProperties"`
	HasEULA          bool              `json:"hasEula"`
	EULAAccepted     bool              `json:"eulaAccepted"`
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

	detection := detectType(serverDir)
	report.ServerType = detection.kind
	report.LaunchEntry = detection.entry
	report.LaunchJar = detection.jar
	report.LaunchCandidates = detection.candidates
	report.JavaRequirement = javaRequirementFor(detection.kind)
	if report.LaunchEntry != "" {
		report.SuggestedCommand = suggestedCommand(report.LaunchEntry)
	}
	if !report.HasProperties {
		report.Warnings = append(report.Warnings, "未找到 server.properties，请确认选择的是服务端根目录。")
	}
	eulaExists, eulaAccepted, eulaErr := checkEULA(serverDir)
	report.HasEULA = eulaExists
	report.EULAAccepted = eulaAccepted
	if eulaErr != nil {
		report.Warnings = append(report.Warnings, "读取 eula.txt 失败: "+eulaErr.Error())
	} else if !eulaExists {
		report.Warnings = append(report.Warnings, "未找到 eula.txt。必须确认 Minecraft EULA 后才能写入 eula=true。")
	} else if !eulaAccepted {
		report.Warnings = append(report.Warnings, "eula.txt 中的 eula 不是 true。请设置为 eula=true 后才能启动服务端。")
	}
	if report.HasProperties {
		props, err := readProperties(filepath.Join(serverDir, "server.properties"))
		if err != nil {
			return Report{}, err
		}
		report.Properties = props
		report.RCON = inspectRCON(props)
		if port := strings.TrimSpace(props["server-port"]); port != "" {
			report.ServerPort = port
		}
		if levelName := strings.TrimSpace(props["level-name"]); levelName != "" {
			report.WorldDir = filepath.Join(serverDir, filepath.Clean(levelName))
		}
	} else {
		report.RCON.ChineseMessage = "未找到 server.properties，无法检测 RCON。"
	}
	if report.ServerPort == "" {
		report.ServerPort = "25565"
	}
	if report.WorldDir == "" && report.HasWorld {
		report.WorldDir = filepath.Join(serverDir, "world")
	}
	return report, nil
}

type launchDetection struct {
	kind       ServerType
	entry      string
	jar        string
	candidates []string
}

func detectType(serverDir string) launchDetection {
	var candidates []string
	for _, script := range []string{"run.bat", "start.bat"} {
		if exists(filepath.Join(serverDir, script)) {
			candidates = append(candidates, script)
		}
	}
	jarCandidates := detectJarCandidates(serverDir)
	candidates = append(candidates, jarCandidates...)
	if len(candidates) == 0 {
		return launchDetection{kind: Unknown}
	}

	entry := candidates[0]
	kind := kindForEntry(entry)
	jar := ""
	if strings.HasSuffix(strings.ToLower(entry), ".jar") {
		jar = entry
	}
	if strings.HasSuffix(strings.ToLower(entry), ".bat") {
		for _, candidate := range jarCandidates {
			jar = candidate
			break
		}
	}
	return launchDetection{kind: kind, entry: entry, jar: jar, candidates: candidates}
}

func detectJarCandidates(serverDir string) []string {
	entries, err := os.ReadDir(serverDir)
	if err != nil {
		return nil
	}
	var matches []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		lower := strings.ToLower(name)
		if !strings.HasSuffix(lower, ".jar") || excludedJarName(lower) {
			continue
		}
		switch {
		case name == "fabric-server-launch.jar":
			matches = append(matches, name)
		case strings.HasPrefix(lower, "paper-"), lower == "paper.jar":
			matches = append(matches, name)
		case strings.HasPrefix(lower, "purpur-"), lower == "purpur.jar":
			matches = append(matches, name)
		case strings.HasPrefix(lower, "forge-"), lower == "forge.jar":
			matches = append(matches, name)
		case strings.HasPrefix(lower, "neoforge-"), lower == "neoforge.jar":
			matches = append(matches, name)
		case strings.HasPrefix(lower, "cleanroom-"), lower == "cleanroom.jar":
			matches = append(matches, name)
		case strings.HasPrefix(lower, "velocity-"), lower == "velocity.jar":
			matches = append(matches, name)
		case lower == "server.jar":
			matches = append(matches, name)
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		return jarPriority(matches[i]) < jarPriority(matches[j])
	})
	return matches
}

func excludedJarName(lower string) bool {
	excluded := []string{"installer", "sources", "javadoc", "client", "dev", "api", "shadow", "remapped", "mappings"}
	for _, marker := range excluded {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func jarPriority(name string) int {
	lower := strings.ToLower(name)
	switch {
	case lower == "fabric-server-launch.jar":
		return 10
	case strings.HasPrefix(lower, "paper-"), lower == "paper.jar":
		return 20
	case strings.HasPrefix(lower, "purpur-"), lower == "purpur.jar":
		return 21
	case strings.HasPrefix(lower, "velocity-"), lower == "velocity.jar":
		return 30
	case lower == "server.jar":
		return 40
	case strings.HasPrefix(lower, "forge-"), lower == "forge.jar":
		return 50
	case strings.HasPrefix(lower, "neoforge-"), lower == "neoforge.jar":
		return 51
	case strings.HasPrefix(lower, "cleanroom-"), lower == "cleanroom.jar":
		return 52
	default:
		return 100
	}
}

func kindForEntry(entry string) ServerType {
	lower := strings.ToLower(entry)
	switch {
	case strings.HasSuffix(lower, ".bat"):
		return Unknown
	case lower == "fabric-server-launch.jar":
		return Fabric
	case strings.HasPrefix(lower, "paper-"), lower == "paper.jar":
		return Paper
	case strings.HasPrefix(lower, "purpur-"), lower == "purpur.jar":
		return Purpur
	case strings.HasPrefix(lower, "forge-"), lower == "forge.jar":
		return Forge
	case strings.HasPrefix(lower, "neoforge-"), lower == "neoforge.jar":
		return NeoForge
	case strings.HasPrefix(lower, "cleanroom-"), lower == "cleanroom.jar":
		return Cleanroom
	case strings.HasPrefix(lower, "velocity-"), lower == "velocity.jar":
		return Velocity
	case lower == "server.jar":
		return Vanilla
	default:
		return Unknown
	}
}

func suggestedCommand(entry string) string {
	lower := strings.ToLower(entry)
	if strings.HasSuffix(lower, ".bat") {
		if runtime.GOOS == "windows" {
			return "cmd /c " + entry
		}
		return entry
	}
	return fmt.Sprintf("java -Xms2G -Xmx4G -jar %s nogui", entry)
}

func javaRequirementFor(kind ServerType) string {
	switch kind {
	case Forge, Cleanroom:
		return "17"
	case Velocity:
		return "17"
	default:
		return "17"
	}
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

func checkEULA(serverDir string) (exists bool, accepted bool, err error) {
	p := filepath.Join(serverDir, "eula.txt")
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return false, false, nil
		}
		return false, false, err
	}
	exists = true
	lower := strings.ToLower(string(data))
	sc := bufio.NewScanner(strings.NewReader(lower))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.Contains(line, "eula=true") {
			accepted = true
			return exists, accepted, nil
		}
	}
	if err := sc.Err(); err != nil {
		return exists, accepted, err
	}
	return exists, accepted, nil
}
