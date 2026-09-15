# Installation and Operations

## Updated first-run workflow

Run `videodl setup --destination "$HOME/Videos/videodl"`, then `videodl doctor`.
Setup creates a private configuration bundle with a guide, man page and Bash/Zsh/Fish
completions, preserving existing files. `videodl help COMMAND` shows command help,
`videodl man` prints the bundled guide. `bin/install.sh [PREFIX]` installs the binary,
manual and completions (default prefix: `~/.local`), without editing shell profiles.
See [configuration guide](../config/README.md) and [README](../README.md) for the
new transfer options, verified HTTP resume and upgrade procedure.

New configuration keys: `retries` (3, range 0–10), `resume` (true), `max_height`
(0 = best, range 0–8640), `idle_timeout` (60s). Matching flags: `--retries`,
`--resume=false`, `--max-height`, `--idle-timeout`. Their `VIDEODL_*` overrides
follow the existing precedence rules. Explicit transfer flags passed to `add` are
saved per job; explicit worker flags override those job settings.

Existing output files are never replaced. Recoverable direct-HTTP partials are
saved in `DESTINATION/.videodl/` and reused only with a strong ETag and valid range
response. HLS and FFmpeg restart from the beginning. Stop old daemons before
upgrading; older executables cannot read queues containing new per-job options.


## Requirements

- Go 1.23 or newer for a source build.
- `ffmpeg` for `--ffmpeg`, DASH `.mpd`, advanced HLS (separate audio, byte ranges, discontinuities), and MPEG-TS to MP4/MKV remuxing. The executable can be found automatically from `PATH`, or selected with `--ffmpeg-path`.
- A network-accessible HTTP(S) source that you are authorized to download.

The downloader does not provide site extraction, scraping, login handling, API/database/UI services, DRM bypass, or paywall bypass. It cannot download content that requires authentication unless the source is already publicly accessible to the HTTP client, and no authentication mechanism is provided by the CLI.

## Install

### Source build

From this repository:

```bash
go build -trimpath -o videodl ./cmd/videodl
./videodl --help
```

To place the binary in a user-local executable directory:

```bash
mkdir -p "$HOME/.local/bin"
install -m 0755 ./videodl "$HOME/.local/bin/videodl"
videodl --help
```

The repository does not publish a separate release archive. A binary install therefore means installing a binary built from this source tree.

### Verify the checkout

```bash
go test -race -shuffle=on -count=1 ./...
go build -trimpath -o /tmp/videodl-check ./cmd/videodl
```

## Global first-run setup

For normal interactive use, run `videodl setup` or create the default global config once and avoid repeating `--config`:

```bash
videodl config init
videodl config set destination "$HOME/Videos/videodl"
videodl config show
```

`videodl config init` writes `$XDG_CONFIG_HOME/videodl/config.json`, or `~/.config/videodl/config.json` when `XDG_CONFIG_HOME` is unset. The default queue, state, and log files live under `$XDG_STATE_HOME/videodl`, or `~/.local/state/videodl` when `XDG_STATE_HOME` is unset. The built-in download destination is `$HOME/Downloads/videodl`; `config set destination PATH` stores a cleaned absolute path and creates the directory.

Useful config commands:

```bash
videodl config path
videodl config get destination
videodl config set auto_start_worker true
videodl config edit
```

`config edit` opens `$VISUAL`, then `$EDITOR`, then the configured `editor`, then `nano`, then `vi`. `videodl config edit --editor nvim` overrides the editor for one invocation. If the editor exits non-zero or leaves invalid JSON, the previous config bytes are restored.

## Configuration

The optional configuration file is strict JSON. Unknown keys, malformed JSON, and multiple JSON values are errors. In addition to the new keys listed above, the allowed keys are:

