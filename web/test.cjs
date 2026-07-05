const { chromium } = require('playwright');
const { execSync, spawn } = require('child_process');
const path = require('path');

const T57_DIR = path.resolve(__dirname, '..');
const PORT = 17421;

function waitForServer(url, timeout = 30000) {
  const start = Date.now();
  return new Promise((resolve, reject) => {
    const check = () => {
      if (Date.now() - start > timeout) return reject(new Error('timeout'));
      fetch(url).then(r => r.ok ? resolve() : setTimeout(check, 200)).catch(() => setTimeout(check, 200));
    };
    check();
  });
}

function buildResponseData(...data) {
  const body = [0x00, 1 + data.length, 0x00, ...data];
  let bcc = 0; for (const b of body) bcc ^= b;
  return [0xAA, ...body, bcc, 0xBB];
}

async function main() {
  let passed = 0, failed = 0;

  function assert(label, ok, detail) {
    if (ok) { passed++; console.log(`  ✓ ${label}`); }
    else { failed++; console.log(`  ✗ ${label}: ${detail}`); }
  }

  console.log('Building...');
  execSync('go build -ldflags="-s -w" -o dist/t57 ./cmd/t57/', { cwd: T57_DIR, stdio: 'pipe' });
  execSync('GOOS=js GOARCH=wasm go build -ldflags="-s -w" -o web/dist/t57.wasm ./cmd/t57wasm/', { cwd: T57_DIR, stdio: 'pipe' });

  console.log('Starting server...');
  const server = spawn(path.join(T57_DIR, 'dist/t57'), ['serve', '--listen', `:${PORT}`], {
    cwd: T57_DIR, stdio: 'pipe',
  });
  server.stderr.on('data', d => process.stderr.write(d));

  try {
    await waitForServer(`http://localhost:${PORT}`);
    console.log('Server up.');

    const browser = await chromium.launch({ headless: true });
    try {
      const page = await browser.newPage();

      // Mock navigator.serial before page loads
      await page.addInitScript(() => {
        let rxBuffer = new Uint8Array(0);
        let readIndex = 0;

        class MockPort {
          constructor() { this._open = false; this._readable = null; this._writable = null; }
          getInfo() { return { usbVendorId: 0x0483, usbProductId: 0x5740 }; }

          async open(opts) {
            this._open = true;
            const buf = rxBuffer;
            this._readable = new ReadableStream({
              pull(ctrl) {
                if (readIndex < buf.length) {
                  const chunk = buf.slice(readIndex, Math.min(readIndex + 64, buf.length));
                  readIndex += chunk.length;
                  ctrl.enqueue(chunk);
                } else {
                  ctrl.close();
                }
              }
            });
            this._writable = new WritableStream({
              write() { /* discard writes */ }
            });
          }

          get readable() { return this._readable; }
          get writable() { return this._writable; }
          async close() { this._open = false; }
        }

        const mockPort = new MockPort();
        globalThis.__mockPort = mockPort;
        globalThis.__mockSetResponse = (bytes) => { rxBuffer = new Uint8Array(bytes); readIndex = 0; };

        navigator.serial = {
          requestPort: async () => mockPort,
          getPorts: async () => [mockPort],
        };
      });

      page.on('console', msg => console.log('  [page]', msg.type(), msg.text()));
      page.on('pageerror', err => console.log('  [page error]', err.message));

      await page.goto(`http://localhost:${PORT}`, { waitUntil: 'networkidle', timeout: 15000 });
      console.log('  page loaded, title:', await page.title());
      const html = await page.content();
      console.log('  body length:', html.length);

      // Wait for the WASM loading div to disappear (appears)
      try {
        await page.waitForSelector('#app', { state: 'visible', timeout: 20000 });
        console.log('  app visible');
      } catch(e) {
        console.log('  app not visible, checking loading state:');
        const loading = await page.textContent('#loading');
        console.log('  loading:', loading);
      }
      console.log('  checking for t57Init...');
      await page.waitForFunction(() => typeof t57Init !== 'undefined', { timeout: 15000 });
      console.log('  t57Init found');
      await page.waitForTimeout(800);

      // Test 1: Page loads, WASM initialized
      {
        const init = await page.evaluate(() => t57Init());
        assert('WASM initializes', !init.error, init.error);
      }

      // Test 2: Connect with mock
      {
        const snResp = buildResponseData(0x44, 0x58, 0x5f, 0x4d, 0x31, 0x32, 0x35, 0x00);
        await page.evaluate(r => globalThis.__mockSetResponse(r), snResp);
        const result = await page.evaluate(() => t57ConnectPort(globalThis.__mockPort, 9600));
        const r = result.ok || result;
        assert('connect succeeds', r && r.success, r ? r.error : JSON.stringify(result));
        if (r && r.success) {
          assert('serial number returned', r.serial === '44585F4D31323500', `got ${r.serial}`);
        }
      }

      // Test 3: ReadAll
      {
        const cfgResp = buildResponseData(0x00, 0x08, 0x80, 0xE8);
        const blk1 = buildResponseData(0xA0, 0xB0, 0xC0, 0xD0);
        const blk2 = buildResponseData(0xA1, 0xB1, 0xC1, 0xD1);
        const blk3 = buildResponseData(0xA2, 0xB2, 0xC2, 0xD2);
        const blk4 = buildResponseData(0xA3, 0xB3, 0xC3, 0xD3);
        const blk5 = buildResponseData(0xA4, 0xB4, 0xC4, 0xD4);
        const blk6 = buildResponseData(0xA5, 0xB5, 0xC5, 0xD5);
        const blk7 = buildResponseData(0xA6, 0xB6, 0xC6, 0xD6);
        const all = [...cfgResp, ...blk1, ...blk2, ...blk3, ...blk4, ...blk5, ...blk6, ...blk7];
        await page.evaluate(r => globalThis.__mockSetResponse(r), all);

        const result = await page.evaluate(() => t57ReadAll());
        const r = result.ok || result;
        assert('readAll succeeds', Array.isArray(r), typeof r);
        if (Array.isArray(r)) {
          assert('readAll returns 8 blocks', r.length === 8, `got ${r.length}`);
          assert('config = 000880E8', r[0].hex === '000880E8', `got ${r[0].hex}`);
          assert('block 1 = A0B0C0D0', r[1].hex === 'A0B0C0D0', `got ${r[1].hex}`);
        }
      }

      // Test 4: WriteBlock
      {
        const writeResp = [0xAA, 0x00, 0x02, 0x00, 0x80, 0x82, 0xBB];
        await page.evaluate(r => globalThis.__mockSetResponse(r), writeResp);
        const result = await page.evaluate(() => t57WriteBlock(1, 'DEADBEEF'));
        const r = result.ok || result;
        assert('writeBlock succeeds', !(r && r.error), JSON.stringify(r));
      }

      // Test 5: WriteBlocks (for the hex editor's write-changed)
      {
        const resp = [0xAA, 0x00, 0x02, 0x00, 0x80, 0x82, 0xBB];
        await page.evaluate(r => globalThis.__mockSetResponse(r), resp);
        const result = await page.evaluate(() => t57WriteBlocks([{block:1, hex:'DEADBEEF'}]));
        const r = result.ok || result;
        assert('writeBlocks succeeds', !(r && r.error), JSON.stringify(r));
      }

    } finally {
      await browser.close();
    }
  } finally {
    server.kill();
  }

  console.log(`\n${passed} passed, ${failed} failed`);
  process.exit(failed > 0 ? 1 : 0);
}

main().catch(e => { console.error(e); process.exit(1); });
