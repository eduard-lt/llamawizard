# llamawizard — Architecture & Build Plan

> Companion to `llamawizard-spec.md`. That doc says *what* and *why*; this
> one breaks it into the smallest tasks that make sense to hand to a local
> model one at a time, in build order, with what each task needs as input
> and what it should produce.

---

## 0. How to use this doc with a local model

Each task below is scoped to be a single focused prompt: one file or one
small package, with a stated goal, inputs, and a done-condition you can
actually check. Suggested loop per task:

1. Paste the task block (goal + inputs + done-condition) to your coding
   model, plus any files it needs to see (previous package's exported
   types, mainly).
2. Build it in isolation — most tasks include a "test standalone" note so
   you're not debugging the whole wizard just to check if a port-probe
   function works.
3. Commit before moving to the next task. Small commits make it easy to
   tell which task introduced a regression, especially since you're
   swapping in different model outputs across the project.
4. Don't hand a model the whole spec at once — bounded context per task
   gets more reliable output from a 30B-class local model than "build the
   downloader" with no boundaries.

Rough sizing: 🟢 small (one sitting), 🟡 medium (needs a couple passes),
🔴 fiddly (test carefully, has real-world edge cases that are easy to miss).

---

## 1. Architecture overview

```
cmd/llamawizard/main.go
        │
        ▼
internal/wizard/            ← Bubble Tea screens, orchestrates everything below
        │
   ┌────┼─────────┬─────────────┬─────────────┬───────────┬──────────┐
   ▼    ▼         ▼             ▼             ▼           ▼          ▼
hardware whichllm download    build         llamaswap   launchd   state
(§ chip, (exec       (HF file  (llama.cpp    (config     (plist    (state.json
 RAM,     wrapper +   resolve   brew/source   templating  bootstrap read/write,
 Metal)   JSON        + resume  build)        + write)    /enable/  the source
          parsing)    + verify)                           kickstart/ of truth
                                                            kill/     every
                                                            print)    package
                                                                      reads/
                                                                      writes)
                                                                        │
                                                                        ▼
                                                                  internal/health/
                                                                  (post-install +
                                                                   `doctor` checks)
```

Design rule for all packages except `wizard/`: **no package imports
`bubbletea`.** Every non-UI package should be a plain Go library with
functions that return `(result, error)` — this is what lets you test
`download`, `build`, `llamaswap`, etc. from a throwaway `main.go` or unit
test without running the TUI at all, and it's what lets a local model work
on one package without needing to understand Bubble Tea's update loop.

`wizard/` is the only package that knows about screens, key handling, and
rendering. It calls into the other packages and turns their results into
UI state.

---

## 2. Task list, in build order

### Phase A — Foundations (no UI yet, pure logic + `go test`)

#### A1. 🟢 Project scaffold
**Goal:** Repo skeleton, `go.mod`, empty package dirs, a `main.go` that just
prints "llamawizard" and exits.
**Input:** none — this is the starting point.
**Done when:** `go build ./...` succeeds, `go run ./cmd/llamawizard` prints
the placeholder line.

#### A2. 🟢 `internal/state` — state.json read/write
**Goal:** Go structs matching the `state.json` schema in the spec (§9),
with `Load() (*State, error)` (returns zero-value state if file doesn't
exist yet — not an error) and `(*State) Save() error`.
**Input:** the JSON schema from spec §9.
**Done when:** a unit test writes a state, reloads it, and gets an
identical struct back. Handle the file-not-found case explicitly — that's
the "first run" signal the wizard uses.

#### A3. 🟡 `internal/hardware` — chip + RAM detection
**Goal:** `Detect() (HardwareInfo, error)` returning chip family
(apple-silicon / intel), total RAM in bytes, and Metal availability.
**Input:** `sysctl hw.memsize`, `sysctl machdep.cpu.brand_string` (or
`uname -m` for `arm64` vs `x86_64`) via `os/exec`.
**Done when:** running it on your M5 Pro reports Apple Silicon, correct RAM,
Metal available. Keep this deliberately thin — this is informational
display only, `whichllm` owns the actual ranking logic (spec §3).

