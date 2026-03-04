package fixture

import "testing"

func TestValidateModelResponse_OpenAICompatiblePasses(t *testing.T) {
	err := ValidateModelResponse("openai_compatible", []byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	if err != nil {
		t.Fatalf("expected valid openai response, got %v", err)
	}
}

func TestValidateModelResponse_OpenAICompatibleMissingChoicesFails(t *testing.T) {
	err := ValidateModelResponse("openai_compatible", []byte(`{"id":"resp-1"}`))
	if err == nil {
		t.Fatal("expected missing choices error")
	}
}

func TestValidateModelResponse_MalformedJSONFails(t *testing.T) {
	err := ValidateModelResponse("openai_compatible", []byte(`not json`))
	if err == nil {
		t.Fatal("expected malformed json error")
	}
}

func TestValidateModelResponse_UnknownProviderAllowsGenericObject(t *testing.T) {
	err := ValidateModelResponse("unknown", []byte(`{"any":"shape"}`))
	if err != nil {
		t.Fatalf("expected unknown provider object to pass, got %v", err)
	}
}

func TestValidateModelResponse_ErrorPayloadAllowed(t *testing.T) {
	err := ValidateModelResponse("openai_compatible", []byte(`{"error":{"message":"rate limit"}}`))
	if err != nil {
		t.Fatalf("expected error payload to be accepted, got %v", err)
	}
}
