package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

const chatURL = "http://127.0.0.1:11434/api/chat"
const DefaultModel = "qwen3.5:9b-q4_K_M"

type Result struct {
	Model        string
	Text         string
	InputTokens  int
	OutputTokens int
	Duration     time.Duration
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model     string          `json:"model"`
	Messages  []message       `json:"messages"`
	Stream    bool            `json:"stream"`
	Think     bool            `json:"think"`
	KeepAlive int             `json:"keep_alive"`
	Options   map[string]int  `json:"options"`
	Format    json.RawMessage `json:"format,omitempty"`
}

type chatResponse struct {
	Model        string  `json:"model"`
	Message      message `json:"message"`
	InputTokens  int     `json:"prompt_eval_count"`
	OutputTokens int     `json:"eval_count"`
	Done         bool    `json:"done"`
	Error        string  `json:"error"`
}

func Ask(ctx context.Context, prompt string) (Result, error) {
	return AskWithFormat(ctx, DefaultModel, prompt, nil)
}

func AskWithFormat(ctx context.Context, model, prompt string, format json.RawMessage) (Result, error) {
	client := &http.Client{Timeout: 3 * time.Minute}
	return askWithClient(ctx, client, model, prompt, format)
}

func askWithClient(ctx context.Context, client *http.Client, model, prompt string, format json.RawMessage) (Result, error) {
	body, err := json.Marshal(chatRequest{
		Model:     model,
		Messages:  []message{{Role: "user", Content: prompt}},
		Stream:    false,
		Think:     false,
		KeepAlive: 0,
		Options:   map[string]int{"num_ctx": 4096},
		Format:    format,
	})
	if err != nil {
		return Result{}, fmt.Errorf("encode Ollama request: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, chatURL, bytes.NewReader(body))
	if err != nil {
		return Result{}, fmt.Errorf("create Ollama request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")

	started := time.Now()
	response, err := client.Do(request)
	if err != nil {
		return Result{}, fmt.Errorf("request local Ollama: %w", err)
	}
	defer response.Body.Close()

	var reply chatResponse
	decodeErr := json.NewDecoder(response.Body).Decode(&reply)
	if response.StatusCode != http.StatusOK {
		if reply.Error != "" {
			return Result{}, fmt.Errorf("Ollama returned %s: %s", response.Status, reply.Error)
		}
		return Result{}, fmt.Errorf("Ollama returned %s", response.Status)
	}
	if decodeErr != nil {
		return Result{}, fmt.Errorf("decode Ollama response: %w", decodeErr)
	}
	if reply.Error != "" {
		return Result{}, errors.New(reply.Error)
	}
	if !reply.Done || reply.Message.Content == "" {
		return Result{}, errors.New("Ollama returned no complete answer")
	}

	return Result{
		Model:        reply.Model,
		Text:         reply.Message.Content,
		InputTokens:  reply.InputTokens,
		OutputTokens: reply.OutputTokens,
		Duration:     time.Since(started),
	}, nil
}
