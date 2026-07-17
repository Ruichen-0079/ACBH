//go:build windows

package cli

import (
	"reflect"
	"testing"

	"golang.org/x/sys/windows"
)

func TestServiceCommandLinePreservesUnicodeAndSpaceArguments(t *testing.T) {
	executable := "C:\\Program Files\\ACBH 测试\\acbh-agent.exe"
	arguments := []string{
		"hobby", "service",
		"--frpc", "C:\\Program Files\\ACBH 测试\\frpc.exe",
		"--app-data-dir", "C:\\ProgramData\\ACBH 数据",
	}
	commandLine := serviceCommandLine(executable, arguments)
	actual, err := windows.DecomposeCommandLine(commandLine)
	if err != nil {
		t.Fatal(err)
	}
	want := append([]string{executable}, arguments...)
	if !reflect.DeepEqual(actual, want) {
		t.Fatalf("service command line changed arguments:\nactual: %#v\nwant:   %#v", actual, want)
	}
}