#### A4. 🟡 `internal/whichllm` — exec wrapper + JSON parsing
**Goal:** `IsAvailable() bool` (checks for `uv` on PATH), `Rank(profile
string, top int) ([]ModelCandidate, error)` that shells out to
`uvx whichllm@latest --profile <profile> --top <n> --json` and unmarshals
the result into a `ModelCandidate` struct (model_id, params, quant, size,
estimated_tok_per_sec, fit_type, benchmark_confidence — match whichllm's
actual JSON field names, check its `--json` output directly rather than
guessing).
**Input:** run `uvx whichllm@latest --top 3 --json` yourself once first and
paste the real output as a fixture — don't let the model guess the schema.
**Done when:** a unit test feeds a saved JSON fixture through the parser
and gets the expected struct back; a manual run against the real command
also works.

#### A5. 🟡 `internal/download` — HF file resolution
**Goal:** `ResolveFiles(repo, quant string) ([]RemoteFile, error)` — hits
`https://huggingface.co/api/models/{repo}`, filters for `.gguf` files
matching the quant string plus any `mmproj*` file.
**Input:** spec §6. Test against a real repo you already use, e.g.
`ggml-org/gemma-4-26B-A4B-it-GGUF`.
**Done when:** it correctly returns both the main GGUF and the mmproj file
for a multimodal model, and just the one GGUF for a text-only model.

#### A6. 🔴 `internal/download` — resumable download + verify
**Goal:** `Download(file RemoteFile, destDir string, progress
chan<- Progress) error` — range-resume on retry, size check against the HF
API's reported size, writes to a `.partial` file and renames on success.
**Input:** output of A5.
**Done when:** killing the process mid-download and re-running resumes
rather than restarting (test this deliberately — it's the easiest part of
this task to get subtly wrong), and a completed file's size matches what
the API reported.

#### A7. 🟢 `internal/network` (small helper, could live in `wizard/` if you
prefer, but useful standalone) — port probing
**Goal:** `IsFree(port int) bool` via a real `net.Listen` bind+close (spec
§5 — not `lsof`), and `SuggestAlternatives(preferred int, count int) []int`
using the spread pattern from the spec (8080, 8081, 8090, 8180, 8880 style,
not purely sequential).
**Done when:** unit test occupies a port with a dummy listener, confirms
`IsFree` returns false for it and true for a genuinely open one.

---

### Phase B — System integration (still no UI; these touch the real machine, so test carefully)

#### B1. 🟡 `internal/build` — dependency detection
**Goal:** `CheckDeps() DepStatus` reporting presence of: Homebrew, Xcode
CLT (`xcode-select -p`), cmake, git, `uv`. Just detection, no
installation yet.
**Done when:** running it on your machine reports accurate status for
everything (temporarily uninstall/rename something on a throwaway VM or
just trust the `which`/`-p` checks if you don't want to risk your real
setup).

