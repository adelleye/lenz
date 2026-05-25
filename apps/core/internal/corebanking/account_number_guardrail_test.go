package corebanking

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAccountNumberGuardrailDocumentation(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..")
	files := []string{
		filepath.Join(root, "README.md"),
		filepath.Join(root, "PROJECT_STRUCTURE.md"),
		filepath.Join(root, "docs", "SIMPLE_TRANSACTION_CBA_V0_1.md"),
		filepath.Join(root, "docs", "testing", "cba-v0.1-02-accounts.md"),
		filepath.Join(root, "design", "openapi", "core", "corebanking.yaml"),
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			body, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			text := string(body)
			if !strings.Contains(text, "unique 10-digit") {
				t.Fatalf("%s should document the v0.1 unique 10-digit account-number guardrail", file)
			}
			if !strings.Contains(text, "NUBAN generation/check-digit validation") || !strings.Contains(strings.ToLower(text), "deferred") {
				t.Fatalf("%s should document that full NUBAN generation/check-digit validation is deferred", file)
			}
		})
	}
}
