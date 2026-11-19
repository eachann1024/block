package ai

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"block/internal/model"
	"block/internal/theme"
)

const (
	providerConfigName     = "ai.json"
	providerCredentialsDir = "ai-credentials"
)

type providerConfigFile struct {
	model.AIProviderConfig
	CredentialRef string `json:"credential_ref,omitempty"`
}

func (cfg providerConfigFile) hasInlineCredentials() bool {
	if cfg.APIKey != "" {
		return true
	}
	for _, profile := range cfg.Providers {
		if profile.APIKey != "" {
			return true
		}
	}
	return false
}

type providerCredentials struct {
	Active   string            `json:"active,omitempty"`
	Profiles map[string]string `json:"profiles,omitempty"`
}

func (keys providerCredentials) empty() bool {
	return keys.Active == "" && len(keys.Profiles) == 0
}

func detachProviderCredentials(cfg *model.AIProviderConfig) providerCredentials {
	keys := providerCredentials{Active: cfg.APIKey}
	cfg.APIKey = ""
	for id, profile := range cfg.Providers {
		if profile.APIKey != "" {
			if keys.Profiles == nil {
				keys.Profiles = make(map[string]string)
			}
			keys.Profiles[id] = profile.APIKey
			profile.APIKey = ""
			cfg.Providers[id] = profile
		}
	}
	return keys
}

func restoreProviderCredentials(cfg *model.AIProviderConfig, keys providerCredentials) {
	if cfg.APIKey == "" {
		cfg.APIKey = keys.Active
	}
	for id, profile := range cfg.Providers {
		if profile.APIKey == "" {
			profile.APIKey = keys.Profiles[id]
			cfg.Providers[id] = profile
		}
	}
}

var (
	errProviderCredentialsRead = errors.New("could not read AI credentials")
	errProviderCredentialsSave = errors.New("could not save AI credentials")
)

func providerCredentialsMessage(err error) string {
	if err == errProviderCredentialsRead {
		return theme.Tr("无法读取 AI 凭证。请恢复凭证文件后重新打开 Block。", "Could not read AI credentials. Restore access to the credential files and reopen Block.")
	}
	return theme.Tr("无法保存 AI 设置。上次保存的密钥仍保留。", "Could not save AI settings. Previously saved keys are still stored.")
}

func decodeProviderConfig(data []byte, err error) (providerConfigFile, error) {
	defaults := providerConfigFile{AIProviderConfig: model.AIProviderConfig{
		Enabled: true, Provider: "deepseek", Protocol: model.ProtocolOpenAI, BaseURL: "https://api.deepseek.com/v1",
	}}
	if errors.Is(err, os.ErrNotExist) {
		return defaults, nil
	}
	if err != nil {
		return defaults, err
	}
	cfg := defaults
	if err := json.Unmarshal(data, &cfg); err != nil {
		return defaults, err
	}
	return cfg, nil
}

type providerStore struct {
	data        *os.Root
	credentials *os.Root
}

