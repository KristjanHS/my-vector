# Reply cap by kind of ask

Status: [USER DECISION] D1 owed (open it mybrain-first), then author stages. Parent: `2026-11-07-system-prompt-redesign.md (docs/plans/)` ruling P-budget.

## Rulings

| id | ruling | edit site |
|---|---|---|
| A1 | Cap follows the kind of ask: long asks (story, detail, "N sentences") get the long cap, others keep today's; no flat high cap. An LLM classify call is allowed if it is fast (D1-d). | `forkReplyCap` |
| A2 | The decision comes from wire-pod's own routing (knowledge graph or intent logic), not a word list bolted onto the request. | per D1 |
| A3 | short = `prompt.ReplyCap` today (Ollama 300 / Claude 1024); long = Ollama 1000 / Claude 2048 (pick at plan time; a live story ask validates). | `prompt.ReplyCap` |

## [USER DECISION] D1 — what decides "long"

| option | signal | edit site |
|---|---|---|
| D1-a | `isKG` (only `preqs/knowledgegraph.go` passes true: "I have a question") → long; intent-graph fallback → short | `forkAIReq(r, esn, isKG)` |
| D1-b | fork "long answer" phrase list (en + et) beside the other intent phrases → long, any wake path | intent matcher data + `forkAIReq` |
| D1-c | D1-a or D1-b | both |
| D1-d | one classify call (LONG/SHORT, max 4 tokens) to the answering model before the answer; measured median: qwen3.5:4b 193 ms 6/6, claude-haiku-5-5 thinking off 752 ms 6/6, gemma4:e2b-it-qat 139 ms 1/6 (prompt check owed) | `forkAIReq`; probe `robot/work/live-check/classify_speed.py` |
