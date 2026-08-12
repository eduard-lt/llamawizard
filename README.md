# llamawizard

```
██╗     ██╗      █████╗ ███╗   ███╗ █████╗ ██╗    ██╗██╗███████╗ █████╗ ██████╗ ██████╗ 
██║     ██║     ██╔══██╗████╗ ████║██╔══██╗██║    ██║██║╚══███╔╝██╔══██╗██╔══██╗██╔══██╗
██║     ██║     ███████║██╔████╔██║███████║██║ █╗ ██║██║  ███╔╝ ███████║██████╔╝██║  ██║
██║     ██║     ██╔══██║██║╚██╔╝██║██╔══██║██║███╗██║██║ ███╔╝  ██╔══██║██╔══██╗██║  ██║
███████╗███████╗██║  ██║██║ ╚═╝ ██║██║  ██║╚███╔███╔╝██║███████╗██║  ██║██║  ██║██████╔╝
╚══════╝╚══════╝╚═╝  ╚═╝╚═╝     ╚═╝╚═╝  ╚═╝ ╚══╝╚══╝ ╚═╝╚══════╝╚═╝  ╚═╝╚═╝  ╚═╝╚═════╝ 
```

```
                                                                                                    
                                                                                                    
                                                                                                    
                                                                                                    
                                                                                                    
                                                             +####%%@%%%%##                         
                                                         :*#%%%@%%%%@@@@@@@@@%                      
                                                       #*##@%@@%@%@@@@@@@@@%@@%#                    
                                                    **#@@@@@@@@@@%@@@@+   #@@@@%%+                  
                                                  .#*@@@@@@@@@@@@@@%           %%#=                 
                                                 ##@@@%@@@@@@@@@-                =#=                
                                                ##%@@@@%%%@@@@@@                                    
                                              +#%@@@@@@@%%#@@@@%                                    
                                             *#%@@@@@@@@%%@@@@@                                     
                                            ##%@@@@%@@@@@%@@@@.                                     
                                           ##%@@@@@@@@@@@@@@@                                       
                                          ##@%@@@@@@%@@@@@@@                                        
                                         ##%%@@@@@@@@@@@@@@%                                        
                                         #%%@@@@@@@@@@@@@@@@:                                       
                                        *%%@@@@@@@%@@@@@@@@@@                                       
                                       *%%%@@@-@@@@-%%@@@@@@@+                                      
                                       #%%@@@--@@@- @%%#@@%@@%                                      
                                      #%%@@@---@@*  @@@%%@%%@%                                      
                                     =%%%%@@-------*@@@@@@@@@%                                      
                                     %%#%@@@+---------@@@@@@@%                                      
                                    %#%#@@@-----@@@------@@%%%                                      
                                   .%##@@@%--------------@@@@%                                      
                                   %#%%@@@----=--------@+@@@@@.                                     
                                   #%%@@@@@-----@---@@%@@@@@@@%.                                    
                                  -%@@@@@@---------=@@@@@@@@@@@@.                                   
                                  *%@@@%@@@---------@@@@@@@@@@@@%                                   
                                 *%%@@@@@@+---------=@@%@@@@@@@@@                                   
                                +#@@@@@@@@-----------@@@@@%%%%%@@.                                  
                                #%%@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@#                                  
                                %#@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@%                                 
                               *%%%@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@%%                                 
                              ++++%@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@+++                                
                              ++++++++++++++++@@@@@@@@@@@@@@@@++++++                                
                              +++++++++++++++++++++++%@@@#++++++++++.                               
                             *+++++++++++++++++++++++++++++++++++++++                               
                            ++++++++++++++++++++++++++++++++++++++++++                              
                        *#*%%%+++++++++++++++++++++++++++++++++++++++%##                            
                    ****##%%%%@%%@@@@*+*++++++++*++*++++++#%%%%%%%%%%%%%%%%#                        
               **#*##%%%%%%%%%@@@@@@@@@@@%@@@@@@%%%%%%%%%%%%%%%%%%%%%%%%%%%%%%#:                    
             ###%%%%%%%@%%%%@@@@@@@@@@@@@@@@@@@@%%%%%%@%%%%@@%%%%%%%%%%%%%%%%%%%%%#*                
             #%%%%%%%@@@%@@@@@@@@@@@@@@@@@@@@@@%%%%%%@%%%%%%@@%%%%%%%%%%%%%####**#####              
               %%%@@%@%@%@@@@@@@@@@@@@@@@@@@@@@@%@%%@@%%%%%%@%@@@%%%%%##%#%%@%%%%.                  
                   %@@@@@@@%@@@@@@@@@@@@@@@@@@@@@@@@%%%%%%@%%%%#@@%%%%%%-                           
                       %%@@@@@@@@%@@@@@@@@@@@@@@@@@@@%###%%@@@@@%.                                  
                              *%%@%@@@@@@@@@@@@@@@@@@@@@@+                                          
```

