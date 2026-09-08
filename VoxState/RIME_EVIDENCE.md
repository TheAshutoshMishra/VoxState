# Rime Evidence

This document records concrete, source-verified evidence of VoxState's
Rime TTS integration. Every value below was read directly from
`backend/internal/voice/rime/`, `backend/internal/config/config.go`,
`.env.example`, and `backend/cmd/server/main.go` — nothing here is
invented. No secret values (API keys) are included or were ever read;
this repository has no `.env` file and no Rime/LiveKit/Deepgram
credentials are configured in this development environment (confirmed:
`find . -iname ".env*"` returns only the two `.env.example` templates,
and no `RIME_*`/`LIVEKIT_*`/`DEEPGRAM_*` variables are set in the shell
environment).

## 1. Hard voice claim

VoxState is a **voice-first** product: the correctness guarantee it
exists to demonstrate (stale results must never be spoken) is only
meaningful if there is real spoken output to keep honest. The text-only
path (`internal/agent`, exercised via `POST /machines/{id}/agent/run`)
exists as a lower layer the voice path is built on, not as the intended
end-user experience — see `docs/ARCHITECTURE.md`'s voice-flow section
and `CLAUDE.md`'s M6/M7 milestone entries.

## 2. Why speech is essential to the product

An industrial technician diagnosing a machine is usually not free to
read a screen — hands on the equipment, hearing protection on, moving
between stations. The product's core risk (an agent confidently stating
something that stopped being true seconds ago) is specifically a
*spoken*-output risk: a technician who hears "vibration is normal" acts
on it immediately, with no chance to notice a stale timestamp the way a
dashboard viewer might. Voice is what makes the staleness bug dangerous,
and therefore what makes the fix (Policy gating before Rime is ever
called — see below) worth building.

## 3. Exact Rime model ID

`coda` — `defaultRimeModelID` in
`backend/internal/config/config.go:26`, overridable via `RIME_MODEL_ID`.
Comment in `internal/voice/rime/tts.go`: "`coda` (Rime's flagship model;
mistv3 is the low-latency alternative)".

## 4. Rime speaker

`astra` — `defaultRimeSpeaker` in `backend/internal/config/config.go:27`,
overridable via `RIME_SPEAKER`.

## 5. Language

**Not explicitly configured.** `rime.Config` has a `Lang` field (sent as
the request's `lang` field when non-empty), but `backend/cmd/server/main.go`'s
construction of `rime.Config{...}` (lines 75-80) never sets it, and
`internal/config.Config` has no `RimeLang`/`RIME_LANG` field or env var
at all. The outgoing JSON's `lang` key is `omitempty` and is therefore
never sent — Rime's own server-side default applies, whatever that is.
This is a genuine gap, not a documented default: earlier code comments
in this package used `"eng"` as an illustrative example value, but that
value was never wired into `Config` construction. Reported here exactly
as found, not as `"eng"`.

## 6. Rime endpoint

`https://users.rime.ai/v1/rime-tts` — `const endpoint` in
`backend/internal/voice/rime/tts.go`. `Config.Endpoint` can override this
(used by `rime`'s own tests to point at an `httptest.Server` instead of
the real network).

## 7. Audio format

Request: `audioFormat: "wav"`, `Accept: audio/wav` header — the client
always requests raw WAV bytes, never Rime's JSON-envelope
(`base64`-encoded `audioContent`) fallback shape; `Synthesize` treats
receiving that shape as an error rather than silently decoding it (see
`tts.go`'s doc comment on why: Rime's docs say the JSON envelope only
happens if `Accept` is missing/wrong).
Response: RIFF/WAVE container, PCM, parsed by a hand-rolled chunk scanner
(`parseWAV`) that reads the actual sample rate and bit depth from the
`fmt ` chunk rather than trusting the requested rate — `Synthesize`
rejects any bit depth other than 16.
Requested sample rate: `RimeSampleRateHz`, default **24000 Hz**
(`defaultRimeSampleRateHz`, `RIME_SAMPLE_RATE_HZ`) — sent explicitly on
every request because "Rime's own documentation gives inconsistent
implicit defaults across different pages" (observed 16000, 22050, and
24000 all described as "the default" on different doc pages during the
M6 research pass, per `tts.go`'s comment).

## 8. Audio transport

Rime's WAV/PCM16 output is decoded in-process by `parseWAV` into raw
`[]int16` samples, then handed to `voice.Transport.Send` (see
`session.go:289`), which is backed by `internal/voice/livekit.Transport`
in production. That wraps an `lkmedia.PCMLocalTrack`
(`server-sdk-go/v2/pkg/media`), which encodes the PCM16 samples to Opus
and publishes them as a real LiveKit WebRTC audio track — the browser/
participant hears standard LiveKit audio, not a raw file transfer. `Send`
returns an error if the sample rate handed to it doesn't match the
track's configured rate (`transport.go:106-107`), which is what makes
`Synthesize` reporting the WAV's *actual* rate (rather than trusting the
requested one) load-bearing rather than cosmetic.

