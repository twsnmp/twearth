#!/usr/bin/env bash
set -euo pipefail

info() {
  echo -e "\033[1;34m[INFO]\033[0m $1"
}

success() {
  echo -e "\033[1;32m[SUCCESS]\033[0m $1"
}

error() {
  echo -e "\033[1;31m[ERROR]\033[0m $1" >&2
}

warn() {
  echo -e "\033[1;33m[WARNING]\033[0m $1"
}

VERSION_ARG="${1:-}"

for tool in wails3 codesign pkgbuild productsign xcrun ditto plutil security shasum; do
  if ! command -v "$tool" &> /dev/null; then
    error "$tool is required but not installed. Exiting."
    exit 1
  fi
done

info "Starting macOS Universal Release Build and Packaging Process for twearth"

# Resolve version from argument, env, or Git tag
if [ -n "$VERSION_ARG" ]; then
  VERSION="$VERSION_ARG"
elif [ -n "${RELEASE_VERSION:-}" ]; then
  VERSION="$RELEASE_VERSION"
else
  VERSION=$(git describe --tags --abbrev=0 2>/dev/null || echo "v0.1.0")
fi
info "Build version: ${VERSION}"

# Step 1: Build Universal Binary & .app bundle via Wails v3
export GOEXPERIMENT=simd
info "Building Universal Binary (arm64 + amd64) and .app bundle..."
wails3 task darwin:package:universal VERSION="${VERSION}"

ORIG_APP_PATH="bin/twearth.app"
if [ ! -d "$ORIG_APP_PATH" ]; then
  error "Build failed: $ORIG_APP_PATH not found."
  exit 1
fi

mkdir -p dist
APP_PATH="dist/twearth.app"
rm -rf "$APP_PATH"
mv "$ORIG_APP_PATH" "$APP_PATH"
xattr -cr "$APP_PATH"
success "Universal .app bundle prepared at $APP_PATH"

