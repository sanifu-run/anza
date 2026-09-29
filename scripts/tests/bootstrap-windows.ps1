[CmdletBinding()]
param()
Set-StrictMode -Version 2
$ErrorActionPreference = 'Stop'

$repo = (Resolve-Path (Join-Path $PSScriptRoot '..\..\..')).Path
. (Join-Path $repo 'install.ps1') -Version '1.2.3' -ExpectedManifestSha256 ('a' * 64) -ReleaseBaseUri 'https://releases.anza.dev' -InstallRoot (Join-Path $env:TEMP 'unused') -Download { param($u, $p) } -ArchitectureOverride 'amd64' -AddToPath $false

$script:passed = 0
function Assert-True { param([bool] $Condition, [string] $Name); if (-not $Condition) { throw "FAIL: $Name" }; $script:passed++; Write-Host "PASS: $Name" }
function Assert-Throws { param([scriptblock] $Action, [string] $Name, [string] $Pattern = ''); $failed = $false; try { & $Action } catch { $failed = $true; if ($Pattern -and $_.Exception.Message -notmatch $Pattern) { throw "FAIL: $Name (wrong error: $($_.Exception.Message))" } }; Assert-True $failed $Name }

$root = Join-Path $env:TEMP ('Anza T9.3 é Home ' + [guid]::NewGuid().ToString('N'))
[void][IO.Directory]::CreateDirectory($root)
try {
    $artifactBytes = [Text.Encoding]::UTF8.GetBytes('synthetic local Anza executable fixture')
    $artifactHash = [BitConverter]::ToString(([Security.Cryptography.SHA256]::Create()).ComputeHash($artifactBytes)).Replace('-', '').ToLowerInvariant()
    $manifestObject = [ordered]@{ version = '1.2.3'; assets = @{ amd64 = @{ url = 'https://releases.anza.dev/anza/1.2.3/windows/anza-windows-amd64.exe'; sha256 = $artifactHash }; arm64 = @{ url = 'https://releases.anza.dev/anza/1.2.3/windows/anza-windows-arm64.exe'; sha256 = $artifactHash } } }
    $manifestBytes = [Text.Encoding]::UTF8.GetBytes(($manifestObject | ConvertTo-Json -Depth 8 -Compress))
    $manifestHash = [BitConverter]::ToString(([Security.Cryptography.SHA256]::Create()).ComputeHash($manifestBytes)).Replace('-', '').ToLowerInvariant()
    $manifestObjectBadArtifact = [ordered]@{ version = '1.2.3'; assets = @{ amd64 = @{ url = 'https://releases.anza.dev/anza/1.2.3/windows/anza-windows-amd64.exe'; sha256 = ('b' * 64) } } }
    $badArtifactManifestBytes = [Text.Encoding]::UTF8.GetBytes(($manifestObjectBadArtifact | ConvertTo-Json -Depth 8 -Compress))
    $badArtifactManifestHash = [BitConverter]::ToString(([Security.Cryptography.SHA256]::Create()).ComputeHash($badArtifactManifestBytes)).Replace('-', '').ToLowerInvariant()

    $syntheticDownload = { param($Uri, $Destination); if ($Uri -match '/manifest\.json$') { [IO.File]::WriteAllBytes($Destination, $manifestBytes) } else { [IO.File]::WriteAllBytes($Destination, $artifactBytes) } }.GetNewClosure()
    $install = Join-Path $root 'User 名 with spaces\Programs\Anza\bin'
    Install-Anza '1.2.3' $manifestHash 'https://releases.anza.dev' $install $syntheticDownload 'amd64' $false
    Assert-True ([IO.File]::ReadAllBytes((Join-Path $install 'anza.exe')).Length -eq $artifactBytes.Length) 'installs verified synthetic artifact beneath non-ASCII, spaced home'
    $arm64Root = Join-Path $root 'arm64 install'
    $arm64Marker = Join-Path $root 'arm64-selected-url.txt'
    $arm64Download = { param($Uri, $Destination); if ($Uri -match '/manifest\.json$') { [IO.File]::WriteAllBytes($Destination, $manifestBytes) } else { [IO.File]::WriteAllText($arm64Marker, $Uri); [IO.File]::WriteAllBytes($Destination, $artifactBytes) } }.GetNewClosure()
    Install-Anza '1.2.3' $manifestHash 'https://releases.anza.dev' $arm64Root $arm64Download 'arm64' $false
    Assert-True ((Get-Content -LiteralPath $arm64Marker -Raw) -match 'anza-windows-arm64\.exe$') 'supported arm64 selects and installs its matching synthetic asset'
    Assert-True ((Get-AnzaUpdatedUserPath 'C:\Tools;D:\Existing;C:\Tools\' 'C:\Tools') -eq 'C:\Tools;D:\Existing') 'user PATH update preserves unrelated entries and deduplicates only its own entry'

    Assert-Throws { Get-AnzaArchitecture 'x86' } 'unsupported architecture is rejected' 'Unsupported Windows architecture'
    Assert-Throws { Install-Anza '1.2.3' ('c' * 64) 'https://releases.anza.dev' (Join-Path $root 'bad-manifest') $syntheticDownload 'amd64' $false } 'corrupt manifest rejected before install' 'manifest SHA256 mismatch'

    $badArtifactDownload = { param($Uri, $Destination); if ($Uri -match '/manifest\.json$') { [IO.File]::WriteAllBytes($Destination, $badArtifactManifestBytes) } else { [IO.File]::WriteAllBytes($Destination, $artifactBytes) } }.GetNewClosure()
    Assert-Throws { Install-Anza '1.2.3' $badArtifactManifestHash 'https://releases.anza.dev' (Join-Path $root 'bad-artifact') $badArtifactDownload 'amd64' $false } 'corrupt artifact rejected before target write' 'Artifact SHA256 mismatch'
    Assert-True (-not [IO.File]::Exists((Join-Path (Join-Path $root 'bad-artifact') 'anza.exe'))) 'corrupt artifact leaves no installed executable'

    $cancel = { param($Uri, $Destination); throw [OperationCanceledException]::new('fixture download cancelled') }
    Assert-Throws { Install-Anza '1.2.3' $manifestHash 'https://releases.anza.dev' (Join-Path $root 'cancelled') $cancel 'amd64' $false } 'cancelled download leaves no installation' 'cancelled'
    Assert-True (-not [IO.Directory]::Exists((Join-Path $root 'cancelled'))) 'cancelled download does not create target directory'

    $lockedPath = Join-Path $install 'anza.exe'
    $oldBytes = [IO.File]::ReadAllBytes($lockedPath)
    $lock = [IO.File]::Open($lockedPath, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::Read)
    try { Assert-Throws { Install-Anza '1.2.3' $manifestHash 'https://releases.anza.dev' $install $syntheticDownload 'amd64' $false } 'locked executable is reported without destructive replacement' 'Close Anza' }
    finally { $lock.Dispose() }
    Assert-True ([Convert]::ToBase64String([IO.File]::ReadAllBytes($lockedPath)) -eq [Convert]::ToBase64String($oldBytes)) 'locked executable remains byte-for-byte intact'

    Assert-Throws { Install-Anza '__ANZA_VERSION__' ('a' * 64) 'https://releases.anza.dev' (Join-Path $root 'unstamped') $syntheticDownload 'amd64' $false } 'unstamped release version is rejected'
    Assert-Throws { Install-Anza '1.2.3' '__T9_4_STAMP_MANIFEST_SHA256__' 'https://releases.anza.dev' (Join-Path $root 'unstamped-digest') $syntheticDownload 'amd64' $false } 'unstamped production digest is rejected' 'T9.4'
} finally { Remove-Item -LiteralPath $root -Recurse -Force -ErrorAction SilentlyContinue }

Write-Host "Windows bootstrap fixture completed: $script:passed assertions."
