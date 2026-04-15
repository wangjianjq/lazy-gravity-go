package testutils

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wangjianjq/lazy-gravity-go/internal/config"
	"github.com/wangjianjq/lazy-gravity-go/internal/database"
)

// Sandbox provides an isolated environment for testing
type Sandbox struct {
	BaseDir string
	t       *testing.T
}

// SetupSandbox creates a temporary workspace and redirects config/database to it
func SetupSandbox(t *testing.T) *Sandbox {
	tmpDir, err := os.MkdirTemp("", "lazy-gravity-test-*")
	if err != nil {
		t.Fatalf("failed to create sandbox directory: %v", err)
	}

	// Override config path
	config.SetBaseDir(tmpDir)

	s := &Sandbox{
		BaseDir: tmpDir,
		t:       t,
	}

	return s
}

// InitIsolatedDB initializes the database inside the sandbox
func (s *Sandbox) InitIsolatedDB() error {
	dbPath := filepath.Join(s.BaseDir, "workspace")
	return database.InitDB(dbPath)
}

// Cleanup removes all sandbox files and resets global config state
func (s *Sandbox) Cleanup() {
	database.CloseDB()
	config.SetBaseDir("") // Reset to default
	_ = os.RemoveAll(s.BaseDir)
}

// GetConfigPath returns the path to the config file in the sandbox
func (s *Sandbox) GetConfigPath() string {
	return filepath.Join(s.BaseDir, "config.json")
}
