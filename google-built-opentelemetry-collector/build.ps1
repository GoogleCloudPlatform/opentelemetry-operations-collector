$ErrorActionPreference = "Stop"

if (-not $env:_WINDOWS_VERSION) {
    Write-Error "_WINDOWS_VERSION environment variable is required but not set."
    exit 1
}

# Determine the artifacts output directory relative to $env:KOKORO_ARTIFACTS_DIR
$ArtifactsDir = if ($env:KOKORO_ARTIFACTS_DIR) { $env:KOKORO_ARTIFACTS_DIR } else { (Get-Item .).FullName }
$TarDir = Join-Path $ArtifactsDir "container"
$TarPath = Join-Path $TarDir "container.tar"

Write-Host "Artifacts directory: $ArtifactsDir"
Write-Host "Target tarball path: $TarPath"

# Make sure the target directory exists
if (-not (Test-Path $TarDir)) {
    New-Item -ItemType Directory -Force -Path $TarDir
}

# Ensure the correct version of Go is installed
$GoVersion = "1.26.7"
$ToolsDir = Join-Path $PSScriptRoot ".tools"
$GoDir = Join-Path $ToolsDir "go"
$GoBinDir = Join-Path $GoDir "bin"
$GoExePath = Join-Path $GoBinDir "go.exe"

$GoUrl = "https://go.dev/dl/go$GoVersion.windows-amd64.zip"

Write-Host "Downloading Go $GoVersion from $GoUrl..."
curl.exe -L $GoUrl -o go.zip

Write-Host "Extracting Go to $ToolsDir..."
unzip.exe -q go.zip -d $ToolsDir
Remove-Item -Force go.zip

# Compile the Go binary on the host (inside the build environment)
Write-Host "Compiling Go binary..."
Set-Location $PSScriptRoot
$env:PATH = "$GoBinDir;$env:PATH"
$env:GOROOT = $GoDir

# Format paths with forward slashes for maximum compatibility with make
$NormalizedPwd = $PSScriptRoot.Replace("\", "/")

& make PWD=$NormalizedPwd --trace TARGETOS=windows TARGETARCH=amd64 build-win
if ($LASTEXITCODE -ne 0) {
    Write-Error "Compilation failed with exit code $LASTEXITCODE"
    exit $LASTEXITCODE
}


# Execute docker build inside the distribution directory
Write-Host "Building Docker image..."
docker build --build-arg WINDOWS_VERSION=$env:_WINDOWS_VERSION -f Dockerfile.windows.build -t otelcol-google-windows:latest .
if ($LASTEXITCODE -ne 0) {
    Write-Error "Docker build failed with exit code $LASTEXITCODE"
    exit $LASTEXITCODE
}

# Save the docker image as a tar archive
Write-Host "Saving Docker image to $TarPath..."
docker save otelcol-google-windows:latest -o $TarPath
if ($LASTEXITCODE -ne 0) {
    Write-Error "Docker save failed with exit code $LASTEXITCODE"
    exit $LASTEXITCODE
}

Write-Host "Windows container build complete."
