#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

echo "==> Building WASM module..."
GOOS=js GOARCH=wasm go build -ldflags="-s -w" -o web/dist/t57.wasm ./cmd/t57wasm/

echo "==> Copying WASM runtime + assets into CLI embed dir..."
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" web/dist/
mkdir -p cmd/t57/web/dist
cp web/index.html cmd/t57/web/
cp web/dist/t57.wasm web/dist/wasm_exec.js cmd/t57/web/dist/

echo "==> Building native CLI with embedded web assets..."
go build -ldflags="-s -w" -o dist/t57 ./cmd/t57/

echo "==> Preparing web deploy bundle..."
rm -rf public
mkdir -p public/dist
cp web/index.html web/_headers public/
cp web/dist/t57.wasm web/dist/wasm_exec.js public/dist/

ls -lh dist/t57 web/dist/t57.wasm web/dist/wasm_exec.js public/
echo ""
echo "Done."
echo ""
echo "  Local CLI:    ./dist/t57"
echo "  Local web:    ./dist/t57 serve"
echo "  Cloudflare:   npx wrangler pages deploy web/"
echo "                 or upload web/ to Cloudflare Pages dashboard"
