[CmdletBinding()]
# If policy blocks this script, run `powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\install.ps1`.
# The policy option applies to that process only. Installation is per-user; no elevation is needed.
param(
    [string] $Version = '__ANZA_VERSION__',
    [string] $ExpectedManifestSha256 = '__T9_4_STAMP_MANIFEST_SHA256__',
    [string] $ReleaseBaseUri = '__T9_4_STAMP_REVIEWED_RELEASE_ORIGIN__',
    [string] $InstallRoot = (Join-Path $env:LOCALAPPDATA 'Programs\Anza\bin'),
    [scriptblock] $Download = $null,
    [string] $ArchitectureOverride = $null,
    [Nullable[bool]] $AddToPath = $null
)

Set-StrictMode -Version 2
$ErrorActionPreference = 'Stop'

function Get-AnzaArchitecture {
    param([string] $Override)
    $value = if ($Override) { $Override } else {
        $runtime = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
        if ($runtime -eq 'X64') { 'amd64' } elseif ($runtime -eq 'Arm64') { 'arm64' } else { $runtime.ToLowerInvariant() }
    }
    if ($value -notin @('amd64', 'arm64')) { throw "Unsupported Windows architecture '$value'. Anza bootstrap supports Windows amd64 and arm64." }
    return $value
}

function Get-AnzaSha256 {
    param([string] $Path)
    $stream = [System.IO.File]::OpenRead($Path)
    try {
        $sha = [System.Security.Cryptography.SHA256]::Create()
        try { return ([BitConverter]::ToString($sha.ComputeHash($stream))).Replace('-', '').ToLowerInvariant() }
        finally { $sha.Dispose() }
    } finally { $stream.Dispose() }
}

function Invoke-AnzaDownload {
    param([uri] $Uri, [string] $Destination, [scriptblock] $Downloader)
    if ($Uri.Scheme -ne 'https') { throw "Refusing non-HTTPS download URI: $Uri" }
    if ($Downloader) { & $Downloader $Uri.AbsoluteUri $Destination; return }
    $ProgressPreference = 'SilentlyContinue'
    Invoke-WebRequest -Uri $Uri -OutFile $Destination -UseBasicParsing
}

