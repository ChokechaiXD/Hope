# Cortex operations

## Installed layout

```text
%LOCALAPPDATA%\Cortex\
├── bin\cortex.exe
├── config.json
├── cortex.db
└── backups\
```

Cortex accepts loopback listeners only; the default is `127.0.0.1:7777`.
Windows autostart is registered in
the current user's `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` key;
administrator permission is not required.

## Daily use

Open **Cortex Dashboard** from the Windows Start Menu. The shortcut checks
`/v1/health`, starts the installed service only when needed, and opens the
configured loopback URL. Use the dashboard for memory search/review,
Hermes-agent discovery, restart, and graceful stop.

The commands below remain operator diagnostics, not daily requirements:

```powershell
Invoke-RestMethod http://127.0.0.1:7777/v1/health
bin\cortex.exe service status
hermes memory status
```

`service status` reports autostart registration. The health endpoint above is
the authoritative runtime readiness check.

Open `http://127.0.0.1:7777/` directly only when diagnosing the shortcut.
Generate a new administrator login token when needed:

```powershell
bin\cortex.exe agent token --id mika
```

Only the printed token is sensitive. Cortex persists its SHA-256 hash.

## Add or refresh Hermes profiles

Press **Discover & connect agents** in the dashboard after creating a profile.
It uses the same transactional connector sync as the operator command:

```powershell
bin\cortex.exe connector sync hermes `
  --home "$env:LOCALAPPDATA\hermes"
```

Existing valid profile tokens are reused. New profiles receive a distinct
credential and the running Cortex process reloads it without restart. A newly
granted governor role requires restart because governance membership is loaded
when the hub opens.

The command prints `backup=...` before its profile list. Cortex creates that
timestamped snapshot before changing credentials or any Hermes profile. It
contains prior profile configs, connector files, and legacy Holographic
database files. If any profile fails, Cortex restores every changed profile and
its own credential config before returning an error.

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

Every connector sync creates a timestamped pre-migration snapshot under
`%LOCALAPPDATA%\Cortex\backups`. Keep the `backup=...` path printed by the
sync command.

1. Remove Cortex autostart with `bin\cortex.exe service uninstall`.
2. Stop the verified Cortex process.
3. Restore each profile's matching `hermes\<agent>\config.yaml` from the
   timestamped backup to its Hermes home.
4. Start a new Hermes session and run `hermes memory status`.

The original `memory_store.db`, WAL, and SHM files are never modified by the
importer, so rollback does not require converting Cortex data back to
Holographic.
