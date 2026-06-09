# Releasing a Windows build

The Windows binary is pure Go (no cgo), so it cross-compiles from Linux/macOS
and runs as a single self-contained `.exe` — the recipient needs no runtime.

## Build it

```
make tools            # one-time: installs goversioninfo (+ lint tools)
make release-windows  # builds 99dps.exe + 99dps.exe.sha256
```

`release-windows` does the standard hygiene automatically:

- **`-trimpath`** — strips local filesystem paths (`/home/<you>/...`) from the
  binary. (Without it, ~47 such strings leak in.)
- **Version metadata** (`versioninfo.json` → `resource_windows_amd64.syso`) —
  ProductName / version / description show up in Explorer → Properties → Details
  and Task Manager, instead of a blank, anonymous exe. A metadata-less binary
  also looks more suspicious to AV heuristics. Bump the version in
  `versioninfo.json` per release.
- **SHA-256** (`99dps.exe.sha256`) — so the recipient can verify the download
  wasn't tampered with.

Run `make lint-windows` to lint the build-tagged Windows code (`make lint` runs
against the host and skips it).

## Code signing — deliberately skipped

Authenticode signing is *the* standard Windows thing, and we **don't** do it,
on purpose:

- A trusted code-signing cert costs money (~$200–400/yr OV; EV more) from a CA.
- **Self-signed certs do not help** — SmartScreen still warns, because the cert
  isn't chained to a trusted CA. So self-signing is pure effort for no benefit.
- Even a fresh paid OV cert carries no SmartScreen reputation until it has been
  downloaded enough; only EV certs get instant reputation.

For a hobby tool this isn't worth it. Instead we mitigate:

- The recipient clicks **More info → Run anyway** on the SmartScreen prompt
  (expected for any unsigned indie binary).
- Ship the **SHA-256** so they can verify integrity.
- **Scan the exe on https://www.virustotal.com** before sending — catches
  heuristic AV false positives (Go binaries occasionally trip them); if a
  scanner flags it, you can submit a false-positive report to that vendor.

If this ever graduates to a wide release, the upgrade path is: buy an OV/EV
cert, sign with `signtool sign /fd sha256 /tr <timestamp-url> ...` as a final
step after `make release-windows`.

## Optional polish (not wired yet)

- **Icon** — add an `.ico` and reference it from `versioninfo.json`
  (`"IconPath": "icon.ico"`); goversioninfo embeds it so the exe gets a real
  icon in Explorer.

## Sending it

`.exe` attachments are blocked by most email providers — send via Discord /
Google Drive / WeTransfer, or zip it first. Include the `.sha256`. Tell the
recipient to enable EQ logging (`/log on` in game) or the meter has nothing to
read.
