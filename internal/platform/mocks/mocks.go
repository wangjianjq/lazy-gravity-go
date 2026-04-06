package mocks

import (
	"time"

	"github.com/tokyoweb3/lazy-gravity-go/internal/platform"
)

// MockAdapter implements platform.Adapter
type MockAdapter struct {
	Handler func(msg platform.PlatformMessage)
	Running bool
}

func (m *MockAdapter) Start() error {
	m.Running = true
	return nil
}

func (m *MockAdapter) Stop() error {
	m.Running = false
	return nil
}

func (m *MockAdapter) OnMessage(handler func(msg platform.PlatformMessage)) {
	m.Handler = handler
}

// InjectMessage simulates an incoming message from the platform
func (m *MockAdapter) InjectMessage(msg platform.PlatformMessage) {
	if m.Handler != nil {
		m.Handler(msg)
	}
}

// MockMessage implements platform.PlatformMessage
type MockMessage struct {
	MID         string
	MContent    string
	MAuthor     platform.PlatformUser
	MChannel    platform.PlatformChannel
	MAttach     []string
	SentReplies []platform.MessagePayload
}

func (m *MockMessage) ID() string               { return m.MID }
func (m *MockMessage) Platform() string         { return "mock" }
func (m *MockMessage) Content() string          { return m.MContent }
func (m *MockMessage) Author() platform.PlatformUser { return m.MAuthor }
func (m *MockMessage) Channel() platform.PlatformChannel { return m.MChannel }
func (m *MockMessage) CreatedAt() time.Time     { return time.Now() }
func (m *MockMessage) Attachments() []string    { return m.MAttach }
func (m *MockMessage) Reply(payload platform.MessagePayload) (platform.PlatformSentMessage, error) {
	m.SentReplies = append(m.SentReplies, payload)
	return &MockSentMessage{msgID: "reply-1"}, nil
}

// MockChannel implements platform.PlatformChannel
type MockChannel struct {
	CID string
}

func (m *MockChannel) ID() string       { return m.CID }
func (m *MockChannel) Platform() string { return "mock" }
func (m *MockChannel) Send(payload platform.MessagePayload) (platform.PlatformSentMessage, error) {
	return &MockSentMessage{msgID: "sent-1"}, nil
}

// MockSentMessage implements platform.PlatformSentMessage
type MockSentMessage struct {
	msgID string
}

func (m *MockSentMessage) ID() string        { return m.msgID }
func (m *MockSentMessage) Platform() string  { return "mock" }
func (m *MockSentMessage) ChannelID() string { return "chan-1" }
func (m *MockSentMessage) Edit(payload platform.MessagePayload) (platform.PlatformSentMessage, error) {
	return m, nil
}
func (m *MockSentMessage) Delete() error { return nil }
