package openai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client wraps OpenAI API calls
type Client struct {
	apiKey     string
	model      string
	httpClient *http.Client
}

// MessageContent represents content in a message (can be text or image)
type MessageContent struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

// ImageURL represents an image URL in a message
type ImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"` // "low", "high", or "auto"
}

// ChatMessage represents a message in a chat conversation
type ChatMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // Can be string or []MessageContent
}

// ResponseFormat specifies the format of the response
type ResponseFormat struct {
	Type string `json:"type"` // "json_object" for JSON mode
}

// ChatRequest represents a chat completion request
type ChatRequest struct {
	Model               string          `json:"model"`
	Messages            []ChatMessage   `json:"messages"`
	Temperature         float64         `json:"temperature,omitempty"`
	MaxCompletionTokens int             `json:"max_completion_tokens,omitempty"`
	ResponseFormat      *ResponseFormat `json:"response_format,omitempty"`
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

// NewClient creates a new OpenAI client
func NewClient(apiKey, model string) *Client {
	return &Client{
		apiKey: apiKey,
		model:  model,
		httpClient: &http.Client{
			Timeout: 120 * time.Second, // Vision models can take longer
		},
	}
}

// ChatCompletionWithImage sends a chat completion request with an image
// imageBase64 should be the base64-encoded image data (without data: prefix)
// imageType is the MIME type (e.g., "image/png", "image/jpeg")
func (c *Client) ChatCompletionWithImage(systemPrompt, userPrompt, imageBase64, imageType string) (*ChatResponse, error) {
	return c.ChatCompletionWithImageJSON(systemPrompt, userPrompt, imageBase64, imageType, false)
}

// ChatCompletionWithImageJSON sends a chat completion request with an image, optionally forcing JSON output
func (c *Client) ChatCompletionWithImageJSON(systemPrompt, userPrompt, imageBase64, imageType string, forceJSON bool) (*ChatResponse, error) {
	// Build the data URL for the image
	dataURL := fmt.Sprintf("data:%s;base64,%s", imageType, imageBase64)

	// Build messages with vision content
	messages := []ChatMessage{
		{
			Role:    "system",
			Content: systemPrompt,
		},
		{
			Role: "user",
			Content: []MessageContent{
				{
					Type: "text",
					Text: userPrompt,
				},
				{
					Type: "image_url",
					ImageURL: &ImageURL{
						URL:    dataURL,
						Detail: "high", // Use high detail for better accuracy
					},
				},
			},
		},
	}

	req := &ChatRequest{
		Model:               c.model,
		Messages:            messages,
		Temperature:         0.1, // Low temperature for more consistent JSON output
		MaxCompletionTokens: 1000,
	}

	if forceJSON {
		req.ResponseFormat = &ResponseFormat{Type: "json_object"}
	}

	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.apiKey))

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &chatResp, nil
}

// ReferenceImage represents a labeled reference image for few-shot learning
type ReferenceImage struct {
	Label      string // e.g., "mountain", "godly"
	Base64Data string // base64-encoded image data
	MimeType   string // e.g., "image/png"
}

// ChatCompletionWithReferences sends a chat completion with reference images followed by the main image
func (c *Client) ChatCompletionWithReferences(systemPrompt, userPrompt string, references []ReferenceImage, mainImageBase64, mainImageType string, forceJSON bool) (*ChatResponse, error) {
	// Build content array with reference images first, then the main image
	content := []MessageContent{
		{
			Type: "text",
			Text: userPrompt,
		},
	}

	// Add reference images with labels
	for _, ref := range references {
		// Add label for this reference
		content = append(content, MessageContent{
			Type: "text",
			Text: fmt.Sprintf("[Reference: %s key]", ref.Label),
		})
		// Add the reference image
		dataURL := fmt.Sprintf("data:%s;base64,%s", ref.MimeType, ref.Base64Data)
		content = append(content, MessageContent{
			Type: "image_url",
			ImageURL: &ImageURL{
				URL:    dataURL,
				Detail: "low", // Use low detail for reference images to save tokens
			},
		})
	}

	// Add separator and main image
	content = append(content, MessageContent{
		Type: "text",
		Text: "[Image to analyze:]",
	})
	mainDataURL := fmt.Sprintf("data:%s;base64,%s", mainImageType, mainImageBase64)
	content = append(content, MessageContent{
		Type: "image_url",
		ImageURL: &ImageURL{
			URL:    mainDataURL,
			Detail: "high", // Use high detail for the main image
		},
	})

	messages := []ChatMessage{
		{
			Role:    "system",
			Content: systemPrompt,
		},
		{
			Role:    "user",
			Content: content,
		},
	}

	req := &ChatRequest{
		Model:               c.model,
		Messages:            messages,
		Temperature:         0.1,
		MaxCompletionTokens: 1000,
	}

	if forceJSON {
		req.ResponseFormat = &ResponseFormat{Type: "json_object"}
	}

	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.apiKey))

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &chatResp, nil
}

// GetResponseContent extracts the text content from a chat response
func GetResponseContent(resp *ChatResponse) string {
	if resp == nil || len(resp.Choices) == 0 {
		return ""
	}
	return resp.Choices[0].Message.Content
}

