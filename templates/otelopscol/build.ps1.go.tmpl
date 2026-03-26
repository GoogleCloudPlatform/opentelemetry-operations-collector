# Copyright 2025 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

#################################################################################
# The build tooling from this repo is designed to be run on Linux. Eventually
# we will be able to do cross-platform builds for the Ops Agent, but for now
# we are still running in a Windows container. This script exists as a stop-gap
# until proper cross-platform builds are possible.
#################################################################################
# This script builds the otelopscol binary with a particular name and path to
# match the expectations of our build tooling. It will do the same thing as
# our Makefiles in this repo do, mainly to ensure there is one source of truth
# in this repo for exactly what version of Go is used to build the binary.
#################################################################################

param (
    [string]$jmxHash = "",
    [string]$outDir = "."
)

$ErrorActionPreference = "Stop"

function Run-Command {
    param ([string]$Command)
    Write-Host "Running: $Command"
    Invoke-Expression -Command $Command
    if ($LASTEXITCODE -ne 0) {
        throw "Command failed with exit code $LASTEXITCODE : $Command"
    }
}

function Ensure-ToolsDirectory {
    $toolsDir = Join-Path (Get-Location) ".tools"
    if (-not (Test-Path $toolsDir)) {
        Write-Host "Creating tools directory: $toolsDir"
        New-Item -ItemType Directory -Force -Path $toolsDir | Out-Null
    }
    return $toolsDir
}

function Ensure-MSYS2 {
    if (-not (Test-Path "C:\msys64")) {
        Write-Host "Installing MSYS2..."
        $msysInstallerPath = Join-Path $toolsDir "msys2-x86_64.exe"
        $msysDownloadURL = "https://github.com/msys2/msys2-installer/releases/download/2025-06-22/msys2-x86_64-20250622.exe"
        Invoke-WebRequest $msysDownloadURL -OutFile $msysInstallerPath
        Start-Process $msysInstallerPath -ArgumentList 'in', '--confirm-command', `
            '--accept-messages', '--root', 'C:/msys64' -NoNewWindow -Wait
        Remove-Item $msysInstallerPath
    } else {
        Write-Host "MSYS2 already installed at C:\msys64"
    }
}

function Get-GoBin {
    param($toolsDir)

    if ($env:GO_BIN_PATH -and (Test-Path $env:GO_BIN_PATH)) {
        Write-Host "Using customized Go from GO_BIN_PATH: $env:GO_BIN_PATH"
        return $env:GO_BIN_PATH
    }

    $goInstallDir = Join-Path $toolsDir "go"
    $goBin = Join-Path $goInstallDir "bin\go.exe"

    if (-not (Test-Path $goBin)) {
        Write-Host "Installing Go..."
        $goZipPath = Join-Path $toolsDir "go.windows-amd64.zip"
        $goDownloadURL = "https://go.dev/dl/go1.26.1.windows-amd64.zip"
        Invoke-WebRequest $goDownloadURL -OutFile $goZipPath
        Expand-Archive -Path $goZipPath -DestinationPath $toolsDir
        Remove-Item $goZipPath
    } else {
        Write-Host "Go already installed at $(Split-Path $goBin)"
    }
    return $goBin
}

function Ensure-OCB {
    param($toolsDir, $goBin)

    $ocbBin = Join-Path $toolsDir "builder.exe"
    if (-not (Test-Path $ocbBin)) {
        Write-Host "Installing OCB..."
        $installOcbCommand = "`$env:GOBIN='$toolsDir'; `$env:CGO_ENABLED=1; $goBin install -trimpath -ldflags='-s -w' go.opentelemetry.io/collector/cmd/builder@v0.148.0"
        Run-Command $installOcbCommand
    } else {
        Write-Host "OCB already installed at $ocbBin"
    }
    return $ocbBin
}

function Generate-CollectorSource {
    param($ocbBin, $goBin)

    Write-Host "Generating collector source..."
    $goBinDir = Split-Path $goBin
    $ocbGenerateCommand = "`$env:PATH='${goBinDir};${env:Path}'; `$env:CGO_ENABLED=1; $ocbBin --skip-compilation --verbose --config manifest.yaml"
    Run-Command $ocbGenerateCommand
}

function Set-BuildEnvironment {
    Write-Host "Temporarily updating PATH for build and ensuring GCC is present..."
    $originalPath = $env:Path
    $env:Path = "C:\msys64\usr\bin;C:\msys64\mingw64\bin;$originalPath"

    if (-not (Test-Path "C:\msys64\mingw64\bin\gcc.exe")) {
        Write-Host "Installing MINGW GCC..."
        Run-Command "pacman -S --noconfirm mingw-w64-x86_64-gcc"
    } else {
        Write-Host "MINGW GCC already installed"
    }
    return $originalPath
}

function Build-WindowsCollector {
    param (
        [string]$goBin,
        [string]$jmxHash,
        [string]$outDir
    )

    $originalPath = $null
    try {
        $originalPath = Set-BuildEnvironment

        Write-Host "Building the collector..."
        $ldFlags = "-s -w"
        if ($jmxHash -ne "") {
            $ldFlags += " -X github.com/open-telemetry/opentelemetry-collector-contrib/receiver/jmxreceiver.MetricsGathererHash=$jmxHash"
        }

        # Ensure output directory exists
        if (-not (Test-Path $outDir)) {
            New-Item -ItemType Directory -Force -Path $outDir | Out-Null
        }

        $buildCollectorCommand = @"
`$env:GOWORK='off'; `$env:CGO_ENABLED=1; cd _build; $goBin build -p 32 -buildvcs=false -o '{0}/google-cloud-metrics-agent_windows_amd64.exe' --ldflags='{1}' --gcflags="all=-l" .
"@ -f $outDir, $ldFlags
        Run-Command $buildCollectorCommand
        Write-Host "Build complete."
    }
    finally {
        if ($originalPath) {
            Write-Host "Restoring original PATH."
            $env:Path = $originalPath
        }
    }
}

# Main script execution
$toolsDir = Ensure-ToolsDirectory
Ensure-MSYS2
$goBin = Get-GoBin -toolsDir $toolsDir
$ocbBin = Ensure-OCB -toolsDir $toolsDir -goBin $goBin
Generate-CollectorSource -ocbBin $ocbBin -goBin $goBin
Build-WindowsCollector -goBin $goBin -jmxHash $jmxHash -outDir $outDir

Write-Host "Script finished."