| Key | Meaning | Default |
| --- | --- | --- |
| `state_path` | Persistent queue JSON file | `$XDG_STATE_HOME/videodl/queue.json`, or `~/.local/state/videodl/queue.json` |
| `log_path` | Directory for `events.jsonl`, outcome logs, and webhook error logs | `$XDG_STATE_HOME/videodl/logs`, or `~/.local/state/videodl/logs` |
| `destination` | Download directory | `$HOME/Downloads/videodl` |
| `concurrency` | Maximum simultaneous downloads | `1` |
| `timeout` | HTTP connection/header timeout as a Go duration | `30s` |
| `ffmpeg` | Force ffmpeg for queued downloads | `false` |
| `ffmpeg_path` | Explicit ffmpeg executable path | automatic `PATH` lookup |
| `webhook_url` | Observability webhook endpoint | disabled |
| `editor` | Preferred editor for `videodl config edit` after `$VISUAL` and `$EDITOR` | fallback to `nano`, then `vi` |
| `auto_start_worker` | Start the daemon after `videodl add` when no matching daemon is running | `false` |
| `daemon_pid_path` | PID file for the background worker daemon | `$XDG_STATE_HOME/videodl/daemon.pid`, or `~/.local/state/videodl/daemon.pid` |
| `daemon_log_path` | stdout/stderr log file for the detached daemon process | `$XDG_STATE_HOME/videodl/logs/daemon.log`, or `~/.local/state/videodl/logs/daemon.log` |
| `notify_command` | Optional direct-exec notification command for succeeded/failed jobs | disabled |

`concurrency` must be an integer from `1` through `8`. Absolute paths are safest for global config because they work from any launch directory. If a relative path is supplied by a flag or hand-edited config, it is resolved from the command working directory before use. The program creates the destination and log directories when needed.

To start from [`../config.example.json`](../config.example.json) instead of `config init`, copy it and replace `/home/you` with your home directory:

```bash
mkdir -p "$HOME/.config/videodl"
cp config.example.json "$HOME/.config/videodl/config.json"
```

The path is selected in this order:

1. `--config PATH`.
2. `VIDEODL_CONFIG`.
3. `$XDG_CONFIG_HOME/videodl/config.json`, or `~/.config/videodl/config.json`.

If the default file is absent, defaults are used. A file selected explicitly by `--config` or `VIDEODL_CONFIG` must exist and be valid.

For values inside the selected file, precedence is:

1. Built-in defaults.
2. JSON configuration file.
3. Environment variables.
4. Command-line flags.

Supported environment overrides are `VIDEODL_STATE_PATH`, `VIDEODL_LOG_PATH`, `VIDEODL_DESTINATION`, `VIDEODL_CONCURRENCY`, `VIDEODL_TIMEOUT`, `VIDEODL_FFMPEG`, `VIDEODL_FFMPEG_PATH`, `VIDEODL_WEBHOOK_URL`, `VIDEODL_EDITOR`, `VIDEODL_AUTO_START_WORKER`, `VIDEODL_DAEMON_PID_PATH`, `VIDEODL_DAEMON_LOG_PATH`, and `VIDEODL_NOTIFY_COMMAND`. `VIDEODL_CONFIG` selects the file and does not replace the other overrides. Command-line `--state`, `--log`, `--destination`, `--concurrency`, `--timeout`, `--ffmpeg`, `--ffmpeg-path`, and `--webhook` take priority over those environment values where the command supports them. The `--config` flag selects the file; it is not a JSON setting.

## Queue operations

All queue commands accept the shared flags shown by their help output, including `--config`, `--state`, `--log`, `--destination`, `--concurrency`, `--timeout`, `--ffmpeg-path`, `--ffmpeg`, `--webhook`, and `--json` where applicable. Only `add` uses `--name` and `--output` to choose a safe output file name; `--output` is an alias there. Other queue commands do not use those flags for output selection. A name must be a single file name, not a path. The examples below use fixed, safe names inside the configured destination.

### Add without downloading

```bash
videodl add --name example.mp4 'https://example.com/video.mp4'
```

`add` validates the HTTP(S) URL, creates a queued job, and returns its job ID. It does not fetch the URL. If no name is supplied, the final URL path component is used where possible, otherwise `video.bin` is used. When `auto_start_worker` is `true`, `add` starts the daemon after the job is persisted unless a matching daemon is already running.

For machine-readable output:

```bash
videodl add --json --name example.mp4 'https://example.com/video.mp4'
```

### Run queued work

```bash
videodl worker
videodl worker --watch
```

`worker` drains the jobs currently in the queue and then exits. `worker --watch` keeps the foreground worker alive and processes jobs added later until cancelled. Continuous mode keeps running after individual job failures, while storage errors remain fatal. Both modes use concurrency `1` by default and accept `--concurrency 1` through `--concurrency 8`; the flag overrides configuration. For example:

