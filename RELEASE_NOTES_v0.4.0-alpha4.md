# v0.4.0-alpha4

Alpha4 continues the Go desktop runtime track without rolling back alpha3.

Highlights:

- Adds a loopback-only desktop session with a one-time startup URL, HttpOnly/SameSite cookie, session header support, and Origin/Host/Content-Type checks for mutating APIs.
- Adds operation lookup by ID so the GUI can enqueue long work and poll for terminal results.
- Adds first-run configuration APIs for network testing/saving, group create/join/whoami/repair/leave, server directory inspection, launch entry selection, and server preflight.
- Adds backup profiles with manual folder roots and automatic Minecraft world-root suggestions.
- Keeps remote backup manifests root-relative so local absolute paths are not exposed to other hosts.
- Adds alpha4 regression tests for malicious localhost request rejection and manual backup profile manifest paths.

Known limitation:

- Native Windows file picker dialogs are not enabled in the Go-only runtime yet. The alpha4 GUI supports manual path entry and the picker endpoints accept caller-provided paths.
