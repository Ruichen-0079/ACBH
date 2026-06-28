package desktop

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

func TestDesktopHTMLIdempotencyKeyIsASCIIOnly(t *testing.T) {
	if strings.Contains(desktopHTML, "idempotencyKey(path,body)") {
		t.Fatal("desktop HTML still serializes request body into Idempotency-Key header")
	}
	if !strings.Contains(desktopHTML, "function idempotencyKey(){return 'req_'+crypto.randomUUID()}") {
		t.Fatal("desktop HTML must generate ASCII-only Idempotency-Key values")
	}
	if strings.Contains(desktopHTML, "'ui:'+path+':'") {
		t.Fatal("desktop HTML still uses path/body in idempotency key")
	}
}

func TestDesktopHTMLRequestIdsAreASCIIOnly(t *testing.T) {
	patterns := []string{
		`'create:'+groupName`,
		`'join:'+inviteCode`,
		`'create:'+profileId`,
	}
	for _, pattern := range patterns {
		if strings.Contains(desktopHTML, pattern) {
			t.Fatalf("desktop HTML still embeds non-ASCII-prone requestId pattern: %s", pattern)
		}
	}
	if !strings.Contains(desktopHTML, "requestId:'req_'+crypto.randomUUID()") {
		t.Fatal("desktop HTML should use ASCII requestId values")
	}
}

func TestDesktopHTMLPickerButtonsAndFilters(t *testing.T) {
	required := []string{
		"选择服务端目录",
		"选择启动文件",
		"选择 Java",
		"启动脚本或服务端核心",
		"backupRootsTable",
		"/api/backup/summary",
	}
	for _, snippet := range required {
		if !strings.Contains(desktopHTML, snippet) {
			t.Fatalf("desktop HTML missing %q", snippet)
		}
	}
}

func TestDesktopHTMLFetchErrorMessage(t *testing.T) {
	if !strings.Contains(desktopHTML, "路径保存失败：请求构造错误，已修复后请重试") {
		t.Fatal("desktop HTML should show user-friendly fetch construction error")
	}
}

func TestIdempotencyKeyFromRequestReadsASCIIHeader(t *testing.T) {
	req, err := http.NewRequest(http.MethodPut, "/api/config/server", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Idempotency-Key", "req_7f3c2a1b-4d5e-6789-abcd-ef0123456789")
	key := idempotencyKeyFromRequest(req)
	if key != "req_7f3c2a1b-4d5e-6789-abcd-ef0123456789" {
		t.Fatalf("idempotency key = %q, want ASCII header value", key)
	}
	if !regexp.MustCompile(`^[\x00-\x7F]+$`).MatchString(key) {
		t.Fatalf("idempotency key %q must be ASCII-only", key)
	}
}