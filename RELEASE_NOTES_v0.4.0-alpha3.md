# v0.4.0-alpha3

Alpha3 replaces the production Windows PowerShell GUI path with a Go desktop
runtime and introduces protocol v2 operation envelopes.

Highlights:

- `acbh-desktop-windows-amd64.exe` is now the default desktop entry.
- Production Windows bundles no longer include `acbh-desktop-gui.ps1`.
- Added Go Operation Manager with mutex classes, deadlines, cancellation,
  progress, single terminal results, and debug log redaction.
- Added Coordinator `/v1/capabilities`, `whoami`, and host lease endpoints.
- Invite management now uses authenticated owner/admin server identity.
- Host lease status now separates ID match from validity and expiry.
- World backup treats no remote snapshot as an empty state and calls
  EnsureActiveLease before publishing.
- Network configuration warnings now return `success_with_warnings` with
  `ok=true` after configuration is saved.

Known limits:

- The Go desktop UI is an embedded local web UI opened by the OS URL handler.
- Legacy PowerShell GUI remains in the repository only as an explicit developer
  fallback.
