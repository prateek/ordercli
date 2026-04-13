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

async function waitForURL(page, substrings, timeoutMillis) {
  if (!Array.isArray(substrings) || substrings.length === 0) return;
  const deadline = Date.now() + Math.max(10_000, timeoutMillis || 0);
  for (;;) {
    const current = page.url();
    if (substrings.every((needle) => current.includes(needle))) return;
    if (Date.now() >= deadline) {
      throw new Error(`timed out waiting for URL containing: ${substrings.join(', ')}`);
    }
    await page.waitForTimeout(250);
  }
}

const input = await readStdinJSON();

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

try {
  await page.goto(input.url, {
    waitUntil: 'domcontentloaded',
    timeout: Math.max(10_000, Number(input.timeout_millis || 0)),
  });
  await waitForURL(page, input.wait_for_url_substrings, Number(input.timeout_millis || 0));
  await page.waitForLoadState('networkidle', { timeout: 15_000 }).catch(() => {});

  const targetOrigin = new URL(input.url).origin;
  const cookies = await context.cookies(targetOrigin);
  const cookieHeader = cookies.map((cookie) => `${cookie.name}=${cookie.value}`).join('; ');
  const userAgent = await page.evaluate(() => navigator.userAgent).catch(() => '');

  fs.writeFileSync(outputPath, JSON.stringify({
    final_url: page.url(),
    user_agent: userAgent,
    cookie_header: cookieHeader,
  }), 'utf8');
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
