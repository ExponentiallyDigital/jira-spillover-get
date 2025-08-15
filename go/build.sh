#!/bin/bash

# Build script with automatic version synchronization
# Extracts version from Go source and updates versioninfo.json before building

set -euo pipefail

show_help() {
  echo
  echo "Build script for jira-spillover-get"
  echo
  echo "Usage: ./build.sh [options]"
  echo
  echo "Options:"
  echo "  --help, -help, -?    Show this help message"
  echo
  echo "This script will:"
  echo "  1. Extract version from jira-spillover-get.go"
  echo "  2. Update versioninfo.json with the extracted version"
  echo "  3. Run go generate to update version resources"
  echo "  4. Build Windows, macOS, and Linux binaries"
  echo "  5. Output all binaries to ../build"
  echo
  exit 0
}

case "${1:-}" in
  --help|-help|-\?|/?) show_help ;;
esac

echo "Building jira-spillover-get..."

# Ensure we're in the correct directory
if [[ ! -f "jira-spillover-get.go" ]]; then
  echo "Error: Must be run from the project root directory"
  exit 1
fi

echo "Extracting version from Go source..."
version_line=$(grep -E 'programVersion[[:space:]]*=' jira-spillover-get.go \
  | head -n1 \
  | sed -E 's/.*=[[:space:]]*"([^"]+)".*/\1/' \
  | tr -d '\r')

current_version="$version_line"

if [[ -z "$current_version" ]]; then
  echo "Error: Could not extract version from jira-spillover-get.go"
  exit 1
fi

echo "Detected version: $current_version"

IFS='.' read -r major minor patch <<< "$current_version"
full_version="${current_version}.0"

echo "Updating versioninfo.json to version $current_version..."

# update JSON using awk without breaking formatting
awk -v major="$major" -v minor="$minor" -v patch="$patch" -v full="$full_version" '
  BEGIN {
    in_fixed = 0
    in_string = 0
  }

  { sub(/\r$/, "") }  # strip CR if present

  /"FixedFileInfo"[[:space:]]*:/ { in_fixed = 1; in_string = 0 }
  /"StringFileInfo"[[:space:]]*:/ { in_fixed = 0; in_string = 1 }
  /"VarFileInfo"[[:space:]]*:/ { in_string = 0 }

  {
    if (in_fixed && $0 ~ /"Major"[[:space:]]*:/) sub(/: *[0-9]+/, ": " major)
    if (in_fixed && $0 ~ /"Minor"[[:space:]]*:/) sub(/: *[0-9]+/, ": " minor)
    if (in_fixed && $0 ~ /"Patch"[[:space:]]*:/) sub(/: *[0-9]+/, ": " patch)

    if (in_string && $0 ~ /"FileVersion"[[:space:]]*:/) sub(/: *"[^"]*"/, ": \"" full "\"")
    if (in_string && $0 ~ /"ProductVersion"[[:space:]]*:/) sub(/: *"[^"]*"/, ": \"" full "\"")
  }

  { print }
' versioninfo.json > versioninfo.json.tmp && mv versioninfo.json.tmp versioninfo.json

echo "Updated versioninfo.json with version $current_version"

echo "Running go generate..."
go generate
echo "go generate completed successfully"

# Create build output folder
BUILD_DIR="../build"
mkdir -p "$BUILD_DIR"

echo "Building Windows binary..."
GOOS=windows GOARCH=amd64 go build -o "$BUILD_DIR/jira-spillover-get.exe"

echo "Building macOS binary..."
GOOS=darwin GOARCH=amd64 go build -o "$BUILD_DIR/jira-spillover-get-macos" jira-spillover-get.go

echo "Building Linux binary..."
GOOS=linux GOARCH=amd64 go build -o "$BUILD_DIR/jira-spillover-get-linux" jira-spillover-get.go

echo "All builds completed successfully."
echo "Output folder: $BUILD_DIR"

# Show file details
for file in "$BUILD_DIR"/*; do
  echo "File: $(basename "$file")"
  if command -v stat &> /dev/null; then
    if stat --version &> /dev/null; then
      # GNU stat (Linux)
      echo "  Size: $(stat -c%s "$file") bytes"
      echo "  Modified: $(stat -c%y "$file" | cut -d'.' -f1)"
    else
      # BSD stat (macOS)
      echo "  Size: $(stat -f%z "$file") bytes"
      echo "  Modified: $(stat -f%Sm "$file")"
    fi
  fi
done

echo "Build process completed."
