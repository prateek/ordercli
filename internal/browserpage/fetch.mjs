import fs from 'node:fs';
import { chromium } from 'playwright';

const outputPath = process.env.ORDERCLI_OUTPUT_PATH;
if (!outputPath) {
  process.stderr.write('ORDERCLI_OUTPUT_PATH missing\n');
  process.exit(2);
}

async function readStdinJSON() {
  const chunks = [];
  for await (const chunk of process.stdin) chunks.push(chunk);
  const raw = Buffer.concat(chunks).toString('utf8').trim();
  if (!raw) throw new Error('stdin empty');
  return JSON.parse(raw);
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

const input = await readStdinJSON();
const patterns = Array.isArray(input.capture_response_url_substrings)
  ? input.capture_response_url_substrings.filter(Boolean)
  : [];
const waitForURLSubstrings = Array.isArray(input.wait_for_url_substrings)
  ? input.wait_for_url_substrings.filter(Boolean)
  : [];
const captureBodyBytes = Math.max(0, Number(input.capture_response_body_bytes || 0));
const capturedResponses = [];

let browser = null;
let context = null;
if (input.profile_dir) {
  context = await chromium.launchPersistentContext(input.profile_dir, {
    headless: input.headless !== false,
  });
} else {
  browser = await chromium.launch({ headless: input.headless !== false });
  context = await browser.newContext();
}
const page = await context.newPage();

if (patterns.length > 0) {
  context.on('response', async (response) => {
    const url = response.url();
    if (!patterns.some((pattern) => url.includes(pattern))) {
      return;
    }

    const headers = response.headers();
    const contentType = headers['content-type'] || '';
    if (!contentType.toLowerCase().includes('application/json')) {
      return;
    }

    let body = '';
    try {
      body = await response.text();
    } catch {}
    if (captureBodyBytes > 0 && body.length > captureBodyBytes) {
      body = body.slice(0, captureBodyBytes);
    }

    capturedResponses.push({
      url,
      status: response.status(),
      content_type: contentType,
      body,
    });
  });
}

try {
  await page.goto(input.url, {
    waitUntil: 'domcontentloaded',
    timeout: Math.max(10_000, Number(input.timeout_millis || 0)),
  });
  await page.waitForLoadState('networkidle', { timeout: 15_000 }).catch(() => {});

  if (waitForURLSubstrings.length > 0) {
    const deadline = Date.now() + Math.max(10_000, Number(input.timeout_millis || 0));
    while (Date.now() < deadline) {
      const currentURL = page.url();
      if (waitForURLSubstrings.some((pattern) => currentURL.includes(pattern))) {
        break;
      }
      await sleep(1000);
    }
  }

  const body = await page.locator('body').innerText();
  const userAgent = await page.evaluate(() => navigator.userAgent).catch(() => '');

  fs.writeFileSync(
    outputPath,
    JSON.stringify({
      final_url: page.url(),
      title: await page.title(),
      text: body,
      user_agent: userAgent,
      responses: capturedResponses,
    }),
    'utf8',
  );
  await context.close();
  if (browser) await browser.close();
  process.exit(0);
} catch (err) {
  try {
    await context.close().catch(() => {});
    if (browser) await browser.close().catch(() => {});
  } catch {}
  process.stderr.write(String(err?.stack || err) + '\n');
  process.exit(1);
}