A terminal wizard that takes a Mac from zero to a running, hardware-appropriate local LLM stack. It detects your hardware, picks the right model via [whichllm](https://github.com/Andyyyy64/whichllm), downloads it, builds llama.cpp (Metal-accelerated on Apple Silicon), installs and configures llama-swap, sets up a LaunchAgent for auto-start on boot, and runs a health check before it says "done."

After setup, it doubles as an ongoing management tool: start, stop, restart, status, health checks, log viewing, model add/remove, and guided uninstall.

## Requirements

- macOS (Apple Silicon or Intel)
- Go 1.26+ (to build from source)

The wizard also needs these tools and will install them for you when possible:

| Dependency | Purpose | Auto-installed? |
|------------|---------|:---:|
| Xcode Command Line Tools | `git`, `make`, C/C++ compiler | No — requires GUI popup |
| Homebrew | macOS package manager | No — must install manually |
| cmake | Build llama.cpp | Yes (via `brew install cmake`) |
| git | Source checkout | Yes (via `brew install git`) |
| uv | Run whichllm for model ranking | Yes (via `brew install uv`) |

### Setting up dependencies on a fresh Mac

If you're starting from a clean macOS install, run these in order:

**1. Install Xcode Command Line Tools**

```bash
xcode-select --install
```

A GUI dialog will appear. Click "Install", agree to the license, and wait for it to finish. Then verify:

```bash
xcode-select -p
```

**2. Install Homebrew**

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

**3. Install Go (if building from source)**

```bash
brew install go
```

Or download the installer from [go.dev](https://go.dev/dl/).

**4. Add Go tools to your PATH** (see install section below)

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

A Homebrew tap is planned but not yet available. For now, use `go install` or
manual build above.

The wizard walks through every step:
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

## Usage

### `llamawizard`

```
llamawizard
```

Launch the setup wizard. Starts the interactive TUI that walks through dependency checks, hardware detection, model selection, download, build, configuration, LaunchAgent install, and health verification.

### `llamawizard status`

```
llamawizard status
```

Reads `state.json`, queries launchd for the service state, and runs a health check against `/v1/models`. Prints installed models, port, API key, binary paths, and pass/fail status.

### `llamawizard start`

```
llamawizard start
```

Loads and starts the llama-swap LaunchAgent service. If the service is already bootstrapped, uses `kickstart`. Otherwise bootstraps from the plist first.

### `llamawizard stop`

```
llamawizard stop
```

Unloads the llama-swap service from launchd via `bootout`. The plist uses KeepAlive with `SuccessfulExit=false`, so `bootout` is the correct way to stop (plain `kill` would be restarted).

### `llamawizard restart`

```
llamawizard restart
```

Stops the service (errors ignored if already stopped) and starts it again.

### `llamawizard doctor`

```
llamawizard doctor
```

Standalone health check. Polls `http://127.0.0.1:<port>/v1/models` with exponential backoff (1s, 2s, 4s, 8s, 16s). Confirms every expected model ID is present. On failure, prints the last 20 lines of the llama-swap error log. Saves the result to `state.json`.

### `llamawizard logs`

```
llamawizard logs
```

Prints the last 30 lines of both `~/.local/ai/logs/llama-swap-error.log` and `llama-swap.log`. Points to `tail -f` for live following.

### `llamawizard models add`

```
llamawizard models add
```

Prompts to launch the TUI model selection screen to add a model interactively.

### `llamawizard models remove`

```
llamawizard models remove
```

Lists installed models, prompts for which to remove, deletes the entry from `state.json`, regenerates the llama-swap config, and restarts the service. Model files on disk are left untouched.

### `llamawizard uninstall`

```
llamawizard uninstall
```

Interactive teardown. Prompts for confirmation, then stops the service, removes the LaunchAgent plist, and deletes `state.json`. Model files and the config directory are left in place for manual cleanup.

### `llamawizard help`

```
llamawizard help
```

Prints a command reference.

## Architecture

```
cmd/llamawizard/main.go
        |
        v
internal/wizard/            Bubble Tea TUI (12 screens)
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
|---------|---------|
| `internal/hardware` | Chip type, RAM, Metal detection via sysctl |
| `internal/whichllm` | Exec wrapper around `uvx whichllm` for model ranking |
| `internal/download` | HuggingFace file resolution and resumable download |
| `internal/build` | Dependency detection, brew install, llama.cpp three-tier build |
| `internal/llamaswap` | llama-swap install, YAML config generation, safe write with diff |
| `internal/launchd` | Plist generation, modern launchctl wrappers (bootstrap/bootout/enable/kickstart/print) |
| `internal/health` | Doctor check: polls `/v1/models` with exponential backoff |
| `internal/state` | state.json read/write, model entry definitions |
| `internal/network` | Port probing via bind+close, alternative port suggestions |
| `internal/wizard` | Bubble Tea TUI with 12 screens orchestrating all packages |

## Files on disk

| Path | Purpose |
|------|---------|
| `~/.local/ai/state.json` | Source of truth: port, API key, chip, binary paths, installed models |
| `~/.local/ai/config/llama-swap.yaml` | llama-swap model configuration |
| `~/Library/LaunchAgents/com.local.llama-swap.plist` | LaunchAgent for auto-start |
| `~/.local/ai/logs/llama-swap.log` | llama-swap stdout log |
| `~/.local/ai/logs/llama-swap-error.log` | llama-swap stderr log |
| `~/models/<slug>/` | Downloaded model files |
| `~/.local/ai/llama.cpp/` | Source build tree (reused if present) |

## License

MIT
