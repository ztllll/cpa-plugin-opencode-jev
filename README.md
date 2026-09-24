# cpa-plugin-opencode-jev

A minimal [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) plugin that
exposes the OpenCode **Jev** System One decision API
(`https://opencode.ai/zen/v1/systemone`) through a CPA management route,
rotating over the OpenCode Go subscription API keys already configured in CPA.

Jev is not a chat model: it evaluates a state against typed questions
(`noul` yes/no, `choice`, `score`) and returns values and probabilities.

## CPA 原生模型接入（v0.2.0 起）

插件现在以 provider `opencode-jev` 注册执行器：模型 `jev-1.13` 与 `jev-1.13-free`
出现在 CPA 的 `/v1/models` 中，下游用标准 `/v1/chat/completions` + CPA 的
sk- key 即可调用。约定：最后一条 user message 的 content 为 System One 载荷
`{"state":"...","questions":{...}}`，返回的 message.content 是 Jev 的
`answers` JSON；非流式与流式（SSE）均支持。key 轮询、429 冷却与用量统计
由 CPA 原生调度完成（凭据文件 `opencode-jev-go-*.json` 放在 auth-dir）。

## Management /ask 路由（补充通道）

管理密钥调用方仍可使用：

```

```
POST /v0/management/plugins/opencode-jev/ask
Authorization: Bearer <CPA management key>
Content-Type: application/json

{
  "state": "A customer has been charged twice.",
  "questions": {
    "is_urgent": {"type": "noul", "instructions": "Does this require urgent attention?"},
    "department": {"type": "choice", "instructions": "Which team?",
                   "criteria": {"billing": "payment issues", "support": "product support"}}
  },
  "model": "jev-1.13"   // optional, default from config
}
```

`GET /v0/management/plugins/opencode-jev/status` shows the configured model,
discovered key count and last error.

## Key rotation

The plugin reads every OpenCode Go `api-key-entries` key from the CPA config and
round-robins across them, retrying on 401/403/402/429. No extra credentials are
needed; expired keys are skipped automatically.

## Config (CPA config.yaml)

```yaml
plugins:
  configs:
    opencode-jev:
      enabled: true
      model: jev-1.13        # optional
      cpa-config-path: config.yaml  # optional override
```
