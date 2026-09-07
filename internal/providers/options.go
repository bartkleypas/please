package providers

// ModelOptions specifies runtime inference parameters for LLM providers.
type ModelOptions struct {
	Temperature      *float64 `json:"temperature,omitempty"`
	TopP             *float64 `json:"top_p,omitempty"`
	TopK             *int     `json:"top_k,omitempty"`
	MinP             *float64 `json:"min_p,omitempty"`
	NumCtx           *int     `json:"num_ctx,omitempty"`
	MaxTokens        *int     `json:"max_tokens,omitempty"`
	RepeatPenalty    *float64 `json:"repeat_penalty,omitempty"`
	RepeatLastN      *int     `json:"repeat_last_n,omitempty"`
	FrequencyPenalty *float64 `json:"frequency_penalty,omitempty"`
}
