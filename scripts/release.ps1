[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string] $Version,
    [Parameter(Mandatory = $true)][ValidateSet('darwin', 'linux', 'windows')][string] $GOOS,
    [Parameter(Mandatory = $true)][ValidateSet('amd64', 'arm64')][string] $GOARCH,
    [Parameter(Mandatory = $true)][string] $OutputDirectory
)

Set-StrictMode -Version 2
$ErrorActionPreference = 'Stop'
if ($Version -notmatch '^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$') { throw 'Version must be canonical numeric major.minor.patch to match the updater contract.' }
$releasePublicKey = $env:ANZA_RELEASE_PUBLIC_KEY_BASE64
if ([string]::IsNullOrWhiteSpace($releasePublicKey)) { throw 'ANZA_RELEASE_PUBLIC_KEY_BASE64 must contain the pinned Ed25519 public key.' }
try { $keyBytes = [Convert]::FromBase64String($releasePublicKey) } catch { throw 'ANZA_RELEASE_PUBLIC_KEY_BASE64 must be valid base64.' }
if ($keyBytes.Length -ne 32 -or [Convert]::ToBase64String($keyBytes) -cne $releasePublicKey) { throw 'ANZA_RELEASE_PUBLIC_KEY_BASE64 must be canonical base64 for a 32-byte Ed25519 public key.' }
$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$null = New-Item -ItemType Directory -Force -Path $OutputDirectory
$extension = if ($GOOS -eq 'windows') { '.exe' } else { '' }
$output = Join-Path (Resolve-Path $OutputDirectory).Path "anza-$GOOS-$GOARCH$extension"
$oldGOOS = $env:GOOS; $oldGOARCH = $env:GOARCH; $oldCGO = $env:CGO_ENABLED
try {
    $env:GOOS = $GOOS; $env:GOARCH = $GOARCH; $env:CGO_ENABLED = '0'
    Push-Location $root
    try { & go build -trimpath -buildvcs=false -ldflags "-buildid= -X main.releasePublicKeyBase64=$releasePublicKey" -o $output ./cmd/anza; if ($LASTEXITCODE -ne 0) { throw "go build failed with exit code $LASTEXITCODE" } }
    finally { Pop-Location }
} finally {
    $env:GOOS = $oldGOOS; $env:GOARCH = $oldGOARCH; $env:CGO_ENABLED = $oldCGO
}
Get-Item -LiteralPath $output
