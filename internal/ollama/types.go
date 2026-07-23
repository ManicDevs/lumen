package ollama

// Message represents a single turn in a chat conversation.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Options holds generation parameters passed to the model.
type Options struct {
	NumCtx      int     `json:"num_ctx,omitempty"`
	NumPredict  int     `json:"num_predict,omitempty"`
	Seed        int     `json:"seed,omitempty"`
	Temperature float64 `json:"temperature,omitempty"`
	TopP        float64 `json:"top_p,omitempty"`
	TopK        int     `json:"top_k,omitempty"`
	Stop        []string `json:"stop,omitempty"`
}

// ChatRequest is the request body for POST /api/chat.
type ChatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
	Options  Options   `json:"options,omitempty"`
}

// ChatResponse is the full (non-streaming) response from POST /api/chat.
type ChatResponse struct {
	Model      string  `json:"model"`
	Message    Message `json:"message"`
	Done       bool    `json:"done"`
	DoneReason string  `json:"done_reason,omitempty"`

	TotalDuration      int64 `json:"total_duration,omitempty"`
	LoadDuration       int64 `json:"load_duration,omitempty"`
	PromptEvalCount    int   `json:"prompt_eval_count,omitempty"`
	PromptEvalDuration int64 `json:"prompt_eval_duration,omitempty"`
	EvalCount          int   `json:"eval_count,omitempty"`
	EvalDuration       int64 `json:"eval_duration,omitempty"`
}

// ChatStreamChunk is a single NDJSON line from a streaming chat response.
type ChatStreamChunk struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	Done       bool   `json:"done"`
	DoneReason string `json:"done_reason,omitempty"`

	TotalDuration      int64 `json:"total_duration,omitempty"`
	LoadDuration       int64 `json:"load_duration,omitempty"`
	PromptEvalCount    int   `json:"prompt_eval_count,omitempty"`
	PromptEvalDuration int64 `json:"prompt_eval_duration,omitempty"`
	EvalCount          int   `json:"eval_count,omitempty"`
	EvalDuration       int64 `json:"eval_duration,omitempty"`
}

// GenerateRequest is the request body for POST /api/generate.
type GenerateRequest struct {
	Model   string `json:"model"`
	Prompt  string `json:"prompt"`
	System  string `json:"system,omitempty"`
	Stream  bool   `json:"stream"`
	Options Options `json:"options,omitempty"`
}

// GenerateResponse is the full (non-streaming) response from /api/generate.
type GenerateResponse struct {
	Model      string `json:"model"`
	Response   string `json:"response"`
	Done       bool   `json:"done"`
	DoneReason string `json:"done_reason,omitempty"`

	TotalDuration      int64 `json:"total_duration,omitempty"`
	LoadDuration       int64 `json:"load_duration,omitempty"`
	PromptEvalCount    int   `json:"prompt_eval_count,omitempty"`
	PromptEvalDuration int64 `json:"prompt_eval_duration,omitempty"`
	EvalCount          int   `json:"eval_count,omitempty"`
	EvalDuration       int64 `json:"eval_duration,omitempty"`
}

// GenerateStreamChunk is a single NDJSON line from a streaming generate.
type GenerateStreamChunk struct {
	Model      string `json:"model"`
	Response   string `json:"response"`
	Done       bool   `json:"done"`
	DoneReason string `json:"done_reason,omitempty"`

	TotalDuration      int64 `json:"total_duration,omitempty"`
	LoadDuration       int64 `json:"load_duration,omitempty"`
	PromptEvalCount    int   `json:"prompt_eval_count,omitempty"`
	PromptEvalDuration int64 `json:"prompt_eval_duration,omitempty"`
	EvalCount          int   `json:"eval_count,omitempty"`
	EvalDuration       int64 `json:"eval_duration,omitempty"`
}

// Model describes a single model returned by the List endpoint.
type Model struct {
	Name       string `json:"name"`
	Model      string `json:"model"`
	ModifiedAt string `json:"modified_at"`
	Size       int64  `json:"size"`
	Digest     string `json:"digest"`
	Details    struct {
		Format            string   `json:"format"`
		Family            string   `json:"family"`
		Families          []string `json:"families"`
		ParameterSize     string   `json:"parameter_size"`
		QuantizationLevel string   `json:"quantization_level"`
	} `json:"details,omitempty"`
}

// ListResponse is the response from GET /api/tags.
type ListResponse struct {
	Models []Model `json:"models"`
}

// DeleteRequest is the request body for DELETE /api/delete.
type DeleteRequest struct {
	Model string `json:"model"`
}

// CopyRequest is the request body for POST /api/copy.
type CopyRequest struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
}

// PullRequest is the request body for POST /api/pull.
type PullRequest struct {
	Model   string `json:"model"`
	Stream  bool   `json:"stream"`
	Insecure bool  `json:"insecure,omitempty"`
}

// PullProgressChunk is an NDJSON line from a streaming pull operation.
type PullProgressChunk struct {
	Status    string `json:"status"`
	Digest    string `json:"digest,omitempty"`
	Total     int64  `json:"total,omitempty"`
	Completed int64  `json:"completed,omitempty"`
}

// PushRequest is the request body for POST /api/push.
type PushRequest struct {
	Model   string `json:"model"`
	Stream  bool   `json:"stream"`
	Insecure bool  `json:"insecure,omitempty"`
}

// CreateRequest is the request body for POST /api/create.
type CreateRequest struct {
	Model  string `json:"model"`
	From   string `json:"from,omitempty"`
	Stream bool   `json:"stream,omitempty"`
}

// ShowRequest is the request body for POST /api/show.
type ShowRequest struct {
	Model string `json:"model"`
}

// ShowResponse is the response from POST /api/show.
type ShowResponse struct {
	Modelfile  string `json:"modelfile"`
	Parameters string `json:"parameters"`
	Template   string `json:"template"`
	Details    struct {
		Format            string   `json:"format"`
		Family            string   `json:"family"`
		Families          []string `json:"families"`
		ParameterSize     string   `json:"parameter_size"`
		QuantizationLevel string   `json:"quantization_level"`
	} `json:"details,omitempty"`
	ModelInfo map[string]any `json:"model_info,omitempty"`
}

// EmbedRequest is the request body for POST /api/embed.
type EmbedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

// EmbedResponse is the response from POST /api/embed.
type EmbedResponse struct {
	Model           string      `json:"model"`
	Embeddings      [][]float64 `json:"embeddings"`
	TotalDuration   int64       `json:"total_duration,omitempty"`
	PromptEvalCount int         `json:"prompt_eval_count,omitempty"`
}

// PsResponse lists currently loaded models on the server.
type PsResponse struct {
	Models []LoadedModel `json:"models"`
}

// LoadedModel describes a model currently loaded in memory.
type LoadedModel struct {
	Name     string `json:"name"`
	Model    string `json:"model"`
	Size     int64  `json:"size"`
	Digest   string `json:"digest"`
	SizeVRAM int64  `json:"size_vram,omitempty"`
}
