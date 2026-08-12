# llamawizard — Spec

> **llamawizard** is a terminal wizard that takes a Mac from zero to a
> running, hardware-appropriate local LLM stack — pick the model whichllm
> says actually runs well on your machine, and llamawizard downloads it,
> builds llama.cpp, installs llama-swap, configures both, and sets them to
> auto-start, with a health check before it ever says "done."

Status: draft spec, ready for implementation
Language: Go (Bubble Tea + Bubbles + Lip Gloss)
Depends on: [`whichllm`](https://github.com/Andyyyy64/whichllm) (Python/uv) for hardware-aware model ranking
Serves via: llama.cpp + llama-swap (see §2 for why not llamactl)

---

## 1. Scope

### v1 — in scope
- Detect hardware (chip type, RAM, Metal support)
- Shell out to `whichllm --json` for the ranked model list, render the **same
  columns it does** (score, params, quant, size, est. tok/s, fit type) in a
  multi-select list
- Download the GGUF(s) the user picked
- Install/build llama.cpp (Homebrew binary if available, source build via
  cmake+Metal otherwise; guided manual steps if neither is possible)
- Install llama-swap (Homebrew tap; guided fallback)
- Generate `llama-swap.yaml` from the selected models
- Ask for a port, detect conflicts, offer backups
- Ask for an API key (default `dummy`, opt-in to set a real one)
- Install LaunchAgents for llama-swap (auto-start on boot/login)
- Run a post-install health check against `/v1/models` and fail loudly with
  logs if it doesn't pass
- `status` / `start` / `stop` / `restart` / `add-model` / `uninstall` as
  ongoing management commands, not just a one-shot installer

### v1 — explicitly out of scope (flag for later)
- Open WebUI install/config (your doc has this; add as `llamawizard ui install`
  once the core loop is solid)
- Windows support (architecture should stay OS-abstracted so this isn't a
  rewrite later, but don't build the Windows paths now)
- Continue / Qwen Code config file generation (nice follow-up, low effort
  once llama-swap is stable)
- Multi-machine central management (your roadmap's Phase 3)

---

## 2. Backend: llama-swap, not llamactl

[lordmathis/llamactl](https://github.com/lordmathis/llamactl) exists in the
same space and is worth naming explicitly so the boundary is clear: it's a
maintained instance manager with its own web dashboard, HF downloader,
multi-backend support (llama.cpp/MLX/vLLM), and auto-assigned ports/API
keys. It overlaps with the *serving* layer of this project, not the
*onboarding* layer.

**Decision: llamawizard targets llama-swap directly, per the original plan.**
Reasons:
- Your existing config, models, and mental model are already built around
  llama-swap — targeting it keeps this a drop-in upgrade of what you have,
  not a migration.
- llama-swap's config is a static YAML file llamawizard fully owns and
  generates; llamactl's instance model is a live API/DB-backed system
  designed to be driven from its own dashboard. Driving it from a second
  tool means either fighting its instance lifecycle or duplicating its
  state tracking — more integration surface than it saves.
- llamactl is still evolving (35 releases at time of writing) — coupling
  llamawizard to its API adds a second moving dependency to track breakage
  against, on top of whichllm and llama-swap itself.

**Not a closed door:** because §8's `llamaswap/` package is the only place
that knows about llama-swap's config format, a `backend/` abstraction could
swap in an llamactl driver later (create instance via its API instead of
writing YAML) without touching the wizard screens, download logic, or
whichllm integration at all. Worth keeping that seam in mind while building
§8, but not worth building now — YAGNI until there's a real reason (e.g. you
want MLX or vLLM backends, which llama-swap doesn't natively give you).

---

## 3. Why shell out to `whichllm` instead of reimplementing it

`whichllm` already does hardware detection (NVIDIA/AMD/Apple Silicon/CPU),
VRAM/RAM estimation, and benchmark-weighted ranking, refreshed against live
HF data with a 6h cache. Reimplementing that in Go is a lot of surface area
(benchmark merging, quant penalty curves, lineage-aware recency demotion) for
no real benefit — you'd just be maintaining a second copy of the same logic.

Treat it as an external data source:

```
whichllm --profile general --top 15 --json   # or coding/vision, per screen
```

Requirements this creates:
- `uv` must be present (or `python3` + `pip install whichllm` as fallback) —
  **this itself becomes an early dependency check/install step**, same
  pattern as the llama.cpp/llama-swap checks below.
- Prefer `uvx whichllm@latest --json` so you don't force a permanent install
  — but cache the fact that it worked once, so repeat runs don't re-resolve
  the environment every time (`uv` caches this itself, but worth confirming
  it's fast enough for a TUI wizard step; if not, fall back to `uv tool
  install whichllm` after first successful run).
- Parse the JSON schema: rows include `model_id`, `params`, `quant`,
  `estimated_tok_per_sec`, `fit_type`, `vram_required_bytes`,
  `benchmark_confidence`, etc. — display these as columns, don't invent your
  own scoring.
- `model_id` is a HF repo, not a resolved GGUF filename+quant pair 1:1 —
  you'll need a second step to resolve the actual file to download (see §6).

---

## 4. Wizard flow (first run)

```
1. Welcome screen
2. Dependency check
   ├── uv / python3           → offer to `brew install uv`
   ├── homebrew                → offer official install script if missing
   ├── xcode command line tools → `xcode-select --install` (GUI, can't be
   │                              silent — pause wizard, poll until done)
   ├── cmake, git               → `brew install cmake git`
   └── llama.cpp / llama-swap   → detect existing installs, skip if present
3. Hardware detection banner (chip, RAM, Metal yes/no) — sourced from
   `whichllm hardware` or your own `sysctl` call, either is fine here since
   it's just informational, not ranking logic
4. Model selection screen
   ├── run `whichllm --json` (spinner)
   ├── render table: same columns whichllm shows
   ├── multi-select (space to toggle, matches whichllm rows to checkboxes)
   └── confirm total download size + disk space check before proceeding
5. Download screen — per-model progress bars, resumable, checksum-verified
6. Build/install screen
   ├── llama.cpp: brew formula OR cmake build with live build log tail
   └── llama-swap: brew tap+install
7. Config generation
   ├── write ~/.local/ai/config/llama-swap.yaml from selected models
   └── show a diff/preview if a config already exists, don't silently clobber
8. Port screen
   ├── suggest 8080, check with a real bind attempt
   ├── if taken: show 3 free alternatives (8081, 8090, 8180 style spread,
   │   not just +1 sequentially, in case a range is congested)
   └── user picks or types their own, re-validated live
9. API key screen
   ├── default: dummy
   └── "Set a real key instead? [y/N]" → free-text entry, masked
10. LaunchAgent install
    ├── write plist, `launchctl bootstrap gui/$(id -u) <plist>`
    └── (modern launchctl syntax — avoid deprecated `load`, see §8)
11. Health check (mandatory, blocking)
    ├── poll http://127.0.0.1:<port>/v1/models, backoff up to ~30s
    ├── verify every selected model id appears in the response
    └── on failure: don't claim success — show the exact log tail and the
        log file path, offer retry
12. Done screen
    ├── desktop .command shortcut (optional toggle)
    └── summary: port, models installed, config path, log path
```

Re-running the wizard (or `llamawizard install`) should detect prior state and
offer **add/remove models** rather than starting over — see §9.

---

## 5. Port selection & conflict handling (your point 5)

- Bind-test, don't just `lsof` grep — actually attempt
  `net.Listen("tcp", "127.0.0.1:<port>")` and immediately close it. This
  catches processes that `lsof` might miss (e.g. something mid-bind) and
  avoids false negatives.
- Default suggestion: `8080` (matches your existing setup, so upgrades from
  the manual config don't silently move the port).
- On conflict, generate backups programmatically, not a hardcoded list:
  probe `8080, 8081, 8090, 8180, 8880` (spread, not sequential — sequential
  ports are more likely to also be taken if something grabbed a range) and
  present the first 3 free ones as buttons plus a "type your own" option.
- Store the chosen port in the state file so `add-model` / `restart` don't
  re-ask.

---

## 6. Model resolution & download (whichllm → actual file)

`whichllm`'s `model_id` + `quant` tell you *which* HF repo and quantization
tier, but you still need the exact filename to download. Steps:

1. Hit `https://huggingface.co/api/models/{model_id}` (no auth needed for
   public repos) to list files.
2. Filter for `.gguf` files matching the chosen quant string (e.g.
   `Q4_K_M`), plus any `mmproj*` file if the model is multimodal (Gemma 4 —
   your current setup needs this, don't drop it).
3. Download via plain HTTP GET with range-resume support — no need to shell
   out to the `hf` CLI (that's a Python dependency you don't otherwise need
   in the Go binary). Track progress by `Content-Length`.
4. Verify size matches what the HF API reported before marking complete;
   redo the download if truncated.
5. Disk space check *before* starting: sum selected file sizes, compare
   against free space on the target volume, refuse with a clear message if
   short (not a warning buried at the end).

Store models at the same layout your doc already uses —
`~/models/<slug>/<file>.gguf` — so this is a drop-in replacement for what
you already have, not a parallel structure.

---

## 7. llama.cpp — install or guide (your point 6: "if possible install, if not guide")

Order of attempts:

1. **Already built?** Check `~/.local/ai/llama.cpp/build/bin/llama-server` (or
   wherever state.json says it lives) — skip entirely if present and
   `--version` runs.
2. **Homebrew formula.** `brew install llama.cpp` gets you a binary without a
   local build — fastest path, prefer it if Homebrew is present and the
   formula is current enough (check `--version` supports the flags your
   config needs, e.g. `--jinja`).
3. **Source build.** If brew isn't available or the formula is stale:
   - Require: Xcode CLT (`xcode-select -p` check), cmake, git — all
     installable via brew if brew exists.
   - `git clone --depth 1 https://github.com/ggml-org/llama.cpp`
   - `cmake -B build -DGGML_METAL=ON` (Apple Silicon) or plain CPU build
     (Intel Mac, no Metal) — detect chip type first and branch the cmake
     flags accordingly, this is the Intel-vs-Apple-Silicon fork your
     original doc didn't fully separate.
   - `cmake --build build --config Release -j$(sysctl -n hw.ncpu)` — stream
     the build log into a scrollable pane so a 5+ minute compile doesn't
     look frozen.
4. **Can't do any of the above** (no brew, no Xcode CLT, user declines the
   GUI installer prompt): print the exact manual commands and stop that
   step cleanly — don't block the rest of the wizard if the user wants to
   come back and finish it by hand, but don't fake success either.

Xcode CLT is the one piece that **cannot** be silently automated — it pops
a native GUI installer. The wizard should kick it off, then poll
`xcode-select -p` every few seconds with a "waiting for Xcode tools
install..." spinner rather than blocking on a fixed sleep.

---

## 8. llama-swap — install + service management (your point 8: must be handled by the app)

Install:
```bash
brew tap mostlygeek/llama-swap && brew install llama-swap
```
Fallback if no brew: download the prebuilt binary from GitHub releases for
`darwin-arm64` / `darwin-amd64` based on detected chip, same idea as the
Windows table in your doc but for Mac binaries.

Config (`~/.local/ai/config/llama-swap.yaml`):
- Generate the `models:` block from the wizard's selections — mirror the
  structure you already have (cmd, `-ngl 999` only on Apple Silicon with
  Metal, `--ctx-size` sized sensibly against available RAM, `--jinja`).
- Set `apiKeys:` at the top level per your point 12:
  ```yaml
  apiKeys:
    - "dummy"   # or the user-supplied key
  ```
  This is llama-swap's real config field (confirmed from its schema) — the
  earlier assumption that it needed a custom auth layer was wrong, it's
  native.
- Never blind-overwrite an existing config: if one exists, diff it, show
  what would change, and confirm before writing.

Service (LaunchAgent):
- Generate the plist exactly like your existing template
  (`com.local.llama-swap.plist`), pointing at the *actual* resolved brew or
  built binary path (don't hardcode — this differs between Intel/Apple
  Silicon Homebrew prefixes, `/usr/local` vs `/opt/homebrew`).
- Load with `launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.local.llama-swap.plist`
  and `launchctl enable gui/$(id -u)/com.local.llama-swap` — the modern
  launchctl subcommands, since `load`/`unload` are deprecated on current
  macOS and can behave inconsistently.
- `llamawizard start` / `stop` / `restart` / `status` wrap `launchctl kickstart`,
  `launchctl kill`, and `launchctl print gui/$(id -u)/com.local.llama-swap`
  respectively — don't ask the user to remember launchctl syntax ever again.

---

## 9. State & idempotency

`~/.local/ai/state.json`:
```json
{
  "port": 8080,
  "api_key": "dummy",
  "chip": "apple-silicon",
  "llama_cpp_path": "/opt/homebrew/bin/llama-server",
  "llama_swap_path": "/opt/homebrew/bin/llama-swap",
  "models": [
    {
      "slug": "gemma4-26b",
      "hf_repo": "ggml-org/gemma-4-26B-A4B-it-GGUF",
      "quant": "Q4_K_M",
      "file": "gemma-4-26B-A4B-it-Q4_K_M.gguf",
      "mmproj": "mmproj-gemma-4-26B-A4B-it-bf16.gguf",
      "size_bytes": 18042093568,
      "installed_at": "2026-08-10T12:00:00Z"
    }
  ],
  "last_health_check": "2026-08-10T12:05:00Z"
}
```

This is what makes `llamawizard add-model`, `llamawizard status`, and re-running
the installer safe — every step checks state before acting instead of
re-doing work. It's also what an eventual `llamawizard uninstall` reads to know
exactly what it's responsible for removing (and what it should *leave
alone*, e.g. a user's own manually-added llama-swap models).

---

## 10. Health check (your point 13 — mandatory)

After every config write + service (re)start:

```
1. Poll GET http://127.0.0.1:<port>/v1/models
   - retry with backoff: 1s, 2s, 4s, 8s... cap ~30s total
2. Parse response, confirm every model id from state.json is present
3. Pass  → green confirmation screen, show curl example for the user to test
4. Fail  → red screen with:
   - last 20 lines of ~/.local/ai/logs/llama-swap-error.log
   - exact log file path
   - a "retry health check" action (in case it's just still loading a model)
   - do NOT print "installed successfully" anywhere above this if it fails
```

This should also be a standalone command — `llamawizard doctor` — runnable any
time, not just right after install, so it doubles as your ongoing
troubleshooting tool.

---

## 11. CLI surface

```
llamawizard                 # launches the wizard if unconfigured, else shows a dashboard
llamawizard install         # explicit wizard invocation
llamawizard models add      # whichllm-backed picker, appends to existing config
llamawizard models remove   # pick from installed models, removes + optionally deletes GGUF
llamawizard status          # is llama-swap running, which models configured, port, health
llamawizard start|stop|restart
llamawizard doctor           # health check, standalone
llamawizard logs             # tail llama-swap logs
llamawizard uninstall        # guided teardown, state-driven
```

---

## 12. Suggested Go project layout

```
llamawizard/
├── cmd/
│   └── llamawizard/main.go
├── internal/
│   ├── wizard/           # bubbletea models, one per screen in §4
│   ├── hardware/         # chip/RAM detection (thin, whichllm owns ranking)
│   ├── whichllm/         # exec wrapper + JSON schema structs
│   ├── download/         # HF file resolution + resumable download
│   ├── build/            # llama.cpp brew/source build logic
│   ├── llamaswap/        # install, config templating, launchd plist gen
│   ├── launchd/          # bootstrap/enable/kickstart/kill/print wrappers
│   ├── state/            # state.json read/write
│   └── health/           # doctor / post-install check
└── go.mod
```

Keeping `hardware/` thin and delegating ranking to `whichllm/` avoids the
trap of slowly reimplementing whichllm's scoring inside your own codebase.

---

## 13. Open questions worth resolving before you start coding

1. **`uv` as a hard dependency** — are you comfortable requiring `uv` on a
   fresh Mac just to shell out to `whichllm`? It's one extra brew install,
   but worth deciding now vs. vendoring a static list as a no-network
   fallback for the "corporate laptop blocks HuggingFace" case your original
   doc already flagged.
2. **Context size auto-sizing** — do you want the wizard to pick
   `--ctx-size` automatically based on remaining RAM after the model's
   weights, or always default to a fixed value (65536) like your current
   config and let the user override? Auto-sizing is nicer but is one more
   place estimates can be wrong.
3. **Multi-model RAM budget warning** — since llama-swap loads one model at
   a time, total disk isn't the constraint, but if two selected models both
   have huge `--ctx-size`, the *largest single* model's peak RAM is what
   matters. Worth a warning if the biggest selected model's estimated peak
   exceeds ~80% of total RAM, since macOS itself needs headroom.
4. **Signing/Gatekeeper** — a Go binary you distribute to yourself isn't an
   issue, but if this ever leaves your own machine (colleagues, per your
   original doc's ambition), unsigned binaries hit the same Gatekeeper
   friction you already flagged for the Phase 2 installer.

---

*Spec status: ready to start on §4–§10 (core wizard + install logic) for a
personal-use v1. §11 CLI dashboard and §12 layout can follow once the wizard
path works end-to-end on your own M5 Pro.*