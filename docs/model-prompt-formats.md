# Model prompt formats

How each LLM in use wants its prompt formatted, and what this fork sends. Researched 2026-11-08 against
Ollama 0.40.14 (Windows), the Hugging Face model cards and chat templates, and Ollama's `model/renderers/`.

Models in use: `gemma4:e4b-it-qat` (en-US + et-EE chat default, `listen.Model`, vision fallback); `qwen3.5:4b` (vision bench candidate only).
Claude is optional per language.

## Main rule: same prompt text for every model, per-model request fields

We never write turn tokens or end tokens ourselves. Both models are called on Ollama's OpenAI-style
`/v1/chat/completions` endpoint with role messages (`system`, `user`, `assistant`). Ollama's built-in renderer
for each model (`RENDERER gemma4` / `RENDERER qwen3.5`, shown by `ollama show --modelfile`; their `TEMPLATE` is
only `{{ .Prompt }}`) turns those into the model's own tokens. Writing `<|im_start|>` or `<|turn>` into the
content would double them and break the format.

So the shared system prompt (`prompt.Default` + `{user}`, `{search}`, `{speech}`, ... blocks) is the same whatever
the model. The model-specific part is a few request fields, all set in one table:
`llm.SamplingFor(provider, model)` in `chipper/pkg/myvector/llm/sampling.go`. Changing a model changes only which
row applies.

