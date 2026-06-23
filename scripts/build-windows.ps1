$ErrorActionPreference = "Stop"

$ROOT_DIR = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$DIST_DIR = if ($env:DIST_DIR) { $env:DIST_DIR } else { Join-Path $ROOT_DIR "dist" }
$APP_NAME = if ($env:APP_NAME) { $env:APP_NAME } else { "Go PACS" }
$APP_ID = if ($env:APP_ID) { $env:APP_ID } else { "com.thalesmms.gopacs" }
$FYNE_TOOLS_VERSION = if ($env:FYNE_TOOLS_VERSION) { $env:FYNE_TOOLS_VERSION } else { "v1.7.2" }
$TARGET_OS = if ($env:TARGET_OS) { $env:TARGET_OS } else { "windows" }
$GUI_BINARY = "pacs-gui.exe"
$BINARIES = @("pacs-gui", "pacs-web", "pacs-receiver")
$ICON_PATH = Join-Path $ROOT_DIR "go-pacs.png"

if ($TARGET_OS -ne "windows") {
    Write-Error "TARGET_OS must be windows to create a Windows build"
    exit 2
}

if (-not (Test-Path $ICON_PATH)) {
    Write-Error "Missing icon: $ICON_PATH"
    exit 2
}

# Check if fyne command is available
$fyneCmd = Get-Command fyne -ErrorAction SilentlyContinue
if ($fyneCmd) {
    $fyneArgs = @("fyne")
} else {
    $fyneArgs = @("go", "run", "fyne.io/tools/cmd/fyne@$FYNE_TOOLS_VERSION")
}

# Clean dist directory
if (Test-Path $DIST_DIR) {
    Remove-Item -Recurse -Force $DIST_DIR
}
New-Item -ItemType Directory -Path $DIST_DIR -Force | Out-Null

foreach ($bin in $BINARIES) {
    $output = Join-Path $DIST_DIR "$bin.exe"
    Write-Host "Building $output"
    go build -trimpath -o $output "./cmd/$bin"
}

Write-Host "Packaging for Windows"
Push-Location $ROOT_DIR
$guiPath = Join-Path $DIST_DIR $GUI_BINARY
& $fyneArgs package `
    --target $TARGET_OS `
    --executable $guiPath `
    --name $APP_NAME `
    --app-id $APP_ID `
    --icon $ICON_PATH `
    --release
Pop-Location

Remove-Item -Force $guiPath -ErrorAction SilentlyContinue

Write-Host "Windows build complete in $DIST_DIR"
foreach ($bin in $BINARIES) {
    if ($bin -eq "pacs-gui") {
        continue
    }
    Write-Host "  $(Join-Path $DIST_DIR "$bin.exe")"
}
