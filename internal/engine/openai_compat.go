package engine

// openAIRequest/openAIMessage/openAIStreamChunk describe the standard
// /v1/chat/completions streaming wire format. LocalEngine's OpenAI-compat
// backend (LM Studio, or any other local server that speaks this API) uses
// these directly — see local.go.
type openAIRequest struct {
	Model    string          `json:"model"`
	Messages []openAIMessage `json:"messages"`
	Stream   bool            `json:"stream"`
}
type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type openAIStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}
