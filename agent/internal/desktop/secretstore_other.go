//go:build !windows

package desktop

func NewDefaultSecretStore(opts Options) SecretStore {
	return NewMemorySecretStore()
}
