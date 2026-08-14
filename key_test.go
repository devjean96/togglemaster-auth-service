package main

import (
	"strings"
	"testing"
)

func TestGenerateAPIKey(t *testing.T) {
	first, err := generateAPIKey()
	if err != nil {
		t.Fatalf("generating API key: %v", err)
	}
	second, err := generateAPIKey()
	if err != nil {
		t.Fatalf("generating second API key: %v", err)
	}

	if !strings.HasPrefix(first, "tm_key_") || len(first) != len("tm_key_")+64 {
		t.Fatalf("unexpected key format: %q", first)
	}
	if first == second {
		t.Fatal("generated keys should be unique")
	}
}

func TestHashAPIKeyIsDeterministic(t *testing.T) {
	const expected = "33dc025d78e9d936c330595364f216b48b29fa6679dd1eec34cb3630f26e4013"

	if actual := hashAPIKey("tm_key_example"); actual != expected {
		t.Fatalf("expected %s, got %s", expected, actual)
	}
}
