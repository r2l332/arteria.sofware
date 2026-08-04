#!/bin/bash
# Build Arteria Capillary agent for all platforms
# Produces standalone binaries — no Docker required

set -e

VERSION="${VERSION:-0.1.0}"
OUTPUT_DIR="${OUTPUT_DIR:-./dist}"
SOURCE="./cmd/tunnel-agent"

mkdir -p "$OUTPUT_DIR"

echo "Building Arteria Capillary v${VERSION}"
echo "=================================="

platforms=(
  "linux/amd64"
  "linux/arm64"
  "windows/amd64"
  "darwin/amd64"
  "darwin/arm64"
)

for platform in "${platforms[@]}"; do
  GOOS="${platform%/*}"
  GOARCH="${platform#*/}"
  output="$OUTPUT_DIR/capillary-${VERSION}-${GOOS}-${GOARCH}"

  if [ "$GOOS" = "windows" ]; then
    output="${output}.exe"
  fi

  echo "  Building ${GOOS}/${GOARCH}..."
  CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" go build -ldflags="-s -w -X main.version=${VERSION}" -o "$output" "$SOURCE"
done

echo ""
echo "Binaries:"
ls -lh "$OUTPUT_DIR"/capillary-*
echo ""
echo "Done. Distribute the appropriate binary to each remote site."
echo ""
echo "Usage:"
echo "  ./capillary enroll <token> --broker arteria.software:9443"
echo "  ./capillary connect --broker arteria.software:9443"
