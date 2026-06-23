#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="${DIST_DIR:-"$ROOT_DIR/dist"}"
APP_NAME="${APP_NAME:-Go PACS}"
APP_ID="${APP_ID:-com.thalesmms.gopacs}"
FYNE_TOOLS_VERSION="${FYNE_TOOLS_VERSION:-v1.7.2}"
TARGET_OS="${TARGET_OS:-linux}"
GUI_BINARY="pacs-gui"
BINARIES=(pacs-gui pacs-web pacs-receiver)
ICON_PATH="$ROOT_DIR/Go-PACS.png"

if [[ "$TARGET_OS" != "linux" ]]; then
	echo "TARGET_OS must be linux to create a Linux build" >&2
	exit 2
fi

if [[ ! -f "$ICON_PATH" ]]; then
	echo "Missing icon: $ICON_PATH" >&2
	exit 2
fi

if command -v fyne >/dev/null 2>&1; then
	fyne_cmd=(fyne)
else
	fyne_cmd=(go run "fyne.io/tools/cmd/fyne@$FYNE_TOOLS_VERSION")
fi

rm -rf "$DIST_DIR"
mkdir -p "$DIST_DIR"

for bin in "${BINARIES[@]}"; do
	echo "Building $DIST_DIR/$bin"
	go build -trimpath -o "$DIST_DIR/$bin" "./cmd/$bin"
done

echo "Packaging for Linux"
(
	cd "$ROOT_DIR"
	"${fyne_cmd[@]}" package \
		--target "$TARGET_OS" \
		--executable "$DIST_DIR/$GUI_BINARY" \
		--name "$APP_NAME" \
		--app-id "$APP_ID" \
		--icon "$ICON_PATH" \
		--release
)

rm -f "$DIST_DIR/$GUI_BINARY"

echo "Linux build complete in $DIST_DIR"
for bin in "${BINARIES[@]}"; do
	if [[ "$bin" == "$GUI_BINARY" ]]; then
		continue
	fi
	echo "  $DIST_DIR/$bin"
done
