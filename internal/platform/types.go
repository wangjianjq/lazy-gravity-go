package platform

import "time"

// MessagePayload represents the generic payload sent to any platform
type MessagePayload struct {
	Text  string
	Files []FileAttachment
}

// FileAttachment represents a file to be sent
type FileAttachment struct {
	Name        string
	ContentType string
	Data        []byte
}

// PlatformUser represents an abstract user
type PlatformUser struct {
	ID          string
	Platform    string
	Username    string
	DisplayName string
	IsBot       bool
}

// PlatformSentMessage represents a message that has been sent by the bot
type PlatformSentMessage interface {
	ID() string
	Platform() string
	ChannelID() string
	Edit(payload MessagePayload) (PlatformSentMessage, error)
	Delete() error
}

// PlatformChannel represents an abstract channel/chat
type PlatformChannel interface {
	ID() string
	Platform() string
	Send(payload MessagePayload) (PlatformSentMessage, error)
}

// PlatformMessage represents an abstract incoming message
type PlatformMessage interface {
	ID() string
	Platform() string
	Content() string
	Author() PlatformUser
	Channel() PlatformChannel
	CreatedAt() time.Time
	Reply(payload MessagePayload) (PlatformSentMessage, error)
	Attachments() []string
}

// Adapter defines the interface for chat platform integrations
type Adapter interface {
	Start() error
	Stop() error
	// Register a callback for incoming messages
	OnMessage(handler func(msg PlatformMessage))
}
