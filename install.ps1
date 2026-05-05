# PowerShell install script for cloudcent (Windows)

$ErrorActionPreference = "Stop"

$Repo = "OverloadBlitz/cloudcent-cli"
$Binary = "cloudcent"
$InstallDir = "$env:USERPROFILE\.cloudcent\bin"

function Detect-Arch {
    try {
        $arch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
    } catch {
        $arch = $env:PROCESSOR_ARCHITECTURE
    }
    switch ($arch) {
        "X64"   { return "amd64" }
        "AMD64" { return "amd64" }
        "Arm64" { return "arm64" }
        "ARM64" { return "arm64" }
        default {
            Write-Error "Unsupported architecture: $arch"
            exit 1
        }
    }
}

function Get-LatestVersion {
    $url = "https://api.github.com/repos/$Repo/releases/latest"
    $response = Invoke-RestMethod -Uri $url -Headers @{ "User-Agent" = "cloudcent-installer" }
    return $response.tag_name
}

function Add-ToPath($dir) {
    $currentPath = [Environment]::GetEnvironmentVariable("Path", "User")
    if ($currentPath -notlike "*$dir*") {
        [Environment]::SetEnvironmentVariable("Path", "$currentPath;$dir", "User")
        $env:Path = "$env:Path;$dir"
        Write-Host "Added $dir to your PATH (restart your terminal for it to take effect)."
    }
}

function Main {
    $arch = Detect-Arch
    Write-Host "Detected architecture: $arch"

    Write-Host "Fetching latest release..."
    $version = Get-LatestVersion
    if (-not $version) {
        Write-Error "Could not determine latest version. Check https://github.com/$Repo/releases"
        exit 1
    }
    Write-Host "Latest version: $version"

    # goreleaser archive format: cloudcent_<version>_windows_<arch>.zip
    $ver = $version.TrimStart('v')
    $archiveName = "${Binary}_${ver}_windows_${arch}.zip"
    $url = "https://github.com/$Repo/releases/download/$version/$archiveName"

    $tmpDir = Join-Path ([System.IO.Path]::GetTempPath()) ([System.Guid]::NewGuid().ToString())
    New-Item -ItemType Directory -Path $tmpDir | Out-Null

    try {
        Write-Host "Downloading $url..."
        Invoke-WebRequest -Uri $url -OutFile (Join-Path $tmpDir $archiveName) -UseBasicParsing

        Write-Host "Extracting..."
        Expand-Archive -Path (Join-Path $tmpDir $archiveName) -DestinationPath $tmpDir -Force

        if (-not (Test-Path $InstallDir)) {
            New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
        }

        $src = Join-Path $tmpDir "$Binary.exe"
        $dest = Join-Path $InstallDir "$Binary.exe"
        Move-Item -Path $src -Destination $dest -Force

        Add-ToPath $InstallDir

        Write-Host ""
        Write-Host "Done! Run 'cloudcent' to get started."
    }
    finally {
        Remove-Item -Recurse -Force $tmpDir -ErrorAction SilentlyContinue
    }
}

Main
