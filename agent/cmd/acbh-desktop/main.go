package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/Ruichen-0079/ACBH/agent/internal/desktop"
)

func main() {
	if len(os.Args) == 1 || os.Args[1] == "--gui" {
		if err := launchGUI(); err == nil {
			return
		} else {
			fmt.Fprintf(os.Stderr, "桌面 GUI 启动失败，将回退到命令行模式：%v\n", err)
		}
	}
	runCLI()
}

func launchGUI() error {
	exePath, err := os.Executable()
	if err != nil {
		return err
	}
	exeDir := filepath.Dir(exePath)
	guiScript := filepath.Join(exeDir, "scripts", "acbh-minimal-core-gui.ps1")
	if _, err := os.Stat(guiScript); err != nil {
		guiScript = filepath.Join(exeDir, "scripts", "acbh-desktop-gui.ps1")
	}
	if _, err := os.Stat(guiScript); err != nil {
		return fmt.Errorf("找不到 GUI 脚本 %s: %w", guiScript, err)
	}

	agentPath := filepath.Join(exeDir, "acbh-agent-windows-amd64.exe")
	coordinatorPath := filepath.Join(exeDir, "coordinator", "dist", "index.js")
	args := []string{
		"-NoProfile",
		"-ExecutionPolicy", "Bypass",
		"-STA",
		"-File", guiScript,
		"-AgentPath", agentPath,
	}
	if filepath.Base(guiScript) == "acbh-desktop-gui.ps1" {
		args = append(args, "-CoordinatorPath", coordinatorPath)
	}
	cmd := exec.Command("powershell.exe", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func runCLI() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var log bytes.Buffer
	status, err := desktop.Start(ctx, desktop.Options{}, &log)
	if log.Len() > 0 {
		fmt.Print(log.String())
	}
	if status.AppDataDir != "" {
		fmt.Printf("数据目录: %s\n", status.AppDataDir)
		fmt.Printf("控制端地址: %s\n", status.CoordinatorURL)
		if status.GroupID != "" {
			fmt.Printf("当前服务器组: %s\n", status.GroupID)
		}
		if status.HostID != "" {
			fmt.Printf("当前主机: %s\n", status.HostID)
		}
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "启动失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("ACBH 已完成一键启动。关闭窗口前请先运行 acbh-agent desktop stop，或按 Ctrl+C 结束本进程。")
	<-ctx.Done()
	var stopLog bytes.Buffer
	_, _ = desktop.Stop(desktop.Options{}, &stopLog)
	if stopLog.Len() > 0 {
		fmt.Print(stopLog.String())
	}
}
