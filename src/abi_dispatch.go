package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

// The C-ABI dispatch for the plugin-host contract. Executors and providers are
// served through the same method dispatch as management routes; shapes follow
// the official gemini-cli plugin ABI.

type abiIdentifierResponse struct {
	Identifier string `json:"identifier"`
}

type abiAuthRefreshRequest struct {
	pluginapi.AuthRefreshRequest
	HostCallbackID string `json:"host_callback_id,omitempty"`
}

type abiExecutorRequest struct {
	pluginapi.ExecutorRequest
	HostCallbackID string `json:"host_callback_id,omitempty"`
	StreamID       string `json:"stream_id,omitempty"`
}

type abiExecutorStreamResponse struct {
	Headers http.Header                     `json:"headers,omitempty"`
	Chunks  []pluginapi.ExecutorStreamChunk `json:"chunks,omitempty"`
}

type abiExecutorHTTPRequest struct {
	pluginapi.ExecutorHTTPRequest
	HostCallbackID string `json:"host_callback_id,omitempty"`
}

type abiHostHTTPRequest struct {
	pluginapi.HTTPRequest
	HostCallbackID string `json:"host_callback_id,omitempty"`
}

type abiCapabilities struct {
	Scheduler             bool     `json:"scheduler"`
	UsagePlugin           bool     `json:"usage_plugin"`
	ManagementAPI         bool     `json:"management_api"`
	AuthProvider          bool     `json:"auth_provider"`
	ModelProvider         bool     `json:"model_provider"`
	Executor              bool     `json:"executor"`
	ExecutorModelScope    string   `json:"executor_model_scope"`
	ExecutorInputFormats  []string `json:"executor_input_formats"`
	ExecutorOutputFormats []string `json:"executor_output_formats"`
}

type abiRegistration struct {
	SchemaVersion uint32             `json:"schema_version"`
	Metadata      pluginapi.Metadata `json:"metadata"`
	Capabilities  abiCapabilities    `json:"capabilities"`
}

func registrationResponse() abiRegistration {
	return abiRegistration{
		SchemaVersion: pluginabi.SchemaVersion,
		Metadata: pluginapi.Metadata{
			Name:             pluginID,
			Version:          pluginVersion,
			Author:           "xiaosan",
			GitHubRepository: "https://github.com/ztllll/cpa-plugin-opencode-jev",
			ConfigFields: []pluginapi.ConfigField{
				{
					Name:        "model",
					Type:        pluginapi.ConfigFieldTypeString,
					Description: "Jev model id used when the request omits one (default jev-1.13).",
				},
			},
		},
		Capabilities: abiCapabilities{
			ManagementAPI:         true,
			AuthProvider:          true,
			ModelProvider:         true,
			Executor:              true,
			ExecutorModelScope:    "static",
			ExecutorInputFormats:  []string{"openai"},
			ExecutorOutputFormats: []string{"openai"},
		},
	}
}

// abiHostHTTPClient bridges the executor's HTTP client to host transport
// (request-log capture included on the host side).
type abiHostHTTPClient struct {
	callbackID string
}

func (c abiHostHTTPClient) Do(ctx context.Context, req pluginapi.HTTPRequest) (pluginapi.HTTPResponse, error) {
	raw, err := hostCall(pluginabi.MethodHostHTTPDo, abiHostHTTPRequest{HTTPRequest: req, HostCallbackID: c.callbackID})
	if err != nil {
		return pluginapi.HTTPResponse{}, err
	}
	var resp pluginapi.HTTPResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return pluginapi.HTTPResponse{}, err
	}
	return resp, nil
}

func (c abiHostHTTPClient) DoStream(ctx context.Context, req pluginapi.HTTPRequest) (pluginapi.HTTPStreamResponse, error) {
	return pluginapi.HTTPStreamResponse{}, fmt.Errorf("opencode-jev: streaming upstream HTTP is not supported")
}

