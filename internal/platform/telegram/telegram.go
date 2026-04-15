package telegram

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/tokyoweb3/lazy-gravity-go/internal/config"
	"github.com/tokyoweb3/lazy-gravity-go/internal/platform"
	"github.com/tokyoweb3/lazy-gravity-go/internal/tts"
)

// ensure interface implementations
var _ platform.Adapter = (*TelegramAdapter)(nil)
var _ platform.PlatformSentMessage = (*telegramSentMessage)(nil)
var _ platform.PlatformMessage = (*telegramMessage)(nil)
var _ platform.PlatformChannel = (*telegramChannel)(nil)

type ttsJob struct {
	api     *tgbotapi.BotAPI
	chatID  int64
	text    string
	replyID int
}

type TelegramAdapter struct {
	token    string
	bot      *tgbotapi.BotAPI
	onMsg    func(msg platform.PlatformMessage)
	msgMu    sync.RWMutex // protects onMsg from concurrent access
	ctx      context.Context
	cancel   context.CancelFunc
	ttsJobs  chan ttsJob
	stopOnce sync.Once
}

func NewTelegramAdapter(token string) *TelegramAdapter {
	ctx, cancel := context.WithCancel(context.Background())
	return &TelegramAdapter{
		token:   token,
		ctx:     ctx,
		cancel:  cancel,
		ttsJobs: make(chan ttsJob, 100),
	}
}

func (a *TelegramAdapter) Start() error {
	// Custom HTTP client: 60 s TLS handshake timeout (default is 10 s),
	// automatic proxy from HTTPS_PROXY / HTTP_PROXY environment variables.
	httpClient := &http.Client{
		Timeout: 120 * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   60 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout:   60 * time.Second,
			ResponseHeaderTimeout: 60 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}

	var err error
	a.bot, err = tgbotapi.NewBotAPIWithClient(a.token, tgbotapi.APIEndpoint, httpClient)
	if err != nil {
		return err
	}

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := a.bot.GetUpdatesChan(u)

	// Start bot event loop
	go func() {
		for {
			select {
			case <-a.ctx.Done():
				return
			case update := <-updates:
				a.msgMu.RLock()
				handler := a.onMsg
				a.msgMu.RUnlock()

				if update.Message == nil || handler == nil {
					continue
				}

				// ── Whitelist check ──────────────────────────────────
				allowed := a.isAllowed(update.Message.Chat.ID)
				if !allowed {
					log.Printf("[Telegram] Message from unauthorized chat %d ignored", update.Message.Chat.ID)
					continue
				}

				msgToProcess := update.Message
				go func() {
					msg := wrapTelegramMessage(msgToProcess, a)
					if msg != nil && handler != nil {
						handler(msg)
					}
				}()
			}
		}
	}()

	// Start TTS workers bounded by the adapter lifecycle
	for i := 0; i < 3; i++ {
		go func() {
			for {
				select {
				case <-a.ctx.Done():
					return
				case job, ok := <-a.ttsJobs:
					if !ok {
						return
					}
					trySendVoice(a.ctx, job.api, job.chatID, job.text, job.replyID)
				}
			}
		}()
	}

	return nil
}

// isAllowed returns true if the chatID is in the whitelist, or if the
// whitelist is empty (allow-all mode).
func (a *TelegramAdapter) isAllowed(chatID int64) bool {
	ids := config.GetConfig().AllowedChatIDs
	if len(ids) == 0 {
		return true
	}
	chatIDStr := strconv.FormatInt(chatID, 10)
	for _, id := range ids {
		if id == chatIDStr {
			return true
		}
	}
	return false
}

func (a *TelegramAdapter) Stop() error {
	a.stopOnce.Do(func() {
		a.cancel()
		if a.bot != nil {
			a.bot.StopReceivingUpdates()
		}
	})
	return nil
}

