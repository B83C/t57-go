import { chromium } from 'playwright';
import { spawn } from 'child_process';
import { fileURLToPath } from 'url';
import path from 'path';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const PROJECT_ROOT = path.resolve(__dirname, '..');
const PORT = 17426;

function waitForServer(url, timeout = 15000) {
  return new Promise((resolve, reject) => {
    const start = Date.now();
    const check = () => {
      if (Date.now() - start > timeout) return reject(new Error('timeout'));
      fetch(url).then(r => r.ok ? resolve() : setTimeout(check, 200)).catch(() => setTimeout(check, 200));
    };
    check();
  });
}

async function main() {
  let passed = 0, failed = 0;
  const assert = (label, ok, detail) => { if (ok) { passed++; console.log(`  ✓ ${label}`); } else { failed++; console.log(`  ✗ ${label}: ${detail}`); } };

  console.log('Building...');
  const { execSync } = await import('child_process');
  execSync('go build -ldflags="-s -w" -o dist/t57 ./cmd/t57/', { cwd: PROJECT_ROOT, stdio: 'pipe' });

  console.log('Starting server...');
  const server = spawn(path.join(__dirname, 'dist/t57'), ['serve', '--listen', `:${PORT}`], {
    cwd: __dirname, stdio: 'pipe',
  });
  server.stderr.on('data', d => process.stderr.write(d));

  try {
    await waitForServer(`http://localhost:${PORT}`);
    console.log('Server up.');

    const browser = await chromium.launch({ headless: true });
    try {
      const context = await browser.newContext();

      // Inject mock Web Serial before page loads
      await context.addInitScript(() => {
        class MockPort {
          constructor() {
            this._open = false;
            this._rx = new Uint8Array(0);
            this._rxPos = 0;
            this._writer = null;
          }
          getInfo() { return { usbVendorId: 0x0483, usbProductId: 0x5740 }; }

          async open(opts) {
            this._open = true;
            this._readable = new ReadableStream({
              pull: (ctrl) => {
                if (this._rxPos < this._rx.length) {
                  const chunk = this._rx.slice(this._rxPos, this._rxPos + 64);
                  this._rxPos += chunk.length;
                  ctrl.enqueue(chunk);
                } else {
                  ctrl.close();
                }
              }
            });
            this._writable = new WritableStream({ write: () => {} });
          }
          get readable() { return this._readable; }
          get writable() { return this._writable; }
          async close() { this._open = false; }
        }

        const mockPort = new MockPort();
        window.__mockPort = mockPort;
        window.__mockSetResponse = (bytes) => {
          mockPort._rx = new Uint8Array(bytes);
          mockPort._rxPos = 0;
        };

        navigator.serial = {
          requestPort: async () => mockPort,
          getPorts: async () => [mockPort],
        };
      });

      const page = await context.newPage();
      page.on('console', msg => {
        if (msg.type() === 'error') console.log('  [browser error]', msg.text());
      });
      page.on('pageerror', err => console.log('  [page error]', err.message));

      await page.goto(`http://localhost:${PORT}`, { waitUntil: 'networkidle', timeout: 15000 });
      await page.waitForTimeout(500);

      // Check the page loaded the pure JS version (no WASM)
      const subtitle = await page.textContent('.sub');
      assert('Pure JS version loaded', subtitle.includes('pure JS'), subtitle);

      // Test click Connect with mock port — should show "Connected"
      console.log('  clicking Connect...');
      await page.click('#btnConn');
      await page.waitForTimeout(500);
      const statusText = await page.textContent('#status');
      assert('Connected status shown', statusText === 'Connected', statusText);

      // Set up mock response for Read All (page-only cascade: 6 blocks)
      const page0resp = (() => {
        const data = [
          0xA6,0x44,0xD9,0xFB, 0x0B,0x34,0xD2,0x4F,
          0x02,0x01,0x10,0x4F, 0x63,0x63,0xF0,0x4F,
          0x02,0x01,0xF0,0xA0, 0xFF,0x61,0x8A,0xC6
        ];
        const body = [0x00, 1 + data.length, 0x00, ...data];
        let bcc = 0; for (const b of body) bcc ^= b;
        return [0xAA, ...body, bcc, 0xBB];
      })();

      // Config block response
      const cfgResp = (() => {
        const data = [0x00, 0x08, 0x80, 0xE8];
        const body = [0x00, 1 + data.length, 0x00, ...data];
        let bcc = 0; for (const b of body) bcc ^= b;
        return [0xAA, ...body, bcc, 0xBB];
      })();

      // Push cascade + config responses
      await page.evaluate(([p, c]) => {
        window.__mockSetResponse(new Uint8Array([...p, ...c]));
      }, [page0resp, cfgResp]);

      // Click Read All
      console.log('  clicking Read All...');
      await page.click('#btnRead');
      await page.waitForTimeout(1000);

      // Check that blocks were filled
      const block1val = await page.$eval('#b1_0', el => el.value);
      const block2val = await page.$eval('#b2_0', el => el.value);
      assert('Block 1 first byte = A6', block1val === 'A6', `got ${block1val}`);
      assert('Block 2 first byte = 0B', block2val === '0B', `got ${block2val}`);

      // Test edit: change block 1 first byte
      await page.fill('#b1_0', 'FF');
      await page.waitForTimeout(100);
      const changed = await page.$eval('#s1', el => el.textContent);
      assert('Block 1 marked as CHANGED', changed === 'CHANGED', changed);

      console.log(`\n${passed} passed, ${failed} failed`);
    } finally {
      await browser.close();
    }
  } finally {
    server.kill();
  }
  if (failed > 0) process.exit(1);
}

main().catch(e => { console.error(e); process.exit(1); });
