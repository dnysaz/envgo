[CmdletBinding()]
param(
    [ValidateSet("amd64", "arm64")]
    [string]$Architecture = "",
    [string]$InstallDirectory = ""
)

# Windows installer for envGo.
#
# Guarantees, in the same order as Install_envGo.command on macOS:
#   1. the binary is checksum-verified against the published SHA256SUMS before
#      anything is executed;
#   2. the install is staged on the same filesystem and the final move is
#      atomic, so a failed run never leaves a half-written envgo.exe;
#   3. the staged copy is verified again after its startup check, in case the
#      file was swapped while the check was running;
#   4. the advertised version is read from the release, never hardcoded here.

$ErrorActionPreference = "Stop"

# The script is shipped inside dist/, but is also useful sitting next to a bare
# binary. Look in both places for the binary and the manifest independently, so
# a partial download still installs if it has everything that is needed.
$searchDirs = @((Join-Path (Split-Path -Parent $PSScriptRoot) "dist"), $PSScriptRoot) |
    Where-Object { $_ } | Select-Object -Unique

if ([string]::IsNullOrWhiteSpace($Architecture)) {
    $processor = $env:PROCESSOR_ARCHITEW6432
    if ([string]::IsNullOrWhiteSpace($processor)) {
        $processor = $env:PROCESSOR_ARCHITECTURE
    }
    if ([string]::IsNullOrWhiteSpace($processor)) {
        throw "Cannot detect the Windows architecture. Re-run with -Architecture amd64 or -Architecture arm64."
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

$filename = "envgo-windows-$Architecture.exe"

function Find-Artifact {
    param([string]$Name)
    foreach ($dir in $searchDirs) {
        $candidate = Join-Path $dir $Name
        if (Test-Path -LiteralPath $candidate -PathType Leaf) {
            return $candidate
        }
    }
    return $null
}

$source = Find-Artifact -Name $filename
if (-not $source) {
    throw "envGo binary not found: $filename (looked in: $($searchDirs -join ', '))"
}
$manifest = Find-Artifact -Name "SHA256SUMS"

# The version is never hardcoded here. It comes from the VERSION file the
# release process stamps next to the binary, or straight from the binary
# itself, so this installer can never advertise a version it did not install.
#
# Reading the stamp never executes anything. The `-v` fallback runs only after
# the binary has been checksum-verified, so a tampered download is never run.
function Get-StampedVersion {
    $stamp = Find-Artifact -Name "VERSION"
    if (-not $stamp) { return $null }
    $text = (Get-Content -LiteralPath $stamp -Raw).Trim()
    if ($text) { return $text }
    return $null
}

function Get-BinaryVersion {
    param([string]$Binary)
    $restore = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    try {
        foreach ($line in (& $Binary "-v" 2>$null)) {
            if ("$line" -match '^\s*envGo\s+(\S+)\s*$') { return $Matches[1] }
        }
    } catch {
        # fall through to the unknown label
    } finally {
        $ErrorActionPreference = $restore
    }
    return $null
}

# expected_checksum reads the published SHA256SUMS entry for a binary. Both
# manifest layouts are accepted: "dist/<name>" as shipped in the release
# directory, and a bare "<name>" as used by the release checksum self-check.
# A leading "*" marks binary mode and is ignored.
function Get-ExpectedChecksum {
    param([string]$Manifest, [string]$Name)
    $pattern = '(?i)^(?<hash>[0-9a-f]{64})\s+\*?(?:.*[\\/])?' + [regex]::Escape($Name) + '$'
    $hashes = @(Get-Content -LiteralPath $Manifest |
        Where-Object { $_ -match $pattern } |
        ForEach-Object { [regex]::Match($_, '(?i)^[0-9a-f]{64}').Value.ToLowerInvariant() } |
        Select-Object -Unique)
    switch ($hashes.Count) {
        0 { return $null }
        1 { return $hashes[0] }
        default { throw "Conflicting checksum entries for $Name in $Manifest" }
    }
}

function Get-Sha256 {
    param([string]$Path)
    return (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant()
}

$version = Get-StampedVersion
if (-not $version) { $version = "unknown" }

Write-Host ""
Write-Host "  envGo v$version - Zero-dependency micro-runtime for HTML/Vanilla JS/PHP" -ForegroundColor Cyan
Write-Host "  Secure .env injection - secrets never reach the browser" -ForegroundColor Cyan
Write-Host ""
Write-Host "Installing envGo v$version ($Architecture)..."
Write-Host "  From: $source"
Write-Host "  To  : $InstallDirectory"

# Verify before anything is executed. A downloaded binary is untrusted input.
$expected = $null
if ($manifest) {
    $expected = Get-ExpectedChecksum -Manifest $manifest -Name $filename
    if (-not $expected) {
        throw "No checksum entry for $filename in $manifest. Refusing to install an unverified binary."
    }
} else {
    Write-Warning "No SHA256SUMS manifest found next to the binary, so this download cannot be verified."
    Write-Warning "This is expected when you unpacked a .zip that ships only the binary."
    Write-Warning "To check it yourself, compare against the release page with:"
    Write-Warning "  Get-FileHash -Path `"$source`" -Algorithm SHA256"
}

New-Item -ItemType Directory -Force -Path $InstallDirectory | Out-Null
$destination = Join-Path $InstallDirectory "envgo.exe"
if (Test-Path -LiteralPath $destination) {
    $destinationItem = Get-Item -LiteralPath $destination -Force
    if ($destinationItem.PSIsContainer) {
        throw "Install destination is a directory: $destination"
    }
}

# Staged copy: install from a temp file on the same filesystem so the final move
# is atomic, and re-verify afterwards in case the staged file was swapped while
# its startup check ran.
$temporary = Join-Path $InstallDirectory ".envgo.$PID.exe"
try {
    Copy-Item -LiteralPath $source -Destination $temporary -Force

    if ($expected) {
        $actual = Get-Sha256 -Path $temporary
        if ($actual -ne $expected) {
            throw "Checksum mismatch for $filename (expected $expected, got $actual). Not installing."
        }
        Write-Host "  [ok] checksum verified" -ForegroundColor Green
    }

    # Startup check: prove the binary actually runs before it replaces anything.
    #
    # Start-Process is deliberate, not stylistic. Reading $LASTEXITCODE after
    # "& $temporary" returns 0 on PowerShell 7.6 whenever this script is invoked
    # with the call operator (or dot-sourced) instead of -File, even though the
    # child process failed. That silently let a broken binary replace a working
    # install. ExitCode comes off the process object, so it is correct in every
    # invocation mode. It also avoids the older problem where a native command
    # writing to stderr became a terminating error under "Stop" and aborted an
    # otherwise good binary, and it keeps the binary's help text off the screen.
    $outFile = Join-Path $InstallDirectory ".envgo.$PID.out"
    $errFile = Join-Path $InstallDirectory ".envgo.$PID.err"
    $startupExit = -1
    try {
        $process = Start-Process -FilePath $temporary -ArgumentList "-h" `
            -NoNewWindow -Wait -PassThru `
            -RedirectStandardOutput $outFile -RedirectStandardError $errFile
        $startupExit = $process.ExitCode
    } finally {
        foreach ($scratch in @($outFile, $errFile)) {
            if (Test-Path -LiteralPath $scratch) {
                Remove-Item -LiteralPath $scratch -Force
            }
        }
    }
    if ($startupExit -ne 0) {
        throw "envGo failed its startup check for $Architecture (exit $startupExit). Not installing."
    }

    if ($expected) {
        $actual = Get-Sha256 -Path $temporary
        if ($actual -ne $expected) {
            throw "Checksum changed during the startup check for $filename. Not installing."
        }
    }

    # Only now is it safe to ask the binary what it is: it has been verified and
    # proven to run. This fills in the version when no VERSION file shipped.
    if ($version -eq "unknown") {
        $detected = Get-BinaryVersion -Binary $temporary
        if ($detected) { $version = $detected }
    }

    if ([IO.File]::Exists($destination)) {
        # Replace needs a non-empty backup path: passing $null is rejected with
        # "The value cannot be an empty string", which would make every
        # reinstall and upgrade fail. The backup exists only so the swap is
        # atomic; it is removed immediately afterwards.
        $backup = Join-Path $InstallDirectory ".envgo.$PID.bak"
        try {
            [IO.File]::Replace($temporary, $destination, $backup)
        } finally {
            if (Test-Path -LiteralPath $backup) {
                Remove-Item -LiteralPath $backup -Force
            }
        }
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
    Write-Host "  [ok] added to your user PATH" -ForegroundColor Green
}
$env:Path = "$InstallDirectory;$env:Path"

Write-Host ""
Write-Host "Installed envGo v$version -> $destination" -ForegroundColor Green
Write-Host ""
Write-Host "Open a new terminal, then run:"
Write-Host "  envgo -v                 # version"
Write-Host "  envgo -h                 # help"
Write-Host "  envgo -b -e .env -a httpbin.org   # start + open browser"
Write-Host "  envgo run dev --qr       # share on your phone's network via QR code"
