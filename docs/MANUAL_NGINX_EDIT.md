# Manual nginx.conf Editing

sslly-nginx supports two workflows for nginx.conf:

1. **Managed mode** (default): `proxy.yaml` / `cors.yaml` / SSL changes trigger automatic config regeneration and reload.
2. **Manual edit mode**: Edit either nginx.conf location; sslly-nginx detects the change, syncs both files, and reloads nginx.

Both workflows coexist. When a managed reload runs after a manual edit, the manually edited file is backed up before being overwritten.

---

## File paths

| Path | Role |
|------|------|
| `/etc/nginx/nginx.conf` | The file nginx actually reads. |
| `./configs/.sslly-runtime/current/nginx/nginx.conf` | Runtime cache copy — kept in sync with `/etc/nginx/nginx.conf`. |
| `./configs/.sslly-runtime/current/certs/` | Stable cert paths referenced inside nginx.conf. Managed by sslly-nginx; do not edit manually. |

Both nginx.conf locations are equivalent for manual editing. Editing either one has the same effect.

---

## Behavior

### Dual-file watching

sslly-nginx watches both nginx.conf locations simultaneously:

- `/etc/nginx/nginx.conf`
- `./configs/.sslly-runtime/current/nginx/nginx.conf`

When a manual edit is detected on either file:

1. The edited content is copied to the other file (sync).
2. `nginx -t` is run to validate the config.
3. If valid: nginx is reloaded (SIGHUP).
4. If invalid: an error is logged; the previous good config is restored to both files and nginx is reloaded.

Changes written by sslly-nginx itself are ignored — only external edits trigger this path.

### Loop prevention

Writing to either file sets a `suppressUntil = now + 2s` timestamp. Both watchers ignore events that arrive within this window, preventing a sync write from triggering a second reload.

### Re-registration after snapshot activation

`./configs/.sslly-runtime/current/` is replaced atomically on every managed reload via `os.Rename`. After each activation, the runtime nginx.conf watcher is re-registered against the new file inode. The `/etc/nginx/nginx.conf` watcher is unaffected (stable path).

### Backing up manually edited nginx.conf

When a managed reload runs and the current `/etc/nginx/nginx.conf` differs from the last generated config, sslly-nginx backs up the manually edited file before overwriting it.

Backup location:
```
./configs/.sslly-backups/manual-nginx-<timestamp>.conf
```

---

## Implementation

### Files changed

| File | Change |
|------|--------|
| `internal/app/app.go` | Add `runtimeNginxWatcher *fsnotify.Watcher`, `suppressNginxWatchUntil time.Time`, `suppressNginxMu sync.Mutex`; stop watcher in `Stop()` |
| `internal/app/watchers.go` | Add watcher for `/etc/nginx/nginx.conf`; add `reRegisterRuntimeNginxWatcher()`; add `handleNginxConfEdit(src, dst)` for bidirectional sync |
| `internal/app/reload.go` | Set suppress timestamp before writing either file; call `reRegisterRuntimeNginxWatcher()` after `activateRuntimeSnapshot()`; back up manual edits before overwrite |
