#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

echo "==> Building native CLI with embedded web UI..."
cp web/index.html cmd/t57/web/
go build -ldflags="-s -w" -o dist/t57 ./cmd/t57/

echo "==> Preparing web deploy bundle..."
rm -rf public
mkdir -p public
cp web/index.html web/_headers public/

ls -lh dist/t57 web/dist/t57.wasm web/dist/wasm_exec.js public/
echo ""
echo "Done."
echo ""
echo "  Local CLI:    ./dist/t57"
echo "  Local web:    ./dist/t57 serve"
echo "  Cloudflare:   npx wrangler pages deploy web/"
echo "                 or upload web/ to Cloudflare Pages dashboard"
