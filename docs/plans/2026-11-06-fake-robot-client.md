# Plan: fakebot — stream an utterance into chipper gRPC as the robot does

Status: 📝 DRAFT · 2026-11-06 · sibling of the text→intent route probe (not this plan)

## Contract (ruled from recon)
| id | ruling | edit site |
|---|---|---|
| C1 | RPC = `StreamingIntentGraph`, not `StreamingIntent`: WireOS vic-cloud `process.go` always takes `WithIntentGraphOptions` for voice; KG mode out of scope | `fakebot/main.go` |
| C2 | Auth: none to supply. `grpcServe` builds hugh `grpcserver` with `WithInsecureSkipVerify` and no interceptors; listener is `tls.Listen` without `ClientAuth` → no client cert, no token check (robot's token metadata goes unread). Client sends no metadata | `fakebot/main.go` |
| C3 | TLS: `credentials.NewTLS(&tls.Config{InsecureSkipVerify: true})` (wire-pod's self-signed cert); `-addr` default `127.0.0.1:443` (`APIConfig.Server.Port` default); never read `apiConfig.json` | `fakebot/main.go` |
| C4 | First message = vic-cloud's fields: `DeviceId`=ESN, `Session`=`uuid.New().String()[:16]`, `LanguageCode_ENGLISH_US`, `AudioEncoding_LINEAR_PCM`, `RobotMode_VOICE_COMMAND`, `InputAudio`=chunk 0 (server ignores `LangString`; the fork lang file picks STT) | `fakebot/main.go` |
| C5 | Server sniffs Opus by `InputAudio[0]==0x4f` (`OpusDetect`), not `AudioEncoding` → always prepend 100 ms zeros | `fakebot/main.go` |
| C6 | Chunks 3200 B (100 ms, proto comment) at real-time pace; append 1 s zeros so `DetectEndOfSpeech` (`inactiveNumMax`) fires before `CloseSend` | `fakebot/main.go` |
| C7 | Print each `IntentGraphResponse` until EOF: `intent <Action> <Parameters>` / `kg "<SpokenText>"`, plus `QueryText`; exit 0 got one, 1 none / `-want` mismatch, 2 SETUP | `fakebot/main.go` |
| C8 | ESN: `-esn`, default `robots[0].esn` of `jdocs/botSdkInfo.json` (`BOTINFO` override, as voiceprobe); never in tracked files | `botinfo` (Stage 1) |
| C9 | Input: `-pcm file` (16 kHz s16le mono, e.g. miccap `.pcm`) or `-say text` → POST `$TTS_URL/tts` (default `http://127.0.0.1:8091`) `{"text","voice":"af_bella","lang":"en-US"}`; PCM only, Opus out of scope | `fakebot/main.go` |

## Refactor-first assessment
| id | ruling | edit site |
|---|---|---|
| R1 | Extract voiceprobe's `botInfo` + read into `chipper/pkg/myvector/botinfo` (`Robot{Esn, IPAddress, GUID}`, `Path()` honours `BOTINFO`, `Load(path) (Robot, error)` errs on zero robots); voiceprobe calls it, behaviour unchanged | `voiceprobe/main.go`, new `botinfo/botinfo.go` |

## Stage 1 — `botinfo` extraction (no behaviour change)
- Focus: new `botinfo/botinfo.go`, `botinfo/botinfo_test.go`; `voiceprobe/main.go` (`connect` uses `botinfo.Load`).
- Test: `cd ~/projects/my-vector/chipper && go test ./pkg/myvector/botinfo/ && go vet ./pkg/myvector/voiceprobe/`
- Falsifier: `{"robots":[]}` case expects an error — dropping the zero-robots check fails it.

## Stage 2 — `fakebot -pcm`: stream, print, `-want`
- Focus: new `chipper/pkg/myvector/fakebot/main.go` (`run(ctx, client, esn, pcm) ([]*pb.IntentGraphResponse, error)` split from flag parsing), `main_test.go`.
- Test: `bufconn` fake `ChipperGrpcServer` records requests, replies `IntentGraphMode_INTENT` `Action:"intent_clock_time"`. `cd ~/projects/my-vector/chipper && go test ./pkg/myvector/fakebot/`
- Asserts: first req carries C4 fields; every chunk ≤3200 B; reassembled audio = 100 ms zeros + input + 1 s zeros; `-want intent_clock_time` → 0, `-want x` → 1.
- Falsifier: input PCM starting `0x4f` → asserts first-req `InputAudio[0]!=0x4f`; removing the C5 pad fails it.

## Stage 3 — `fakebot -say` via the fork TTS
- Focus: `fakebot/main.go` (`synth(url, text) ([]byte, error)`), `main_test.go`.
- Test: `httptest` server asserts the C9 JSON body, returns PCM → streamed bytes match. Same command as Stage 2.
- Falsifier: TTS returns 500 → `synth` errors and main maps it to exit 2; returning the body anyway fails the test.

## Stage 4 — gate, live e2e, review
1. `cd ~/projects/my-vector && make check` (picks up `./pkg/myvector/...`).
2. Preconditions: wire-pod up (user's Windows shortcut — ask), TTS `:8091/health` OK, fork lang `en-US`.
3. Live (highest impact first):
   1. `cd ~/projects/my-vector/chipper && go run ./pkg/myvector/fakebot -say "what time is it" -want intent_clock_time` → exit 0; `journalctl -u wire-pod | grep 'Intent Sent: intent_clock_time'` (C2 proven: no token sent).
   2. `-pcm` on a miccap `.pcm` (`robot/work/et-stt/mic/`) → exit 0, `QueryText` matches its `.txt`.
   3. Falsifier: `-say "what is the capital of France" -want intent_clock_time` → exit 1 (LLM path; robot speaks via SDK — ask first, volume stays 1).
4. `Agent(subagent_type="code-reviewer")` over the Stage 1–3 commits.

## Rulings
| id | ruling | edit site |
|---|---|---|
| U1 | Robot may speak in e2e 3.iii at volume 1 (user, 2026-11-06) | Stage 4 |
| U2 | e2e runs with miccap off (capture dir absent); 3.ii skips when `robot/work/et-stt/mic/` has no `.pcm` | Stage 4 |
