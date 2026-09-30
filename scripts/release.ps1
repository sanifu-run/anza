[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string] $Version,
    [Parameter(Mandatory = $true)][ValidateSet('darwin', 'linux', 'windows')][string] $GOOS,
    [Parameter(Mandatory = $true)][ValidateSet('amd64', 'arm64')][string] $GOARCH,
    [Parameter(Mandatory = $true)][string] $OutputDirectory
)

Set-StrictMode -Version 2
$ErrorActionPreference = 'Stop'
if ($Version -notmatch '^\d+\.\d+\.\d+([+-][A-Za-z0-9.-]+)?$') { throw 'Version must be semantic x.y.z with an optional prerelease/build suffix.' }
$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$null = New-Item -ItemType Directory -Force -Path $OutputDirectory
$extension = if ($GOOS -eq 'windows') { '.exe' } else { '' }
$output = Join-Path (Resolve-Path $OutputDirectory).Path "anza-$GOOS-$GOARCH$extension"
$oldGOOS = $env:GOOS; $oldGOARCH = $env:GOARCH; $oldCGO = $env:CGO_ENABLED
try {
    $env:GOOS = $GOOS; $env:GOARCH = $GOARCH; $env:CGO_ENABLED = '0'
    Push-Location $root
    try { & go build -trimpath -buildvcs=false -ldflags '-buildid=' -o $output ./cmd/anza; if ($LASTEXITCODE -ne 0) { throw "go build failed with exit code $LASTEXITCODE" } }
    finally { Pop-Location }
} finally {
    $env:GOOS = $oldGOOS; $env:GOARCH = $oldGOARCH; $env:CGO_ENABLED = $oldCGO
}
Get-Item -LiteralPath $output