#### B2. 🟡 `internal/build` — dependency install (brew-based)
**Goal:** `InstallMissing(deps DepStatus) error` — runs `brew install
cmake git uv` for whatever B1 found missing. Handle "Homebrew itself is
missing" as its own branch that prints the official install command rather
than trying to pipe-to-shell it automatically (that one-liner curls a
script — deliberately don't automate blindly executing it without the user
seeing what's about to run).
**Input:** result of B1.
**Done when:** run on a machine missing `cmake`, confirm it gets installed
and `CheckDeps()` reports true afterward.

#### B3. 🔴 `internal/build` — llama.cpp: detect existing / brew / source build
**Goal:** `EnsureLlamaCpp(chip hardware.Chip) (binaryPath string, err
error)` implementing the three-tier fallback from spec §7: check existing
binary → try brew formula → source build with chip-appropriate cmake flags
(`-DGGML_METAL=ON` only for Apple Silicon).
**Input:** B1/B2 for deps, A3 for chip detection.
**Done when:** on a machine with nothing installed, this produces a working
`llama-server` binary and its path, streaming build output somewhere
readable (stdout is fine for this standalone test — wiring it into a
scrollable TUI pane is a `wizard/` task later).
**Note:** this is the single riskiest task in the whole project — an
interrupted or malformed cmake build can leave a half-built tree. Make this
idempotent: if `build/` exists but the binary doesn't, that's a "rebuild"
case, not a "someone tampered with this" case.

#### B4. 🟡 `internal/llamaswap` — install
**Goal:** `EnsureLlamaSwap() (binaryPath string, err error)` — brew tap +
install, with the GitHub-releases-binary fallback from spec §7 if brew
isn't available.
**Done when:** produces a working `llama-swap` binary path, `--version`
runs successfully.

#### B5. 🟡 `internal/llamaswap` — config generation
**Goal:** `GenerateConfig(models []state.ModelEntry, port int, apiKey
string, llamaServerPath string) (yamlBytes []byte, err error)` — templates
the YAML structure from your existing manual config (spec has the full
example), including `apiKeys:` at the top level.
**Input:** the exact YAML shape from `llamawizard-spec.md` §8 and your
current `llama-swap.yaml` (already in your uploaded doc) as the ground-truth
example to match.
**Done when:** generated YAML for a known model set diffs cleanly against
what you'd hand-write for the same models — literally compare it against
your existing file's structure for the models you already run.

#### B6. 🟡 `internal/llamaswap` — safe write (diff-before-overwrite)
**Goal:** `WriteConfig(path string, newContent []byte) (changed bool, diff
string, err error)` — if the file exists and differs, return the diff
without writing; caller (wizard) decides whether to confirm and call
`ForceWrite`.
**Done when:** first run on a nonexistent path writes cleanly; second run
with identical content reports no change; a run with different content
returns a diff and does *not* overwrite until told to.

#### B7. 🔴 `internal/launchd` — plist generation + lifecycle
**Goal:** wrap `launchctl bootstrap`, `enable`, `kickstart`, `kill`,
`print` (spec §7 — modern subcommands, not deprecated `load`/`unload`),
plus `WritePlist(binaryPath, configPath string, port int) (plistPath
string, err error)` templating the plist from your existing example.
**Done when:** `Install()` gets llama-swap running and surviving a reboot
(actually reboot and check, don't just trust `launchctl print` — this is
the step most likely to silently "work" until you close your laptop lid);
`Stop()`/`Start()`/`Status()` round-trip correctly.

#### B8. 🟢 `internal/health` — the doctor check
**Goal:** `Check(port int, expectedModels []string) (Report, error)` —
polls `/v1/models` with the backoff schedule from spec §9, confirms every
expected model id is present, returns a pass/fail report plus (on failure)
the tail of the llama-swap error log.
**Done when:** returns pass against a manually-started llama-swap instance,
and a useful failure report when you point it at a wrong port.

---

### Phase C — The wizard itself (this is where Bubble Tea comes in)

By this point every non-UI package is independently testable and (mostly)
tested. The wizard's job is now just: sequence these calls, render
progress, and handle user input — the "hard" logic is already done, which
is exactly why it's last.

#### C1. 🟢 Bubble Tea shell
**Goal:** a `wizard.Model` implementing `Init/Update/View`, with a single
static welcome screen and a way to advance to a "screen 2 placeholder."
Get the skeleton (state machine between screens) working before any real
screen has real content.
**Done when:** `go run ./cmd/llamawizard install` shows a screen, pressing
a key advances to the next.

#### C2. 🟡 Dependency check screen
Wraps B1/B2 with a checklist UI (spinner per item, ✓/✗ on completion).

#### C3. 🟢 Hardware banner screen
Wraps A3, purely informational, auto-advances after a beat or on keypress.

#### C4. 🟡 Model selection screen
Wraps A4 (whichllm), renders the ranked table, multi-select via Bubbles'
`list`/`table` component, shows running total download size against free
disk space (need a small disk-space check helper here too — `syscall.Statfs`
on the target volume).

#### C5. 🟡 Download screen
Wraps A5/A6, one progress bar per selected model (Bubbles' `progress`
component), sequential or a couple in parallel — sequential is simpler and
fine for v1, don't over-engineer concurrency here.

#### C6. 🟡 Build/install screen
Wraps B3/B4, scrollable log tail (Bubbles' `viewport`) so a multi-minute
cmake build doesn't look frozen.

#### C7. 🟢 Config generation screen
Wraps B5/B6, shows the diff if one exists, confirm-to-write prompt.

#### C8. 🟡 Port screen
Wraps A7, text input with live validation, shows alternatives as
selectable buttons if the default's taken.

#### C9. 🟢 API key screen
Default `dummy`, y/N to override, masked text input if yes.

#### C10. 🟡 LaunchAgent screen
Wraps B7, straightforward progress + result.

#### C11. 🟢 Health check screen
Wraps B8, mandatory blocking pass/fail, retry action on failure — spec §10
is explicit that this must never show a false "success."

#### C12. 🟢 Done screen
Summary (port, models, config path, log path), optional desktop `.command`
shortcut toggle.

---

### Phase D — Ongoing management commands (once the wizard path works end-to-end)

#### D1. 🟢 `llamawizard status`
Reads state.json + calls B7's `Status()` + B8's `Check()`, prints a summary.

#### D2. 🟡 `llamawizard models add` / `models remove`
Re-enters C4-style selection against existing state, calls B5/B6 to
regenerate config, B7 to restart the service, B8 to re-verify.

#### D3. 🟢 `llamawizard start` / `stop` / `restart`
Thin wrappers over B7.

#### D4. 🟢 `llamawizard doctor`
Standalone B8 invocation, runnable any time.

#### D5. 🟡 `llamawizard logs`
Tail the log file path from state.json.

#### D6. 🟡 `llamawizard uninstall`
State-driven teardown: stop + unload LaunchAgent, optionally delete
downloaded models (confirm first), leave anything not tracked in state.json
alone.

---

## 3. Suggested milestones (checkpoints, not deadlines)

1. **Milestone 1 — "it can tell me what to install."** A1–A5 done. Running
   a throwaway `main.go` shows your hardware and a whichllm-ranked model
   list. No downloading, no building yet.
2. **Milestone 2 — "it can get the bytes."** A6–A7 added. Can actually pull
   a model file to disk with resume working.
3. **Milestone 3 — "it can stand up the stack."** B1–B8 done. From a
   command line (still no TUI), you can go from nothing to a running,
   health-checked llama-swap instance by calling these packages in
   sequence from a temporary `main.go`. This is the milestone worth
   celebrating — everything after this is UI over working logic.
4. **Milestone 4 — "it's actually a wizard."** C1–C12 done, full guided
   flow works end to end on your own machine.
5. **Milestone 5 — "it's a tool, not a script I ran once."** D1–D6 done.

---

## 4. Notes for prompting your local Qwen models

- Feed each task's **Goal / Input / Done-when** block plus the relevant
  exported types from whichever earlier package it depends on — not the
  whole spec, not the whole codebase.
- For the 🔴 tasks (A6, B3, B7) especially: ask explicitly for error
  handling on partial/interrupted states, since that's exactly where a
  fast first draft tends to assume the happy path.
- Your Qwen3.6 35B (medium) tier is probably the right fit for most of
  these — they're bounded, well-specified package-level tasks. Save
  Qwen3 30B Q8 (heavy) for B3 and B7 specifically, since those two have
  the most real-world edge cases (interrupted builds, launchd's fussier
  subcommand behavior across macOS versions).
- Since none of the non-UI packages import `bubbletea`, you can validate
  every model-written package with a plain `go test` before it ever touches
  the wizard — use that as your acceptance gate before moving to the next
  task, not just "the code compiles."

---

*Companion doc to `llamawizard-spec.md`. Update task statuses as you go —
this is meant to be a working checklist, not a one-time plan.*