```bash
videodl worker --concurrency 3 --timeout 30s
```

The worker recovers stale running jobs after 24 hours and processes queued jobs only. Stop it with Ctrl-C; an interrupted job is requeued when cancellation reaches the worker.

### Background daemon lifecycle

```bash
videodl daemon start
videodl daemon status
videodl daemon logs
videodl daemon restart
videodl daemon stop
```

`daemon start` launches the current `videodl` executable as a detached worker-watch process using the configured queue, destination, PID, and daemon log paths. It refuses to start a duplicate matching daemon. `daemon status` reports `stopped`, `running`, or `stale`. `daemon logs` prints the configured daemon log path and any current log contents. `daemon stop` signals only the PID recorded in the daemon PID file and refuses a live PID that is not the current `videodl` executable.

Use `worker` for a one-shot foreground drain, `worker --watch` for a continuous foreground worker you can supervise yourself, and `daemon start` for a background worker managed by `videodl daemon ...` commands.

### Watch queue status

```bash
videodl watch --once
videodl watch --json --once
videodl watch --interval 5s
```

`videodl watch` displays daemon status, active/running counts, queued/completed/failed/canceled totals, all jobs, and recent completed/failed events. `--once` prints one report and exits, which is useful for scripts and examples. Without `--once`, the command refreshes repeatedly until cancelled. `--json` emits the same report as newline-delimited JSON objects.

### Inspect and transition jobs

```bash
videodl list
videodl list --json
videodl status JOB_ID
videodl retry JOB_ID
videodl cancel JOB_ID
```

Jobs can be `queued`, `running`, `completed`, `failed`, or `canceled`. `retry` requeues a retryable job; `cancel` cancels a queued or otherwise cancellable job according to the queue state rules. Both commands print the resulting job status. Use `--json` when another program needs the job record.

## Direct legacy mode

When the first argument is not a queue command, the CLI uses direct mode. It accepts exactly one URL and requires a destination file:

```bash
videodl --output video.mp4 'https://example.com/video.mp4'
videodl -o stream.mp4 'https://example.com/master.m3u8'
videodl --ffmpeg --output result.mp4 'https://example.com/manifest.mpd'
```

The default network timeout is `30s` for dialing, the TLS handshake, and HTTP response headers. It does not limit the response body, so a download may run longer than `30s`. Use `--timeout 2m` to change those connection and header limits. Direct mode does not write a queue job, and its `--output` value is a path rather than the safe queue file name. It now loads transfer settings from the configuration too. The independent `idle_timeout` also bounds stalled response-body reads.

## Progress and media behavior

- Direct HTTP downloads report reliable bytes written when the server supplies a content length. If the total is unknown, the byte count is real but no exact percentage can be calculated.
- Public, unencrypted HLS playlists report reliable bytes written and segment position when segment totals are known. Encrypted HLS is rejected by the built-in downloader.
- DASH `.mpd` sources automatically use `ffmpeg`; `--ffmpeg` forces the same path for other sources. `--ffmpeg-path PATH` selects the executable.
- The `ffmpeg` path reports lifecycle start and completion callbacks; the completion byte count is the final output file size, not a transfer percentage.
- Successful output is written atomically where the downloader supports it; an interrupted or failed operation must not be treated as a completed download merely because a temporary file existed.

## Webhooks and logs

Pass a webhook endpoint per invocation:

```bash
videodl worker --webhook 'https://hooks.example.test/videodl'
```

The endpoint receives JSON `POST` observability events with `Content-Type: application/json`. URLs and error text are sanitized before they are emitted, including removal of user information, query strings, and fragments. Webhook delivery has a five-second timeout.

Configure a local notification command for completed and failed jobs:

```bash
videodl config set notify_command 'notify-send videodl {state} {name}'
```

`notify_command` is optional. The command string is parsed into an executable and arguments, placeholders are replaced, and the executable is run directly with `exec`, not through a shell. Shell features such as pipes, redirects, globbing, and environment-variable expansion are not interpreted unless you deliberately make a shell the executable. Supported placeholders are `{job_id}`, `{state}`, `{name}`, `{output_path}`, `{url}`, and `{error}`.