## 9. Where Rime is used

Exactly one call site: `VoiceSession.handleUtterance` in
`backend/internal/voice/session.go:289`, called only after the agent's
tool-calling loop has produced a response that passed Policy — i.e.
Rime only ever synthesizes text that has already cleared the
version-fencing check (M4). There is no other TTS call site in the
codebase (confirmed: `grep -rn "tts\." internal/voice/session.go` finds
exactly this one call).

## 10. Acceptance test

`internal/voice/rime.TestSynthesize_SendsExplicitSamplingRateAndParsesWAV`
(`backend/internal/voice/rime/tts_test.go`) is the closest thing to a
Rime "acceptance test" that runs without a live API key: it stands up an
`httptest.Server` that returns a real WAV payload at a *different*
sample rate (22050) than requested (24000), and asserts (a) the request
explicitly stated `samplingRate: 24000`, `audioFormat: "wav"`,
`Accept: audio/wav`, and the correct `Authorization: Bearer <key>`
header, and (b) `Synthesize` returns the WAV's actual rate (22050), not
the requested one, plus the exact decoded PCM16 samples. This validates
the client's contract with Rime's documented API shape; it does not
validate against Rime's real, live service (see Limitations).

## 11. Test procedure

```bash
cd backend
go test ./internal/voice/rime/...           # -v for per-test output
```
All four tests in that package (`TestSynthesize_SendsExplicitSamplingRateAndParsesWAV`,
`TestSynthesize_NonOKStatusIsError`, `TestParseWAV_RejectsNonWAVPayload`,
`TestParseWAV_RejectsUnsupportedBitDepth`) run against an in-process
`httptest.Server`, not the real `users.rime.ai` endpoint.

## 12. Actual result

Executed in this environment on 2026-09-08:

```
$ go test -v ./internal/voice/rime/...
=== RUN   TestSynthesize_SendsExplicitSamplingRateAndParsesWAV
--- PASS: TestSynthesize_SendsExplicitSamplingRateAndParsesWAV (0.00s)
=== RUN   TestSynthesize_NonOKStatusIsError
--- PASS: TestSynthesize_NonOKStatusIsError (0.00s)
=== RUN   TestParseWAV_RejectsNonWAVPayload
--- PASS: TestParseWAV_RejectsNonWAVPayload (0.00s)
=== RUN   TestParseWAV_RejectsUnsupportedBitDepth
--- PASS: TestParseWAV_RejectsUnsupportedBitDepth (0.00s)
PASS
ok  	voxstate/backend/internal/voice/rime	0.002s
```

## 13. Limitations

- **No live Rime API call has ever been made in this environment.**
  There is no `RIME_API_KEY` configured, and this task's instructions
  prohibit inventing one. Everything above validates the client's
  request/response *contract* against a fake server, not Rime's actual
  production behavior, latency, voice quality, or auth failure modes.
- `cmd/server` (which wires the real `rime.NewTTS`) does not build in
  this environment at all — see the `libopus`/`pkg-config` limitation in
  `CLAUDE.md`'s "Current status" and the M9 final-verification section
  of the accompanying report — so even the client construction path was
  verified by reading `main.go`, not by running it.
- The `Lang` gap (§5) means language is currently whatever Rime defaults
  to server-side for the `coda`/`astra` combination; this has not been
  confirmed against Rime's actual behavior since no live call has been
  made.
- Real-world audio quality, latency-under-load, and rate-limiting
  behavior are unmeasured — see the benchmark report for what latency
  *was* measured (all in-process, fake-provider paths).

## 14. Environment/configuration example

From `.env.example` (template values only, no real secret ever present):

```
RIME_API_KEY=
RIME_MODEL_ID=coda
RIME_SPEAKER=astra
RIME_SAMPLE_RATE_HZ=24000
```

`RIME_API_KEY` must be set to a real key for `cmd/server`'s voice path to
authenticate against Rime; the other three have the working defaults
shown above if left unset.

## 15. Fallback behavior (explicitly disclosed)

**There is no fallback.** If `Synthesize` returns an error (bad/missing
API key, non-200 response, malformed WAV, unsupported bit depth,
network failure), `handleUtterance` propagates that error up through
`Session.Run`'s turn goroutine and the turn ends without ever calling
`Transport.Send` — no cached/canned audio, no silent text-only
degradation, no retry. This is a deliberate simplicity choice for a
hackathon-scope project (see `CLAUDE.md`), not a hidden resilience
feature: a Rime outage currently means that turn produces no spoken
response at all, and the failure is only visible via the returned error/
log line, not via any user-facing fallback message.
