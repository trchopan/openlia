package operator

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProviderInventoryIncludesGateway(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hermes.env")
	if err := os.WriteFile(path, []byte("COPILOT_GITHUB_TOKEN=gho_test\nOPENAI_GATEWAY_API_KEY=gateway-test\nOPENAI_API_KEY=official-test\nOPENAI_API_KEY_2=official-test-2\nOPENAI_BASE_URL=https://example.test/v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	openAISlots, copilot, gateway, baseURL, err := providerInventory(path)
	if err != nil {
		t.Fatal(err)
	}
	if openAISlots != 2 || !copilot || !gateway || !baseURL {
		t.Fatalf("inventory = (%d, %t, %t, %t), want (2, true, true, true)", openAISlots, copilot, gateway, baseURL)
	}
}

func TestValidateSecretFileAcceptsGatewayKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hermes.env")
	if err := os.WriteFile(path, []byte("OPENAI_GATEWAY_API_KEY=gateway-test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateSecretFile(path); err != nil {
		t.Fatalf("gateway credential was rejected: %v", err)
	}
}

func TestHasGatewayProvider(t *testing.T) {
	if !hasGatewayProvider([]FallbackProviderConfig{{Provider: "custom", Model: "gateway", BaseURL: "http://gateway.test/v1"}}) {
		t.Fatal("custom gateway provider was not detected")
	}
	if hasGatewayProvider([]FallbackProviderConfig{{Provider: "openai-api", Model: "official"}}) {
		t.Fatal("official provider was detected as a custom gateway")
	}
}