# Step 2: Select Application Signing Identity
APP_IDENTITY=${MACOS_APP_IDENTITY:-""}
if [ -z "$APP_IDENTITY" ]; then
  info "MACOS_APP_IDENTITY is not set. Scanning Keychain for 'Developer ID Application' certificates..."
  identities=$(security find-identity -v)
  app_certs=()
  while IFS= read -r line; do
    if [[ "$line" =~ \"(Developer\ ID\ Application:[^\"]+)\" ]]; then
      app_certs+=("${BASH_REMATCH[1]}")
    fi
  done <<< "$identities"

  if [ ${#app_certs[@]} -eq 0 ]; then
    error "No valid 'Developer ID Application' certificates found in Keychain."
    exit 1
  elif [ ${#app_certs[@]} -eq 1 ]; then
    APP_IDENTITY="${app_certs[0]}"
    info "Automatically selected: $APP_IDENTITY"
  else
    info "Multiple certificates found. Please select one:"
    for i in "${!app_certs[@]}"; do
      echo "  $((i+1))) ${app_certs[i]}"
    done
    while true; do
      read -r -p "Select certificate number (1-${#app_certs[@]}): " cert_idx < /dev/tty
      if [[ "$cert_idx" =~ ^[0-9]+$ ]] && [ "$cert_idx" -ge 1 ] && [ "$cert_idx" -le "${#app_certs[@]}" ]; then
        APP_IDENTITY="${app_certs[$((cert_idx-1))]}"
        break
      fi
      warn "Invalid selection."
    done
  fi
fi
info "Selected Application Identity: $APP_IDENTITY"

# Step 3: Sign the .app bundle with Hardened Runtime
info "Signing .app bundle with Hardened Runtime..."
codesign --force --options runtime --timestamp --sign "$APP_IDENTITY" --deep "$APP_PATH"
codesign --verify --deep --strict "$APP_PATH"
success "Application bundle signed and verified."

# Step 4: Determine Notarization Profile
NOTARY_PROFILE=${MACOS_NOTARY_PROFILE:-""}
if [ -z "$NOTARY_PROFILE" ]; then
  info "MACOS_NOTARY_PROFILE is not set."
  read -r -p "Enter Apple Notarization profile name (or press Enter to skip notarization & upload): " NOTARY_PROFILE < /dev/tty
  if [ -z "$NOTARY_PROFILE" ]; then
    warn "Notarization profile not provided. Skipping notarization and upload."
    exit 0
  fi
fi

# Step 5: Notarize .app and create self-update .zip
NOTARY_ZIP="dist/twearth-notary.zip"
UPDATE_ZIP="dist/twearth-darwin-universal.zip"
rm -f "$NOTARY_ZIP" "$UPDATE_ZIP"

info "Creating temporary zip for .app notarization..."
ditto -c -k --keepParent "$APP_PATH" "$NOTARY_ZIP"

info "Submitting .app to Apple Notarization (profile: $NOTARY_PROFILE)..."
xcrun notarytool submit "$NOTARY_ZIP" --keychain-profile "$NOTARY_PROFILE" --wait
rm -f "$NOTARY_ZIP"

info "Stapling notarization ticket to $APP_PATH..."
xcrun stapler staple "$APP_PATH"

info "Creating self-update archive at $UPDATE_ZIP..."
ditto -c -k --keepParent "$APP_PATH" "$UPDATE_ZIP"
success "Self-update zip created at $UPDATE_ZIP"

# Step 6: Select Installer Signing Identity
PKG_IDENTITY=${MACOS_PKG_IDENTITY:-""}
if [ -z "$PKG_IDENTITY" ]; then
  info "MACOS_PKG_IDENTITY is not set. Scanning Keychain for 'Developer ID Installer' certificates..."
  identities=$(security find-identity -v)
  pkg_certs=()
  while IFS= read -r line; do
    if [[ "$line" =~ \"(Developer\ ID\ Installer:[^\"]+)\" ]]; then
      pkg_certs+=("${BASH_REMATCH[1]}")
    fi
  done <<< "$identities"

  if [ ${#pkg_certs[@]} -eq 0 ]; then
    error "No valid 'Developer ID Installer' certificates found in Keychain."
    exit 1
  elif [ ${#pkg_certs[@]} -eq 1 ]; then
    PKG_IDENTITY="${pkg_certs[0]}"
    info "Automatically selected: $PKG_IDENTITY"
  else
    info "Multiple installer certificates found. Please select one:"
    for i in "${!pkg_certs[@]}"; do
      echo "  $((i+1))) ${pkg_certs[i]}"
    done
    while true; do
      read -r -p "Select installer certificate number (1-${#pkg_certs[@]}): " cert_idx < /dev/tty
      if [[ "$cert_idx" =~ ^[0-9]+$ ]] && [ "$cert_idx" -ge 1 ] && [ "$cert_idx" -le "${#pkg_certs[@]}" ]; then
        PKG_IDENTITY="${pkg_certs[$((cert_idx-1))]}"
        break
      fi
      warn "Invalid selection."
    done
  fi
fi
info "Selected Installer Identity: $PKG_IDENTITY"

# Step 7: Build, sign, and notarize .pkg installer
PKG_ROOT="dist/pkg-root"
COMPONENT_PLIST="dist/components.plist"
UNSIGNED_PKG="dist/twearth-unsigned.pkg"
SIGNED_PKG="dist/twearth-${VERSION}-darwin-universal.pkg"

rm -rf "$PKG_ROOT" "$COMPONENT_PLIST" "$UNSIGNED_PKG" "$SIGNED_PKG"
mkdir -p "$PKG_ROOT"
cp -R "$APP_PATH" "$PKG_ROOT/twearth.app"

info "Creating installer package..."
pkgbuild --analyze --root "$PKG_ROOT" "$COMPONENT_PLIST"
plutil -replace 0.BundleIsRelocatable -bool NO "$COMPONENT_PLIST"
pkgbuild --root "$PKG_ROOT" --component-plist "$COMPONENT_PLIST" --install-location "/Applications" "$UNSIGNED_PKG"
rm -rf "$PKG_ROOT" "$COMPONENT_PLIST"

info "Signing installer package..."
productsign --sign "$PKG_IDENTITY" "$UNSIGNED_PKG" "$SIGNED_PKG"
rm -f "$UNSIGNED_PKG"

info "Submitting installer package for notarization..."
xcrun notarytool submit "$SIGNED_PKG" --keychain-profile "$NOTARY_PROFILE" --wait

info "Stapling notarization ticket to $SIGNED_PKG..."
xcrun stapler staple "$SIGNED_PKG"
success "macOS packaging and notarization completed successfully!"
info "Release artifacts:"
echo "  - $SIGNED_PKG"
echo "  - $UPDATE_ZIP"
info "SHA-256 checksums:"
(cd dist && shasum -a 256 "$(basename "$UPDATE_ZIP")" "$(basename "$SIGNED_PKG")")
