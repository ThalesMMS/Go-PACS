#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="${DIST_DIR:-"$ROOT_DIR/dist"}"
APP_NAME="${APP_NAME:-Go PACS}"
APP_ID="${APP_ID:-com.thalesmms.gopacs}"
FYNE_TOOLS_VERSION="${FYNE_TOOLS_VERSION:-v1.7.2}"
TARGET_OS="${TARGET_OS:-darwin}"
BINARY_NAME="pacs-gui"
ICON_PATH="$ROOT_DIR/Go-PACS.png"
APP_BUNDLE="$DIST_DIR/$APP_NAME.app"
ROOT_APP_BUNDLE="$ROOT_DIR/$APP_NAME.app"

if [[ "$TARGET_OS" != "darwin" ]]; then
	echo "TARGET_OS must be darwin to create a double-clickable macOS .app bundle" >&2
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

rm -rf "$APP_BUNDLE" "$ROOT_APP_BUNDLE" "$DIST_DIR/$BINARY_NAME"
mkdir -p "$DIST_DIR"

echo "Building $DIST_DIR/$BINARY_NAME"
go build -trimpath -o "$DIST_DIR/$BINARY_NAME" ./cmd/pacs-gui

echo "Packaging $APP_BUNDLE"
(
	cd "$ROOT_DIR"
	"${fyne_cmd[@]}" package \
		--target "$TARGET_OS" \
		--executable "$DIST_DIR/$BINARY_NAME" \
		--name "$APP_NAME" \
		--app-id "$APP_ID" \
		--icon "$ICON_PATH" \
		--release
)

mv "$ROOT_APP_BUNDLE" "$APP_BUNDLE"
rm -f "$DIST_DIR/$BINARY_NAME"

if [[ ! -d "$APP_BUNDLE" ]]; then
	echo "Package step did not create $APP_BUNDLE" >&2
	exit 1
fi

if [[ ! -d "$APP_BUNDLE/Contents/MacOS" ]]; then
	echo "Package step created an invalid .app bundle: missing Contents/MacOS" >&2
	exit 1
fi

echo "Created $APP_BUNDLE"
