package main

import (
	"context"
	"testing"
	"time"

	"github.com/wangjianjq/lazy-gravity-go/internal/config"
	"github.com/wangjianjq/lazy-gravity-go/internal/crypto"
	"github.com/wangjianjq/lazy-gravity-go/internal/platform"
	"github.com/wangjianjq/lazy-gravity-go/internal/platform/mocks"
	"github.com/wangjianjq/lazy-gravity-go/internal/testutils"
)

func TestApp_SimulatedRun(t *testing.T) {
	// 0. Enable Crypto Mock
	crypto.UseMock = true

	// 1. Setup Sandbox
	s := testutils.SetupSandbox(t)
	defer s.Cleanup()

	// 2. Initialize Config in Sandbox
	err := config.SetConfig(config.Config{
		Language:      "zh-CN",
		WorkspacePath: s.BaseDir,
	})
	if err != nil {
		t.Fatalf("failed to set config: %v", err)
	}

	// 3. Create App and Inject Mock Adapter
	app := NewApp()
	app.skipIDELaunch = true
	app.skipEvents = true
	mockAdapter := &mocks.MockAdapter{}
	app.adapter = mockAdapter

	// 4. Start App (Simulated)
	t.Log("Starting app...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	app.startup(ctx)
	t.Log("App startup called")

	if err := app.StartBot(); err != nil {
		t.Fatalf("failed to start bot: %v", err)
	}
	t.Log("Bot started")

	if !app.IsBotRunning() {
		t.Fatal("bot should be running")
	}

	// 5. Inject a simulated message
	t.Log("Injecting message...")
	mockMsg := &mocks.MockMessage{
		MID:      "test-msg-1",
		MContent: "Hello LazyGravity",
		MAuthor:  platform.PlatformUser{DisplayName: "Tester"},
		MChannel: &mocks.MockChannel{CID: "chan-1"},
	}

	mockAdapter.InjectMessage(mockMsg)
	t.Log("Message injected")

	// 6. Verify processing (Wait for queue to process)
	// We wait a bit to let the worker pick it up
	time.Sleep(2 * time.Second)
	
	t.Log("Verifying bot status...")
	if !app.IsBotRunning() {
		t.Fatal("bot crashed after message injection")
	}

	t.Log("Stopping bot...")
	app.StopBot()
	t.Log("Bot stopped")
}
