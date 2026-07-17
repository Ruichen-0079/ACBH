//go:build windows

package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const hobbyWindowsServiceName = "ACBHAgent"

var hobbyRecoveryActions = []mgr.RecoveryAction{
	{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
	{Type: mgr.ServiceRestart, Delay: 15 * time.Second},
	{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
}

type hobbyServiceHandler struct {
	command *cobra.Command
	options hobbyServeOptions
}

func runHobbyService(command *cobra.Command, options hobbyServeOptions) error {
	isService, err := svc.IsWindowsService()
	if err != nil {
		return err
	}
	if !isService {
		return errors.New("hobby service must be started by the Windows Service Control Manager")
	}
	return svc.Run(hobbyWindowsServiceName, hobbyServiceHandler{command: command, options: options})
}

func installHobbyService(_ context.Context, options hobbyServeOptions) error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve Agent executable: %w", err)
	}
	arguments := []string{"hobby", "service", "--address", options.address, "--frpc", options.frpcPath, "--app-data-dir", options.appDataDir}
	manager, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to Windows service manager: %w", err)
	}
	defer manager.Disconnect()

	service, err := manager.OpenService(hobbyWindowsServiceName)
	if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		service, err = manager.CreateService(hobbyWindowsServiceName, executable, mgr.Config{
			StartType:    mgr.StartAutomatic,
			ErrorControl: mgr.ErrorNormal,
			DisplayName:  "ACBH Relay Agent",
			Description:  "ACBH v0.4 relay-only local Agent",
		}, arguments...)
	} else if err == nil {
		config, configErr := service.Config()
		if configErr != nil {
			service.Close()
			return fmt.Errorf("read existing Agent service configuration: %w", configErr)
		}
		config.StartType = mgr.StartAutomatic
		config.ErrorControl = mgr.ErrorNormal
		config.DisplayName = "ACBH Relay Agent"
		config.Description = "ACBH v0.4 relay-only local Agent"
		config.BinaryPathName = serviceCommandLine(executable, arguments)
		err = service.UpdateConfig(config)
	}
	if err != nil {
		return fmt.Errorf("install Agent service: %w", err)
	}
	defer service.Close()
	if err := service.SetRecoveryActions(hobbyRecoveryActions, 86400); err != nil {
		return fmt.Errorf("configure Agent service recovery: %w", err)
	}
	return nil
}

func removeHobbyService(_ context.Context) error {
	manager, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to Windows service manager: %w", err)
	}
	defer manager.Disconnect()
	service, err := manager.OpenService(hobbyWindowsServiceName)
	if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open Agent service: %w", err)
	}
	defer service.Close()
	if err := service.Delete(); err != nil {
		return fmt.Errorf("delete Agent service: %w", err)
	}
	return nil
}

func serviceCommandLine(executable string, arguments []string) string {
	commandLine := syscall.EscapeArg(executable)
	for _, argument := range arguments {
		commandLine += " " + syscall.EscapeArg(argument)
	}
	return commandLine
}

func (h hobbyServiceHandler) Execute(_ []string, requests <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	changes <- svc.Status{State: svc.StartPending}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runHobbyServe(ctx, h.command, h.options) }()
	changes <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}

	for {
		select {
		case err := <-done:
			if err != nil {
				return true, 1
			}
			return false, 0
		case request := <-requests:
			switch request.Cmd {
			case svc.Interrogate:
				changes <- request.CurrentStatus
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending}
				cancel()
				if err := <-done; err != nil {
					return true, 1
				}
				return false, 0
			}
		}
	}
}
