package ollama

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client wraps Ollama API calls with OpenAI-compatible interface
type Client struct {
	baseURL    string
	httpClient *http.Client
	apiKey     string
	model      string
}

// MessageContent represents content in a message (can be text or image)
type MessageContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL *struct {
		URL string `json:"url"`
	} `json:"image_url,omitempty"`
}

// ChatMessage represents a message in a chat conversation
// Content can be a string or array of MessageContent for vision models
type ChatMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

// ChatRequest represents a chat completion request
type ChatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Temperature float64       `json:"temperature,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Stream      bool          `json:"stream,omitempty"`
}

// ChatResponse represents a chat completion response
type ChatResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// NewClient creates a new Ollama client
func NewClient(baseURL, model, apiKey string) *Client {
	return &Client{
		baseURL: baseURL,
		model:   model,
		httpClient: &http.Client{
			Timeout: 120 * time.Second, // Vision models can take longer
		},
		apiKey: apiKey,
	}
}

// ChatCompletion sends a chat completion request
func (c *Client) ChatCompletion(messages []ChatMessage) (*ChatResponse, error) {
	return c.ChatCompletionWithModel(c.model, messages)
}

// ChatCompletionWithModel sends a chat completion request with a specific model
func (c *Client) ChatCompletionWithModel(model string, messages []ChatMessage) (*ChatResponse, error) {
	url := fmt.Sprintf("%s/v1/chat/completions", c.baseURL)

	req := &ChatRequest{
		Model:    model,
		Messages: messages,
		Stream:   false,
	}

	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.apiKey))
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var chatResp ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &chatResp, nil
}

// OllamaMessage represents a message in Ollama's native format
type OllamaMessage struct {
	Role    string   `json:"role"`
	Content string   `json:"content"`
	Images  []string `json:"images,omitempty"` // Base64 encoded images
}

// OllamaChatRequest represents Ollama's native chat request format
type OllamaChatRequest struct {
	Model    string          `json:"model"`
	Messages []OllamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Format   string          `json:"format,omitempty"` // Set to "json" to force JSON output
}

// OllamaChatResponse represents Ollama's native chat response
type OllamaChatResponse struct {
	Model   string `json:"model"`
	Message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"message"`
	Done bool `json:"done"`
}

// ChatCompletionWithImage sends a chat completion request with an image using Ollama's native API
// imageBase64 should be the base64-encoded image data (without data: prefix)
// imageType is ignored for native API but kept for compatibility
func (c *Client) ChatCompletionWithImage(model, systemPrompt, userPrompt, imageBase64, imageType string) (*ChatResponse, error) {
	return c.ChatCompletionWithImageJSON(model, systemPrompt, userPrompt, imageBase64, imageType, false)
}

// ChatCompletionWithImageJSON sends a chat completion request with an image, optionally forcing JSON output
func (c *Client) ChatCompletionWithImageJSON(model, systemPrompt, userPrompt, imageBase64, imageType string, forceJSON bool) (*ChatResponse, error) {
	url := fmt.Sprintf("%s/api/chat", c.baseURL)

	// Build messages in Ollama's native format
	messages := []OllamaMessage{
		{
			Role:    "system",
			Content: systemPrompt,
		},
		{
			Role:    "user",
			Content: userPrompt,
			Images:  []string{imageBase64}, // Ollama expects raw base64, no data: prefix
		},
	}

	req := &OllamaChatRequest{
		Model:    model,
		Messages: messages,
		Stream:   false,
	}

	if forceJSON {
		req.Format = "json"
	}

	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var ollamaResp OllamaChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Convert to ChatResponse format for compatibility
	return &ChatResponse{
		Model: ollamaResp.Model,
		Choices: []struct {
			Index   int `json:"index"`
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		}{
			{
				Index: 0,
				Message: struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				}{
					Role:    ollamaResp.Message.Role,
					Content: ollamaResp.Message.Content,
				},
				FinishReason: "stop",
			},
		},
	}, nil
}

// GetResponseContent extracts the text content from a chat response
func GetResponseContent(resp *ChatResponse) string {
	if resp == nil || len(resp.Choices) == 0 {
		return ""
	}
	return resp.Choices[0].Message.Content
}

