$ErrorActionPreference = "Stop"

Write-Output "fake server ready"
while (($line = [Console]::In.ReadLine()) -ne $null) {
    if ($line.Trim() -eq "stop") {
        Write-Output "fake server stopped gracefully"
        exit 0
    }
}

exit 0
