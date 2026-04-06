package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"sync"
)

//go:embed en.json zh.json
var localeFS embed.FS

var (
	translations map[string]string
	currentLang  = "en"
	mu           sync.RWMutex
)

func InitTranslations(lang string) error {
	mu.Lock()
	defer mu.Unlock()

	currentLang = lang

	filename := fmt.Sprintf("%s.json", lang)
	fileContent, err := localeFS.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("could not load embedded translation file for lang %s: %v", lang, err)
	}

	err = json.Unmarshal(fileContent, &translations)
	if err != nil {
		return fmt.Errorf("failed to parse %s.json: %v", lang, err)
	}

	return nil
}

// T returns the translated string for a given key
func T(key string) string {
	mu.RLock()
	defer mu.RUnlock()
	if val, ok := translations[key]; ok {
		return val
	}
	return key // Fallback to key itself if not found
}
