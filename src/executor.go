package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

const systemoneURL = "https://opencode.ai/zen/v1/systemone"

// Static models contributed to CPA. Requests arrive as standard OpenAI chat
// completions; the last user message must carry the System One payload as
// JSON: {"state": "...", "questions": {...}}.
func staticModels() []pluginapi.ModelInfo {
	defs := []struct {
		id, displayName string
	}{
		{"jev-1.13", "Jev 1.13 (System One)"},
		{"jev-1.13-free", "Jev 1.13 Free (System One)"},
	}
	models := make([]pluginapi.ModelInfo, 0, len(defs))
	for _, d := range defs {
		models = append(models, pluginapi.ModelInfo{
			ID:                         d.id,
			Object:                     "model",
			Created:                    time.Now().Unix(),
			OwnedBy:                    "typesafe-ai",
			Type:                       "chat",
			DisplayName:                d.displayName,
			Name:                       d.id,
			Description:                d.displayName + " — OpenCode Jev System One decision model (structured judgments, not text generation)",
			ContextLength:              131072,
			MaxCompletionTokens:        4096,
			SupportedInputModalities:   []string{"text"},
			SupportedOutputModalities:  []string{"text"},
			SupportedGenerationMethods: []string{"chat"},
			SupportedParameters:        []string{"messages"},
		})
	}
	return models
}

func modelStatic(context.Context, pluginapi.StaticModelRequest) (pluginapi.ModelResponse, error) {
	return pluginapi.ModelResponse{Provider: pluginID, Models: staticModels()}, nil
}

func modelsForAuth(context.Context, pluginapi.AuthModelRequest) (pluginapi.ModelResponse, error) {
	return pluginapi.ModelResponse{Provider: pluginID, Models: staticModels()}, nil
}

// --- Auth provider (static API-key auth files) ---

func authIdentifier() string { return pluginID }

// parseAuthFile recognizes our auth files: {"type":"opencode-jev","api_key":"...","label":"..."}.
func parseAuthFile(req pluginapi.AuthParseRequest) (pluginapi.AuthParseResponse, error) {
	var probe struct {
		Type   string `json:"type"`
		APIKey string `json:"api_key"`
		Label  string `json:"label"`
	}
	if len(req.RawJSON) == 0 {
		return pluginapi.AuthParseResponse{}, nil
	}
	if json.Unmarshal(req.RawJSON, &probe) != nil || probe.Type != pluginID || strings.TrimSpace(probe.APIKey) == "" {
		return pluginapi.AuthParseResponse{}, nil
	}
	name := strings.TrimSpace(req.FileName)
	if name == "" {
		name = "opencode-jev.json"
	}
	label := probe.Label
	if label == "" {
		label = strings.TrimSuffix(name, ".json")
	}
	auth := pluginapi.AuthData{
		Provider:    pluginID,
		ID:          name,
		FileName:    name,
		Label:       label,
		StorageJSON: append([]byte(nil), req.RawJSON...),
		Metadata:    map[string]any{"type": pluginID, "label": label},
		Attributes:  map[string]string{"account": label},
	}
	return pluginapi.AuthParseResponse{Handled: true, Auth: auth, Auths: []pluginapi.AuthData{auth}}, nil
}

// apiKeyFromStorage extracts the account API key from the raw auth file bytes.
func apiKeyFromStorage(storage []byte) string {
	var st struct {
		APIKey string `json:"api_key"`
	}
	if json.Unmarshal(storage, &st) != nil {
		return ""
	}
	return strings.TrimSpace(st.APIKey)
}

// --- Executor ---

