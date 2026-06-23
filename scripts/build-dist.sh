#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="${DIST_DIR:-"$ROOT_DIR/dist"}"
APP_NAME="${APP_NAME:-Go PACS}"
APP_ID="${APP_ID:-com.thalesmms.gopacs}"
FYNE_TOOLS_VERSION="${FYNE_TOOLS_VERSION:-v1.7.2}"
TARGET_OS="${TARGET_OS:-darwin}"
GUI_BINARY="pacs-gui"
WEB_BINARY="pacs-web"
BINARIES=(pacs-gui pacs-web pacs-receiver)
ICON_PATH="$ROOT_DIR/Go-PACS.png"
APP_BUNDLE="$DIST_DIR/$APP_NAME.app"
ROOT_APP_BUNDLE="$ROOT_DIR/$APP_NAME.app"
WEB_APP_NAME="${WEB_APP_NAME:-Go PACS Web}"
WEB_APP_ID="${WEB_APP_ID:-com.thalesmms.gopacs.web}"
WEB_APP_BUNDLE="$DIST_DIR/$WEB_APP_NAME.app"
ROOT_WEB_APP_BUNDLE="$ROOT_DIR/$WEB_APP_NAME.app"

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

for bin in "${BINARIES[@]}"; do
	rm -f "$DIST_DIR/$bin"
done
rm -rf "$APP_BUNDLE" "$ROOT_APP_BUNDLE" "$WEB_APP_BUNDLE" "$ROOT_WEB_APP_BUNDLE"
mkdir -p "$DIST_DIR"

for bin in "${BINARIES[@]}"; do
	echo "Building $DIST_DIR/$bin"
	if [[ "$bin" == "$WEB_BINARY" ]]; then
		CGO_ENABLED=1 go build -trimpath -o "$DIST_DIR/$bin" "./cmd/$bin"
	else
		go build -trimpath -o "$DIST_DIR/$bin" "./cmd/$bin"
	fi
done

echo "Packaging $APP_BUNDLE"
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

mv "$ROOT_APP_BUNDLE" "$APP_BUNDLE"
rm -f "$DIST_DIR/$GUI_BINARY"

echo "Packaging $WEB_APP_BUNDLE"
(
	cd "$ROOT_DIR"
	"${fyne_cmd[@]}" package \
		--target "$TARGET_OS" \
		--executable "$DIST_DIR/$WEB_BINARY" \
		--name "$WEB_APP_NAME" \
		--app-id "$WEB_APP_ID" \
		--icon "$ICON_PATH" \
		--release
)

mv "$ROOT_WEB_APP_BUNDLE" "$WEB_APP_BUNDLE"
rm -f "$DIST_DIR/$WEB_BINARY"

if [[ ! -d "$APP_BUNDLE" ]]; then
	echo "Package step did not create $APP_BUNDLE" >&2
	exit 1
fi

if [[ ! -d "$APP_BUNDLE/Contents/MacOS" ]]; then
	echo "Package step created an invalid .app bundle: missing Contents/MacOS" >&2
	exit 1
fi

if [[ ! -d "$WEB_APP_BUNDLE" ]]; then
	echo "Package step did not create $WEB_APP_BUNDLE" >&2
	exit 1
fi

if [[ ! -d "$WEB_APP_BUNDLE/Contents/MacOS" ]]; then
	echo "Package step created an invalid web .app bundle: missing Contents/MacOS" >&2
	exit 1
fi

echo "Created $APP_BUNDLE"
echo "Created $WEB_APP_BUNDLE"
echo "Created $DIST_DIR/pacs-receiver"
