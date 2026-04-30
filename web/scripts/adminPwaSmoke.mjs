import { chromium } from 'playwright';
import { mkdir, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(__dirname, '..', '..');
const artifactDir = path.join(repoRoot, 'artifacts', 'admin-pwa-smoke');

const baseUrl = process.env.ORBIT_SMOKE_URL || 'http://localhost:3000';
const email = process.env.ORBIT_SMOKE_EMAIL || 'test@orbit.local';
const password = process.env.ORBIT_SMOKE_PASSWORD || 'SuperAdmin123!';
const expectedTabs = ['Users', 'Maintenance', 'Onboarding', 'Push Inspector', 'Audit Log'];
const cyrillicPattern = /[А-Яа-яЁё]/;

function getApiUrl(pathname) {
  return `${baseUrl.replace(/\/$/, '')}/api/v1${pathname}`;
}

async function loginAndOpenApp(context, page) {
  const response = await fetch(getApiUrl('/auth/login'), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'XMLHttpRequest' },
    body: JSON.stringify({ email, password }),
  });

  if (!response.ok) {
    throw new Error(`Login failed: HTTP ${response.status} ${await response.text()}`);
  }

  const data = await response.json();
  await context.addInitScript(({ accessToken, expiresIn }) => {
    sessionStorage.setItem('saturn_access_token', accessToken);
    sessionStorage.setItem('saturn_access_token_expires_at', String(Date.now() + expiresIn * 1000));
    localStorage.setItem('saturn_has_session', '1');
    localStorage.removeItem('langpack-en');
  }, { accessToken: data.access_token, expiresIn: data.expires_in });

  await page.goto(baseUrl, { waitUntil: 'domcontentloaded', timeout: 60000 });
  await page.waitForLoadState('networkidle', { timeout: 20000 }).catch(() => undefined);
}

async function openAdministration(page) {
  const menu = page.locator('[aria-label="Open menu"]').filter({ visible: true });
  if (!await menu.count()) {
    throw new Error('Open menu button is not visible after login');
  }

  await menu.first().click({ force: true });
  const adminItem = page.locator('.MenuItem').filter({ hasText: 'Administration' }).filter({ visible: true });
  await adminItem.waitFor({ state: 'visible', timeout: 10000 });
  await adminItem.first().click({ force: true });
  await page.waitForTimeout(1000);

  const afterClickText = await page.locator('body').innerText();
  if (!afterClickText.includes('Onboarding') || !afterClickText.includes('Audit')) {
    const fallbackItem = page.getByText('Administration', { exact: true }).filter({ visible: true });
    if (await fallbackItem.count()) {
      await fallbackItem.first().click({ force: true });
    }
  }

  await page.waitForFunction(() => (
    document.body.innerText.includes('Users')
    && document.body.innerText.includes('Onboarding')
    && document.body.innerText.includes('Audit')
  ), undefined, { timeout: 10000 });
}

async function getAdminText(page) {
  const adminPanel = page.locator('.modal-dialog').filter({ visible: true });
  if (!await adminPanel.count()) return '';
  return adminPanel.first().innerText();
}

async function clickAdminTab(page, label) {
  if (label === 'Users' && (await getAdminText(page)).includes('Search people')) {
    return;
  }

  let tab = page.locator('.Tab').filter({ hasText: label }).filter({ visible: true });
  if (!await tab.count()) {
    tab = page.getByText(label, { exact: true }).filter({ visible: true });
  }
  if (!await tab.count()) {
    throw new Error(`Admin tab is not visible: ${label}`);
  }

  const target = await tab.evaluateAll((nodes) => {
    const viewportWidth = window.innerWidth;
    const viewportHeight = window.innerHeight;
    return nodes
      .map((node) => {
        const rect = node.getBoundingClientRect();
        return {
          isActive: node.classList.contains('Tab--active'),
          x: rect.x,
          y: rect.y,
          width: rect.width,
          height: rect.height,
          isInsideViewport: rect.width > 0 && rect.height > 0
            && rect.x < viewportWidth && rect.y < viewportHeight
            && rect.x + rect.width > 0 && rect.y + rect.height > 0,
        };
      })
      .find((item) => item.isInsideViewport);
  });

  if (!target) throw new Error(`Admin tab has no visible box: ${label}`);
  if (!target.isActive) {
    await page.mouse.click(target.x + target.width / 2, target.y + target.height / 2);
  }
  await page.waitForTimeout(500);
}

async function runViewport(browser, viewportName, viewport) {
  const context = await browser.newContext({
    viewport,
    deviceScaleFactor: viewportName === 'mobile' ? 2 : 1,
    isMobile: viewportName === 'mobile',
    hasTouch: viewportName === 'mobile',
  });
  const page = await context.newPage();
  const consoleErrors = [];

  page.on('console', (message) => {
    if (message.type() === 'error') consoleErrors.push(message.text());
  });
  page.on('pageerror', (error) => consoleErrors.push(`pageerror: ${error.message}`));

  const result = {
    viewport: viewportName,
    tabs: [],
    consoleErrors,
    screenshots: [],
  };

  await loginAndOpenApp(context, page);
  await openAdministration(page);

  for (const label of expectedTabs) {
    await clickAdminTab(page, label);
    const text = await getAdminText(page);
    const screenshotPath = path.join(artifactDir, `${viewportName}-${label.replace(/\W+/g, '-').toLowerCase()}.png`);
    await page.screenshot({ path: screenshotPath, fullPage: false });
    result.screenshots.push(screenshotPath);
    result.tabs.push({
      label,
      hasCyrillic: cyrillicPattern.test(text),
      textSample: text.slice(0, 300),
    });
  }

  await context.close();
  return result;
}

async function main() {
  await mkdir(artifactDir, { recursive: true });

  const browser = await chromium.launch({ headless: true });
  const report = {
    baseUrl,
    email,
    startedAt: new Date().toISOString(),
    results: [],
    errors: [],
  };

  try {
    report.results.push(await runViewport(browser, 'desktop', { width: 1280, height: 900 }));
    report.results.push(await runViewport(browser, 'mobile', { width: 390, height: 844 }));
  } catch (error) {
    report.errors.push(error.stack || error.message || String(error));
  } finally {
    await browser.close();
  }

  const reportPath = path.join(artifactDir, 'report.json');
  await writeFile(reportPath, JSON.stringify(report, undefined, 2));

  const failedTabs = report.results.flatMap((result) => (
    result.tabs.filter((tab) => tab.hasCyrillic).map((tab) => `${result.viewport}:${tab.label}`)
  ));
  const consoleErrors = report.results.flatMap((result) => result.consoleErrors);

  if (report.errors.length || failedTabs.length || consoleErrors.length) {
    console.error(JSON.stringify({ reportPath, errors: report.errors, failedTabs, consoleErrors }, undefined, 2));
    process.exit(1);
  }

  console.log(JSON.stringify({ reportPath, viewports: report.results.length }, undefined, 2));
}

main();