func (a *TelegramAdapter) OnMessage(handler func(msg platform.PlatformMessage)) {
	a.msgMu.Lock()
	a.onMsg = handler
	a.msgMu.Unlock()
}

// -------------------------------------------------------------
// Wrappers
// -------------------------------------------------------------

type telegramChannel struct {
	adapter *TelegramAdapter
	chatID  int64
}

func (c *telegramChannel) ID() string       { return strconv.FormatInt(c.chatID, 10) }
func (c *telegramChannel) Platform() string { return "telegram" }

// Send sends a plain-text message to the channel (no HTML / Markdown parsing).
func (c *telegramChannel) Send(payload platform.MessagePayload) (platform.PlatformSentMessage, error) {
	msg := tgbotapi.NewMessage(c.chatID, payload.Text)
	sent, err := c.adapter.bot.Send(msg)
	if err != nil {
		return nil, err
	}
	return &telegramSentMessage{api: c.adapter.bot, chatID: c.chatID, msgID: sent.MessageID}, nil
}

// EnqueueTTS explicitly queues a TTS voice synthesis job. Only call this for
// final AI responses — not for status messages, errors, or individual chunks.
func (a *TelegramAdapter) EnqueueTTS(chatID int64, text string, replyToMsgID int) bool {
	select {
	case a.ttsJobs <- ttsJob{api: a.bot, chatID: chatID, text: text, replyID: replyToMsgID}:
		return true
	default:
		log.Println("[TTS] Job queue full, dropping TTS request")
		return false
	}
}

// trySendVoice synthesizes and sends a TTS voice message. Errors are silently logged.
func trySendVoice(ctx context.Context, api *tgbotapi.BotAPI, chatID int64, text string, replyToMsgID int) {
	plainText := tts.StripMarkdown(text)
	if len(strings.TrimSpace(plainText)) == 0 {
		return // Empty
	}
	plainText = tts.SmartTruncate(plainText, 800)

	voice := config.GetConfig().TTSVoice
	if voice == "" {
		voice = "zh-CN-YunyangNeural"
	}
	audio, err := tts.Synthesize(ctx, plainText, voice, "-5%", "+0%")
	if err != nil {
		log.Printf("[TTS] Synthesis failed: %v", err)
		return
	}
	if len(audio) == 0 {
		return
	}
	voiceFile := tgbotapi.FileBytes{Name: "voice.mp3", Bytes: audio}
	voiceMsg := tgbotapi.NewVoice(chatID, voiceFile)
	voiceMsg.ReplyToMessageID = replyToMsgID
	_, sendErr := api.Send(voiceMsg)
	if sendErr != nil {
		log.Printf("[TTS] Voice send failed: %v", sendErr)
	}
}

type telegramSentMessage struct {
	api    *tgbotapi.BotAPI
	chatID int64
	msgID  int
}

func (m *telegramSentMessage) ID() string        { return strconv.Itoa(m.msgID) }
func (m *telegramSentMessage) Platform() string  { return "telegram" }
func (m *telegramSentMessage) ChannelID() string { return strconv.FormatInt(m.chatID, 10) }

func (m *telegramSentMessage) Edit(payload platform.MessagePayload) (platform.PlatformSentMessage, error) {
	editMsg := tgbotapi.NewEditMessageText(m.chatID, m.msgID, payload.Text)
	// Plain text edit — no parsing mode
	_, err := m.api.Send(editMsg)
	if err != nil {
		return nil, err
	}
	return m, nil
}

func (m *telegramSentMessage) Delete() error {
	delMsg := tgbotapi.NewDeleteMessage(m.chatID, m.msgID)
	_, err := m.api.Request(delMsg)
	return err
}

// ------------------------------------------------
// Message Wrapper
// ------------------------------------------------

type telegramMessage struct {
	msg        *tgbotapi.Message
	adapter    *TelegramAdapter
	cChat      *telegramChannel
	localFiles []string
}

