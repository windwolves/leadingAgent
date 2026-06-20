package services

import "testing"

func TestProviderFromModel(t *testing.T) {
	cases := []struct {
		model    string
		expected string
	}{
		{"deepseek-chat", "deepseek"},
		{"deepseek-reasoner", "deepseek"},
		{"DeepSeek-Chat", "deepseek"},
		{"doubao-seed-1-6", "doubao"},
		{"Doubao-Pro", "doubao"},
		{"gpt-4o", "unknown"},
		{"claude-3-opus", "unknown"},
		{"", "unknown"},
	}

	for _, c := range cases {
		got := providerFromModel(c.model)
		if got != c.expected {
			t.Errorf("providerFromModel(%q) = %q, want %q", c.model, got, c.expected)
		}
	}
}
