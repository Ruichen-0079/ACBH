//go:build windows

package desktop

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

type DPAPISecretStore struct {
	dir string
}

func NewDefaultSecretStore(opts Options) SecretStore {
	opts = withDefaults(opts)
	return &DPAPISecretStore{dir: filepath.Join(opts.AppDataDir, "secrets")}
}

func (s *DPAPISecretStore) Put(key string, value string) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	protected, err := cryptProtect([]byte(value))
	if err != nil {
		return err
	}
	return os.WriteFile(s.path(key), []byte(base64.StdEncoding.EncodeToString(protected)), 0o600)
}

func (s *DPAPISecretStore) Get(key string) (string, error) {
	raw, err := os.ReadFile(s.path(key))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	encrypted, err := base64.StdEncoding.DecodeString(string(raw))
	if err != nil {
		return "", err
	}
	plain, err := cryptUnprotect(encrypted)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func (s *DPAPISecretStore) Delete(key string) error {
	if err := os.Remove(s.path(key)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *DPAPISecretStore) path(key string) string {
	return filepath.Join(s.dir, safeSecretKey(key)+".dpapi")
}

func safeSecretKey(key string) string {
	out := make([]rune, 0, len(key))
	for _, r := range key {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return "secret"
	}
	return string(out)
}

type dataBlob struct {
	cbData uint32
	pbData *byte
}

var (
	crypt32                = syscall.NewLazyDLL("crypt32.dll")
	kernel32               = syscall.NewLazyDLL("kernel32.dll")
	procCryptProtectData   = crypt32.NewProc("CryptProtectData")
	procCryptUnprotectData = crypt32.NewProc("CryptUnprotectData")
	procLocalFree          = kernel32.NewProc("LocalFree")
)

func cryptProtect(data []byte) ([]byte, error) {
	in := dataBlob{}
	if len(data) > 0 {
		in.cbData = uint32(len(data))
		in.pbData = &data[0]
	}
	var out dataBlob
	r1, _, err := procCryptProtectData.Call(
		uintptr(unsafe.Pointer(&in)),
		0,
		0,
		0,
		0,
		0,
		uintptr(unsafe.Pointer(&out)),
	)
	if r1 == 0 {
		return nil, fmt.Errorf("DPAPI protect failed: %w", err)
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	copied := make([]byte, out.cbData)
	copy(copied, unsafe.Slice(out.pbData, out.cbData))
	return copied, nil
}

func cryptUnprotect(data []byte) ([]byte, error) {
	in := dataBlob{}
	if len(data) > 0 {
		in.cbData = uint32(len(data))
		in.pbData = &data[0]
	}
	var out dataBlob
	r1, _, err := procCryptUnprotectData.Call(
		uintptr(unsafe.Pointer(&in)),
		0,
		0,
		0,
		0,
		0,
		uintptr(unsafe.Pointer(&out)),
	)
	if r1 == 0 {
		return nil, fmt.Errorf("DPAPI unprotect failed: %w", err)
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	copied := make([]byte, out.cbData)
	copy(copied, unsafe.Slice(out.pbData, out.cbData))
	return copied, nil
}
