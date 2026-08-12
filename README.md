# llamawizard

```
██╗     ██╗      █████╗ ███╗   ███╗ █████╗ ██╗    ██╗██╗███████╗ █████╗ ██████╗ ██████╗ 
██║     ██║     ██╔══██╗████╗ ████║██╔══██╗██║    ██║██║╚══███╔╝██╔══██╗██╔══██╗██╔══██╗
██║     ██║     ███████║██╔████╔██║███████║██║ █╗ ██║██║  ███╔╝ ███████║██████╔╝██║  ██║
██║     ██║     ██╔══██║██║╚██╔╝██║██╔══██║██║███╗██║██║ ███╔╝  ██╔══██║██╔══██╗██║  ██║
███████╗███████╗██║  ██║██║ ╚═╝ ██║██║  ██║╚███╔███╔╝██║███████╗██║  ██║██║  ██║██████╔╝
╚══════╝╚══════╝╚═╝  ╚═╝╚═╝     ╚═╝╚═╝  ╚═╝ ╚══╝╚══╝ ╚═╝╚══════╝╚═╝  ╚═╝╚═╝  ╚═╝╚═════╝ 
```

A terminal wizard that takes a Mac from zero to a running, hardware-appropriate local LLM stack. It detects your hardware, picks the right model via [whichllm](https://github.com/Andyyyy64/whichllm), downloads it, builds llama.cpp (Metal-accelerated on Apple Silicon), installs and configures llama-swap, sets up a LaunchAgent for auto-start on boot, and runs a health check before it says "done."

After setup, it doubles as an ongoing management tool: start, stop, restart, status, health checks, log viewing, and model management.

**Status:** actively developed. Commands and file layout may still change between versions.

## Table of contents

- [Quick start](#quick-start)
- [Requirements](#requirements)
- [Setting up dependencies on a fresh Mac](#setting-up-dependencies-on-a-fresh-mac)
- [Install](#install)
- [What the wizard does](#what-the-wizard-does)
- [Commands](#commands)
- [Architecture](#architecture)
- [Files on disk](#files-on-disk)
- [Troubleshooting](#troubleshooting)
- [License](#license)

## Quick start

Already have Go, Homebrew, and Xcode Command Line Tools set up?

```bash
go install github.com/eduard-lt/llamawizard/cmd/llamawizard@latest
export PATH="$HOME/go/bin:$PATH"   # add to ~/.zshrc if not already there
llamawizard
```

That launches the setup wizard. If you're on a clean Mac with none of the prerequisites installed, see [Setting up dependencies on a fresh Mac](#setting-up-dependencies-on-a-fresh-mac) first.

## Requirements

- macOS (Apple Silicon or Intel)
- Go 1.26+ (to build from source)

On Apple Silicon, llamawizard builds llama.cpp from source with Metal acceleration. On Intel Macs, it falls back to the Homebrew formula (no Metal build) — expect slower inference.

The wizard also needs these tools and will install them for you when possible:

| Dependency | Purpose | Auto-installed? |
| ------------ | --------- | :---: |
| Xcode Command Line Tools | `git`, `make`, C/C++ compiler | No — requires GUI popup |
| Homebrew | macOS package manager | No — must install manually |
| cmake | Build llama.cpp | Yes (via `brew install cmake`) |
| git | Source checkout | Yes (via `brew install git`) |
| uv | Run whichllm for model ranking | Yes (via `brew install uv`) |
| pi | A minimal agent harness - optional. | Yes (via `curl -fsSL <https://pi.dev/install.sh> \| sh`) |

## Setting up dependencies on a fresh Mac

If you're starting from a clean macOS install, run these in order.

### 1. Install Xcode Command Line Tools

```bash
xcode-select --install
```

A GUI dialog will appear. Click "Install", agree to the license, and wait for it to finish. Then verify:

```bash
xcode-select -p
```

### 2. Install Homebrew

```bash
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
```

Follow the prompts. After it finishes, Homebrew will print instructions to add it to your PATH. Follow those — typically:

```bash
echo 'eval "$(/opt/homebrew/bin/brew shellenv)"' >> ~/.zprofile
eval "$(/opt/homebrew/bin/brew shellenv)"
```

Verify it's working:

```bash
brew --version
```

### 3. Install Go (if building from source)

```bash
brew install go
```

Or download the installer from [go.dev](https://go.dev/dl/).

**4. Add Go tools to your PATH** — see [Install](#install) below.

Once these are done, `llamawizard` can handle the rest — it will automatically install cmake, git, and uv if they're missing.

### Faster downloads (optional)

Set a HuggingFace token to get authenticated download speeds:

```bash
export HF_TOKEN=hf_xxxxxxxxxxxxxxxxxxxxxxxxxx
```

Or use the equivalent `HUGGINGFACE_HUB_TOKEN`. Without a token, HuggingFace throttles anonymous downloads. [Create a token here](https://huggingface.co/settings/tokens).

## Install

### go install (requires Go 1.26+)

```bash
go install github.com/eduard-lt/llamawizard/cmd/llamawizard@latest
```

Puts `llamawizard` in `$GOPATH/bin`. If you haven't already, add it to your PATH:

**bash** — add this to `~/.bashrc` or `~/.bash_profile`:

```bash
export PATH="$HOME/go/bin:$PATH"
```

**zsh** (macOS default) — add this to `~/.zshrc`:

```zsh
export PATH="$HOME/go/bin:$PATH"
```

Then reload your shell config (`source ~/.zshrc` or `source ~/.bashrc`) or open a new terminal.

Verify it's installed correctly:

```bash
llamawizard --version
```

### Manual build

```bash
git clone https://github.com/eduard-lt/llamawizard.git
cd llamawizard
go build -o llamawizard ./cmd/llamawizard/
./llamawizard
```

### Homebrew tap (coming soon)

A Homebrew tap is planned but not yet available. For now, use `go install` or manual build above.

## What the wizard does

Running `llamawizard` with no arguments walks through every step:

1. Dependency check (Homebrew, Xcode CLT, cmake, git, uv)
2. Hardware detection (chip, RAM, Metal)
3. Model selection via whichllm's ranked recommendations
4. Download with progress and resume support
5. llama.cpp source build (Metal on Apple Silicon) or Homebrew formula
6. llama-swap install and configuration
7. Port selection with conflict detection
8. API key setup (default: `dummy`)
9. LaunchAgent install for auto-start on boot
10. Health check against `/v1/models`

## Commands

| Command | Description |
| --- | --- |
| `llamawizard` | Launch the setup wizard |
| `llamawizard status` | Show service status and health |
| `llamawizard start` | Start the llama-swap service |
| `llamawizard stop` | Stop the llama-swap service |
| `llamawizard restart` | Restart the llama-swap service |
| `llamawizard doctor` | Run a standalone health check |
| `llamawizard logs` | Show recent service logs |
| `llamawizard models add` | Add a model (interactive) |
| `llamawizard models add --link <url> [--name <name>]` | Add a model from a direct download URL |
| `llamawizard models remove` | Remove a model **from config only** — file stays on disk |
| `llamawizard models delete` | Remove a model from config **and delete its file** |
| `llamawizard uninstall` | Stop service and remove LaunchAgent |
| `llamawizard update` | Check for and install updates |
| `llamawizard version` | Show version |
| `llamawizard help` | Show a command reference |

Notes on commands whose behavior isn't obvious from the name alone:

- **`stop`** unloads the service from launchd via `bootout`, not a plain `kill`. The LaunchAgent plist uses `KeepAlive` with `SuccessfulExit=false`, so a raw kill would just get the process restarted — `bootout` is the correct way to actually stop it.
- **`doctor`** polls `http://127.0.0.1:<port>/v1/models` with exponential backoff (1s, 2s, 4s, 8s, 16s) and confirms every expected model ID is present. On failure it prints the last 20 lines of the llama-swap error log, and saves the result to `state.json`.
- **`models remove`** deletes the entry from `state.json`, regenerates the llama-swap config, and restarts the service — but leaves the model file untouched on disk. Use **`models delete`** if you also want the file gone.
- **`uninstall`** is interactive and asks for confirmation before stopping the service, removing the LaunchAgent plist, and deleting `state.json`. Model files and the config directory are left in place for manual cleanup.
- **`logs`** prints the last 30 lines of both `llama-swap.log` and `llama-swap-error.log`; use `tail -f` yourself for live following.

## Architecture

```
cmd/llamawizard/main.go
        |
        v
internal/wizard/            Bubble Tea TUI, multiple screens
        |
   -----+---------+------------+------------+--------+--------+
   v    v         v            v            v        v        v
hardware  whichllm  download   build       llamaswap launchd  state
(chip,   (exec     (HF file   (llama.cpp   (config   (plist   (state.json
 RAM,     wrapper   resolve    brew/source  templating bootstrap read/write)
 Metal)   + JSON    + resume   build)       + write)  /enable/
          parsing)  + verify)                        kickstart/
                                                       kill/
                                                       print)
                                                         |
                                                         v
                                                   health/
                                                   (post-install +
                                                    doctor checks)
```

### Packages

| Package | Purpose |
| --------- | --------- |
| `internal/hardware` | Chip type, RAM, Metal detection via sysctl |
| `internal/whichllm` | Exec wrapper around `uvx whichllm` for model ranking |
| `internal/download` | HuggingFace file resolution and resumable download |
| `internal/build` | Dependency detection, brew install, llama.cpp three-tier build |
| `internal/llamaswap` | llama-swap install, YAML config generation, safe write with diff |
| `internal/launchd` | Plist generation, modern launchctl wrappers (bootstrap/bootout/enable/kickstart/print) |
| `internal/health` | Doctor check: polls `/v1/models` with exponential backoff |
| `internal/state` | state.json read/write, model entry definitions |
| `internal/network` | Port probing via bind+close, alternative port suggestions |
| `internal/wizard` | Bubble Tea TUI orchestrating all packages |

## Files on disk

| Path | Purpose |
| ------ | --------- |
| `~/.local/ai/state.json` | Source of truth: port, API key, chip, binary paths, installed models |
| `~/.local/ai/config/llama-swap.yaml` | llama-swap model configuration |
| `~/Library/LaunchAgents/com.local.llama-swap.plist` | LaunchAgent for auto-start |
| `~/.local/ai/logs/llama-swap.log` | llama-swap stdout log |
| `~/.local/ai/logs/llama-swap-error.log` | llama-swap stderr log |
| `~/models/<slug>/` | Downloaded model files |
| `~/.local/ai/llama.cpp/` | Source build tree (reused if present) |

## Troubleshooting

**Config YAML looks corrupted after hand-editing (bad indentation, missing fields)**
Avoid editing `llama-swap.yaml` directly in an editor that reflows indentation (e.g. nano with auto-indent). Regenerate the config through `llamawizard models add`/`remove`/`delete` instead, or restore from a backup and reapply changes carefully.

**`doctor` reports a model missing after a config change**
Run `llamawizard restart` after any manual config edit — llama-swap does not hot-reload changes made outside the wizard's own write path.

## License

MIT — see [LICENSE](LICENSE).

---

If llamawizard is useful to you, consider starring the repository.

[![Buy Me A Coffee](https://shields.io/badge/kofi-Buy_a_coffee-ff5f5f?logo=ko-fi&style=for-the-badge)](https://ko-fi.com/eduardolteanu)
