# Task-runner model profile for llama-swap

A dedicated llama-swap entry for plan-runner implementation agents. Same model
weights, memory-tuned server: smaller context, a single slot, cooler sampling.

Background: on 2026-08-19 a plan-runner task crashed the main profile's server
under memory pressure (4 parallel slots × 256K context pushed llama-server to
~36 GB on a 48 GB machine; the system OOM-killed it and pi lost its
connection). Agent work typically peaks around 64K tokens (~50% of 128K), so
128K gives 2× headroom.

## Add to llama-swap.yaml

```yaml
qwen3.8-27b-q4-k-xl-run:
  name: Qwen3.8-27B (runner)
  description: Q4_K_XL Qwen3.8-27B, task-runner profile: 128K ctx, 1 slot, temp 0.7
  cmd: |
      /opt/homebrew/bin/llama-server
      --host
      127.0.0.1
      --port
      ${PORT}
      --model /Users/eduard/models/qwen3.8-27b-q4-k-xl/Qwen3.8-27B-UD-Q4_K_XL.gguf
      --mmproj /Users/eduard/models/qwen3.8-27b-q4-k-xl/mmproj-BF16.gguf
      -ngl 999
      --ctx-size 131072
      --parallel 1
      -t 0.7
      -fa on
      --jinja
```

## What changed vs the main profile

| flag | main | runner | why |
|---|---|---|---|
| `--ctx-size` | 262144 | 131072 | agent tasks peak ~64K; 2× headroom, KV halved |
| `--parallel` | (4) | 1 | one conversation at a time; each slot reserves its own KV — biggest saving |
| `-t` | (1.0) | 0.7 | cooler, more deterministic code output |

`--mmproj` is kept so pi can still send images (its `read` tool attaches
them). Drop the line to save ~1 GB if you never do.

## RAM expectation

| profile | worst case |
|---|---|
| main (observed during the 2026-08-19 crash) | ~36 GB |
| runner (1 slot × 128K) | ~21 GB |

≈15 GB of headroom versus the crash.

## Usage

1. Add the entry to `llama-swap.yaml`, restart llama-swap.
2. Run plan-runner with it: start the orchestrator session with the runner
   model, or tell plan-runner "run the plan with
   `qwen3.8-27b-q4-k-xl-run`" — it passes the model to every spawned agent.
3. Optional check after a run:
   `curl -s localhost:<port>/slots | grep -o '"temperature":[0-9.]*'` — if pi
   sends its own temperature in the request, the server `-t` is only a
   default; the slots endpoint shows what actually applied.

Note: this does not change the main profile — regular interactive sessions
keep using it as-is.
