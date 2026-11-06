# Vector voice: verify on the robot (B5 of the voice research plan)

**Status**: ✅ SHIPPED — V1+V2 run 2026-11-06; V5 listening + loudness decision → `docs/backlog.md` §OWED[USER].
**Parent**: `2026-11-05-vector-voice-research.md` (docs/plans/) — rulings R1–R23 live there.

## Rulings (2026-11-05)

| id | ruling | edit site |
|---|---|---|
| V0 | Robot runs (V1, V2) start only on the user's explicit go | — |
| V1 | Loudness: PC voice within 3 dB of the same track's stock line = pass, else report the gap (R23: no boost headroom) | `robot/volume-probe/pcvoice.sh af_bella cori mari` (exit 1 on a gap) |
| V2 | First sound + fallback: all arms (mari as et-EE); T2 first sound ≤ 0.5 s over stock (R21); `refused`/`blackhole` speak Acapela | `RUNS=3 robot/voice-samples/onset.sh` |
| V3 | Before V1: the first run was cut off after T1 stock — confirm the voice file reads `af_bella` | `ssh … cat /data/data/myvector-voice.json` |
| V4 | After V1+V2: hand the user the V5 checklist with the measured numbers filled in, then archive this plan | this doc |

## V5 — user listening checklist (ranked)

| # | check on Vector | correct looks like | if wrong |
|---|---|---|---|
| 1 | Ask an LLM question with T1 = `af_bella`, then `cori`, then `stock` (page `/myvector/voice`) | soft female for both PC voices, about as loud as stock (stock plays without the robot-voice effect since patch 0010 — baseline moved) | V1 gap → decide in a new plan |
| 2 | "Hey Vector, what time is it?" with T2 = `af_bella` | soft female, same voice as T1 | `grep 'acapela' /var/log/messages` |
| 3 | Say "Hey Vector" mid PC sentence | the sentence stops | cancel `forkSayText` ctx from the KGSim interrupt |
| 4 | Long LLM answer | gaps between sentences acceptable | prefetch the next chunk in `voice.Say` |
| 5 | Real KG question | `bla bla` plays, in Acapela | `grep 'acapela: sdk' /var/log/messages` |
| 6 | `sudo systemctl stop myvector-tts`, ask again | stock voice speaks (R9/R18); restart the service after | — |
