package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
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
	address    string
	frpcPath   string
	appDataDir string
}

var Version = "0.4.0-dev"

func newHobbyCmd() *cobra.Command {
	command := &cobra.Command{
		Use:   "hobby",
		Short: "Run the v0.4 relay-first Hobby Edition Agent",
	}
	command.AddCommand(newHobbyServeCmd(), newHobbyServiceCmd(), newHobbyInstallServiceCmd(), newHobbyRemoveServiceCmd())
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
	addHobbyServeFlags(command, &options)
	return command
}

func newHobbyServiceCmd() *cobra.Command {
	var options hobbyServeOptions
	command := &cobra.Command{
		Use:   "service",
		Short: "Run the Hobby Agent under the Windows Service Control Manager",
		RunE: func(command *cobra.Command, _ []string) error {
			return runHobbyService(command, options)
		},
	}
	addHobbyServeFlags(command, &options)
	return command
}

func newHobbyInstallServiceCmd() *cobra.Command {
	var options hobbyServeOptions
	command := &cobra.Command{
		Use:    "install-service",
		Short:  "Install or update the Windows Hobby Agent service",
		Hidden: true,
		RunE: func(command *cobra.Command, _ []string) error {
			return installHobbyService(command.Context(), options)
		},
	}
	addHobbyServeFlags(command, &options)
	return command
}

func newHobbyRemoveServiceCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "remove-service",
		Short:  "Remove the Windows Hobby Agent service",
		Hidden: true,
		RunE: func(command *cobra.Command, _ []string) error {
			return removeHobbyService(command.Context())
		},
	}
}

func addHobbyServeFlags(command *cobra.Command, options *hobbyServeOptions) {
	command.Flags().StringVar(&options.address, "address", "127.0.0.1:6130", "Loopback API and UI address")
	command.Flags().StringVar(&options.frpcPath, "frpc", "", "Path to the bundled frpc executable")
	command.Flags().StringVar(&options.appDataDir, "app-data-dir", "", "Configuration, state, and log directory")
}

func runHobbyServe(parent context.Context, command *cobra.Command, options hobbyServeOptions) error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve Agent executable: %w", err)
	}
	configDir := options.appDataDir
	if configDir == "" {
		configDir, err = agentconfig.ResolveAppDataDir(executable)
		if err != nil {
			return err
		}
	}
	frpcPath := options.frpcPath
	if frpcPath == "" {
		name := "frpc"
		if runtime.GOOS == "windows" {
			name = "frpc.exe"
		}
		frpcPath = filepath.Join(filepath.Dir(executable), name)
	}
	runtimeDir := filepath.Join(configDir, "hobby-runtime")
	store := hobbyagent.FileStore{
		ConfigPath:  filepath.Join(configDir, "hobby-config.json"),
		DesiredPath: filepath.Join(runtimeDir, "desired.json"),
	}
	logWriter, err := agentlog.New(filepath.Join(configDir, "logs", "agent.jsonl"), agentlog.DefaultMaxBytes, agentlog.DefaultMaxFiles)
	if err != nil {
		return fmt.Errorf("initialize structured log: %w", err)
	}
	relay := frprelay.NewManager(frprelay.Dependencies{})
	runtimeService, err := hobbyagent.NewRuntime(hobbyagent.RuntimeOptions{
		Store: store, Probe: hobbyagent.TCPLocalProbe{Timeout: 500 * time.Millisecond}, Relay: relay,
		Coordinator: hobbyagent.CoordinatorClient{}, FRPCPath: frpcPath,
		RuntimeDir: filepath.Join(runtimeDir, "relay"), LogDir: filepath.Join(configDir, "logs"),
		AgentVersion: Version, Logger: logWriter,
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
	fmt.Fprintln(command.OutOrStdout(), "Closing the browser does not stop the relay.")
	serveErr := localapi.ListenAndServe(ctx, options.address, localapi.New(runtimeService).Handler())
	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelShutdown()
	shutdownErr := runtimeService.Shutdown(shutdownContext)
	if serveErr != nil {
		return serveErr
	}
	return shutdownErr
}
