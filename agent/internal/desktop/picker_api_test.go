package desktop

import (
	"strings"
	"testing"
)

func TestPickerFilterFromRequestStructuredFilters(t *testing.T) {
	filter := pickerFilterFromRequest(PickerRequest{
		Filters: []PickerFilterGroup{{
			Name:     "启动脚本或服务端核心",
			Patterns: []string{"*.bat", "*.cmd", "*.jar"},
		}},
	})
	if !strings.Contains(filter, "启动脚本或服务端核心") {
		t.Fatalf("filter = %q, want display name in body-derived filter", filter)
	}
	if !strings.Contains(filter, "*.bat") || !strings.Contains(filter, "*.jar") {
		t.Fatalf("filter = %q, want bat and jar patterns", filter)
	}
}

func TestPickerFilterFromRequestLegacyFilterField(t *testing.T) {
	got := pickerFilterFromRequest(PickerRequest{Filter: "*.exe"})
	if got != "*.exe" {
		t.Fatalf("filter = %q, want legacy filter passthrough", got)
	}
}

func TestPickerFilterFromRequestJavaPatterns(t *testing.T) {
	filter := pickerFilterFromRequest(PickerRequest{
		Filters: []PickerFilterGroup{{
			Name:     "Java 可执行文件",
			Patterns: []string{"java.exe", "javaw.exe", "*"},
		}},
	})
	for _, want := range []string{"java.exe", "javaw.exe", "*"} {
		if !strings.Contains(filter, want) {
			t.Fatalf("filter = %q, want %q", filter, want)
		}
	}
}