func handleMethod(method string, request []byte) ([]byte, error) {
	switch method {
	case pluginabi.MethodPluginRegister, pluginabi.MethodPluginReconfigure:
		if errConfigure := configure(request); errConfigure != nil {
			return nil, errConfigure
		}
		return okEnvelope(registrationResponse())
	case pluginabi.MethodManagementRegister:
		return handleManagementRegister()
	case pluginabi.MethodManagementHandle:
		return handleManagement(request)
	case pluginabi.MethodAuthIdentifier, pluginabi.MethodExecutorIdentifier:
		return okEnvelope(abiIdentifierResponse{Identifier: authIdentifier()})
	case pluginabi.MethodAuthParse:
		var req pluginapi.AuthParseRequest
		if err := json.Unmarshal(request, &req); err != nil {
			return nil, err
		}
		resp, errParse := parseAuthFile(req)
		if errParse != nil {
			return okEnvelopeWithError(resp, errParse)
		}
		return okEnvelope(resp)
	case pluginabi.MethodAuthLoginStart, pluginabi.MethodAuthLoginPoll:
		return errorEnvelope("unsupported", "opencode-jev uses static API-key auth files; login flows are not used"), nil
	case pluginabi.MethodAuthRefresh:
		var rpcReq abiAuthRefreshRequest
		if err := json.Unmarshal(request, &rpcReq); err != nil {
			return nil, err
		}
		// Static keys never refresh; hand the storage back untouched.
		resp := pluginapi.AuthRefreshResponse{
			Auth: pluginapi.AuthData{
				Provider:    authIdentifier(),
				ID:          rpcReq.AuthID,
				FileName:    rpcReq.AuthID,
				StorageJSON: rpcReq.StorageJSON,
			},
		}
		return okEnvelope(resp)
	case pluginabi.MethodModelStatic:
		var req pluginapi.StaticModelRequest
		if err := json.Unmarshal(request, &req); err != nil {
			return nil, err
		}
		resp, errModels := modelStatic(context.Background(), req)
		return okEnvelopeWithError(resp, errModels)
	case pluginabi.MethodModelForAuth:
		var req pluginapi.AuthModelRequest
		if err := json.Unmarshal(request, &req); err != nil {
			return nil, err
		}
		resp, errModels := modelsForAuth(context.Background(), req)
		return okEnvelopeWithError(resp, errModels)
	case pluginabi.MethodExecutorExecute:
		var rpcReq abiExecutorRequest
		if err := json.Unmarshal(request, &rpcReq); err != nil {
			return nil, err
		}
		req := rpcReq.ExecutorRequest
		req.HTTPClient = abiHostHTTPClient{callbackID: rpcReq.HostCallbackID}
		resp, errExec := execExecute(context.Background(), req)
		return okEnvelopeWithError(resp, errExec)
	case pluginabi.MethodExecutorExecuteStream:
		var rpcReq abiExecutorRequest
		if err := json.Unmarshal(request, &rpcReq); err != nil {
			return nil, err
		}
		req := rpcReq.ExecutorRequest
		req.HTTPClient = abiHostHTTPClient{callbackID: rpcReq.HostCallbackID}
		resp, errExec := execExecuteStream(context.Background(), req)
		if errExec != nil {
			return nil, errExec
		}
		// jev has no upstream streaming: the channel is already closed, so
		// return the chunks buffered in the synchronous response.
		chunks := make([]pluginapi.ExecutorStreamChunk, 0)
		for chunk := range resp.Chunks {
			chunks = append(chunks, chunk)
		}
		return okEnvelope(abiExecutorStreamResponse{Headers: resp.Headers, Chunks: chunks})
	case pluginabi.MethodExecutorCountTokens:
		var rpcReq abiExecutorRequest
		if err := json.Unmarshal(request, &rpcReq); err != nil {
			return nil, err
		}
		req := rpcReq.ExecutorRequest
		req.HTTPClient = abiHostHTTPClient{callbackID: rpcReq.HostCallbackID}
		resp, errCount := executorCountTokens(context.Background(), req)
		return okEnvelopeWithError(resp, errCount)
	case pluginabi.MethodExecutorHTTPRequest:
		var rpcReq abiExecutorHTTPRequest
		if err := json.Unmarshal(request, &rpcReq); err != nil {
			return nil, err
		}
		resp, errExec := executorHTTPNotSupported(context.Background(), rpcReq.ExecutorHTTPRequest)
		return okEnvelopeWithError(resp, errExec)
	default:
		return errorEnvelope("unknown_method", "unknown method: "+method), nil
	}
}

func okEnvelopeWithError(v any, err error) ([]byte, error) {
	if err != nil {
		status := 0
		if sp, ok := err.(interface{ StatusCode() int }); ok {
			status = sp.StatusCode()
		}
		return errorEnvelopeWithStatus("plugin_error", err.Error(), status), nil
	}
	return okEnvelope(v)
}

var _ = http.StatusOK
