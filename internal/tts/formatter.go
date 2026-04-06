package tts

import (
	"regexp"
	"strings"
)

var (
	reCodeBlock    = regexp.MustCompile("(?s)```.*?```")
	reInlineCode   = regexp.MustCompile("`([^`]*)`")
	reImages       = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	reLinks        = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	reHTML         = regexp.MustCompile(`<[^>]+>`)
	reHeaders      = regexp.MustCompile(`(?m)^#{1,6}\s+`)
	reBold         = regexp.MustCompile(`(?s)\*\*(.*?)\*\*|__(.*?)__`)
	reItalic       = regexp.MustCompile(`(?s)\*([^\*]*?)\*|_([^_]*?)_`)
	reStrike       = regexp.MustCompile(`(?s)~~(.*?)~~`)
	reBlockquote   = regexp.MustCompile(`(?m)^>\s?`)
	reHr           = regexp.MustCompile(`(?m)^[-*_]{3,}\s*$`)
	reList1        = regexp.MustCompile(`(?m)^\s*[-*+]\s+`)
	reList2        = regexp.MustCompile(`(?m)^\s*\d+\.\s+`)
	reMultNewlines = regexp.MustCompile(`\n{3,}`)
)

// StripMarkdown removes Markdown / HTML formatting to produce clean text for TTS reading.
func StripMarkdown(text string) string {
	if text == "" {
		return ""
	}
	s := text

	s = reCodeBlock.ReplaceAllString(s, "")
	s = reInlineCode.ReplaceAllString(s, "$1")
	s = reImages.ReplaceAllString(s, "")
	s = reLinks.ReplaceAllString(s, "$1")
	s = reHTML.ReplaceAllString(s, "")
	s = reHeaders.ReplaceAllString(s, "")
	s = reBold.ReplaceAllString(s, "$1$2")
	s = reItalic.ReplaceAllString(s, "$1$2")
	s = reStrike.ReplaceAllString(s, "$1")
	s = reBlockquote.ReplaceAllString(s, "")
	s = reHr.ReplaceAllString(s, "")
	s = reList1.ReplaceAllString(s, "")
	s = reList2.ReplaceAllString(s, "")
	s = reMultNewlines.ReplaceAllString(s, "\n\n")

	return strings.TrimSpace(s)
}

// SmartTruncate truncates text to maxLen runes efficiently. It attempts to find the
// last punctuation mark to avoid cutting sentences halfway.
func SmartTruncate(text string, maxLen int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= maxLen {
		return string(runes)
	}

	truncated := runes[:maxLen]
	lastPeriod := -1
	for i := len(truncated) - 1; i >= 0; i-- {
		r := truncated[i]
		if r == '。' || r == '！' || r == '？' || r == '.' || r == '!' || r == '?' {
			lastPeriod = i
			break
		}
	}

	// If we found a period in the second half of the text, break there cleanly.
	if lastPeriod > int(float64(maxLen)*0.5) {
		return string(truncated[:lastPeriod+1])
	}

	// Fallback to strict truncation with ellipsis
	return string(truncated) + "……"
}
