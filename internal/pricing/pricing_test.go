package pricing

import (
	"encoding/json"
	"testing"
)

func TestParsePriceItem_InputAndOutput(t *testing.T) {
	doc := priceDoc{}
	doc.Product.Attributes = map[string]string{
		"modelId":   "anthropic.claude-3-sonnet",
		"modelName": "Claude 3 Sonnet",
	}
	doc.Terms.OnDemand = map[string]struct {
		PriceDimensions map[string]struct {
			Group        string `json:"group"`
			Description  string `json:"description"`
			PricePerUnit struct {
				USD string `json:"USD"`
			} `json:"pricePerUnit"`
		} `json:"priceDimensions"`
	}{
		"term1": {
			PriceDimensions: map[string]struct {
				Group        string `json:"group"`
				Description  string `json:"description"`
				PricePerUnit struct {
					USD string `json:"USD"`
				} `json:"pricePerUnit"`
			}{
				"dim-input": {
					Group:       "Input Tokens",
					Description: "Price per input token",
					PricePerUnit: struct {
						USD string `json:"USD"`
					}{USD: "0.000003"},
				},
				"dim-output": {
					Group:       "Output Tokens",
					Description: "Price per output token",
					PricePerUnit: struct {
						USD string `json:"USD"`
					}{USD: "0.000015"},
				},
			},
		},
	}

	data, _ := json.Marshal(doc)
	result := make(map[string]ModelPricing)
	parsePriceItem(string(data), result)

	mp, ok := result["anthropic.claude-3-sonnet"]
	if !ok {
		t.Fatal("expected model to be present in result")
	}
	if mp.Name != "Claude 3 Sonnet" {
		t.Errorf("Name = %q, want %q", mp.Name, "Claude 3 Sonnet")
	}
	// 0.000003 * 1_000_000 = 3.0
	if mp.InputPricePerMillion != 3.0 {
		t.Errorf("InputPricePerMillion = %f, want 3.0", mp.InputPricePerMillion)
	}
	// 0.000015 * 1_000_000 = 15.0
	if mp.OutputPricePerMillion != 15.0 {
		t.Errorf("OutputPricePerMillion = %f, want 15.0", mp.OutputPricePerMillion)
	}
}

func TestParsePriceItem_InvalidJSON(t *testing.T) {
	result := make(map[string]ModelPricing)
	parsePriceItem("{invalid-json", result)
	if len(result) != 0 {
		t.Errorf("expected empty result for invalid JSON, got %d entries", len(result))
	}
}

func TestParsePriceItem_NoModelID(t *testing.T) {
	doc := priceDoc{}
	doc.Product.Attributes = map[string]string{
		"someOtherField": "value",
	}
	data, _ := json.Marshal(doc)

	result := make(map[string]ModelPricing)
	parsePriceItem(string(data), result)
	if len(result) != 0 {
		t.Errorf("expected empty result for missing model ID, got %d entries", len(result))
	}
}

func TestParsePriceItem_ZeroPrice(t *testing.T) {
	doc := priceDoc{}
	doc.Product.Attributes = map[string]string{
		"modelId": "test-model",
	}
	doc.Terms.OnDemand = map[string]struct {
		PriceDimensions map[string]struct {
			Group        string `json:"group"`
			Description  string `json:"description"`
			PricePerUnit struct {
				USD string `json:"USD"`
			} `json:"pricePerUnit"`
		} `json:"priceDimensions"`
	}{
		"term1": {
			PriceDimensions: map[string]struct {
				Group        string `json:"group"`
				Description  string `json:"description"`
				PricePerUnit struct {
					USD string `json:"USD"`
				} `json:"pricePerUnit"`
			}{
				"dim-input": {
					Group:       "Input Tokens",
					Description: "Price per input token",
					PricePerUnit: struct {
						USD string `json:"USD"`
					}{USD: "0.0000000000"},
				},
			},
		},
	}

	data, _ := json.Marshal(doc)
	result := make(map[string]ModelPricing)
	parsePriceItem(string(data), result)

	// Zero price should be skipped.
	if mp, ok := result["test-model"]; ok && mp.InputPricePerMillion != 0 {
		t.Errorf("expected zero price to be skipped, got %f", mp.InputPricePerMillion)
	}
}

func TestParsePriceItem_PromptAndCompletionDescriptions(t *testing.T) {
	doc := priceDoc{}
	doc.Product.Attributes = map[string]string{
		"model": "anthropic.claude-v2",
	}
	doc.Terms.OnDemand = map[string]struct {
		PriceDimensions map[string]struct {
			Group        string `json:"group"`
			Description  string `json:"description"`
			PricePerUnit struct {
				USD string `json:"USD"`
			} `json:"pricePerUnit"`
		} `json:"priceDimensions"`
	}{
		"term1": {
			PriceDimensions: map[string]struct {
				Group        string `json:"group"`
				Description  string `json:"description"`
				PricePerUnit struct {
					USD string `json:"USD"`
				} `json:"pricePerUnit"`
			}{
				"dim-prompt": {
					Group:       "",
					Description: "Per prompt token",
					PricePerUnit: struct {
						USD string `json:"USD"`
					}{USD: "0.000008"},
				},
				"dim-completion": {
					Group:       "",
					Description: "Per completion token",
					PricePerUnit: struct {
						USD string `json:"USD"`
					}{USD: "0.000024"},
				},
			},
		},
	}

	data, _ := json.Marshal(doc)
	result := make(map[string]ModelPricing)
	parsePriceItem(string(data), result)

	mp, ok := result["anthropic.claude-v2"]
	if !ok {
		t.Fatal("expected model to be present in result")
	}
	if mp.InputPricePerMillion != 8.0 {
		t.Errorf("InputPricePerMillion = %f, want 8.0", mp.InputPricePerMillion)
	}
	if mp.OutputPricePerMillion != 24.0 {
		t.Errorf("OutputPricePerMillion = %f, want 24.0", mp.OutputPricePerMillion)
	}
}

// Note: isInputPrice/isOutputPrice receive already-lowered strings from parsePriceItem.
func TestIsInputPrice(t *testing.T) {
	tests := []struct {
		group, desc string
		want        bool
	}{
		{"input tokens", "", true},
		{"", "price per input token", true},
		{"", "per prompt token", true},
		{"output tokens", "", false},
		{"", "something else", false},
	}
	for _, tt := range tests {
		got := isInputPrice(tt.group, tt.desc)
		if got != tt.want {
			t.Errorf("isInputPrice(%q, %q) = %v, want %v", tt.group, tt.desc, got, tt.want)
		}
	}
}

func TestIsOutputPrice(t *testing.T) {
	tests := []struct {
		group, desc string
		want        bool
	}{
		{"output tokens", "", true},
		{"", "price per output token", true},
		{"", "per completion token", true},
		{"input tokens", "", false},
		{"", "something else", false},
	}
	for _, tt := range tests {
		got := isOutputPrice(tt.group, tt.desc)
		if got != tt.want {
			t.Errorf("isOutputPrice(%q, %q) = %v, want %v", tt.group, tt.desc, got, tt.want)
		}
	}
}
