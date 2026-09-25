[CmdletBinding()]
param(
    [ValidateSet("amd64", "arm64")]
    [string]$Architecture = "",
    [string]$InstallDirectory = ""
)

$ErrorActionPreference = "Stop"

$parent = Split-Path -Parent $PSScriptRoot
$repoDist = Join-Path $parent "dist"
$dist = $repoDist
if (-not (Test-Path -LiteralPath (Join-Path $dist "SHA256SUMS") -PathType Leaf)) {
    $dist = $PSScriptRoot
}

if ([string]::IsNullOrWhiteSpace($Architecture)) {
    $processor = $env:PROCESSOR_ARCHITEW6432
    if ([string]::IsNullOrWhiteSpace($processor)) {
        $processor = $env:PROCESSOR_ARCHITECTURE
    }
    switch ($processor.ToUpperInvariant()) {
        "AMD64" { $Architecture = "amd64" }
        "ARM64" { $Architecture = "arm64" }
        default { throw "Unsupported Windows architecture: $processor" }
    }
}

if ([string]::IsNullOrWhiteSpace($InstallDirectory)) {
    $InstallDirectory = Join-Path $env:LOCALAPPDATA "Programs\envGo"
}
$InstallDirectory = [IO.Path]::GetFullPath($InstallDirectory)

$source = Join-Path $dist "envgo-windows-$Architecture.exe"
$manifest = Join-Path $dist "SHA256SUMS"
if (-not (Test-Path -LiteralPath $source -PathType Leaf)) {
    throw "envGo binary not found: $source"
}
if (-not (Test-Path -LiteralPath $manifest -PathType Leaf)) {
    throw "Checksum manifest not found: $manifest"
}

$filename = "envgo-windows-$Architecture.exe"
$pattern = '(?i)^[0-9a-f]{64}\s{2}(?:.*[\\/])?' + [regex]::Escape($filename) + '$'
$lines = @(Get-Content -LiteralPath $manifest | Where-Object { $_ -match $pattern })
if ($lines.Count -ne 1) {
    throw "Expected one checksum entry for $filename, found $($lines.Count)"
}
$expectedMatch = [regex]::Match($lines[0], '(?i)^(?<hash>[0-9a-f]{64})\s{2}')
$expected = $expectedMatch.Groups["hash"].Value.ToLowerInvariant()

New-Item -ItemType Directory -Force -Path $InstallDirectory | Out-Null
$destination = Join-Path $InstallDirectory "envgo.exe"
if (Test-Path -LiteralPath $destination) {
    $destinationItem = Get-Item -LiteralPath $destination -Force
    if ($destinationItem.PSIsContainer) {
        throw "Install destination is a directory: $destination"
    }
}
$temporary = Join-Path $InstallDirectory ".envgo.$PID.exe"
try {
    Copy-Item -LiteralPath $source -Destination $temporary -Force
    $actual = (Get-FileHash -LiteralPath $temporary -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actual -ne $expected) {
        throw "Checksum mismatch for $filename"
    }
    & $temporary "-h" *> $null
    if ($LASTEXITCODE -ne 0) {
        throw "envGo failed its startup check for $Architecture"
    }
    $actual = (Get-FileHash -LiteralPath $temporary -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actual -ne $expected) {
        throw "Checksum changed during startup check for $filename"
    }
    if ([IO.File]::Exists($destination)) {
        [IO.File]::Replace($temporary, $destination, $null)
    } else {
        [IO.File]::Move($temporary, $destination)
    }
} finally {
    if (Test-Path -LiteralPath $temporary) {
        Remove-Item -LiteralPath $temporary -Force
    }
}

$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
$trimChars = [char[]]"\/"
$normalizedInstall = $InstallDirectory.TrimEnd($trimChars)
$hasInstallPath = $false
if (-not [string]::IsNullOrWhiteSpace($userPath)) {
    foreach ($entry in ($userPath -split ";")) {
        if (-not [string]::IsNullOrWhiteSpace($entry)) {
            $normalizedEntry = $entry.Trim().TrimEnd($trimChars)
            if ($normalizedEntry.Equals($normalizedInstall, [StringComparison]::OrdinalIgnoreCase)) {
                $hasInstallPath = $true
                break
            }
        }
    }
}
if (-not $hasInstallPath) {
    $newPath = if ([string]::IsNullOrWhiteSpace($userPath)) { $InstallDirectory } else { $userPath.TrimEnd(";") + ";" + $InstallDirectory }
    [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
}
$env:Path = "$InstallDirectory;$env:Path"

Write-Host "Installed verified envGo $Architecture to $destination"
Write-Host "Open a new terminal, then run: envgo -v"
