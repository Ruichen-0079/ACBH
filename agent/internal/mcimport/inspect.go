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
	Fabric       ServerType = "Fabric"
	Paper        ServerType = "Paper"
	Purpur       ServerType = "Purpur"
	Forge        ServerType = "Forge"
	NeoForge     ServerType = "NeoForge"
	Cleanroom    ServerType = "Cleanroom"
	Vanilla      ServerType = "Vanilla"
	Velocity     ServerType = "Velocity"
	CustomScript ServerType = "CustomScript"
	GenericJar   ServerType = "GenericJar"
	Unknown      ServerType = "Unknown"
)

type Report struct {
	ServerDir           string            `json:"serverDir"`
	ServerType          ServerType        `json:"serverType"`
	InspectionOK        bool              `json:"inspectionOk"`
	LaunchReady         bool              `json:"launchReady"`
	LaunchJar           string            `json:"launchJar,omitempty"`
	LaunchEntry         string            `json:"launchEntry,omitempty"`
	LaunchCandidates    []string          `json:"launchCandidates,omitempty"`
	LaunchProfile       LaunchProfile     `json:"launchProfile"`
	Candidates          LaunchCandidates  `json:"candidates"`
	BlockingReasons     []string          `json:"blockingReasons,omitempty"`
	SuggestedCommand    string            `json:"suggestedCommand,omitempty"`
	JavaRequirement     string            `json:"javaRequirement,omitempty"`
	RequiredJavaVersion string            `json:"requiredJavaVersion,omitempty"`
	ServerPort          string            `json:"serverPort,omitempty"`
	WorldDir            string            `json:"worldDir,omitempty"`
	HasMods             bool              `json:"hasMods"`
	HasConfig           bool              `json:"hasConfig"`
	HasWorld            bool              `json:"hasWorld"`
	HasProperties       bool              `json:"hasProperties"`
	HasEULA             bool              `json:"hasEula"`
	EULAAccepted        bool              `json:"eulaAccepted"`
	Properties          map[string]string `json:"properties,omitempty"`
	RCON                RCONReport        `json:"rcon"`
	Warnings            []string          `json:"warnings,omitempty"`
}

type LaunchProfile struct {
	Kind                string     `json:"kind"`
	ServerType          ServerType `json:"serverType"`
	ScriptType          string     `json:"scriptType,omitempty"`
	ScriptPath          string     `json:"scriptPath,omitempty"`
	JarPath             string     `json:"jarPath,omitempty"`
	WorkingDirectory    string     `json:"workingDirectory"`
	Shell               string     `json:"shell,omitempty"`
	ShellArguments      []string   `json:"shellArguments,omitempty"`
	JavaPath            string     `json:"javaPath,omitempty"`
	RequiredJavaVersion string     `json:"requiredJavaVersion,omitempty"`
	DetectedJavaVersion string     `json:"detectedJavaVersion,omitempty"`
	JavaCompatibility   string     `json:"javaCompatibility,omitempty"`
	Confidence          string     `json:"confidence"`
	Evidence            []string   `json:"evidence,omitempty"`
}

type LaunchCandidate struct {
	Path       string     `json:"path"`
	Kind       string     `json:"kind"`
	ServerType ServerType `json:"serverType"`
	ScriptType string     `json:"scriptType,omitempty"`
	Confidence string     `json:"confidence"`
	Evidence   []string   `json:"evidence,omitempty"`
}

