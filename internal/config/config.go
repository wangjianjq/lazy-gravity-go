package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/tokyoweb3/lazy-gravity-go/internal/crypto"
)

// Config is the in-memory and frontend-facing representation.
type Config struct {
	Language       string   `json:"language"`
	TelegramToken  string   `json:"telegram_token"`
	WorkspacePath  string   `json:"workspace_base_dir"`
	TTSVoice       string   `json:"tts_voice"`
	AllowedChatIDs []string `json:"allowed_chat_ids"`
	Theme          string   `json:"theme"`
}

// savedConfig is the on-disk JSON format
type savedConfig struct {
	Language         string   `json:"language"`
	TelegramTokenEnc string   `json:"telegram_token_enc,omitempty"`
	WorkspacePath    string   `json:"workspace_base_dir"`
	TTSVoice         string   `json:"tts_voice"`
	AllowedChatIDs   []string `json:"allowed_chat_ids,omitempty"`
	Theme            string   `json:"theme,omitempty"`
}

var (
	appConfig Config
	mu        sync.RWMutex
	// configBaseDir allows overriding the base directory for config.json (useful for tests)
	configBaseDir string
)

// SetBaseDir sets the directory where config.json is stored.
func SetBaseDir(dir string) {
	mu.Lock()
	defer mu.Unlock()
	configBaseDir = dir
}

// getConfigPath returns the path to the config file.
// Note: Caller must hold mu lock.
func getConfigPath() (string, error) {
	base := configBaseDir

	if base != "" {
		if err := os.MkdirAll(base, 0700); err != nil {
			return "", err
		}
		return filepath.Join(base, "config.json"), nil
	}

	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exePath, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return "", err
	}
	exeDir := filepath.Dir(exePath)
	configDir := filepath.Join(exeDir, "data")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return "", err
	}
	return filepath.Join(configDir, "config.json"), nil
}

// Load reads the config file from disk and decrypts the token.
func Load() error {
	mu.Lock()
	defer mu.Unlock()

	path, err := getConfigPath()
	if err != nil {
		return err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var sc savedConfig
	if err := json.Unmarshal(data, &sc); err != nil {
		return err
	}

	appConfig = Config{
		Language:       sc.Language,
		WorkspacePath:  sc.WorkspacePath,
		TTSVoice:       sc.TTSVoice,
		AllowedChatIDs: sc.AllowedChatIDs,
		Theme:          sc.Theme,
	}

	if sc.TelegramTokenEnc != "" {
		plain, err := crypto.Decrypt(sc.TelegramTokenEnc)
		if err != nil {
			appConfig.TelegramToken = ""
		} else {
			appConfig.TelegramToken = plain
		}
	}

	return nil
}

// Save encrypts the token and writes the config file atomically.
func Save() error {
	mu.RLock()
	c := appConfig
	path, err := getConfigPath()
	mu.RUnlock()

	if err != nil {
		return err
	}

	tokenEnc, err := crypto.Encrypt(c.TelegramToken)
	if err != nil {
		return err
	}

	sc := savedConfig{
		Language:         c.Language,
		TelegramTokenEnc: tokenEnc,
		WorkspacePath:    c.WorkspacePath,
		TTSVoice:         c.TTSVoice,
		AllowedChatIDs:   c.AllowedChatIDs,
		Theme:            c.Theme,
	}

	data, err := json.MarshalIndent(sc, "", "  ")
	if err != nil {
		return err
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return os.WriteFile(path, data, 0600)
	}
	return os.Rename(tmpPath, path)
}

// Exists checks if config exists on disk.
func Exists() bool {
	mu.RLock()
	defer mu.RUnlock()
	path, err := getConfigPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

// GetConfig returns a safe copy of the current in-memory configuration.
func GetConfig() Config {
	mu.RLock()
	defer mu.RUnlock()
	return appConfig
}

// SetConfig updates the in-memory configuration and persists it.
func SetConfig(c Config) error {
	mu.Lock()
	appConfig = c
	mu.Unlock()
	return Save()
}

// ClearConfig deletes the config file and resets the in-memory state.
func ClearConfig() error {
	mu.Lock()
	defer mu.Unlock()
	appConfig = Config{}

	path, err := getConfigPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