| Request field | where it's set | qwen3.5 | gemma4 | Claude | any other model |
|---|---|---|---|---|---|
| `temperature` | `llm.SamplingFor` | 0.7 | 1.0 | 1.0 | 1.0 (upstream) |
| `top_p` | `llm.SamplingFor` | 0.8 | 0.95 | not sent (Anthropic rejects it) | 1.0 (upstream) |
| `presence_penalty` | `llm.SamplingFor` | 1.5 | not sent | not sent | not sent |
| `top_k` | Ollama Modelfile (`/v1` can't send it) | 20 ✓ | 64 ✓ | — | Modelfile |
| thinking off | `reasoning_effort: "none"` on every Ollama request | needed | harmless | not sent | — |
| reply cap (normal / long ask) | by brain, not model: `prompt.ReplyCap` / `prompt.LongReplyCap`; `max_tokens` on Ollama, else `max_completion_tokens` | 300 / 1000 | 300 / 1000 | 1024 / 2048 | Ollama 300 / 1000, other 1024 / 2048 |

Upstream's request builders still set `temperature 1, top_p 1`; `forkAIReq` and `forkCameraReq` overwrite both from the
table. The web page's model-test ping (`llm/http.go`) sends no sampling fields. Before 2026-11-08, every non-Claude
model got that `1 / 1`. That is neither model's recommendation, and it overrode the Modelfile's `top_p 0.95`.
A Qwen3.5 model on a non-Ollama endpoint gets the thinking-off values without `reasoning_effort: none`.

To add a model family: add a row to `families` in `sampling.go` with its model card's thinking-off values, and a
case to `TestSamplingFor`. Families match the model name prefix after any `namespace/`, case-insensitive, so
`hf.co/unsloth/Qwen3.5-4B-GGUF` counts as `qwen3.5`.

## Gemma 4 (`gemma4:e4b-it-qat`)

| Item | Value | Source |
|---|---|---|
| Turn tokens | `<|turn>{role}\n` … `<turn|>\n`; roles `system`, `user`, `model` (`assistant` maps to `model`) | Google prompt-formatting page, HF template |
| BOS | `<bos>` first (Ollama writes it) | Ollama `renderers/gemma4.go` |
| Generation prompt | `<|turn>model\n` | HF template, Ollama |
| System role | **native** (new in Gemma 4; Gemma 3 folded it into the first user turn) | HF model card |
| Thinking | on only when `<|think|>` starts the system turn; output goes in `<|channel>thought\n…<channel|>`. Ollama default is off, and Ollama's parser drops channel text when off | Google docs, Ollama `parsers/gemma4.go` |
| Empty thought block when off | E2B/E4B don't emit one (the larger models do) | HF model card |
| Stop | end of the `model` turn (`<turn|>`); `<|tool_response>` is an extra stop for tools | Google docs (EOS list not checked) |
| Sampling | `temperature 1.0, top_p 0.95, top_k 64` for "all use cases" | HF model card |
| History | earlier turns keep only the final answer, never thoughts | HF model card |
| Images | image **before** the text; budgets of 70–1120 image tokens (low for captioning, high for OCR) | HF model card |
| Audio | after the text, at most 30 s (E2B/E4B/12B only) | HF model card |

Rendered by Ollama for system S plus user U, thinking off:

```
<bos><|turn>system\nS<turn|>\n<|turn>user\nU<turn|>\n<|turn>model\n
```

## Qwen3.5 (`qwen3.5:4b`)

| Item | Value | Source |
|---|---|---|
| Turn tokens | ChatML: `<|im_start|>{role}\n` … `<|im_end|>\n`; roles `system`, `user`, `assistant`, `tool` | HF `tokenizer_config.json` |
| BOS | none (`bos_token: null`) | HF tokenizer config |
| EOS / stop | `<|im_end|>` | HF tokenizer config |
| System role | native; no default system prompt is added when none is given | HF template, Ollama |
| Thinking | **on by default**. Off = `enable_thinking: false`, which pre-fills `<think>\n\n</think>\n\n`. In Ollama: `think: false`, or on `/v1` `reasoning_effort: "none"`. Qwen3's `/think` and `/no_think` switches are not supported in 3.5 | HF model card, Ollama `renderers/qwen35.go` |
| Sampling, thinking off | `temperature 0.7, top_p 0.8, top_k 20, min_p 0, presence_penalty 1.5, repetition_penalty 1.0` | HF model card |
| Sampling, thinking on | `temperature 1.0, top_p 0.95, top_k 20, presence_penalty 1.5`: these are the Modelfile defaults, so the Modelfile is tuned for thinking, not for us | HF model card, `ollama show` |
| presence_penalty | 0–2 curbs repetition; a high value can mix languages | HF model card |
| History | thinking blocks from earlier turns are stripped | HF template, Ollama |
| Images | image before text; Ollama puts every image ahead of the turn's text anyway (`<|vision_start|><|image_pad|><|vision_end|>`) | HF model card, Ollama |

Rendered by Ollama for system S plus user U, thinking off:

```
<|im_start|>system\nS<|im_end|>\n<|im_start|>user\nU<|im_end|>\n<|im_start|>assistant\n<think>\n\n</think>\n\n
```

## Live check (2026-11-08, this PC's Ollama)

System prompt "always reply in one sentence starting with BEEP", question "What is 17 times 3?", `max_tokens 120`:

| Model | `reasoning_effort` | Reply | Thinking text | Finish reason |
|---|---|---|---|---|
| qwen3.5:4b | none | `BEEP: 17 multiplied by 3 equals 51.` | empty | stop |
| qwen3.5:4b | not sent | *(empty)* | 120 tokens of "Thinking Process: …" | length |
| gemma4:e4b-it-qat | none | `BEEP 17 times 3 equals 51.` | empty | stop |
| gemma4:e4b-it-qat | not sent | `BEEP 51.` | empty | stop |

Both models follow the system message, so it reaches each one as a real system turn. Without
`reasoning_effort: none`, Qwen spends the whole reply cap thinking and Vector says nothing.

## Open items (not done)

- **Camera photo order**: upstream `DoGetImage` sends the photo as its own user message after the question. Both
  model cards recommend the image before the text in the same turn. Fixing it means editing upstream
  `kgsim_cmds.go`, which adds upstream drift. Re-test "what do you see?" quality first.
- **Gemma image budget**: Ollama doesn't expose a per-request image-token budget on `/v1`; not checked further.
- **Quality re-check**: the en-US blind scores in `docs/llm-trials.md` (qwen3.5 4.40) were measured at
  `temperature 1, top_p 1`; on the new sampling only a `routeprobe -llm -brain` spot check has run (story + fact, en and et).
- Not verified: Gemma's full EOS list (`generation_config.json`), and how Ollama maps `reasoning_effort` inside
  its OpenAI layer (only its behaviour is verified, by the live check above).

## Sources

- Google, Gemma 4 prompt formatting: https://ai.google.dev/gemma/docs/core/prompt-formatting-gemma4
- Gemma 4 E4B model card: https://huggingface.co/google/gemma-4-E4B-it
- Gemma 4 chat template: https://huggingface.co/google/gemma-4-E4B-it/raw/main/chat_template.jinja
- Qwen3.5-4B model card: https://huggingface.co/Qwen/Qwen3.5-4B
- Qwen3.5 chat template: https://huggingface.co/Qwen/Qwen3.5-4B/raw/main/chat_template.jinja
- Ollama renderers and parsers: https://github.com/ollama/ollama/tree/main/model/renderers and `model/parsers/`
  (`gemma4.go`, `qwen35.go`)
