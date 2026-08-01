$ErrorActionPreference = "Stop"

New-Item -ItemType Directory -Force -Path "dist" | Out-Null
go test ./...
go build -trimpath -ldflags "-s -w -H=windowsgui" -o "dist\sidebox.exe" .

Write-Host "Built: dist\sidebox.exe"
