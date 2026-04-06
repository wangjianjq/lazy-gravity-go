package discord

import (
	"errors"
	"log"

	"github.com/tokyoweb3/lazy-gravity-go/internal/platform"
)

// ensure interface implementations
var _ platform.Adapter = (*DiscordAdapter)(nil)

type DiscordAdapter struct {
	token      string
	onMsg      func(msg platform.PlatformMessage)
}

func NewDiscordAdapter(token string) *DiscordAdapter {
	return &DiscordAdapter{
		token:    token,
	}
}

func (a *DiscordAdapter) Start() error {
	log.Println("[Discord] Discord adapter is not yet implemented.")
	return errors.New("Discord adapter not yet implemented; please use Telegram")
}

func (a *DiscordAdapter) Stop() error {
	return nil
}

func (a *DiscordAdapter) OnMessage(handler func(msg platform.PlatformMessage)) {
	a.onMsg = handler
}