type LaunchCandidates struct {
	Scripts     []LaunchCandidate `json:"scripts"`
	Jars        []LaunchCandidate `json:"jars"`
	Recommended *LaunchProfile    `json:"recommended,omitempty"`
	Warnings    []string          `json:"warnings,omitempty"`
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

	report := Report{ServerDir: serverDir, ServerType: Unknown, InspectionOK: true}
	report.HasMods = exists(filepath.Join(serverDir, "mods"))
	report.HasConfig = exists(filepath.Join(serverDir, "config"))
	report.HasWorld = exists(filepath.Join(serverDir, "world"))
	report.HasProperties = exists(filepath.Join(serverDir, "server.properties"))

	detection := detectType(serverDir)
	report.ServerType = detection.kind
	report.LaunchEntry = detection.entry
	report.LaunchJar = detection.jar
	report.LaunchCandidates = detection.candidatePaths()
	report.Candidates = detection.candidates
	report.LaunchProfile = detection.profile
	report.LaunchReady = detection.profile.Kind != "" && detection.profile.Kind != "unresolved"
	report.JavaRequirement = javaRequirementFor(detection.kind)
	report.RequiredJavaVersion = report.JavaRequirement
	if report.LaunchEntry != "" {
		report.SuggestedCommand = suggestedCommand(report.LaunchEntry)
	}
	if report.LaunchReady && report.SuggestedCommand == "" {
		if report.LaunchProfile.ScriptPath != "" {
			report.SuggestedCommand = suggestedCommand(report.LaunchProfile.ScriptPath)
		} else if report.LaunchProfile.JarPath != "" {
			report.SuggestedCommand = suggestedCommand(report.LaunchProfile.JarPath)
		}
	}
	if !report.LaunchReady {
		report.BlockingReasons = append(report.BlockingReasons, "未识别到可用的启动脚本或服务端核心，请选择启动文件。")
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

func SelectLaunchProfile(serverDir string, selectedPath string) (Report, error) {
	report, err := Inspect(serverDir)
	if err != nil {
		return report, err
	}
	rel, err := normalizeSelectedPath(report.ServerDir, selectedPath)
	if err != nil {
		return report, err
	}
	for _, candidate := range report.Candidates.Scripts {
		if sameLaunchPath(candidate.Path, rel) {
			report.ServerType = candidate.ServerType
			report.LaunchEntry = candidate.Path
			report.LaunchJar = ""
			report.LaunchReady = true
			report.BlockingReasons = nil
			report.LaunchProfile = LaunchProfile{
				Kind: "script", ServerType: candidate.ServerType, ScriptType: candidate.ScriptType, ScriptPath: candidate.Path, WorkingDirectory: report.ServerDir,
				Shell: scriptShell(candidate.ScriptType), ShellArguments: scriptShellArguments(candidate.ScriptType, candidate.Path),
				RequiredJavaVersion: javaRequirementFor(candidate.ServerType), Confidence: candidate.Confidence, Evidence: candidate.Evidence,
			}
			report.SuggestedCommand = suggestedCommand(candidate.Path)
			report.JavaRequirement = report.LaunchProfile.RequiredJavaVersion
			report.RequiredJavaVersion = report.LaunchProfile.RequiredJavaVersion
			return report, nil
		}
	}
	for _, candidate := range report.Candidates.Jars {
		if sameLaunchPath(candidate.Path, rel) {
			report.ServerType = candidate.ServerType
			report.LaunchEntry = candidate.Path
			report.LaunchJar = candidate.Path
			report.LaunchReady = true
			report.BlockingReasons = nil
			report.LaunchProfile = LaunchProfile{
				Kind: "jar", ServerType: candidate.ServerType, JarPath: candidate.Path, WorkingDirectory: report.ServerDir,
				RequiredJavaVersion: javaRequirementFor(candidate.ServerType), Confidence: candidate.Confidence, Evidence: candidate.Evidence,
			}
			report.SuggestedCommand = suggestedCommand(candidate.Path)
			report.JavaRequirement = report.LaunchProfile.RequiredJavaVersion
			report.RequiredJavaVersion = report.LaunchProfile.RequiredJavaVersion
			return report, nil
		}
	}
	if manual, ok, err := manualScriptCandidate(report.ServerDir, rel); err != nil {
		return report, err
	} else if ok {
		report.ServerType = manual.ServerType
		report.LaunchEntry = manual.Path
		report.LaunchJar = ""
		report.LaunchReady = true
		report.BlockingReasons = nil
		report.LaunchProfile = LaunchProfile{
			Kind: "script", ServerType: manual.ServerType, ScriptType: manual.ScriptType, ScriptPath: manual.Path, WorkingDirectory: report.ServerDir,
			Shell: scriptShell(manual.ScriptType), ShellArguments: scriptShellArguments(manual.ScriptType, manual.Path),
			RequiredJavaVersion: javaRequirementFor(manual.ServerType), Confidence: manual.Confidence, Evidence: manual.Evidence,
		}
		report.SuggestedCommand = suggestedCommand(manual.Path)
		report.JavaRequirement = report.LaunchProfile.RequiredJavaVersion
		report.RequiredJavaVersion = report.LaunchProfile.RequiredJavaVersion
		return report, nil
	}
	return report, fmt.Errorf("选择的启动文件不在候选列表中: %s", selectedPath)
}

func normalizeSelectedPath(serverDir string, selectedPath string) (string, error) {
	selectedPath = strings.TrimSpace(selectedPath)
	if selectedPath == "" {
		return "", fmt.Errorf("请选择启动文件")
	}
	clean := filepath.Clean(selectedPath)
	if strings.HasPrefix(clean, `\\`) {
		return "", fmt.Errorf("暂不支持网络 UNC 启动脚本路径")
	}
	if filepath.IsAbs(clean) {
		rel, err := filepath.Rel(serverDir, clean)
		if err != nil {
			return "", err
		}
		if pathEscapesBase(rel) {
			return "", fmt.Errorf("启动文件必须位于服务端目录内")
		}
		clean = rel
	}
	if pathEscapesBase(clean) {
		return "", fmt.Errorf("启动文件必须位于服务端目录内")
	}
	return filepath.ToSlash(clean), nil
}

func sameLaunchPath(a string, b string) bool {
	return strings.EqualFold(filepath.ToSlash(filepath.Clean(a)), filepath.ToSlash(filepath.Clean(b)))
}

type launchDetection struct {
	kind       ServerType
	entry      string
	jar        string
	profile    LaunchProfile
	candidates LaunchCandidates
}

func detectType(serverDir string) launchDetection {
	evidence := detectEvidence(serverDir)
	scriptCandidates := make([]LaunchCandidate, 0)
	for _, script := range []string{"run.bat", "run.ps1", "start.bat", "start.ps1", "server-start.bat", "server-start.ps1"} {
		if actual, ok := findCaseInsensitiveFile(serverDir, script); ok {
			scriptType := scriptTypeForPath(actual)
			scriptCandidates = append(scriptCandidates, LaunchCandidate{
				Path: actual, Kind: "script", ServerType: CustomScript, ScriptType: scriptType, Confidence: "high",
				Evidence: append(scriptEvidence(serverDir, actual), evidence...),
			})
		}
	}
	jarCandidates := detectJarCandidates(serverDir)
	if jarCandidates == nil {
		jarCandidates = make([]LaunchCandidate, 0)
	}
	candidates := LaunchCandidates{Scripts: scriptCandidates, Jars: jarCandidates}

	if len(scriptCandidates) > 0 {
		entry := scriptCandidates[0].Path
		scriptType := scriptCandidates[0].ScriptType
		profile := LaunchProfile{
			Kind: "script", ServerType: CustomScript, ScriptType: scriptType, ScriptPath: entry, WorkingDirectory: serverDir,
			Shell: scriptShell(scriptType), ShellArguments: scriptShellArguments(scriptType, entry),
			RequiredJavaVersion: javaRequirementFor(CustomScript), Confidence: "high",
			Evidence: scriptCandidates[0].Evidence,
		}
		candidates.Recommended = &profile
		jar := ""
		if len(jarCandidates) > 0 {
			jar = jarCandidates[0].Path
		}
		return launchDetection{kind: CustomScript, entry: entry, jar: jar, profile: profile, candidates: candidates}
	}

	if len(jarCandidates) == 1 {
		entry := jarCandidates[0].Path
		kind := jarCandidates[0].ServerType
		profile := LaunchProfile{
			Kind: "jar", ServerType: kind, JarPath: entry, WorkingDirectory: serverDir,
			RequiredJavaVersion: javaRequirementFor(kind), Confidence: jarCandidates[0].Confidence,
			Evidence: jarCandidates[0].Evidence,
		}
		candidates.Recommended = &profile
		return launchDetection{kind: kind, entry: entry, jar: entry, profile: profile, candidates: candidates}
	}
	if len(jarCandidates) > 1 {
		profile := LaunchProfile{Kind: "unresolved", ServerType: Unknown, WorkingDirectory: serverDir, Confidence: "low", Evidence: append([]string{"发现多个可能的服务端核心"}, evidence...)}
		candidates.Warnings = append(candidates.Warnings, "发现多个候选 JAR，请选择要启动的服务端核心。")
		return launchDetection{kind: Unknown, profile: profile, candidates: candidates}
	}
	profile := LaunchProfile{Kind: "unresolved", ServerType: Unknown, WorkingDirectory: serverDir, Confidence: "low", Evidence: evidence}
	return launchDetection{kind: Unknown, profile: profile, candidates: candidates}
}

func (d launchDetection) candidatePaths() []string {
	paths := make([]string, 0, len(d.candidates.Scripts)+len(d.candidates.Jars))
	for _, candidate := range d.candidates.Scripts {
		paths = append(paths, candidate.Path)
	}
	for _, candidate := range d.candidates.Jars {
		paths = append(paths, candidate.Path)
	}
	return paths
}

func detectJarCandidates(serverDir string) []LaunchCandidate {
	entries, err := os.ReadDir(serverDir)
	if err != nil {
		return []LaunchCandidate{}
	}
	var matches []LaunchCandidate
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		lower := strings.ToLower(name)
		if !strings.HasSuffix(lower, ".jar") || excludedJarName(lower) {
			continue
		}
		kind := kindForEntry(name)
		if strings.HasPrefix(lower, "fabric-server-mc.") && strings.Contains(lower, "-launcher.") {
			kind = Fabric
		}
		switch {
		case name == "fabric-server-launch.jar", strings.HasPrefix(lower, "fabric-server-mc.") && strings.Contains(lower, "-launcher."):
			matches = append(matches, launchCandidate(name, "jar", kind, "high", "发现 Fabric launcher JAR"))
		case strings.HasPrefix(lower, "paper-"), lower == "paper.jar":
			matches = append(matches, launchCandidate(name, "jar", kind, "high", "发现 Paper JAR"))
		case strings.HasPrefix(lower, "purpur-"), lower == "purpur.jar":
			matches = append(matches, launchCandidate(name, "jar", kind, "high", "发现 Purpur JAR"))
		case strings.HasPrefix(lower, "forge-"), lower == "forge.jar":
			matches = append(matches, launchCandidate(name, "jar", kind, "high", "发现 Forge JAR"))
		case strings.HasPrefix(lower, "neoforge-"), lower == "neoforge.jar":
			matches = append(matches, launchCandidate(name, "jar", kind, "high", "发现 NeoForge JAR"))
		case strings.HasPrefix(lower, "cleanroom-"), lower == "cleanroom.jar":
			matches = append(matches, launchCandidate(name, "jar", kind, "high", "发现 Cleanroom JAR"))
		case strings.HasPrefix(lower, "velocity"), lower == "velocity.jar":
			matches = append(matches, launchCandidate(name, "jar", kind, "high", "发现 Velocity JAR"))
		case lower == "server.jar":
			matches = append(matches, launchCandidate(name, "jar", Vanilla, "medium", "发现 server.jar"))
		default:
			matches = append(matches, launchCandidate(name, "jar", GenericJar, "medium", "发现单个可用 JAR"))
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		return jarPriority(matches[i].Path) < jarPriority(matches[j].Path)
	})
	for i := range matches {
		matches[i].Evidence = append(matches[i].Evidence, detectEvidence(serverDir)...)
	}
	return matches
}

func launchCandidate(path string, kind string, serverType ServerType, confidence string, evidence string) LaunchCandidate {
	return LaunchCandidate{Path: path, Kind: kind, ServerType: serverType, Confidence: confidence, Evidence: []string{evidence}}
}

func manualScriptCandidate(serverDir string, rel string) (LaunchCandidate, bool, error) {
	if rel == "" {
		return LaunchCandidate{}, false, nil
	}
	lower := strings.ToLower(rel)
	if !strings.HasSuffix(lower, ".ps1") && !strings.HasSuffix(lower, ".bat") && !strings.HasSuffix(lower, ".cmd") {
		return LaunchCandidate{}, false, nil
	}
	actual, err := resolveInsideServerDir(serverDir, rel)
	if err != nil {
		return LaunchCandidate{}, false, err
	}
	info, err := os.Stat(actual)
	if err != nil {
		if os.IsNotExist(err) {
			return LaunchCandidate{}, false, fmt.Errorf("%s 不存在或已被移动", rel)
		}
		return LaunchCandidate{}, false, err
	}
	if info.IsDir() {
		return LaunchCandidate{}, false, fmt.Errorf("启动文件不能是目录: %s", rel)
	}
	relActual, err := filepath.Rel(serverDir, actual)
	if err != nil {
		return LaunchCandidate{}, false, err
	}
	relActual = filepath.ToSlash(relActual)
	scriptType := scriptTypeForPath(relActual)
	return LaunchCandidate{
		Path: relActual, Kind: "script", ServerType: CustomScript, ScriptType: scriptType, Confidence: "manual",
		Evidence: append(scriptEvidence(serverDir, relActual), "用户手动选择启动脚本"),
	}, true, nil
}

func findCaseInsensitiveFile(dir string, want string) (string, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.EqualFold(entry.Name(), want) {
			return entry.Name(), true
		}
	}
	return "", false
}

func scriptTypeForPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".ps1":
		return "powershell"
	case ".bat", ".cmd":
		return "batch"
	default:
		return ""
	}
}

func scriptShell(scriptType string) string {
	switch scriptType {
	case "powershell":
		return "powershell.exe"
	case "batch":
		if runtime.GOOS == "windows" {
			return "cmd.exe"
		}
		return ""
	default:
		return ""
	}
}

func scriptShellArguments(scriptType string, scriptPath string) []string {
	switch scriptType {
	case "powershell":
		return []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", scriptPath}
	case "batch":
		if runtime.GOOS == "windows" {
			return []string{"/c", scriptPath}
		}
		return []string{scriptPath}
	default:
		return nil
	}
}

func scriptEvidence(serverDir string, script string) []string {
	evidence := []string{"发现 " + script}
	if strings.EqualFold(scriptTypeForPath(script), "powershell") {
		evidence = append(evidence, "识别为 PowerShell 启动脚本")
	}
	data, err := os.ReadFile(filepath.Join(serverDir, filepath.FromSlash(script)))
	if err != nil {
		return evidence
	}
	lower := strings.ToLower(string(data))
	for _, marker := range []string{"java.exe", "java", "java_home", "-xms", "-xmx", "-jar", "@libraries", "user_jvm_args.txt", "win_args.txt"} {
		if strings.Contains(lower, marker) {
			evidence = append(evidence, "脚本内容包含 "+marker)
		}
	}
	if strings.Contains(lower, "libraries/net/minecraftforge") || strings.Contains(lower, "net\\minecraftforge") {
		evidence = append(evidence, "脚本内容包含 Forge libraries 引用")
	}
	if strings.Contains(lower, "libraries/net/neoforged") || strings.Contains(lower, "net\\neoforged") {
		evidence = append(evidence, "脚本内容包含 NeoForge libraries 引用")
	}
	return evidence
}

