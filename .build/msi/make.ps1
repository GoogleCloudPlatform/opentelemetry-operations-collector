# Copyright 2020 Google LLC
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

<#
.SYNOPSIS
    Makefile like build commands for commands that can only be run on
    Windows.
    
    Usage:
        .\make.ps1 [-Arch <Arch>] <Command> [-<Param> <Value> ...]
    Examples:
        .\make.ps1 New-MSI
        .\make.ps1 -Arch x86 New-MSI -Config "./config.yaml"
.PARAMETER Target
    Build target to run (Install-Tools, New-MSI, Confirm-MSI)
#>
Param(
    [Parameter(Mandatory=$false)][string]$Arch,
    [Parameter(Position=0,Mandatory=$true, ValueFromRemainingArguments=$true)][string]$Target
)

$ErrorActionPreference = "Stop"

# read PKG_VERSION from VERSION file
$PkgVersion = Select-String -Path "VERSION" -Pattern "^PKG_VERSION=(.*)$" | %{$_.Matches.Groups[1].Value}

# if ARCH is not supplied, set default value based on user's system
if (!$Arch) {
    $Arch = (&{If([System.Environment]::Is64BitProcess) {"x86_64"} Else {"x86"}})
}

# set GOARCH & CANDLEARCH based on ARCH
switch($Arch) {
    "x86_64" { $GoArch = "amd64"; $CandleArch = "x64"; break}
    "x86"    { $GoArch = "386";   $CandleArch = "x86"; break}
    default  { Throw "Arch must be set to one of: x86, x86_64" }
}

function Install-Tools {
    choco install wixtoolset -y
    setx /m PATH "%PATH%;C:\Program Files (x86)\WiX Toolset v3.11\bin"
    refreshenv
}

function New-MSI(
    [string]$Config="./config/config-windows.yaml"
) {
    candle -arch "$CandleArch" -dPkgVersion="$PkgVersion" -dGoArch="$GoArch" -dConfig="$Config" .build/msi/google-cloud-metrics-agent.wxs
    light google-cloud-metrics-agent.wixobj

    New-Item -Force -Type dir dist
    Move-Item -Force google-cloud-metrics-agent.msi dist/google-cloud-metrics-agent.msi
}

function Confirm-MSI {
    # ensure system32 is in Path so we can use executables like msiexec & sc
    $env:Path += ";C:\Windows\System32"

    # install msi, validate service is installed & running
    Start-Process -Wait msiexec "/i `"$pwd\dist\google-cloud-metrics-agent.msi`" /qn"
    sc.exe query state=all | findstr "google-cloud-metrics-agent" | Out-Null
    if ($LASTEXITCODE -ne 0) { Throw "google-cloud-metrics-agent service failed to install" }

    # stop service
    Stop-Service google-cloud-metrics-agent

    # start service
    Start-Service google-cloud-metrics-agent

    # uninstall msi, validate service is uninstalled
    Start-Process -Wait msiexec "/x `"$pwd\dist\google-cloud-metrics-agent.msi`" /qn"
    sc.exe query state=all | findstr "google-cloud-metrics-agent" | Out-Null
    if ($LASTEXITCODE -ne 1) { Throw "google-cloud-metrics-agent service failed to uninstall" }
}

$sb = [scriptblock]::create("$Target")
Invoke-Command -ScriptBlock $sb
