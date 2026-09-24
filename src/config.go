package main

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
	"gopkg.in/yaml.v3"
)

const (
	pluginID             = "opencode-jev"
	defaultModel         = "jev-1.13"
	defaultCPAConfigPath = "config.yaml"
)

type settings struct {
	Model string
	Keys  []string
}

var (
	mu       sync.RWMutex
	cfg      settings
	robin    atomic.Uint64
	lastErr  string
	lastUsed string
)

type registration struct {
	SchemaVersion uint32                 `json:"schema_version"`
	Metadata      pluginapi.Metadata     `json:"metadata"`
	Capabilities  registrationCapability `json:"capabilities"`
}

type registrationCapability struct {
	Scheduler     bool `json:"scheduler"`
	UsagePlugin   bool `json:"usage_plugin"`
	ManagementAPI bool `json:"management_api"`
}

type lifecycleRequest struct {
	ConfigYAML []byte `json:"config_yaml"`
}

// discoverGoKeys reads the CPA config file and collects the OpenCode Go
// subscription API keys from openai-compatibility entries.
func discoverGoKeys(cpaConfigPath string) []string {
	raw, err := os.ReadFile(cpaConfigPath)
	if err != nil {
		return nil
	}
	var doc struct {
		OpenAICompat []struct {
			BaseURL string `yaml:"base-url"`
			Keys    []struct {
				APIKey string `yaml:"api-key"`
			} `yaml:"api-key-entries"`
		} `yaml:"openai-compatibility"`
	}
	if yaml.Unmarshal(raw, &doc) != nil {
		return nil
	}
	keys := []string{}
	for _, entry := range doc.OpenAICompat {
		if !strings.Contains(strings.ToLower(entry.BaseURL), "opencode") {
			continue
		}
		for _, k := range entry.Keys {
			if key := strings.TrimSpace(k.APIKey); key != "" {
				keys = append(keys, key)
			}
		}
	}
	return keys
}

// configure handles plugin.register / plugin.reconfigure. The host passes the
// plugin's own config subtree; the plugin reads the full CPA config file
// (default "config.yaml", relative to the CPA working directory) to discover
// the OpenCode Go keys it rotates over.
func configure(raw []byte) error {
	var req lifecycleRequest
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &req)
	}
	model := defaultModel
	cpaPath := defaultCPAConfigPath
	if len(req.ConfigYAML) > 0 {
		var own struct {
			Plugins struct {
				Configs map[string]struct {
					Model         string `yaml:"model"`
					CPAConfigPath string `yaml:"cpa-config-path"`
				} `yaml:"configs"`
			} `yaml:"plugins"`
			CPAConfigPath string `yaml:"cpa-config-path"`
		}
		if yaml.Unmarshal(req.ConfigYAML, &own) == nil {
			if c, ok := own.Plugins.Configs[pluginID]; ok {
				if strings.TrimSpace(c.Model) != "" {
					model = strings.TrimSpace(c.Model)
				}
				if strings.TrimSpace(c.CPAConfigPath) != "" {
					cpaPath = strings.TrimSpace(c.CPAConfigPath)
				}
			}
			if strings.TrimSpace(own.CPAConfigPath) != "" {
				cpaPath = strings.TrimSpace(own.CPAConfigPath)
			}
		}
	}
	keys := discoverGoKeys(cpaPath)
	mu.Lock()
	cfg = settings{Model: model, Keys: keys}
	mu.Unlock()
	hostLog("info", "configured", map[string]any{"model": model, "keys": len(keys)})
	return nil
}

func pluginRegistration() registration {
	return registration{
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
		Capabilities: registrationCapability{ManagementAPI: true},
	}
}

func handleMethod(method string, request []byte) ([]byte, error) {
	switch method {
	case pluginabi.MethodPluginRegister, pluginabi.MethodPluginReconfigure:
		if errConfigure := configure(request); errConfigure != nil {
			return nil, errConfigure
		}
		return okEnvelope(pluginRegistration())
	case pluginabi.MethodManagementRegister:
		return handleManagementRegister()
	case pluginabi.MethodManagementHandle:
		return handleManagement(request)
	default:
		return errorEnvelope("unknown_method", "unknown method: "+method), nil
	}
}
