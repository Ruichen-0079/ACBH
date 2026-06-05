package cli

import (
	"fmt"
	"os"
	"runtime"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "acbh-agent",
	Short: "ACBH Agent controls Minecraft host handoff on candidate devices",
	Long:  "ACBH Agent downloads snapshots, starts Minecraft servers, reports health, and executes host takeover for ACBH groups.",
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Print local host diagnostics",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("ACBH Agent doctor\n")
		fmt.Printf("OS: %s\n", runtime.GOOS)
		fmt.Printf("ARCH: %s\n", runtime.GOARCH)
		fmt.Printf("CPU cores: %d\n", runtime.NumCPU())
		fmt.Printf("Status: bootstrap diagnostics only\n")
	},
}

func Execute() {
	rootCmd.AddCommand(doctorCmd)
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
