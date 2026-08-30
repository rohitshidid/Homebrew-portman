# Roadmap

Where `portmap` is going. Current release: **v0.3.0**.

`portmap` answers "what is listening?" well. The next release makes it answer
the two questions people actually open a port tool to ask: **"what is on my
port, and how do I get rid of it?"** and **"which port can I use instead?"**

The design constraint stays the same: shell out to the tools that already know
about sockets, keep the dependency list empty, keep every command path behind
the `Runner` interface so it stays testable.

---

## Upcoming — v0.4.0

### 1. `portmap kill <port>`

The single most common reason anyone runs `lsof -i :3000`.

```text
$ portmap kill 3000
3000/tcp is held by node (pid 4821) — node dist/server.js
kill it? [y/N] y
sent SIGTERM to 4821 · port 3000 is now free
```

- [ ] confirm by default; `--yes` to skip, `--force` for SIGKILL
- [ ] refuse PID 1, and refuse processes owned by another user without `sudo`
      (say so rather than failing silently)
- [ ] when the owner is a container, do **not** kill the shim — print
      `docker stop <name>` instead
- [ ] exit `0` on freed, `1` on nothing listening, `2` on refused or failed
- [ ] `--dry-run`

### 2. `portmap free <port>`

Return the next available port from a starting point. Pairs with the existing
exit-code-driven `check` for scripts: `PORT=$(portmap free 3000)`.

- [ ] scan upward from the given port, print the first free one
- [ ] `--count N` to print several
- [ ] `--protocol` respected

### 3. Port ranges and lists

- [ ] `--port 3000-3010` and `--port 3000,8080,5432`
- [ ] `check` accepts several ports; exit `1` if **any** is occupied
- [ ] reuse `parsePortRange`, which already exists for the Docker parser

### 4. A `CMD` column

`app=node, pid=4821` does not tell you *which* node project. `node dist/server.js`
does.

- [ ] `--wide` adds the process command line
- [ ] always present in `--json`
- [ ] truncate to the terminal width in table mode

### 5. Watch-mode change highlighting

A watch view that redraws an identical table is much less useful than one that
shows the delta.

- [ ] mark rows `+` appeared / `-` disappeared since the previous frame
- [ ] hold a removed row for one extra frame so a blink is visible
- [ ] `--watch --json` streams NDJSON, one object per frame, instead of the
      current "not supported" error

### Also in v0.4.0

- [ ] **GoReleaser + GitHub Releases.** Prebuilt `darwin/linux` ×
      `amd64/arm64`, formula pointed at the tarballs, so `brew install` stops
      compiling from source.
- [ ] **Permission awareness.** On macOS `lsof` silently omits other users'
      processes. When a row has a port but no app or PID, print
      `some owners hidden — re-run with sudo` instead of looking broken.

---

## Later

- **Filter by owner.** `--app node`, `--container redis`, `--user postgres`.
  The reverse query ("what ports does node hold?") is as common as the forward
  one.
- **Colour output.** Dim the `-` placeholders, highlight wildcard `*` binds —
  a real signal that a port is exposed beyond localhost — and tint Docker rows.
  Respect `NO_COLOR` and non-TTY output.
- **`--sort port|app|container`** and `--csv`.
- **Podman and Compose awareness.** `podman ps` has the same `--format` shape;
  surface the Compose project/service label rather than a raw container name.
- **`portmap diff`.** Save the current listener set as a baseline, then report
  new listeners later — a light audit trail.
- **systemd / launchd unit attribution** for the owning PID.
- **Established connection counts** per listening port: who is actually
  connected, not just who is listening.
- **Shell completions** for zsh, bash and fish.

---

## Not planned

- Remote or network scanning. `portmap` describes *this* machine; pointing it
  at someone else's is a different tool with different ethics.
- A daemon. Every command is a one-shot scan, and `--watch` is a loop around
  that same scan.
- Bundling its own socket enumeration. `lsof`, `ss` and `netstat` are already
  installed, already correct, and already maintained.