func wrapTelegramMessage(msg *tgbotapi.Message, adapter *TelegramAdapter) *telegramMessage {
	var localFiles []string

	// Check for photo attachment — use a timeout-limited client
	if len(msg.Photo) > 0 {
		highestRes := msg.Photo[len(msg.Photo)-1]
		fileURL, err := adapter.bot.GetFileDirectURL(highestRes.FileID)
		if err == nil && fileURL != "" {
			dlClient := &http.Client{
				Timeout:   30 * time.Second,
				Transport: &http.Transport{Proxy: http.ProxyFromEnvironment},
			}
			resp, err := dlClient.Get(fileURL)
			if err == nil {
				defer resp.Body.Close()
				
				// Limit download to 20MB to prevent disk exhaustion
				const maxImageSize = 20 * 1024 * 1024
				
				if resp.ContentLength > maxImageSize {
					log.Printf("[Telegram] Image %s exceeds 20MB limit (Content-Length: %d)", highestRes.FileID, resp.ContentLength)
				} else {
					tmpDir := os.TempDir()
					fileName := fmt.Sprintf("tg-photo-%s.jpg", highestRes.FileID)
					fullPath := filepath.Join(tmpDir, fileName)

					file, err := os.Create(fullPath)
					if err == nil {
						lr := io.LimitReader(resp.Body, maxImageSize+1)
						written, copyErr := io.Copy(file, lr)
						file.Close()

						if copyErr != nil {
							os.Remove(fullPath)
							log.Printf("[Telegram] Error downloading image: %v", copyErr)
						} else if written > maxImageSize {
							os.Remove(fullPath)
							log.Printf("[Telegram] Image %s exceeds 20MB limit during read", highestRes.FileID)
						} else {
							localFiles = append(localFiles, fullPath)
						}
					}
				}
			}
		}
	}

	return &telegramMessage{
		msg:        msg,
		adapter:    adapter,
		cChat:      &telegramChannel{adapter: adapter, chatID: msg.Chat.ID},
		localFiles: localFiles,
	}
}

func (m *telegramMessage) ID() string       { return strconv.Itoa(m.msg.MessageID) }
func (m *telegramMessage) Platform() string { return "telegram" }
func (m *telegramMessage) Content() string {
	if m.msg.Text != "" {
		return m.msg.Text
	}
	return m.msg.Caption
}
func (m *telegramMessage) Attachments() []string { return m.localFiles }

func (m *telegramMessage) Author() platform.PlatformUser {
	if m.msg.From == nil {
		return platform.PlatformUser{
			ID:          "0",
			Platform:    "telegram",
			Username:    "unknown",
			DisplayName: "Unknown",
			IsBot:       false,
		}
	}
	user := m.msg.From
	displayName := user.FirstName
	if user.LastName != "" {
		displayName += " " + user.LastName
	}
	return platform.PlatformUser{
		ID:          strconv.FormatInt(user.ID, 10),
		Platform:    "telegram",
		Username:    user.UserName,
		DisplayName: displayName,
		IsBot:       user.IsBot,
	}
}

func (m *telegramMessage) Channel() platform.PlatformChannel { return m.cChat }
func (m *telegramMessage) CreatedAt() time.Time              { return time.Unix(int64(m.msg.Date), 0) }

// Reply sends a plain-text reply to the original message.
// Bug fix: removed html.EscapeString + ParseMode="HTML" which caused
// Telegram to reject messages containing unintended HTML entities.
func (m *telegramMessage) Reply(payload platform.MessagePayload) (platform.PlatformSentMessage, error) {
	replyMsg := tgbotapi.NewMessage(m.cChat.chatID, payload.Text)
	replyMsg.ReplyToMessageID = m.msg.MessageID
	// No ParseMode → plain text, always accepted by Telegram
	sent, err := m.adapter.bot.Send(replyMsg)
	if err != nil {
		return nil, err
	}
	return &telegramSentMessage{api: m.adapter.bot, chatID: m.cChat.chatID, msgID: sent.MessageID}, nil
}