Notification delivery failures are logged and non-fatal. They do not change job state, and they do not make a successful download fail.

Logger initialization happens during command startup, so an unavailable log directory or event log can fail the command before queue work begins. After startup, the CLI ignores errors returned while emitting runtime event logs or writing webhook-error records. Webhook delivery is best effort: a timeout, connection failure, or non-2xx response is recorded in `webhook-errors.jsonl` when that record can be written, but does not fail the download or change its queue status.

The event log is `<log_path>/events.jsonl`; successful and failed terminal events are also written to `<log_path>/success.jsonl` and `<log_path>/error.jsonl`. Webhook delivery failures are recorded in `<log_path>/webhook-errors.jsonl`; notification delivery failures are recorded in `<log_path>/notify-errors.jsonl`. The detached daemon writes stdout/stderr to `daemon_log_path`. The log directory is created with restricted permissions by the program.

## Shell completion

Generate completion scripts from the installed `videodl` binary:

```bash
videodl completion bash > /tmp/videodl.bash
videodl completion zsh > /tmp/_videodl
videodl completion fish > /tmp/videodl.fish
```

Install for Bash:

```bash
mkdir -p "$HOME/.local/share/bash-completion/completions"
videodl completion bash > "$HOME/.local/share/bash-completion/completions/videodl"
```

Install for Zsh by writing to a directory on `fpath`:

```zsh
mkdir -p "$HOME/.zfunc"
videodl completion zsh > "$HOME/.zfunc/_videodl"
fpath=("$HOME/.zfunc" $fpath)
autoload -Uz compinit
compinit
```

Install for Fish:

```fish
mkdir -p ~/.config/fish/completions
videodl completion fish > ~/.config/fish/completions/videodl.fish
```

Completions cover top-level commands, common flags, config subcommands and keys, daemon subcommands, completion shells, and job IDs for `status`, `retry`, and `cancel` when the configured queue can be read.

## Troubleshooting

### `ffmpeg` is missing

Install `ffmpeg` using the operating system's package manager, verify `ffmpeg` is on `PATH`, or provide an explicit path:

```bash
videodl worker --ffmpeg --ffmpeg-path /usr/local/bin/ffmpeg
```

### Configuration is rejected

Validate that the file is one JSON object with only the documented configuration keys. Check that concurrency is between `1` and `8`, timeout is a positive duration, and paths are non-empty. Use `videodl config path` to see the default selected file, or `--config /absolute/path/config.json` for a script/custom profile that intentionally uses a separate file.

### A job is not downloaded by `add`

This is expected: `add` only persists the job. Run `worker` for a one-shot drain, `worker --watch` for foreground continuous processing, or `daemon start` for background processing. Use `list`, `status JOB_ID`, and `watch --once` to inspect state.

### Daemon status is stale or locked

Run `videodl daemon status` first. If the PID file points to a dead process, `videodl daemon start` or `videodl daemon stop` can recover the stale PID file. If the PID belongs to a live process that is not the current `videodl` executable, the CLI refuses to overwrite or stop it; inspect that process with operating-system tools before taking any action, and do not kill an unrelated process just because its PID appears in a stale file.

If a queue command reports that the queue is locked, another `videodl worker`, `videodl worker --watch`, or daemon may be using the queue. Stop the known videodl daemon with `videodl daemon stop`, cancel your foreground worker with Ctrl-C, or wait for the active command to finish. Do not delete queue, state, log, lock, or PID files unless you have verified no videodl process is using them and you have backed up anything you need to keep.

### Progress has no percentage

This is expected when an HTTP content length or a usable segment total is unavailable, and during ffmpeg processing. The reported bytes and segment fields are intentionally not fabricated into an exact percentage.

### A source is rejected

Only `http://` and `https://` URLs are accepted. The built-in HLS path supports public, unencrypted playlists. The tool does not authenticate, scrape sites, bypass DRM, bypass paywalls, or defeat access controls. Use a source and destination for which you have permission.

## Legal and safety limits

Use `videodl` only for content you are authorized to download and for destinations you control. Do not use it to bypass DRM, authentication, paywalls, copyright restrictions, or other access controls. The project makes no claim that arbitrary websites are supported, and it does not promise exact progress percentages for unknown-length or ffmpeg-managed transfers.
