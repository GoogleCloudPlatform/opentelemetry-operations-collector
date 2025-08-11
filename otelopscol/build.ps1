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

# Store PATH
$startEnvPath = $env:Path

# Set up tools directory.
$toolsDir="" + (Get-Location) + "\.tools" # Powershell moment
New-Item -ItemType Directory -Force -Path $toolsDir | Out-Null

# Download and install MSYS2.
$msysInstallerPath="./msys2-x86_64.exe"
$msysDownloadURL="https://github.com/msys2/msys2-installer/releases/download/2025-06-22/msys2-x86_64-20250622.exe"
Invoke-WebRequest $msysDownloadURL -OutFile $msysInstallerPath
Start-Process $msysInstallerPath -ArgumentList 'in', '--confirm-command', `
    '--accept-messages', '--root', 'C:/msys64' -NoNewWindow -Wait;
Remove-Item $msysInstallerPath

# Download Go.
$goZipPath="./go.windows-amd64.zip"
$goDownloadURL="https://go.dev/dl/go1.23.2.windows-amd64.zip"
Invoke-WebRequest $goDownloadURL -OutFile $goZipPath
Expand-Archive -Path $goZipPath -DestinationPath $toolsDir
Remove-Item $goZipPath
$goBinDir="$toolsDir\go\bin"
$goBin="$goBinDir\go"

# Download OCB.
$installOcbCommand="`$env:GOBIN='$toolsDir'; `$env:CGO_ENABLED=1; $goBin install -trimpath -ldflags='-s -w' go.opentelemetry.io/collector/cmd/builder@v0.130.0"
Invoke-Expression -Command $installOcbCommand
$ocbBin="$toolsDir\builder.exe"

# Generate the collector source.
$ocbGenerateCommand="`$env:PATH='${goBinDir};${PATH}'; `$env:CGO_ENABLED=1; $ocbBin --skip-compilation --verbose --config manifest.yaml"
Invoke-Expression -Command $ocbGenerateCommand

# Setup MINGW for go build.
$env:Path = "C:\msys64\usr\bin;C:\msys64\mingw64\bin"
pacman -S --noconfirm mingw-w64-x86_64-gcc

# Build the collector.
$ldFlags="-s -w"
if ($jmxHash -ne "") {
    $ldFlags+=" -X github.com/open-telemetry/opentelemetry-collector-contrib/receiver/jmxreceiver.MetricsGathererHash=$jmxHash"
}
$buildCollectorCommand=@"
`$env:GOWORK='off'; `$env:CGO_ENABLED=1; cd _build; $goBin build -p 32 -buildvcs=false -o '{0}/google-cloud-metrics-agent_windows_amd64.exe' --ldflags='{1}' .
"@ -f $outDir, $ldFlags
Invoke-Expression -Command $buildCollectorCommand

# Restore PATH
$env:Path = $startEnvPath