// Read, commit and cleanup must agree on the current reference, even when two
// Block processes share a data directory.
func withProviderStore(dataDir string, fn func(providerStore) error) error {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	data, err := os.OpenRoot(dataDir)
	if err != nil {
		return err
	}
	defer data.Close()
	if err := data.Mkdir(providerCredentialsDir, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err := data.Lstat(providerCredentialsDir)
	if err != nil || !info.IsDir() || !privateCredentialPath(info) {
		return errProviderCredentialsRead
	}
	credentials, err := data.OpenRoot(providerCredentialsDir)
	if err != nil {
		return err
	}
	defer credentials.Close()
	info, err = credentials.Stat(".")
	if err != nil || !privateCredentialPath(info) {
		return errProviderCredentialsRead
	}
	lock, err := credentials.OpenFile("lock", os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	unlock, err := lockProviderCredentials(lock)
	if err != nil {
		return err
	}
	defer unlock()
	return fn(providerStore{data: data, credentials: credentials})
}

func loadProviderConfig(dataDir string) (model.AIProviderConfig, error) {
	// A keyless config does not need filesystem writes on startup.
	initial, err := decodeProviderConfig(os.ReadFile(filepath.Join(dataDir, providerConfigName)))
	if err != nil {
		return initial.AIProviderConfig, errProviderCredentialsRead
	}
	result := initial.AIProviderConfig
	if initial.CredentialRef == "" && !initial.hasInlineCredentials() {
		return result, nil
	}
	read := false
	err = withProviderStore(dataDir, func(store providerStore) error {
		cfg, storedKeys, err := store.read()
		if err != nil {
			return err
		}
		migrate := cfg.hasInlineCredentials()
		restoreProviderCredentials(&cfg.AIProviderConfig, storedKeys)
		result = cfg.AIProviderConfig
		read = true
		if migrate {
			return store.commit(result, cfg.CredentialRef, storedKeys)
		}
		if cfg.CredentialRef != "" && syncCredentialDirectory(store.data) == nil {
			store.removeObsolete(cfg.CredentialRef)
		}
		return nil
	})
	if err != nil {
		if read || initial.CredentialRef == "" {
			return result, errProviderCredentialsSave
		}
		return result, errProviderCredentialsRead
	}
	return result, nil
}

func (store providerStore) read() (providerConfigFile, providerCredentials, error) {
	cfg, err := decodeProviderConfig(store.data.ReadFile(providerConfigName))
	var keys providerCredentials
	if err != nil || cfg.CredentialRef == "" {
		return cfg, keys, err
	}
	name := cfg.CredentialRef + ".bin"
	if !validCredentialRef(cfg.CredentialRef) {
		return cfg, keys, errProviderCredentialsRead
	}
	info, err := store.credentials.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || !privateCredentialPath(info) {
		return cfg, keys, errProviderCredentialsRead
	}
	encoded, err := store.credentials.ReadFile(name)
	if err != nil {
		return cfg, keys, err
	}
	data, err := unprotectProviderCredentials(encoded)
	if err != nil {
		return cfg, keys, err
	}
	defer clear(data)
	if err := json.Unmarshal(data, &keys); err != nil || keys.empty() {
		return cfg, keys, errProviderCredentialsRead
	}
	return cfg, keys, nil
}

func saveProviderConfig(dataDir string, cfg model.AIProviderConfig) error {
	err := withProviderStore(dataDir, func(store providerStore) error {
		prev, keys, err := store.read()
		if err != nil {
			return err
		}
		return store.commit(cfg, prev.CredentialRef, keys)
	})
	if err != nil {
		return errProviderCredentialsSave
	}
	return nil
}

func (store providerStore) commit(cfg model.AIProviderConfig, ref string, previous providerCredentials) error {
	disk := providerConfigFile{AIProviderConfig: cloneProviderConfig(cfg)}
	keys := detachProviderCredentials(&disk.AIProviderConfig)
	switch {
	case keys.empty():
	case ref != "" && keys.Active == previous.Active && maps.Equal(keys.Profiles, previous.Profiles):
		disk.CredentialRef = ref
	default:
		// Publish a fresh record before pointing ai.json at it. The previous
		// record remains usable if writing either file fails.
		disk.CredentialRef = newCredentialRef()
		name := disk.CredentialRef + ".bin"
		if err := store.writeCredentials(name, keys); err != nil {
			return err
		}
	}

	data, err := json.MarshalIndent(disk, "", "  ")
	if err == nil {
		tmp := ".ai-config-" + newCredentialRef() + ".tmp"
		if err = writeProviderData(store.data, tmp, data); err == nil {
			err = store.data.Rename(tmp, providerConfigName)
			if err != nil {
				_ = store.data.Remove(tmp)
			}
		}
	}
	if err != nil {
		if disk.CredentialRef != "" && disk.CredentialRef != ref {
			_ = store.credentials.Remove(disk.CredentialRef + ".bin")
		}
		return err
	}
	if syncCredentialDirectory(store.data) == nil {
		store.removeObsolete(disk.CredentialRef)
	}
	return nil
}

func (store providerStore) writeCredentials(name string, keys providerCredentials) error {
	data, err := json.Marshal(keys)
	if err != nil {
		return err
	}
	defer clear(data)
	encoded, err := protectProviderCredentials(data)
	if err != nil {
		return err
	}
	if err := writeProviderData(store.credentials, name, encoded); err != nil {
		return err
	}
	if err = syncCredentialDirectory(store.credentials); err == nil {
		err = syncCredentialDirectory(store.data)
	}
	if err != nil {
		_ = store.credentials.Remove(name)
	}
	return err
}

func newCredentialRef() string {
	var id [16]byte
	rand.Read(id[:])
	return hex.EncodeToString(id[:])
}

func validCredentialRef(ref string) bool {
	_, err := hex.DecodeString(ref)
	return len(ref) == 32 && err == nil && ref == strings.ToLower(ref)
}

func (store providerStore) removeObsolete(ref string) {
	entries, _ := fs.ReadDir(store.credentials.FS(), ".")
	for _, entry := range entries {
		id, found := strings.CutSuffix(entry.Name(), ".bin")
		if found && id != ref && validCredentialRef(id) && entry.Type().IsRegular() {
			_ = store.credentials.Remove(entry.Name())
		}
	}
}

func writeProviderData(dir *os.Root, name string, data []byte) error {
	f, err := dir.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = dir.Remove(name)
	}
	return err
}
