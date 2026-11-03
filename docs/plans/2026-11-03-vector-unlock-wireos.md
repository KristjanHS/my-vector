# Vector unlock + WireOS: stay on the charger and still answer "Hey Vector"

**Goal**: Vector lives on the charger, still answers "Hey Vector", and the server holds nothing
(`AUTO_HOLD=off`). The way there: unlock the production bot, install WireOS, then patch the firmware's
own stay-on-charger behavior.
**Status**: not started. Every step on the robot is `[USER]`: it runs on the robot or in a browser, and
the unlock is permanent.

## Facts (from web research 2026-11-03; unverified until a slice confirms them)

- The robot is a Vector 1.0 on `2.0.1.6076ep`. WireOS is built on the same 2.0.1.6076 base.
- In the behavior tree (`globalInterruptions.json`), any SDK hold, `RESERVE_CONTROL` included, ranks
  above `TriggerWordDetected`. So the server hold built in 2026-11-03-vector-auto-hold.md keeps him deaf.
- `StayOnChargerUntilCharged` ranks below `TriggerWordDetected`. It exits on battery Full or after the
  `kSafeguardTimeout_s` console var (30 min).
- Unlock: `unlock-prod.ota`, run from recovery. Hold the button 15 s while he's on the charger, then go to
  websetup.skittle.dev → Utility → Unlock-Prod.ota. It takes about 7–10 min and is permanent. Root SSH uses
  a public global key.
- ⚠ Losing power during the unlock or the OTA can brick him permanently. With this battery he must not
  leave the charger at any point.
- WireOS install: websetup's Custom Firmware stack, or `ota-start http://ota.pvic.xyz/vic/latest/dev.ota`
  over SSH. It adds the `wired` web UI on :8080. Boot slots are A/B, so a bad OTA falls back.
- Sources: unlock-prod.skittle.dev · github.com/kercre123/unlocking-vector · github.com/os-vector/wire-os ·
  github.com/os-vector/wire-os-victor · github.com/os-vector/vector-docs

## Open questions (resolve before the slice that needs them)

| id | question | resolved by |
|---|---|---|
| Q1 | Does `unlock-prod.ota` run from `ep` firmware? | the unlock-prod.skittle.dev page / Discord; slice 1 itself |
| Q2 | Does WireOS still connect to this fork (Escape Pod mode, :443)? | WireOS docs; "what time is it" after slice 2 |
| Q3 | Can `kSafeguardTimeout_s` be changed at runtime (console :8888 on dev builds), or only with a rebuild? | slice 3a probe |
| Q4 | Does WireOS DevDoNothing (`ddn on`) still answer the wake word? | slice 3a probe |

## Slices (each visible; gate before each one)

0. **Back up and stage** `[USER]`. Write down the ESN and the fork's bot-setup state. Keep Vector on the
   charger, and the charger on a power strip nobody will switch off. Done when Q1 has an answer.
1. **Unlock** `[USER]`. Recovery (hold the button 15 s on the charger) → websetup.skittle.dev → Utility
   → Unlock-Prod.ota. Done when `ssh root@<vector-ip>` works with the global key.
2. **Install WireOS** `[USER]`. Custom Firmware stack, or `ota-start` over SSH. Re-run the fork's Bot
   Setup if he doesn't reconnect. Done when :8080 `wired` loads and "Hey Vector, what time is it?" is
   answered by the fork (Q2).
3. **Stay on the charger without a hold.** Try in this order, and stop at the first that works:
   a. *Runtime knob*: raise `kSafeguardTimeout_s`, or `ddn on` (Q3/Q4). No rebuild needed.
   b. *Config edit over SSH*: patch the behavior JSON (`stayOnChargerUntilCharged.json` / HighLevelAI) so
      it never exits, and drop the leave-charger transitions. Keep a copy of every file before you edit it.
   c. *Rebuild*: `wire-os-victor` with the C++ patch, deployed via `deploy-v.sh` (needs Docker on a Linux box).
   Done when a full day on the charger passes with no drive-off and "Hey Vector" works throughout, with
   `AUTO_HOLD=off`.
4. **Retire the server hold.** Set `AUTO_HOLD=off` in `chipper/source.sh` once 3 is done.

## Rollback

- Before slice 1: stop. Nothing has changed.
- After slice 1: the unlock is permanent. The bot keeps working on `ep` and the fork as before.
- After slices 2/3: A/B slots fall back after a bad OTA. A config edit is undone by restoring the saved
  file over SSH. If he won't stay put, set `AUTO_HOLD=override` again.

## Alternative that makes this plan optional

Replace the battery. The original is a FullRiver 320 mAh 3.7 V LiPo, soldered on a 1.0. iFixit guide:
ifixit.com/Guide/Anki+Vector++Battery+Replacement/123279.
