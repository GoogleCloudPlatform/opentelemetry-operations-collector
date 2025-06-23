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

# Set up tools directory.
$toolsDir="" + (Get-Location) + "\.tools" # Powershell moment
New-Item -ItemType Directory -Force -Path $toolsDir | Out-Null

# Download Go.
$goZipPath="./go.windows-amd64.zip"
$goDownloadURL="https://go.dev/dl/go1.23.2.windows-amd64.zip"
Invoke-WebRequest $goDownloadURL -OutFile $goZipPath
Expand-Archive -Path $goZipPath -DestinationPath $toolsDir
Remove-Item $goZipPath
$goBinDir="$toolsDir\go\bin"
$goBin="$goBinDir\go"

# Download OCB.
$installOcbCommand="`$env:GOBIN='$toolsDir'; `$env:CGO_ENABLED=0; $goBin install -trimpath -ldflags='-s -w' go.opentelemetry.io/collector/cmd/builder@v0.130.0"
powershell.exe -Command $installOcbCommand
$ocbBin="$toolsDir\builder.exe"

# Generate the collector source.
$ocbGenerateCommand="`$env:PATH='${goBinDir};${PATH}'; `$env:CGO_ENABLED=1; $ocbBin --skip-compilation --verbose --config manifest.yaml"
powershell.exe -Command $ocbGenerateCommand

# Download Visual Studio Tools 2019
$vsBuildtoolsBinDir="$toolsDir\vsBuildtools"
$vsBuildtoolsInstallerPath="./vs_buildtools.exe"
$vsReleaseChannelPath="./VisualStudio.chman"
$vsBuildtoolsDownloadURL="https://aka.ms/vs/16/release/vs_buildtools.exe"
$vsChannelDownloadURL="https://aka.ms/vs/16/release/channel"
Invoke-WebRequest $vsBuildtoolsDownloadURL -OutFile $vsBuildtoolsInstallerPath
Invoke-WebRequest $vsChannelDownloadURL -OutFile $vsReleaseChannelPath
Start-Process /local/vs_buildtools.exe `
    -ArgumentList '--quiet ', '--wait ', '--norestart ', '--nocache', `
    '--installPath C:\BuildTools', `
    '--channelUri C:\local\VisualStudio.chman', `
    '--installChannelUri C:\local\VisualStudio.chman', `
    '--add Microsoft.VisualStudio.Workload.VCTools', `
    '--includeRecommended'  -NoNewWindow -Wait;
Remove-Item $vsBuildtoolsInstallerPath
Remove-Item $vsReleaseChannelPath

# Build Onigmo.
$onigmoBinDir="$toolsDir\onigmo"
$onigmoTarPath="./onigmo-6.1.3.tar.gz"
$onigmoDownloadURL="https://github.com/k-takata/Onigmo/releases/download/Onigmo-6.1.3/onigmo-6.1.3.tar.gz"
Invoke-WebRequest $onigmoDownloadURL -OutFile $onigmoTarPath
tar -C $toolsDir -xvzf $onigmoTarPath
Remove-Item $onigmoTarPath
Move-Item -Path "$toolsDir\onigmo-6.1.3" -Destination $onigmoBinDir
Invoke-Item "$onigmoBinDir\build_nmake.cmd"

# Build the collector.
$ldFlags="-s -w"
if ($jmxHash -ne "") {
    $ldFlags+=" -X github.com/open-telemetry/opentelemetry-collector-contrib/receiver/jmxreceiver.MetricsGathererHash=$jmxHash"
}
$buildCollectorCommand=@"
`$env:GOWORK='off' `$env:CGO_ENABLED=1 `$env:CGO_CFLAGS="-I $onigmoBinDir" `$env:CGO_LDFLAGS="-L $onigmoBinDir\.libs -lonigmo" \; cd _build; $goBin build -p 32 -buildvcs=false -o '{0}/google-cloud-metrics-agent_windows_amd64.exe' --ldflags='{1}' .
"@ -f $outDir, $ldFlags
powershell.exe -Command $buildCollectorCommand
