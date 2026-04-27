---
title: "Fine-Tuning"
summary: "Custom model training on user data"
read_when:
  - Training a model on customer data
---

# Fine-Tuning

## Compatibility

| Provider | API | pi-go | Notes |
|---|---|---|---|
| Anthropic | ⚠️ Bedrock-only (Haiku, LoRA) | ❌ | |
| OpenAI Chat | ✅ SFT, vision FT, DPO, RFT | ❌ | |
| OpenAI Responses | — | — | uses base FT'd models |
| Google Gemini | ✅ supervised tuning (LoRA / full) on Gemini 2.5 Pro/Flash/Flash-lite via Vertex | ❌ | |
| Claude CLI | ❌ | — | |
| Gemini CLI | ❌ | — | |

## Provider Documentation

- [OpenAI — Fine-tuning](https://platform.openai.com/docs/guides/fine-tuning)
- [Google — Gemini supervised tuning](https://cloud.google.com/vertex-ai/generative-ai/docs/models/gemini-supervised-tuning)
- [Anthropic on Bedrock fine-tuning](https://docs.aws.amazon.com/bedrock/latest/userguide/custom-models.html)

## pi-go Gaps

- No fine-tuning helpers anywhere — typically out of scope for an SDK focused on inference.
- Fine-tuned model IDs do work today as plain `Model.ID` strings; no special handling required.
