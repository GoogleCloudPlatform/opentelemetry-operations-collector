# Copyright 2020, Google Inc.
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

$ErrorActionPreference = 'Stop'

# mirror osconfig agent settings
function Set-ServiceConfig {
    # restart after 1s then 2s, reset error counter after 60s
    sc.exe failure google-cloudops-opentelemetry-collector reset=60 actions=restart/1000/restart/2000
    # set delayed start
    sc.exe config google-cloudops-opentelemetry-collector depend="rpcss" start=delayed-auto
    # create trrigger to start the service on first IP address
    sc.exe triggerinfo google-cloudops-opentelemetry-collector start/networkon
}

try
{
    if (-not (Get-Service 'google-cloudops-opentelemetry-collector' -ErrorAction SilentlyContinue))
    {
        New-Service -DisplayName 'Google Cloud Operations OpenTelemetry Collector' `
            -Name 'google-cloudops-opentelemetry-collector' `
            -BinaryPathName '%ProgramFiles%\Google\Cloud Operations\OpenTelemetry Collector\opentelemetry-collector.exe --config="C:\Users\jbebb\go\src\go.opentelemetry.io\collector\examples\otel-local-config.yaml"' `
            -StartupType AutomaticDelayedStart `
            -Description 'Google Cloud Operations OpenTelemetry Collector based Monitoring Agent'

        Set-ServiceConfig
        Start-Service 'google-cloudops-opentelemetry-collector' -Verbose -ErrorAction Stop
    }
    else
    {
        Set-ServiceConfig
        Restart-Service -Name google-cloudops-opentelemetry-collector
    }
}
catch
{
    Write-Output $_.InvocationInfo.PositionMessage
    Write-Output "Install failed: $($_.Exception.Message)"
    exit 1
}