func resolveInsideServerDir(serverDir string, rel string) (string, error) {
	if strings.HasPrefix(rel, `\\`) {
		return "", fmt.Errorf("暂不支持网络 UNC 启动脚本路径")
	}
	baseAbs, err := filepath.Abs(serverDir)
	if err != nil {
		return "", err
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	var candidate string
	if filepath.IsAbs(clean) {
		candidate = clean
	} else {
		candidate = filepath.Join(baseAbs, clean)
	}
	candidateAbs, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	relToBase, err := filepath.Rel(baseAbs, candidateAbs)
	if err != nil {
		return "", err
	}
	if pathEscapesBase(relToBase) {
		return "", fmt.Errorf("run.ps1 不在服务端目录内，已拒绝执行")
	}
	return candidateAbs, nil
}

func pathEscapesBase(rel string) bool {
	rel = filepath.Clean(rel)
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel)
}

func detectEvidence(serverDir string) []string {
	var evidence []string
	checks := []struct {
		path string
		msg  string
	}{
		{"mods", "发现 mods 目录"},
		{"config", "发现 config 目录"},
		{"server.properties", "发现 server.properties"},
		{"user_jvm_args.txt", "发现 Forge/NeoForge user_jvm_args.txt"},
		{"win_args.txt", "发现 Forge/NeoForge win_args.txt"},
		{"unix_args.txt", "发现 Forge/NeoForge unix_args.txt"},
		{filepath.Join("libraries", "net", "minecraftforge"), "发现 Forge libraries"},
		{filepath.Join("libraries", "net", "neoforged"), "发现 NeoForge libraries"},
		{filepath.Join("libraries", "net", "fabricmc"), "发现 Fabric libraries"},
		{".fabric", "发现 .fabric 目录"},
		{"velocity.toml", "发现 velocity.toml"},
	}
	for _, check := range checks {
		if exists(filepath.Join(serverDir, check.path)) {
			evidence = append(evidence, check.msg)
		}
	}
	return evidence
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
	case lower == "fabric-server-launch.jar", strings.HasPrefix(lower, "fabric-server-mc."):
		return 10
	case strings.HasPrefix(lower, "paper-"), lower == "paper.jar":
		return 20
	case strings.HasPrefix(lower, "purpur-"), lower == "purpur.jar":
		return 21
	case strings.HasPrefix(lower, "velocity"), lower == "velocity.jar":
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
	case strings.HasSuffix(lower, ".bat"), strings.HasSuffix(lower, ".ps1"):
		return CustomScript
	case lower == "fabric-server-launch.jar", strings.HasPrefix(lower, "fabric-server-mc."):
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
	case strings.HasPrefix(lower, "velocity"), lower == "velocity.jar":
		return Velocity
	case lower == "server.jar":
		return Vanilla
	default:
		return GenericJar
	}
}

func suggestedCommand(entry string) string {
	lower := strings.ToLower(entry)
	if strings.HasSuffix(lower, ".ps1") {
		return "powershell.exe -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File " + entry
	}
	if strings.HasSuffix(lower, ".bat") {
		if runtime.GOOS == "windows" {
			return "cmd /c " + entry
		}
		return entry
	}
	return fmt.Sprintf("java -Xms2G -Xmx4G -jar %s nogui", entry)
}

func SuggestedCommand(entry string) string {
	return suggestedCommand(entry)
}

func javaRequirementFor(kind ServerType) string {
	switch kind {
	case Forge, NeoForge, Cleanroom:
		return "17"
	case Velocity:
		return "17"
	case Fabric, Paper, Purpur, Vanilla, GenericJar:
		return "17"
	case CustomScript:
		return ""
	default:
		return ""
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
