# Cortex operations

## Installed layout

```text
%LOCALAPPDATA%\Cortex\
├── bin\cortex.exe
├── config.json
├── cortex.db
└── backups\
```

Cortex listens only on `127.0.0.1:7777`. Windows autostart is registered in
the current user's `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` key;
administrator permission is not required.

## Daily checks

```powershell
Invoke-RestMethod http://127.0.0.1:7777/v1/health
bin\cortex.exe service status
hermes memory status
```

Open `http://127.0.0.1:7777/` for the review queue and per-memory event history.
Generate a new administrator login token when needed:

```powershell
bin\cortex.exe agent token --id mika
```

Only the printed token is sensitive. Cortex persists its SHA-256 hash.

## Add or refresh Hermes profiles

Run connector sync after creating a profile:

```powershell
bin\cortex.exe connector sync hermes `
  --home "$env:LOCALAPPDATA\hermes"
```

Existing valid profile tokens are reused. New profiles receive a distinct
credential and the running Cortex process reloads it without restart. A newly
granted governor role requires restart because governance membership is loaded
when the hub opens.

## Holographic migration

The importer opens Holographic SQLite in read-only/query-only mode. Imported
records stay candidates until reviewed.

```powershell
bin\cortex.exe import holographic `
  --database "$env:LOCALAPPDATA\hermes\memory_store.db" `
  --agent mika
```

Repeating an unchanged import is safe and reports the records as replayed.

## Update the installed binary

Stop the currently running `cortex.exe` after verifying its path points inside
`%LOCALAPPDATA%\Cortex\bin`, then run:

```powershell
bin\cortex.exe service install
bin\cortex.exe service start
```

The registry entry is replaced atomically and continues to launch Cortex in a
hidden detached process at the next sign-in.

## Rollback to Holographic

Every activated Hermes profile receives `config.yaml.cortex.bak`. A timestamped
pre-migration snapshot is also stored under `%LOCALAPPDATA%\Cortex\backups`.

1. Remove Cortex autostart with `bin\cortex.exe service uninstall`.
2. Stop the verified Cortex process.
3. Restore each profile's `config.yaml.cortex.bak` to `config.yaml`, or restore
   the matching file from the timestamped backup.
4. Start a new Hermes session and run `hermes memory status`.

The original `memory_store.db`, WAL, and SHM files are never modified by the
importer, so rollback does not require converting Cortex data back to
Holographic.
