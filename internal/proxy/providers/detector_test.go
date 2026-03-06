package providers

import "testing"

func TestDetect_LocalModelEndpointsUseOpenAICompatible(t *testing.T) {
	tests := []struct {
		name     string
		hostname string
		path     string
		body     []byte
	}{
		{
			name:     "ollama_api_chat",
			hostname: "localhost:11434",
			path:     "/api/chat",
			body:     []byte(`{"model":"llama3.2","messages":[{"role":"user","content":"hi"}]}`),
		},
		{
			name:     "localhost_openai_chat",
			hostname: "127.0.0.1:8000",
			path:     "/v1/chat/completions",
			body:     []byte(`{"model":"llama3.2","messages":[{"role":"user","content":"hi"}]}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			normalizer := Detect(tt.hostname, tt.path, tt.body, AllNormalizers())
			if normalizer.Family() != "openai_compatible" {
				t.Fatalf("family = %q, want openai_compatible", normalizer.Family())
			}
		})
	}
}
