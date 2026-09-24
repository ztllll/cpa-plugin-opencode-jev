# cpa-plugin-opencode-jev

A minimal [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) plugin that
exposes the OpenCode **Jev** System One decision API
(`https://opencode.ai/zen/v1/systemone`) through a CPA management route,
rotating over the OpenCode Go subscription API keys already configured in CPA.

Jev is not a chat model: it evaluates a state against typed questions
(`noul` yes/no, `choice`, `score`) and returns values and probabilities.

## Usage

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
