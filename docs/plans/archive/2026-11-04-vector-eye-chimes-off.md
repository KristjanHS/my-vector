# Vector eye-movement chimes off

**Goal**: no chime when his eyes move while idle; the wake chime and spoken answers stay loud at `master_volume=4`.
**Status**: ✅ SHIPPED (committed as `0009-eye-chimes-off.patch`) — ship + listen check in `docs/backlog.md` §OWED[USER].

## Rulings

| id | ruling | edit site |
|---|---|---|
| E1 | Mute, never lower: Behavior RTPC 0.1 quietened the chimes live, but they came back loud after "what time is it?" | — |
| E2 | Source: the audio track of `anim_keepalive_eyesonly_loop_01` (ObservingOnCharger); the `kEnableKeepAliveEye*AudioEvents` vars are already false | `cozmoAudioController.cpp` |
| E3 | The eyesonly `.bin` posts `Scrn_Curious_Short`, `Scrn_Curious`, `Blink` (IDs from `Victor_SFX.txt`) | — |
| E4 | User scope: mute ALL `Play__Robot_Vic_Sfx__Scrn_*` + `Play__Robot_Vic_Sfx__Blink` everywhere at post time (`kMuteEyeChimes`), never `Wake_Word_*` or TTS → `0009-eye-chimes-off.patch` | `cozmoAudioController.cpp` |

## Checklist

- [x] E3: eye events named.
- [x] E4: patch + marker in `robot/firmware/markers.tsv`; `build-v.sh` green, `check-static.sh --built` OK.
- [ ] `[USER]` after `make ship`: no eye chimes before and after "what time is it?"; chime + answer loud.
- [ ] Falsifier: `SECS=30 RESET=Procedural robot/volume-probe/idle.sh base:1.0` spanning an eyesonly loop → `ticks>-45 0`.
