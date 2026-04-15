package telegram

import (
	"testing"

	"github.com/wangjianjq/lazy-gravity-go/internal/config"
)

func TestIsAllowed(t *testing.T) {
	tests := []struct {
		name           string
		allowedChatIDs []string
		checkID        int64
		expected       bool
	}{
		{
			name:           "Empty whitelist allows all",
			allowedChatIDs: []string{},
			checkID:        12345,
			expected:       true,
		},
		{
			name:           "Nil whitelist allows all",
			allowedChatIDs: nil,
			checkID:        99999,
			expected:       true,
		},
		{
			name:           "ID in whitelist",
			allowedChatIDs: []string{"111", "222", "12345"},
			checkID:        12345,
			expected:       true,
		},
		{
			name:           "ID not in whitelist",
			allowedChatIDs: []string{"111", "222"},
			checkID:        12345,
			expected:       false,
		},
	}

	adapter := NewTelegramAdapter("dummy_token")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup global config state temporarily
			currentCfg := config.GetConfig()
			currentCfg.AllowedChatIDs = tt.allowedChatIDs
			_ = config.SetConfig(currentCfg)

			result := adapter.isAllowed(tt.checkID)
			if result != tt.expected {
				t.Errorf("isAllowed(%d) = %v; want %v (whitelist: %v)", tt.checkID, result, tt.expected, tt.allowedChatIDs)
			}
		})
	}
	
	// Clean up global config
	_ = config.ClearConfig()
}
