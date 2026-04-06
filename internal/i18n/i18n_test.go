package i18n

import (
	"strings"
	"sync"
	"testing"
)

// resetState tears down global translation state between tests.
func resetState() {
	mu.Lock()
	defer mu.Unlock()
	translations = nil
	currentLang = "en"
}

func TestInitTranslations_English(t *testing.T) {
	resetState()
	if err := InitTranslations("en"); err != nil {
		t.Fatalf("InitTranslations(\"en\") failed: %v", err)
	}
	// A key that must exist in en.json
	got := T("LanguageEn")
	if got != "English" {
		t.Errorf("T(\"LanguageEn\") = %q; want %q", got, "English")
	}
}

func TestInitTranslations_Chinese(t *testing.T) {
	resetState()
	if err := InitTranslations("zh"); err != nil {
		t.Fatalf("InitTranslations(\"zh\") failed: %v", err)
	}
	got := T("LanguageZh")
	if got != "简体中文" {
		t.Errorf("T(\"LanguageZh\") = %q; want %q", got, "简体中文")
	}
}

func TestInitTranslations_InvalidLang(t *testing.T) {
	resetState()
	err := InitTranslations("xx") // no xx.json embedded
	if err == nil {
		t.Fatal("expected error for unknown language, got nil")
	}
	if !strings.Contains(err.Error(), "xx") {
		t.Errorf("error message should mention language code, got: %v", err)
	}
}

func TestT_FallbackToKey(t *testing.T) {
	resetState()
	if err := InitTranslations("en"); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	key := "NonExistentKey_XYZ"
	got := T(key)
	if got != key {
		t.Errorf("T(%q) = %q; want key itself as fallback", key, got)
	}
}

func TestT_BeforeInit_FallbackToKey(t *testing.T) {
	// Reset without calling Init — translations map is nil
	mu.Lock()
	translations = nil
	currentLang = "en"
	mu.Unlock()

	key := "SomeKey"
	got := T(key)
	if got != key {
		t.Errorf("T(%q) before Init = %q; want key itself", key, got)
	}
}

func TestInitTranslations_HasCommonKeys(t *testing.T) {
	resetState()
	if err := InitTranslations("en"); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	requiredKeys := []string{
		"WelcomeMessage",
		"TelegramStarting",
		"ShutdownMessage",
		"ExitMessage",
		"TTSDisabled",
	}
	for _, k := range requiredKeys {
		v := T(k)
		if v == k {
			t.Errorf("key %q not found in en.json (T returned the key itself)", k)
		}
	}
}

func TestInitTranslations_SwitchLanguage(t *testing.T) {
	resetState()
	if err := InitTranslations("en"); err != nil {
		t.Fatalf("init en failed: %v", err)
	}
	enVal := T("StartingCommand")

	if err := InitTranslations("zh"); err != nil {
		t.Fatalf("init zh failed: %v", err)
	}
	zhVal := T("StartingCommand")

	if enVal == zhVal {
		t.Errorf("expected different translations after language switch, both = %q", enVal)
	}
	if zhVal == "StartingCommand" {
		t.Errorf("zh StartingCommand returned key as fallback; key missing from zh.json")
	}
}

func TestT_ConcurrentAccess(t *testing.T) {
	resetState()
	if err := InitTranslations("en"); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			// mix of reads and re-inits to stress the RWMutex
			_ = T("LanguageEn")
			_ = InitTranslations("zh")
			_ = T("LanguageZh")
			_ = InitTranslations("en")
		}()
	}
	wg.Wait()
	// No panic == concurrent access is safe
}
