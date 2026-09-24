package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

const systemoneURL = "https://opencode.ai/zen/v1/systemone"
const managementBasePath = "/plugins/" + pluginID

func handleManagementRegister() ([]byte, error) {
	return okEnvelope(pluginapi.ManagementRegistrationResponse{
		Routes: []pluginapi.ManagementRoute{
			{Method: http.MethodGet, Path: managementBasePath + "/status"},
			{Method: http.MethodPost, Path: managementBasePath + "/ask"},
		},
	})
}

func jsonResponse(status int, v any) ([]byte, error) {
	body, errMarshal := json.Marshal(v)
	if errMarshal != nil {
		return nil, errMarshal
	}
	return okEnvelope(pluginapi.ManagementResponse{
		StatusCode: status,
		Headers:    http.Header{"Content-Type": []string{"application/json"}},
		Body:       body,
	})
}

func normalizeManagementPath(path string) string {
	if idx := strings.Index(path, "/v0/management/plugins/"+pluginID); idx >= 0 {
		return path[idx+len("/v0/management/plugins/"+pluginID):]
	}
	if strings.HasPrefix(path, managementBasePath) {
		return strings.TrimPrefix(path, managementBasePath)
	}
	if path == "" {
		return "/"
	}
	return path
}

func handleManagement(raw []byte) ([]byte, error) {
	var req pluginapi.ManagementRequest
	if errUnmarshal := json.Unmarshal(raw, &req); errUnmarshal != nil {
		return jsonResponse(http.StatusBadRequest, map[string]string{"error": "invalid management request"})
	}
	switch {
	case req.Method == http.MethodGet && normalizeManagementPath(req.Path) == "/status":
		return jsonResponse(http.StatusOK, buildStatus())
	case req.Method == http.MethodPost && normalizeManagementPath(req.Path) == "/ask":
		return handleAsk(req)
	default:
		return jsonResponse(http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

func buildStatus() map[string]any {
	mu.RLock()
	defer mu.RUnlock()
	status := map[string]any{
		"model":      cfg.Model,
		"keys":       len(cfg.Keys),
		"last_used":  lastUsed,
		"last_error": lastErr,
	}
	return status
}

// handleAsk forwards a System One decision request to Jev, rotating over the
// discovered OpenCode Go keys. Key rotation plus retry on 401/403/429 keeps
// one expired or throttled key from breaking callers.
//
// ponytail: naive round-robin without cooldowns/health tracking; if a key
// stays expired the extra retry per ask is the ceiling — port the pool
// plugin's account state if throughput ever matters.
func handleAsk(req pluginapi.ManagementRequest) ([]byte, error) {
	var body struct {
		Model     string          `json:"model"`
		State     string          `json:"state"`
		Questions json.RawMessage `json:"questions"`
	}
	if errUnmarshal := json.Unmarshal(req.Body, &body); errUnmarshal != nil {
		return jsonResponse(http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
	}
	if strings.TrimSpace(body.State) == "" || len(body.Questions) == 0 {
		return jsonResponse(http.StatusBadRequest, map[string]string{"error": "state and questions are required"})
	}
	mu.RLock()
	model := body.Model
	if strings.TrimSpace(model) == "" {
		model = cfg.Model
	}
	keys := append([]string(nil), cfg.Keys...)
	mu.RUnlock()
	if len(keys) == 0 {
		return jsonResponse(http.StatusServiceUnavailable, map[string]string{"error": "no OpenCode Go keys discovered"})
	}
	payload, errMarshal := json.Marshal(map[string]any{
		"model":     model,
		"state":     body.State,
		"questions": json.RawMessage(body.Questions),
	})
	if errMarshal != nil {
		return jsonResponse(http.StatusBadRequest, map[string]string{"error": "invalid questions payload"})
	}
	var lastMsg string
	for i := 0; i < len(keys); i++ {
		key := keys[int(robin.Add(1))%len(keys)]
		resp, errDo := hostHTTPDo(pluginapi.HTTPRequest{
			Method: http.MethodPost,
			URL:    systemoneURL,
			Headers: http.Header{
				"Authorization": []string{"Bearer " + key},
				"Content-Type":  []string{"application/json"},
				"Accept":        []string{"application/json"},
				"User-Agent":    []string{"opencode/1.0"},
			},
			Body: payload,
		})
		if errDo != nil {
			lastMsg = errDo.Error()
			continue
		}
		if resp.StatusCode == http.StatusOK {
			mu.Lock()
			lastUsed = model
			lastErr = ""
			mu.Unlock()
			return jsonResponse(http.StatusOK, json.RawMessage(resp.Body))
		}
		lastMsg = "HTTP " + strconv.Itoa(resp.StatusCode) + ": " + truncate(string(resp.Body), 200)
			switch resp.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests, http.StatusPaymentRequired:
			continue // try the next key
		}
		break
	}
	mu.Lock()
	lastErr = lastMsg
	mu.Unlock()
	return jsonResponse(http.StatusBadGateway, map[string]string{"error": lastMsg})
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func itoa(n int) string {
	return strconv.Itoa(n)
}
