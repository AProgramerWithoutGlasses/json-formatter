'use strict';

// History belongs to this origin + browser profile, never to the server.
// Exact compound keys avoid hashing requirements and preserve numeric spelling.
window.localHistory = (() => {
  const MAX_ITEMS = 100;
  const MAX_BYTES = 10 * 1024 * 1024;
  let opening;
  let channel;
  try { channel = new BroadcastChannel('json-studio-history'); } catch { /* Focus refresh is the fallback. */ }

  function notify() {
    // A notification failure must not turn a committed save into a false error.
    try { channel?.postMessage('changed'); } catch { /* Focus refresh is the fallback. */ }
  }

  function open() {
    if (opening) return opening;
    const promise = new Promise((resolve, reject) => {
      let finished = false;
      const fail = (error) => { if (!finished) { finished = true; reject(error); } };
      const request = indexedDB.open('json-studio-history', 1);
      const timer = setTimeout(() => fail(new Error('本地历史打开超时，请关闭其他旧版本标签页后重试')), 5000);
      request.onupgradeneeded = () => {
        const store = request.result.createObjectStore('entries', { keyPath: 'id', autoIncrement: true });
        store.createIndex('content', ['input', 'indent', 'mode'], { unique: true });
        store.createIndex('updatedAt', 'updatedAt');
      };
      request.onblocked = () => { clearTimeout(timer); fail(new Error('本地历史被旧标签页占用，请关闭后重试')); };
      request.onerror = () => { clearTimeout(timer); fail(request.error); };
      request.onsuccess = () => {
        clearTimeout(timer);
        const db = request.result;
        if (finished) { db.close(); return; }
        finished = true;
        db.onversionchange = () => { db.close(); opening = null; };
        db.onclose = () => { opening = null; };
        resolve(db);
      };
    });
    opening = promise;
    promise.catch(() => { if (opening === promise) opening = null; });
    return promise;
  }

  async function transaction(mode, work) {
    const db = await open();
    return new Promise((resolve, reject) => {
      const tx = db.transaction('entries', mode);
      let result;
      tx.oncomplete = () => resolve(result);
      tx.onabort = () => reject(tx.error || new Error('本地历史操作已取消'));
      tx.onerror = () => { /* The abort event reports the final failure. */ };
      try { work(tx.objectStore('entries'), (value) => { result = value; }); }
      catch (error) { tx.abort(); reject(error); }
    });
  }

  async function save({ input, indent, mode }) {
    const size = new TextEncoder().encode(input).length;
    if (size > MAX_BYTES) throw new Error('内容超过本地历史的 10 MB 容量限制');
    const id = await transaction('readwrite', (store, done) => {
      const match = store.index('content').get([input, indent, mode]);
      match.onsuccess = () => {
        const compactTitle = input.replace(/\s+/g, ' ').trim();
        const title = [...compactTitle.slice(0, 190)].slice(0, 90).join('');
        const entry = { input, indent, mode, title, bytes: size, updatedAt: Date.now() };
        if (match.result) entry.id = match.result.id;
        const newest = store.index('updatedAt').openCursor(null, 'prev');
        newest.onsuccess = () => {
          // Keep newest ordering deterministic even for simultaneous tabs or
          // system clock changes, including updates to an old existing record.
          entry.updatedAt = Math.max(entry.updatedAt, (newest.result?.value.updatedAt || 0) + 1);
          const put = store.put(entry);
          put.onsuccess = () => {
            done(put.result);
            // Eviction shares the write transaction; concurrent tabs cannot lose
            // updates or exceed the retention limits through read/modify/write races.
            let count = 0, total = 0;
            const cursor = store.index('updatedAt').openCursor(null, 'prev');
            cursor.onsuccess = () => {
              const row = cursor.result;
              if (!row) return;
              count++; total += row.value.bytes;
              if (count > MAX_ITEMS || total > MAX_BYTES) row.delete();
              row.continue();
            };
          };
        };
      };
    });
    notify();
    return id;
  }

  function list() {
    return transaction('readonly', (store, done) => {
      const items = [];
      const request = store.index('updatedAt').openCursor(null, 'prev');
      request.onsuccess = () => {
        const cursor = request.result;
        if (!cursor) { done(items); return; }
        const { input, ...summary } = cursor.value;
        items.push(summary);
        cursor.continue();
      };
    });
  }

  async function get(id) {
    const result = await transaction('readonly', (store, done) => {
      const request = store.get(id);
      request.onsuccess = () => done(request.result);
    });
    if (!result) throw new Error('记录不存在，可能已在其他标签页删除或被自动清理');
    return result;
  }

  async function remove(id) {
    await transaction('readwrite', (store) => { store.delete(id); });
    notify();
  }

  return { save, list, get, remove, subscribe(callback) { if (channel) channel.onmessage = callback; } };
})();
