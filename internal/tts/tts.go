package tts

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html"
	"log"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// The edge-tts protocol in pure Go.
// Connects to Microsoft Edge's speech synthesis WebSocket endpoint,
// sends SSML, and receives binary MP3 audio chunks.

const (
	// edgeTTSClientToken is the public TrustedClientToken for the Edge Read Aloud API.
	// Obtain it by inspecting Edge browser network requests to speech.platform.bing.com,
	// or refer to any open-source edge-tts implementation.
	edgeTTSClientToken = "YOUR_TRUSTED_CLIENT_TOKEN"

	// edgeTTSOrigin is the Chrome extension origin header accepted by the Edge TTS endpoint.
	// Obtain it from the Edge Read Aloud extension's network requests.
	edgeTTSOrigin = "chrome-extension://YOUR_EXTENSION_ID"
)

func uuid() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func connectID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

func generateSecMSGec() (string, string) {
	// Microsoft Edge TTS Sec-MS-GEC token generation logic
	now := float64(time.Now().Unix())
	now += 11644473600 // Windows Epoch offset
	nowInt := int64(now)
	nowInt -= nowInt % 300 // Round down to nearest 5 minutes
	ticks := nowInt * 10000000
	strToHash := fmt.Sprintf("%d"+edgeTTSClientToken, ticks)
	hash := sha256.Sum256([]byte(strToHash))
	return strings.ToUpper(hex.EncodeToString(hash[:])), "1-143.0.3650.75"
}

// Synthesize converts text to MP3 audio using Edge TTS.
// Returns the raw MP3 bytes, or an error if synthesis fails.
func Synthesize(ctx context.Context, text string, voice string, rate string, volume string) ([]byte, error) {
	if len(strings.TrimSpace(text)) < 1 {
		return nil, nil // Empty text, skip
	}

	// 1. Establish WebSocket Connection to Edge with timeout
	secGec, secGecVer := generateSecMSGec()
	url := fmt.Sprintf("wss://speech.platform.bing.com/consumer/speech/synthesize/readaloud/edge/v1?TrustedClientToken=%s&ConnectionId=%s&Sec-MS-GEC=%s&Sec-MS-GEC-Version=%s", edgeTTSClientToken, connectID(), secGec, secGecVer)
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	headers := map[string][]string{
		"Origin":     {edgeTTSOrigin},
		"User-Agent": {"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/143.0.0.0 Safari/537.36 Edg/143.0.0.0"},
	}

	conn, _, err := dialer.DialContext(ctx, url, headers)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to TTS: %w", err)
	}
	defer conn.Close()

	// Interrupt connection on context cancellation to prevent hung read loops
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		<-ctx.Done()
		conn.Close()
	}()

	// 2. Send Configuration
	timestamp := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	configReq := fmt.Sprintf("X-Timestamp:%s\r\n"+
		"Content-Type:application/json; charset=utf-8\r\n"+
		"Path:speech.config\r\n\r\n"+
		`{"context":{"synthesis":{"audio":{"metadataoptions":{"sentenceBoundaryEnabled":"false","wordBoundaryEnabled":"true"},"outputFormat":"audio-24khz-48kbitrate-mono-mp3"}}}}`, timestamp)

	if err := conn.WriteMessage(websocket.TextMessage, []byte(configReq)); err != nil {
		return nil, err
	}

	// 3. Send SSML Request — escape user text to prevent XML injection
	requestID := uuid()
	escapedText := html.EscapeString(text)
	ssml := fmt.Sprintf(`<speak version='1.0' xmlns='http://www.w3.org/2001/10/synthesis' xml:lang='en-US'><voice name='%s'><prosody rate='%s' volume='%s'>%s</prosody></voice></speak>`, voice, rate, volume, escapedText)

	ssmlReq := fmt.Sprintf("X-RequestId:%s\r\nContent-Type:application/ssml+xml\r\nX-Timestamp:%s\r\nPath:ssml\r\n\r\n%s", requestID, timestamp, ssml)
	if err := conn.WriteMessage(websocket.TextMessage, []byte(ssmlReq)); err != nil {
		return nil, err
	}

	var audioData []byte
	receivedEnd := false
	// 4. Listen for binary fragments and turn.end
	for {
		conn.SetReadDeadline(time.Now().Add(15 * time.Second))
		msgType, msg, err := conn.ReadMessage()
		if err != nil {
			break // Connection closed or error
		}

		if msgType == websocket.TextMessage && strings.Contains(string(msg), "Path:turn.end") {
			receivedEnd = true
			break
		}

		if msgType == websocket.BinaryMessage {
			// The binary message format: 2-byte header length (big-endian),
			// then ASCII headers, then raw audio data after "Path:audio\r\n"
			if len(msg) > 2 {
				headerLen := int(msg[0])<<8 | int(msg[1])
				if headerLen+2 <= len(msg) {
					audioData = append(audioData, msg[headerLen+2:]...)
				}
			}
		}
	}

	// If we collected data but never received turn.end, the audio may be incomplete
	if !receivedEnd && len(audioData) > 0 {
		return nil, fmt.Errorf("TTS stream interrupted: received %d bytes without turn.end", len(audioData))
	}

	log.Printf("[TTS] Synthesized %d bytes for voice: %s\n", len(audioData), voice)
	return audioData, nil
}
