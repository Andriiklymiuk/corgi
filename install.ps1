# corgi installer for Windows
#
# Usage (PowerShell):
#   irm https://raw.githubusercontent.com/Andriiklymiuk/corgi/main/install.ps1 | iex
#
# Environment variables:
#   $env:CORGI_VERSION         Pin a specific version (e.g. "1.10.0"). Default: latest GitHub release.
#   $env:CORGI_INSTALL_DIR     Override install directory. Default: $env:LOCALAPPDATA\corgi\bin
#   $env:CORGI_NO_MODIFY_PATH  Set to 1 to skip adding the install dir to your user PATH.

$ErrorActionPreference = 'Stop'

$Repo = 'Andriiklymiuk/corgi'

function Fail($msg) { Write-Error $msg; exit 1 }

# Detect arch.
$archRaw = $env:PROCESSOR_ARCHITECTURE
switch ($archRaw) {
    'AMD64' { $arch = 'amd64' }
    'ARM64' { $arch = 'arm64' }
    'x86'   { $arch = '386' }
    default { Fail "unsupported architecture: $archRaw" }
}

# Resolve version.
$version = $env:CORGI_VERSION
if (-not $version) {
    Write-Host 'fetching latest release...'
    try {
        $release = Invoke-RestMethod -UseBasicParsing `
            -Uri "https://api.github.com/repos/$Repo/releases/latest" `
            -Headers @{ 'User-Agent' = 'corgi-installer' }
    } catch {
        Fail "could not fetch latest release: $_"
    }
    $version = $release.tag_name -replace '^v',''
}

$asset = "corgi_${version}_windows_${arch}.tar.gz"
$url   = "https://github.com/$Repo/releases/download/v$version/$asset"

# Pick install dir.
$installDir = $env:CORGI_INSTALL_DIR
if (-not $installDir) {
    $installDir = Join-Path $env:LOCALAPPDATA 'corgi\bin'
}
New-Item -ItemType Directory -Force -Path $installDir | Out-Null

# tar.exe ships with Windows 10+ — required for .tar.gz extraction.
if (-not (Get-Command tar -ErrorAction SilentlyContinue)) {
    Fail 'tar.exe not found. Windows 10 1803+ ships tar; please update Windows or extract the release manually.'
}

$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("corgi-" + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
    Write-Host "downloading $asset (v$version, windows/$arch)..."
    Invoke-WebRequest -UseBasicParsing -Uri $url -OutFile (Join-Path $tmp $asset)

    # Optional checksum verification.
    try {
        $sumsPath = Join-Path $tmp 'checksums.txt'
        Invoke-WebRequest -UseBasicParsing `
            -Uri "https://github.com/$Repo/releases/download/v$version/checksums.txt" `
            -OutFile $sumsPath
        $expectedLine = Select-String -Path $sumsPath -Pattern ([regex]::Escape($asset)) | Select-Object -First 1
        if ($expectedLine) {
            $expected = ($expectedLine.Line -split '\s+')[0].ToLower()
            $actual = (Get-FileHash -Algorithm SHA256 (Join-Path $tmp $asset)).Hash.ToLower()
            if ($expected -ne $actual) { Fail "checksum mismatch for $asset" }
            Write-Host 'checksum ok'
        } else {
            Write-Warning "$asset not found in checksums.txt, skipping verification"
        }
    } catch {
        Write-Warning "could not verify checksum: $_"
    }

    tar -xzf (Join-Path $tmp $asset) -C $tmp
    if ($LASTEXITCODE -ne 0) { Fail 'tar extraction failed' }

    $extracted = Join-Path $tmp 'corgi.exe'
    if (-not (Test-Path $extracted)) { Fail 'archive did not contain corgi.exe' }

    $target = Join-Path $installDir 'corgi.exe'

    # Windows can rename a running .exe but not delete it. If the target is in use
    # by the currently-running process, move it aside before writing the new one.
    if (Test-Path $target) {
        try {
            Remove-Item $target -Force
        } catch {
            $sidelined = "$target.old"
            if (Test-Path $sidelined) { Remove-Item $sidelined -Force -ErrorAction SilentlyContinue }
            Move-Item $target $sidelined
        }
    }
    Move-Item $extracted $target

    Write-Host "installed corgi $version -> $target"
} finally {
    Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
}

# Add to user PATH if not already present.
$pathParts = ($env:Path -split ';') | Where-Object { $_ -ne '' }
$inPath = $pathParts -contains $installDir

if (-not $inPath) {
    if ($env:CORGI_NO_MODIFY_PATH -eq '1') {
        Write-Warning ""
        Write-Warning "$installDir is not in your PATH (CORGI_NO_MODIFY_PATH=1)."
        Write-Warning "add it to your user PATH manually."
    } else {
        $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
        if (-not $userPath) { $userPath = '' }
        $userParts = ($userPath -split ';') | Where-Object { $_ -ne '' }
        if ($userParts -notcontains $installDir) {
            $newUserPath = if ($userPath) { "$userPath;$installDir" } else { $installDir }
            [Environment]::SetEnvironmentVariable('Path', $newUserPath, 'User')
            Write-Host "added $installDir to user PATH"
        }
        # Update current session too so a follow-up `corgi -h` works.
        $env:Path = "$env:Path;$installDir"
        Write-Warning ""
        Write-Warning 'open a new terminal so the PATH change takes effect in other shells.'
    }
}

Write-Host "run 'corgi -h' to get started."
