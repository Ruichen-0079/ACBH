# Local Two-Host Takeover Demo

This demo uses two isolated Agent config directories, Coordinator local storage, one fake world file, and a fake PowerShell server process. It does not install or run Minecraft.

From the repository root on Windows:

```powershell
$env:PATH = "C:\path\to\go\bin;$env:PATH"
powershell -ExecutionPolicy Bypass -File examples/two-host-takeover-demo/demo.ps1
```

The script:

1. Builds the Coordinator and Agent.
2. Starts an in-memory Coordinator with a one-second heartbeat timeout.
3. Creates a group and registers Host A and Host B.
4. Elects and completes an initial assignment for Host A.
5. Scans and pushes a fake world snapshot from Host A.
6. Lets Host A become stale while Host B reports the latest snapshot locally.
7. Runs timeout election and assigns Host B.
8. Runs `acbh-agent takeover run` for Host B.
9. Restores the world snapshot, starts the fake server, sends a hosting heartbeat, and completes the assignment.
10. Verifies the restored SHA256, `currentHostId`, and `currentHostGeneration`.

The expected final generation is `2`: one completed assignment establishes Host A and the second completed assignment transfers current-host authority to Host B.
