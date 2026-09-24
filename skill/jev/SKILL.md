---
name: jev
description: Use when you need a fast structured decision instead of an LLM chat turn — yes/no triage (noul), routing/classification among fixed options (choice), or rubric scoring (score) of a described state. Calls the OpenCode Jev System One API via the local `jev-ask` CLI; do NOT use for text generation, summarization, translation, or any open-ended prose task.
---

# Jev decision API (via `jev-ask`)

Jev answers **typed questions about a state** and returns values + probabilities
in one cheap, fast call. Use it for: intent triage, ticket routing, urgency
checks, pass/fail gates, 0–10 risk scoring, choosing between 2–5 fixed options.

## How to run

```bash
jev-ask "state text" '{"is_urgent":{"type":"noul","instructions":"Does this require urgent attention?"}}'
```

- `$1` = state: the situation to evaluate, plain text.
- `$2` = questions JSON (see types below).
- Output: `{"answers":{...},"model":"jev-1.13","usage":{...}}` on stdout; non-zero exit on failure.

On remote hosts without the wrapper, POST to
`$JEV_ENDPOINT/v0/management/plugins/opencode-jev/ask` (set JEV_ENDPOINT to your CPA base URL)
with the CPA management key and body `{"state": "...", "questions": {...}}`.

## Question types

- `noul` (yes/no): `{"type":"noul","instructions":"..."}` → answer `{"noul":0.84}` (probability of "yes").
- `choice`: `{"type":"choice","instructions":"...","criteria":{"key1":"when key1","key2":"when key2"}}` → `{"choice":"key1","confidence":...}`. The `criteria` map is REQUIRED.
- `score`: `{"type":"score","instructions":"How frustrated is the customer?","criteria":["Calm","Frustrated","Very angry"]}` — `criteria` is an ordered rubric-level array, REQUIRED (upstream returns 422 without it).

## Rules

- Ask at most a handful of questions in one call; each question is evaluated against the same state.
- This is a decision/classification tool: if the user asks for prose, thinking, or generation, use a normal model instead of jev.
- The tool rotates the pool's OpenCode Go keys automatically — do not pass API keys.
