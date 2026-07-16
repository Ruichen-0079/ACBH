package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/agentconfig"
	"github.com/Ruichen-0079/ACBH/agent/internal/agentlog"
	"github.com/Ruichen-0079/ACBH/agent/internal/frprelay"
	"github.com/Ruichen-0079/ACBH/agent/internal/hobbyagent"
	"github.com/Ruichen-0079/ACBH/agent/internal/localapi"
	"github.com/spf13/cobra"
)

type hobbyServeOptions struct {
	address              string
	frpcPath             string
	autoRestartMinecraft bool
	maxMinecraftRestarts int
}

func newHobbyCmd() *cobra.Command {
	command := &cobra.Command{
		Use:   "hobby",
		Short: "Run the v0.4 relay-first Hobby Edition Agent",
	}
	command.AddCommand(newHobbyServeCmd())
	return command
}

func newHobbyServeCmd() *cobra.Command {
	var options hobbyServeOptions
	command := &cobra.Command{
		Use:   "serve",
		Short: "Run the long-lived Agent and loopback web control service",
		RunE: func(command *cobra.Command, _ []string) error {
			return runHobbyServe(command.Context(), command, options)
		},
	}
	command.Flags().StringVar(&options.address, "address", "127.0.0.1:6130", "Loopback API and UI address")
	command.Flags().StringVar(&options.frpcPath, "frpc", "frpc", "Path to the frpc executable")
	command.Flags().BoolVar(&options.autoRestartMinecraft, "minecraft-auto-restart", true, "Restart Minecraft after an unexpected exit")
	command.Flags().IntVar(&options.maxMinecraftRestarts, "minecraft-max-restarts", 3, "Maximum Minecraft restarts per hosting operation")
	return command
}

func runHobbyServe(parent context.Context, command *cobra.Command, options hobbyServeOptions) error {
	configDir, err := agentconfig.DefaultDir()
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve Agent executable: %w", err)
	}
	runtimeDir := filepath.Join(configDir, "hobby-runtime")
	store := hobbyagent.FileStore{
		ConfigPath:  filepath.Join(configDir, "hobby-config.json"),
		ImportPath:  filepath.Join(configDir, "hobby-import.json"),
		DesiredPath: filepath.Join(runtimeDir, "desired.json"),
	}
	logWriter, err := agentlog.New(filepath.Join(configDir, "logs", "agent.jsonl"), agentlog.DefaultMaxBytes, agentlog.DefaultMaxFiles)
	if err != nil {
		return fmt.Errorf("initialize structured log: %w", err)
	}
	relay := frprelay.NewManager(frprelay.Dependencies{})
	minecraft := hobbyagent.ManagedMinecraft{
		Executable: executable,
		RuntimeDir: filepath.Join(runtimeDir, "minecraft"),
		LogDir:     filepath.Join(configDir, "logs", "minecraft"),
		Timeout:    30 * time.Second,
	}
	runtimeService, err := hobbyagent.NewRuntime(hobbyagent.RuntimeOptions{
		Store: store, Minecraft: minecraft, Relay: relay,
		Coordinator: hobbyagent.CoordinatorClient{}, FRPCPath: options.frpcPath,
		RuntimeDir: filepath.Join(runtimeDir, "relay"), AgentVersion: "0.4.0-hobby",
		Logger: logWriter, AutoRestartMinecraft: options.autoRestartMinecraft,
		MaxMinecraftRestarts: options.maxMinecraftRestarts,
	})
	if err != nil {
		return err
	}
	if operation, resumed := runtimeService.Resume(); resumed {
		fmt.Fprintf(command.OutOrStdout(), "Recovering desired hosting state with operation %s\n", operation.ID)
	}

	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()
	fmt.Fprintf(command.OutOrStdout(), "ACBH Hobby Edition Agent listening on http://%s\n", options.address)
	fmt.Fprintln(command.OutOrStdout(), "Closing the browser does not stop Minecraft or the relay.")
	return localapi.ListenAndServe(ctx, options.address, localapi.New(runtimeService).Handler())
}
