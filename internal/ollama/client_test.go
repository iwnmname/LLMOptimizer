package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (transport transportFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestAskWithClientSendsPromptAndReadsUsage(t *testing.T) {
	for _, tc := range []struct {
		name   string
		model  string
		format json.RawMessage
	}{
		{name: "plain text", model: DefaultModel},
		{name: "structured answer", model: "fixture:light", format: json.RawMessage(`{"type":"object"}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: transportFunc(func(request *http.Request) (*http.Response, error) {
				if request.URL.String() != chatURL || request.Method != http.MethodPost ||
					request.Header.Get("Content-Type") != "application/json" {
					t.Fatalf("unexpected HTTP request: %v", request)
				}
				defer request.Body.Close()
				var sent struct {
					chatRequest
					Streaming  *bool `json:"stream"`
					KeepLoaded *int  `json:"keep_alive"`
				}
				if err := json.NewDecoder(request.Body).Decode(&sent); err != nil {
					t.Fatal(err)
				}
				if sent.Model != tc.model || sent.Options["num_ctx"] != 4096 || !bytes.Equal(sent.Format, tc.format) {
					t.Fatalf("unexpected chat settings: %+v", sent)
				}
				if sent.Streaming == nil || *sent.Streaming || sent.KeepLoaded == nil || *sent.KeepLoaded != 0 {
					t.Fatalf("unexpected streaming or memory settings: %+v", sent)
				}
				if len(sent.Messages) != 1 || sent.Messages[0] != (message{Role: "user", Content: "Sample task"}) {
					t.Fatalf("sent messages: %+v", sent.Messages)
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(strings.NewReader(
						`{"model":"fixture","message":{"content":"result"},"done":true,"prompt_eval_count":12,"eval_count":4}`)),
				}, nil
			})}
			reply, err := askWithClient(t.Context(), client, tc.model, "Sample task", tc.format)
			if err != nil || reply.Model != "fixture" || reply.Text != "result" ||
				reply.InputTokens != 12 || reply.OutputTokens != 4 {
				t.Fatalf("reply = %+v, error = %v", reply, err)
			}
		})
	}
}

func TestAskWithClientReportsInvalidResponses(t *testing.T) {
	cases := []struct {
		name      string
		code      int
		body      string
		wantError string
	}{
		{name: "model unavailable", code: 404, body: `{"error":"model not found"}`, wantError: "model not found"},
		{name: "invalid JSON", code: 200, body: "invalid", wantError: "decode Ollama response"},
		{name: "incomplete response", code: 200, body: `{"done":false}`, wantError: "no complete answer"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: transportFunc(func(request *http.Request) (*http.Response, error) {
				request.Body.Close()
				return &http.Response{
					StatusCode: tc.code, Status: http.StatusText(tc.code),
					Body: io.NopCloser(strings.NewReader(tc.body)),
				}, nil
			})}
			_, err := askWithClient(t.Context(), client, DefaultModel, "Sample task", nil)
			if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("error = %v, want %q", err, tc.wantError)
			}
		})
	}
}

func TestAskWithClientPreservesCancellation(t *testing.T) {
	client := &http.Client{Transport: transportFunc(func(request *http.Request) (*http.Response, error) {
		request.Body.Close()
		return nil, request.Context().Err()
	})}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := askWithClient(ctx, client, DefaultModel, "Sample task", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context cancellation", err)
	}
}
