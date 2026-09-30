[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][ValidateSet('before', 'after')][string] $Phase,
    [Parameter(Mandatory = $true)][ValidatePattern('^[A-Za-z0-9][A-Za-z0-9._-]*$')][string] $Target,
    [Parameter(Mandatory = $true)][string] $EvidenceDirectory,
    [string[]] $Owned = @(),
    [string[]] $Unowned = @()
)

# Read-only evidence helper. It does not invoke Anza/installers, edit PATH or
# configuration, contact the network, or remove files. It hashes only paths
# explicitly passed by the operator and stores no path strings in its output.
Set-StrictMode -Version 2
$ErrorActionPreference = 'Stop'

function Add-SnapshotRow {
    param([string] $Kind, [string] $Pair, [System.Collections.Generic.List[string]] $Rows,
          [System.Collections.Generic.HashSet[string]] $Labels)

    $separator = $Pair.IndexOf('=')
    if ($separator -lt 1 -or $separator -eq ($Pair.Length - 1)) {
        throw "Expected LABEL=FILE for $Kind entry."
    }
    $label = $Pair.Substring(0, $separator)
    $path = $Pair.Substring($separator + 1)
    if ($label -notmatch '^[A-Za-z0-9][A-Za-z0-9._-]*$') { throw "Invalid label: $label" }
    if (-not $Labels.Add($label)) { throw "Duplicate label: $label" }

    if (-not (Test-Path -LiteralPath $path)) {
        $state = 'absent'; $digest = '-'
    } else {
        $item = Get-Item -LiteralPath $path -Force
        if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            $state = 'reparse-point'; $digest = '-'
        } elseif ($item.PSIsContainer) {
            $state = 'other'; $digest = '-'
        } else {
            $state = 'file'
            $digest = (Get-FileHash -LiteralPath $item.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
        }
    }
    [void] $Rows.Add("$Kind`t$label`t$state`t$digest")
}

$directory = [IO.Path]::GetFullPath($EvidenceDirectory)
if (Test-Path -LiteralPath $directory) {
    $directoryItem = Get-Item -LiteralPath $directory -Force
    if (($directoryItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
        throw 'Evidence directory must not be a reparse point.'
    }
} else {
    $null = New-Item -ItemType Directory -Path $directory
}
$output = Join-Path $directory "$Target-$Phase.tsv"
$existingOutput = Get-Item -LiteralPath $output -Force -ErrorAction SilentlyContinue
if ($null -ne $existingOutput) { throw "Refusing to overwrite $Target-$Phase.tsv" }

$rows = [System.Collections.Generic.List[string]]::new()
$labels = [System.Collections.Generic.HashSet[string]]::new([StringComparer]::OrdinalIgnoreCase)
[void] $rows.Add("kind`tlabel`tstate`tsha256")
foreach ($pair in $Owned) { Add-SnapshotRow 'owned' $pair $rows $labels }
foreach ($pair in $Unowned) { Add-SnapshotRow 'unowned' $pair $rows $labels }
[IO.File]::WriteAllLines($output, $rows, [Text.UTF8Encoding]::new($false))
Write-Output "Wrote $output (hashes only; no file paths recorded)."
