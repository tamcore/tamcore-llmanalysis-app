package plugin

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSanitizePrompt_RemovesControlChars(t *testing.T) {
	t.Parallel()

	input := "Hello\x00World\x01\x02"
	got := sanitizePrompt(input)

	if got != "HelloWorld" {
		t.Errorf("sanitizePrompt(%q) = %q, want %q", input, got, "HelloWorld")
	}
}

func TestSanitizePrompt_PreservesNormalText(t *testing.T) {
	t.Parallel()

	input := "What is the CPU usage trend?"
	got := sanitizePrompt(input)

	if got != input {
		t.Errorf("sanitizePrompt(%q) = %q, want unchanged", input, got)
	}
}

func TestSanitizePrompt_TruncatesLongInput(t *testing.T) {
	t.Parallel()

	long := make([]byte, maxPromptLength+100)
	for i := range long {
		long[i] = 'a'
	}

	got := sanitizePrompt(string(long))
	if len([]rune(got)) != maxPromptLength {
		t.Errorf("len(runes) = %d, want %d", len([]rune(got)), maxPromptLength)
	}
}

func TestSanitizePrompt_TruncatesMultiByteRunes(t *testing.T) {
	t.Parallel()

	// Build a string of emoji runes that exceeds maxPromptLength runes
	var b strings.Builder
	for range maxPromptLength + 10 {
		b.WriteRune('🔥') // 4 bytes each
	}
	got := sanitizePrompt(b.String())
	runes := []rune(got)
	if len(runes) != maxPromptLength {
		t.Errorf("len(runes) = %d, want %d", len(runes), maxPromptLength)
	}
	// Verify valid UTF-8
	for _, r := range got {
		if r == '\uFFFD' {
			t.Error("found replacement character — truncation split a rune")
		}
	}
}

func TestSanitizePrompt_PreservesCJK(t *testing.T) {
	t.Parallel()

	input := "你好世界" // 4 runes, 12 bytes
	got := sanitizePrompt(input)
	if got != input {
		t.Errorf("sanitizePrompt(%q) = %q, want unchanged", input, got)
	}
}

func TestSanitizePrompt_PreservesNewlines(t *testing.T) {
	t.Parallel()

	input := "Line 1\nLine 2\nLine 3"
	got := sanitizePrompt(input)

	if got != input {
		t.Errorf("sanitizePrompt(%q) = %q, want unchanged", input, got)
	}
}

func TestSanitizePrompt_PreservesTabs(t *testing.T) {
	t.Parallel()

	input := "col1\tcol2\tcol3"
	got := sanitizePrompt(input)

	if got != input {
		t.Errorf("sanitizePrompt(%q) = %q, want unchanged", input, got)
	}
}

func TestSanitizeContextSize_UnderLimit(t *testing.T) {
	t.Parallel()

	small := []byte(`{"panel":{"title":"test"}}`)
	err := sanitizeContextSize(small, maxContextBytes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSanitizeContextSize_OverLimit(t *testing.T) {
	t.Parallel()

	big := make([]byte, maxContextBytes+1)
	for i := range big {
		big[i] = 'x'
	}

	err := sanitizeContextSize(big, maxContextBytes)
	if err == nil {
		t.Error("expected error for oversized context")
	}
}

func TestValidateURL_ValidHTTPS(t *testing.T) {
	t.Parallel()
	if err := validateURL("https://api.openai.com/v1"); err != nil {
		t.Errorf("expected valid, got error: %v", err)
	}
}

func TestValidateURL_ValidHTTP(t *testing.T) {
	t.Parallel()
	if err := validateURL("http://localhost:3000"); err != nil {
		t.Errorf("expected valid, got error: %v", err)
	}
}

func TestValidateURL_RejectsFileScheme(t *testing.T) {
	t.Parallel()
	if err := validateURL("file:///etc/passwd"); err == nil {
		t.Error("expected error for file:// scheme")
	}
}

func TestValidateURL_RejectsJavascript(t *testing.T) {
	t.Parallel()
	if err := validateURL("javascript:alert(1)"); err == nil {
		t.Error("expected error for javascript: scheme")
	}
}

func TestValidateURL_RejectsEmptyHost(t *testing.T) {
	t.Parallel()
	if err := validateURL("http://"); err == nil {
		t.Error("expected error for empty host")
	}
}

func TestValidateURL_RejectsEmpty(t *testing.T) {
	t.Parallel()
	if err := validateURL(""); err == nil {
		t.Error("expected error for empty URL")
	}
}

func TestValidateURL_RejectsGopher(t *testing.T) {
	t.Parallel()
	if err := validateURL("gopher://evil.com"); err == nil {
		t.Error("expected error for gopher:// scheme")
	}
}

func TestSettingsAPIKeyNeverInJSON(t *testing.T) {
	t.Parallel()

	s := Settings{
		EndpointURL:    "https://example.com/v1",
		Model:          "test",
		TimeoutSeconds: 60,
		MaxTokens:      100,
		APIKey:         "super-secret-key",
	}

	// The APIKey field has json:"-" tag, so it should never appear in JSON output
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if strings.Contains(string(data), "super-secret-key") {
		t.Error("API key appeared in JSON output — json:\"-\" tag is not working")
	}

	if strings.Contains(string(data), "apiKey") {
		t.Error("apiKey field appeared in JSON output")
	}
}