function Get-AnzaUpdatedUserPath {
    param([string] $Current, [string] $Directory)
    $entries = @($Current -split ';' | Where-Object { $_ -and $_.TrimEnd('\') -ine $Directory.TrimEnd('\') })
    return (@($Directory) + $entries) -join ';'
}

function Set-AnzaUserPath {
    param([string] $Directory)
    $current = [Environment]::GetEnvironmentVariable('Path', 'User')
    $updated = Get-AnzaUpdatedUserPath $current $Directory
    [Environment]::SetEnvironmentVariable('Path', $updated, 'User')
    if (($env:Path -split ';' | Where-Object { $_.TrimEnd('\') -ieq $Directory.TrimEnd('\') }).Count -eq 0) {
        $env:Path = "$Directory;$env:Path"
    }
}

function Install-Anza {
    param(
        [string] $ReleaseVersion,
        [string] $ManifestDigest,
        [string] $BaseUri,
        [string] $DestinationDirectory,
        [scriptblock] $Downloader,
        [string] $Architecture,
        [Nullable[bool]] $PathConsent
    )
    if ($ReleaseVersion -notmatch '^\d+\.\d+\.\d+([+-][A-Za-z0-9.-]+)?$') { throw 'Anza release version is not stamped. Use a versioned installer generated for a reviewed release.' }
    if ($ManifestDigest -notmatch '^[0-9a-fA-F]{64}$' -or $ManifestDigest -match '^0{64}$') { throw 'Anza release manifest digest is not stamped. Obtain the reviewed digest from the release process (T9.4). No file was installed.' }
    if ($BaseUri -match '^__' -or $BaseUri -notmatch '^https://') { throw 'Reviewed release origin is not stamped. No production endpoint is assumed; use the origin supplied by the release process.' }
    $base = [uri]$BaseUri
    if ($base.Scheme -ne 'https' -or $base.IsDefaultPort -eq $false -or $base.UserInfo -or $base.AbsolutePath -ne '/' -or $base.Query -or $base.Fragment) { throw 'Release base URI must be a plain HTTPS origin.' }
    $arch = Get-AnzaArchitecture $Architecture
    $manifestUri = [uri]::new($base, "/anza/$ReleaseVersion/windows/manifest.json")
    $stage = Join-Path ([IO.Path]::GetTempPath()) ('anza-bootstrap-' + [guid]::NewGuid().ToString('N'))
    [void][IO.Directory]::CreateDirectory($stage)
    try {
        $manifestPath = Join-Path $stage 'manifest.json'
        Invoke-AnzaDownload $manifestUri $manifestPath $Downloader
        if (-not [IO.File]::Exists($manifestPath)) { throw 'Manifest download was cancelled or produced no file.' }
        if ((Get-AnzaSha256 $manifestPath) -ne $ManifestDigest.ToLowerInvariant()) { throw 'Release manifest SHA256 mismatch; refusing the artifact.' }
        $manifest = Get-Content -LiteralPath $manifestPath -Raw | ConvertFrom-Json
        if ($manifest.version -ne $ReleaseVersion) { throw 'Release manifest version does not match the requested version.' }
        $asset = $manifest.assets.PSObject.Properties[$arch].Value
        if (-not $asset -or $asset.sha256 -notmatch '^[0-9a-fA-F]{64}$' -or $asset.sha256 -match '^0{64}$') { throw "Manifest has no valid Windows $arch artifact digest." }
        $artifactUri = [uri]$asset.url
        if ($artifactUri.Scheme -ne 'https' -or $artifactUri.Host -ine $base.Host -or $artifactUri.UserInfo) { throw 'Manifest artifact must use HTTPS on the reviewed release host.' }
        $artifactPath = Join-Path $stage 'anza.exe'
        Invoke-AnzaDownload $artifactUri $artifactPath $Downloader
        if (-not [IO.File]::Exists($artifactPath)) { throw 'Artifact download was cancelled or produced no file.' }
        if ((Get-AnzaSha256 $artifactPath) -ne $asset.sha256.ToLowerInvariant()) { throw 'Artifact SHA256 mismatch; refusing installation.' }

        try { [void][IO.Directory]::CreateDirectory($DestinationDirectory) }
        catch [System.UnauthorizedAccessException] { throw "Cannot write the per-user install folder '$DestinationDirectory'. Choose a writable per-user folder and retry; Anza does not require Administrator rights." }
        $target = Join-Path $DestinationDirectory 'anza.exe'
        $incoming = Join-Path $DestinationDirectory ('.anza-' + [guid]::NewGuid().ToString('N') + '.tmp')
        [IO.File]::Copy($artifactPath, $incoming, $false)
        try {
            if ([IO.File]::Exists($target)) {
                $backup = Join-Path $DestinationDirectory ('anza.exe.previous-' + [DateTime]::UtcNow.ToString('yyyyMMddHHmmssfff'))
                try { [IO.File]::Replace($incoming, $target, $backup, $true) }
                catch [System.IO.IOException] { throw "Could not replace $target. Close Anza if it is running; the existing executable was preserved. Retry the installer." }
                catch [System.UnauthorizedAccessException] { throw "Access denied replacing $target. Close Anza if it is running and confirm this per-user folder is writable; no administrator rights are requested." }
            } else { [IO.File]::Move($incoming, $target) }
        } finally { if ([IO.File]::Exists($incoming)) { [IO.File]::Delete($incoming) } }

        if ($null -eq $PathConsent) {
            Write-Host "Anza is installed at $target. Add $DestinationDirectory to your user PATH? This changes only your account's PATH and preserves other entries. (Y/N)"
            $PathConsent = ((Read-Host 'Add user PATH entry') -match '^(?i)y(es)?$')
        }
        if ($PathConsent.Value) {
            try { Set-AnzaUserPath $DestinationDirectory }
            catch { Write-Warning "Anza installed, but user PATH could not be updated: $($_.Exception.Message). Add '$DestinationDirectory' to your account's PATH in Windows Settings." }
        }
        Write-Host "Anza $ReleaseVersion installed for this user at $target. Open a new terminal, then run anza.exe."
    } finally { Remove-Item -LiteralPath $stage -Recurse -Force -ErrorAction SilentlyContinue }
}

if ($MyInvocation.InvocationName -ne '.') {
    try { Install-Anza $Version $ExpectedManifestSha256 $ReleaseBaseUri $InstallRoot $Download $ArchitectureOverride $AddToPath }
    catch {
        $message = $_.Exception.Message
        if ($message -match 'running scripts is disabled|execution policy') {
            $message += "`nFor this invocation only, launch Windows PowerShell with: powershell.exe -NoProfile -ExecutionPolicy Bypass -File `"$($MyInvocation.MyCommand.Path)`". This does not change machine or user policy."
        }
        Write-Error $message
        exit 1
    }
}