// execExecute serves a standard OpenAI chat-completions request whose last
// user message content is the System One payload:
//
//	{"state": "...", "questions": {...}}
//
// It returns a standard OpenAI chat completion whose message content is the
// JSON `answers` object from Jev. Credential selection, cooldowns and usage
// accounting stay in CPA's native scheduler; the executor only picks the key
// bound to this auth record.
func execExecute(ctx context.Context, req pluginapi.ExecutorRequest) (pluginapi.ExecutorResponse, error) {
	apiKey := apiKeyFromStorage(req.StorageJSON)
	if apiKey == "" {
		return pluginapi.ExecutorResponse{}, fmt.Errorf("opencode-jev: auth storage has no api_key")
	}
	state, questions, modelID, errParse := extractSystemOne(req)
	if errParse != nil {
		return pluginapi.ExecutorResponse{}, errParse
	}
	upstream, errMarshal := json.Marshal(map[string]any{"model": modelID, "state": state, "questions": questions})
	if errMarshal != nil {
		return pluginapi.ExecutorResponse{}, errMarshal
	}
	client, errClient := executorHTTPClient(req)
	if errClient != nil {
		return pluginapi.ExecutorResponse{}, errClient
	}
	resp, errDo := client.Do(ctx, pluginapi.HTTPRequest{
		Method: http.MethodPost,
		URL:    systemoneURL,
		Headers: http.Header{
			"Authorization": []string{"Bearer " + apiKey},
			"Content-Type":  []string{"application/json"},
			"Accept":        []string{"application/json"},
			"User-Agent":    []string{"opencode/1.0"},
		},
		Body: upstream,
	})
	if errDo != nil {
		return pluginapi.ExecutorResponse{}, errDo
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return pluginapi.ExecutorResponse{}, &statusError{status: resp.StatusCode, body: fmt.Sprintf("systemone HTTP %d: %.200s", resp.StatusCode, string(resp.Body))}
	}
	var payload struct {
		Answers  json.RawMessage `json:"answers"`
		Usage    struct {
			InputTokens  int64 `json:"input_tokens"`
			OutputTokens int64 `json:"output_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(resp.Body, &payload) != nil || len(payload.Answers) == 0 {
		return pluginapi.ExecutorResponse{}, fmt.Errorf("systemone returned an unexpected body")
	}
	completion := chatCompletion(modelID, string(payload.Answers), int64(payload.Usage.InputTokens), int64(payload.Usage.OutputTokens))
	raw, errMarshal := json.Marshal(completion)
	if errMarshal != nil {
		return pluginapi.ExecutorResponse{}, errMarshal
	}
	return pluginapi.ExecutorResponse{Payload: raw}, nil
}

// extractSystemOne parses the client request (OpenAI chat format) and returns
// the System One request fields. The last user message content must be a JSON
// object with "state" and "questions"; this keeps the decision semantics
// explicit instead of inventing questions from prose.
func extractSystemOne(req pluginapi.ExecutorRequest) (state string, questions json.RawMessage, model string, err error) {
	model = req.Model
	if strings.TrimSpace(model) == "" {
		model = defaultModel
	}
	body := req.Payload
	if len(body) == 0 {
		body = req.OriginalRequest
	}
	var chat struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &chat); err != nil {
		return "", nil, model, fmt.Errorf("opencode-jev: cannot parse request: %w", err)
	}
	content := ""
	for i := len(chat.Messages) - 1; i >= 0; i-- {
		if chat.Messages[i].Role == "user" {
			var text string
			if json.Unmarshal(chat.Messages[i].Content, &text) == nil {
				content = text
			} else {
				content = string(chat.Messages[i].Content)
			}
			break
		}
	}
	if strings.TrimSpace(content) == "" {
		return "", nil, model, &statusError{status: http.StatusBadRequest, body: "opencode-jev: no user message found"}
	}
	var so struct {
		State     string          `json:"state"`
		Questions json.RawMessage `json:"questions"`
	}
	if err := json.Unmarshal([]byte(content), &so); err != nil || strings.TrimSpace(so.State) == "" || len(so.Questions) == 0 {
		return "", nil, model, &statusError{status: http.StatusBadRequest, body: "opencode-jev: the last user message must be JSON {\"state\":\"...\",\"questions\":{...}} (see the typesafe-ai skill for question types)"}
	}
	return so.State, so.Questions, model, nil
}

func chatCompletion(model, content string, in, out int64) map[string]any {
	return map[string]any{
		"id":      "chatcmpl-jev-" + time.Now().UTC().Format("20060102T150405"),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]any{{
			"index":         0,
			"message":       map[string]any{"role": "assistant", "content": content},
			"finish_reason": "stop",
		}},
		"usage": map[string]any{
			"prompt_tokens":     in,
			"completion_tokens": out,
			"total_tokens":      in + out,
		},
	}
}

// execExecuteStream synthesizes an OpenAI streaming response from a single
// upstream judgment call. Chunk payloads are complete SSE data frames; CPA
// appends the final "data: [DONE]" for the openai format itself.
func execExecuteStream(ctx context.Context, req pluginapi.ExecutorRequest) (pluginapi.ExecutorStreamResponse, error) {
	resp, errExec := execExecute(ctx, req)
	if errExec != nil {
		return pluginapi.ExecutorStreamResponse{}, errExec
	}
	var completion chatCompletionShape
	if json.Unmarshal(resp.Payload, &completion) != nil || len(completion.Choices) == 0 {
		return pluginapi.ExecutorStreamResponse{}, fmt.Errorf("opencode-jev: internal stream conversion failed")
	}
	content := completion.Choices[0].Message.Content
	chunk := func(delta map[string]any, finish any) []byte {
		b, _ := json.Marshal(map[string]any{
			"id": completion.ID, "object": "chat.completion.chunk",
			"created": completion.Created, "model": completion.Model,
			"choices": []map[string]any{{"index": 0, "delta": delta, "finish_reason": finish}},
		})
		return b
	}
	out := make(chan pluginapi.ExecutorStreamChunk, 4)
	out <- pluginapi.ExecutorStreamChunk{Payload: chunk(map[string]any{"role": "assistant", "content": ""}, nil)}
	out <- pluginapi.ExecutorStreamChunk{Payload: chunk(map[string]any{"content": content}, nil)}
	out <- pluginapi.ExecutorStreamChunk{Payload: chunk(map[string]any{}, "stop")}
	close(out)
	return pluginapi.ExecutorStreamResponse{
		Headers: http.Header{"Content-Type": []string{"text/event-stream"}},
		Chunks:  out,
	}, nil
}

func executorHTTPNotSupported(ctx context.Context, req pluginapi.ExecutorHTTPRequest) (pluginapi.ExecutorHTTPResponse, error) {
	return pluginapi.ExecutorHTTPResponse{}, fmt.Errorf("opencode-jev: executor.http_request is not supported")
}

func executorCountTokens(ctx context.Context, req pluginapi.ExecutorRequest) (pluginapi.ExecutorResponse, error) {
	return pluginapi.ExecutorResponse{}, fmt.Errorf("opencode-jev: token counting is not supported for System One models")
}

// executorHTTPClient returns the host HTTP client bound to this auth callback,
// falling back to the plugin's direct host call when no callback id is present.
func executorHTTPClient(req pluginapi.ExecutorRequest) (pluginapi.HostHTTPClient, error) {
	if req.HTTPClient != nil {
		return req.HTTPClient, nil
	}
	return abiHostHTTPClient{}, nil
}

// chatCompletionShape is a defensive decode target for the completion built
// by chatCompletion; the plugin process crashes CPA on panic, so typed
// decoding is mandatory here.
type chatCompletionShape struct {
	ID      string `json:"id"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}
