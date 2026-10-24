// Uses a fresh, task-owned browser profile and isolated contexts, never user data.
const { chromium } = require('playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const path = require('node:path');
const baseURL = process.env.TEST_URL || 'http://127.0.0.1:8080';
const root = path.resolve(__dirname, '..', '.local');
const waitFor = async (fn) => {
  for (let i = 0; i < 80; i++) { if (await fn()) return; await new Promise((r) => setTimeout(r, 100)); }
  throw new Error('Local history condition timed out');
};
const fill = async (page, value) => {
  await page.locator('.cm-content').focus();
  await page.locator('.cm-content').press('ControlOrMeta+A');
  await page.keyboard.insertText(value);
};

(async () => {
  await fs.mkdir(root, { recursive: true });
  const profile = await fs.mkdtemp(path.join(root, 'history-test-profile-'));
  let persistent, browser;
  try {
    persistent = await chromium.launchPersistentContext(profile, { headless: true });
    let page = await persistent.newPage();
    await page.goto(baseURL);
    await fill(page, '{"name":"本地持久化😀","n":9007199254740993,"a":1,"a":2}');
    await page.waitForFunction(() => document.querySelector('#save-status').textContent.includes('已保存'));
    const initial = await page.evaluate(async () => {
      const rows = await window.localHistory.list();
      return window.localHistory.get(rows[0].id);
    });
    assert.match(initial.input, /9007199254740993/);
    assert.equal(initial.input, '{"name":"本地持久化😀","n":9007199254740993,"a":1,"a":2}');
    assert.equal((await persistent.cookies()).length, 0, 'history must not create cookies');
    await persistent.close(); persistent = null;
    persistent = await chromium.launchPersistentContext(profile, { headless: true });
    page = await persistent.newPage();
    await page.goto(baseURL);
    assert.deepEqual(await page.evaluate(async (id) => window.localHistory.get(id), initial.id), initial);
    await persistent.clearCookies(); await page.reload();
    assert.equal((await page.evaluate(() => window.localHistory.list())).length, 1);

    const second = await persistent.newPage();
    await second.goto(baseURL);
    await waitFor(async () => await second.locator('.history-row').count() === 1);
    const same = { input: '{"same":"two tabs"}', mode: 'format', indent: '2' };
    const ids = await Promise.all([page, second].map((tab) => tab.evaluate((entry) => window.localHistory.save(entry), same)));
    assert.equal(ids[0], ids[1], 'concurrent duplicate saves must use one row');
    await waitFor(async () => await second.locator('.history-row').count() === 2);
    await page.evaluate((id) => window.localHistory.remove(id), ids[0]);
    await waitFor(async () => await second.locator('.history-row').count() === 1);
    assert.equal((await second.evaluate(() => window.localHistory.list())).length, 1);

    browser = await chromium.launch({ headless: true });
    const other = await browser.newContext();
    const otherPage = await other.newPage();
    await otherPage.goto(baseURL);
    assert.equal((await otherPage.evaluate(() => window.localHistory.list())).length, 0, 'separate browser profile must have no records');
    assert.equal(await otherPage.evaluate(async (id) => {
      try { await window.localHistory.get(id); return false; } catch { return true; }
    }, initial.id), true);
    await otherPage.evaluate(() => window.localHistory.remove(1));
    assert.equal((await page.evaluate(() => window.localHistory.list())).length, 1, 'another profile cannot delete our record');

    // Keep the latest 100 across concurrent writers, including an updated old row.
    await Promise.all([page, second].map((tab, index) => tab.evaluate(async (index) => {
      for (let i = 0; i < 60; i++) await window.localHistory.save({ input: `{"n":${index * 60 + i}}`, mode: 'format', indent: '2' });
    }, index)));
    let rows = await page.evaluate(() => window.localHistory.list());
    assert.equal(rows.length, 100);
    const oldest = rows[rows.length - 1];
    const refreshed = await page.evaluate(async (id) => {
      const entry = await window.localHistory.get(id);
      return window.localHistory.save(entry);
    }, oldest.id);
    rows = await page.evaluate(() => window.localHistory.list());
    assert.equal(rows[0].id, refreshed);
    assert.equal(rows.length, 100);

    // Bound retained JSON bytes; large entries force eviction in the same transaction.
    await page.evaluate(async () => {
      for (let i = 0; i < 12; i++) await window.localHistory.save({ input: `{"i":${i},"text":"${'x'.repeat(1024 * 1024 - 100)}"}`, mode: 'format', indent: '2' });
    });
    rows = await page.evaluate(() => window.localHistory.list());
    assert.ok(rows.reduce((total, row) => total + row.bytes, 0) <= 10 * 1024 * 1024);
    assert.equal(rows.length, 10);
    assert.match((await page.evaluate(async (id) => window.localHistory.get(id), rows[0].id)).input, /"i":11/);

    // A denied IndexedDB must not stop normal formatting or pretend it saved.
    const denied = await browser.newContext();
    await denied.addInitScript(() => {
      IDBFactory.prototype.open = function () { throw new DOMException('Local storage disabled', 'SecurityError'); };
    });
    const deniedPage = await denied.newPage();
    await deniedPage.goto(baseURL);
    await fill(deniedPage, '{"blocked":true}');
    await deniedPage.waitForFunction(() => document.querySelector('#save-status').textContent.includes('未保存'));
    assert.match(await deniedPage.locator('#output-status').textContent(), /有效/);
    assert.ok(await deniedPage.evaluate(() => window.jsonEditor.value.includes('\n')));
    assert.ok(await deniedPage.locator('#error-panel').isHidden());
    assert.match(await deniedPage.locator('#history-list').textContent(), /本地历史暂不可用/);

    // Simulate an aborted write caused by quota without mocking the history API.
    const quota = await browser.newContext();
    await quota.addInitScript(() => {
      const original = IDBDatabase.prototype.transaction;
      window.rejectHistoryWrites = true;
      IDBDatabase.prototype.transaction = function (...args) {
        const tx = original.apply(this, args);
        if (args[1] === 'readwrite' && window.rejectHistoryWrites) {
          Object.defineProperty(tx, 'error', { get: () => new DOMException('Quota exhausted', 'QuotaExceededError') });
          queueMicrotask(() => tx.abort());
        }
        return tx;
      };
    });
    const quotaPage = await quota.newPage();
    await quotaPage.goto(baseURL);
    await fill(quotaPage, '{"quota":true}');
    await quotaPage.waitForFunction(() => document.querySelector('#save-status').textContent.includes('空间不足'));
    assert.equal((await quotaPage.evaluate(() => window.localHistory.list())).length, 0);
    assert.match(await quotaPage.locator('#output-status').textContent(), /有效/);
    await quotaPage.evaluate(() => { window.rejectHistoryWrites = false; });
    await fill(quotaPage, '{"quota":"retry"}');
    await quotaPage.waitForFunction(() => document.querySelector('#save-status').textContent.includes('已保存'));
    assert.equal((await quotaPage.evaluate(() => window.localHistory.list())).length, 1);
    console.log('PASS: history survives browser restart and cookie clearing; isolated profiles; cross-tab sync and atomic deduplication; 100-record/10-MB retention; denied storage and quota failure do not block formatting; retry succeeds.');
  } finally {
    await persistent?.close(); await browser?.close();
    // Only remove the fresh profile this test created inside the workspace.
    if (path.dirname(path.resolve(profile)) !== root || !path.basename(profile).startsWith('history-test-profile-')) throw new Error('Unexpected test profile path');
    await fs.rm(profile, { recursive: true, force: true });
  }
})().catch((error) => { console.error(error); process.exitCode = 1; });
