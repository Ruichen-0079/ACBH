//go:build !windows

package cli

import (
	"context"
	"errors"

	"github.com/spf13/cobra"
)

func runHobbyService(_ *cobra.Command, _ hobbyServeOptions) error {
	return errors.New("hobby service is only available on Windows")
}

func installHobbyService(_ context.Context, _ hobbyServeOptions) error {
	return errors.New("hobby service installation is only available on Windows")
}

func removeHobbyService(_ context.Context) error {
	return errors.New("hobby service removal is only available on Windows")
}
