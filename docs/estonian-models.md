# Estonian models

Every Estonian model Vector uses — speech recognition, LLM brain and voice — all local, picked per language on
wire-pod's **Language, brain & voice** page (`/myvector/voice`). English keeps its own models on each step.

| step | model | get it |
|---|---|---|
| hear (STT) | [TalTechNLP/whisper-large-v3-turbo-et-verbatim](https://huggingface.co/TalTechNLP/whisper-large-v3-turbo-et-verbatim) (`ct2/`, faster-whisper on the GPU) | `make stt-setup && make stt-enable` |
| think (LLM) | `apertus-estllm:8b` — [tartuNLP/Apertus-EstLLM-8B-Instruct-0326](https://huggingface.co/tartuNLP/Apertus-EstLLM-8B-Instruct-0326) | [Estonian LLMs](#estonian-llms-ollama) |
| speak (TTS) | Piper [`et_EE-news-medium`](https://huggingface.co/rhasspy/piper-voices/tree/main/et/et_EE/news/medium), speakers Mari, Albert, Kalev | [Estonian voices](#estonian-voices-tts) |

## Estonian speech recognition (STT)

`et-stt/` serves one faster-whisper model on the GPU (`127.0.0.1:8092`) and swaps it when the language changes:
TalTech's Estonian fine-tune for `et-EE`, stock [deepdml/faster-whisper-large-v3-turbo-ct2](https://huggingface.co/deepdml/faster-whisper-large-v3-turbo-ct2) for `en-US`.
`make stt-setup` downloads both into `robot/work/et-stt/models/` (~6 GB); `make stt-enable` installs `myvector-stt.service`.
Word error rate on FLEURS et: TalTech 8.5 %, stock turbo 18.1 % ([research](plans/archive/2026-11-05-estonian-language-mode.md)).

## Estonian voices (TTS)

`tts/` speaks Estonian through Piper's multi-speaker `et_EE-news-medium` (voices `mari` — the default — `albert`,
`kalev`; table `VOICES` in `tts/server.py`); pick them as the Estonian voices on `/myvector/voice`.
`make tts-setup` fetches only the English Piper voice — get the Estonian one into the same folder first:

```bash
d=robot/work/voice-samples/piper-voices u=https://huggingface.co/rhasspy/piper-voices/resolve/main/et/et_EE/news/medium/et_EE-news-medium.onnx
mkdir -p $d && curl -fsSL -o $d/et_EE-news-medium.onnx.json $u.json && curl -fsSL -o $d/et_EE-news-medium.onnx $u
```

## Estonian LLMs (Ollama)

The `et-EE` brain runs on a local Ollama model trained for Estonian by [TartuNLP](https://huggingface.co/tartuNLP).
General small models (gemma4, qwen3.5) write broken Estonian; these two don't. Both are Estonian-only here: English
stays on `gemma4:e4b-it-qat` / `qwen3.5:4b`, which do better in English.

| Ollama name | base model | GGUF quant used | size in VRAM | use |
|---|---|---|---|---|
| `apertus-estllm:8b` | [tartuNLP/Apertus-EstLLM-8B-Instruct-0326](https://huggingface.co/tartuNLP/Apertus-EstLLM-8B-Instruct-0326) (Apache-2.0) | [mradermacher/Apertus-EstLLM-8B-Instruct-0326-i1-GGUF](https://huggingface.co/mradermacher/Apertus-EstLLM-8B-Instruct-0326-i1-GGUF) `i1-Q4_K_M` | 5.4 GB | **the et brain in use** — most accurate facts |
| `hf.co/mradermacher/Llama-3.1-EstLLM-8B-Instruct-1125-i1-GGUF:i1-Q4_K_M` | [tartuNLP/Llama-3.1-EstLLM-8B-Instruct-1125](https://huggingface.co/tartuNLP/Llama-3.1-EstLLM-8B-Instruct-1125) | [mradermacher/Llama-3.1-EstLLM-8B-Instruct-1125-i1-GGUF](https://huggingface.co/mradermacher/Llama-3.1-EstLLM-8B-Instruct-1125-i1-GGUF) `i1-Q4_K_M` | 5.3 GB | alternative — the most natural spoken register |

How they were picked: 16 Estonian asks judged side by side against gemma4, EuroLLM and gemma3-12b
(EstLLM-1125 62.4, Apertus 60.2, gemma4 29.8 of 100; replies and scores in
[the research plan](plans/archive/2026-11-08-et-llm-research.md)). Apertus stayed the brain after the Estonian prompt
tuning ([2026-11-10-ollama-et-prompt.md](plans/archive/2026-11-10-ollama-et-prompt.md), ruling R1).

### Use one

1. Get the model into Ollama (below).
2. Open wire-pod's **Language, brain & voice** page (`/myvector/voice`), press **Refresh lists**, pick the model as the
   Estonian **Brain (LLM)** and save. The list shows what Ollama has installed (`/api/tags`).

With nothing saved, Estonian falls back to `gemma4:e4b-it-qat` (`llm.DefaultETModel`).
Sampling is set per model family in `chipper/pkg/myvector/llm/sampling.go`: `apertus` gets temperature 0.4, top_p 0.9;
EstLLM-1125 matches no family and gets wire-pod's defaults.

#### EstLLM-1125 — plain pull

```bash
ollama pull hf.co/mradermacher/Llama-3.1-EstLLM-8B-Instruct-1125-i1-GGUF:i1-Q4_K_M
```

#### Apertus-EstLLM — download, swap the chat template, create

Don't `ollama pull` the hf.co tag: its GGUF's chat template has tool-call macros Ollama's llama-server can't parse, and
every chat fails with HTTP 500 `Unable to generate parser for this template`. `apertus-estllm:8b` is the same weights
with a short template that keeps Apertus's turn tokens and turns deliberation and tools off,
[`robot/et-llm/apertus-chat_template.jinja`](../robot/et-llm/apertus-chat_template.jinja). From the repo root
(re-run 2026-11-16 on Ollama 0.40.14: same GGUF sha and Ollama model ID as the live copy, a few minutes, ~10 GB disk):

```bash
d=robot/work/apertus && mkdir -p $d   # gitignored scratch
curl -fL -o $d/apertus.gguf https://huggingface.co/mradermacher/Apertus-EstLLM-8B-Instruct-0326-i1-GGUF/resolve/main/Apertus-EstLLM-8B-Instruct-0326.i1-Q4_K_M.gguf

uv venv $d/venv && uv pip install -p $d/venv/bin/python gguf   # llama.cpp's GGUF tools (0.19.0 used)
$d/venv/bin/gguf-new-metadata --chat-template-file robot/et-llm/apertus-chat_template.jinja \
  $d/apertus.gguf $d/apertus-estllm-8b-notools.gguf
sha256sum $d/apertus-estllm-8b-notools.gguf   # 49792d6721363f28c78fda74d8ce59b5685843c61074b10b0dea60bafd412e48

printf 'FROM ./apertus-estllm-8b-notools.gguf\nPARAMETER stop <|assistant_end|>\nPARAMETER stop <|user_start|>\n' > $d/Modelfile
ollama create apertus-estllm:8b -f $d/Modelfile   # the CLI uploads the GGUF, also to Ollama on another host (OLLAMA_HOST)
```

Check it: `ollama run apertus-estllm:8b "Kes kirjutas raamatu Tõde ja Õigus?"` should answer in Estonian (Tammsaare);
`ollama list` shows ID `f5ceaf6f3823`. Then `rm -r robot/work/apertus` — Ollama keeps its own copy.
