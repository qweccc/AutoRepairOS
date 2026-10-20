// AutoRepair OS - 離線優先前端
// 提供 IndexedDB 草稿快取、Service Worker 註冊、離線佇列與背景重試。

(function () {
  'use strict';

  const DB_NAME = 'autorepairos-offline';
  const DB_VERSION = 2;
  const DRAFT_STORE = 'drafts';
  const QUEUE_STORE = 'uploadQueue';
  const PHOTO_QUEUE_STORE = 'photoUploadQueue';

  // ---------- IndexedDB 包裝 ----------

  function openDB() {
    return new Promise((resolve, reject) => {
      if (!('indexedDB' in window)) {
        reject(new Error('IndexedDB 不可用'));
        return;
      }
      const req = indexedDB.open(DB_NAME, DB_VERSION);
      req.onupgradeneeded = function (e) {
        const db = e.target.result;
        if (!db.objectStoreNames.contains(DRAFT_STORE)) {
          const s = db.createObjectStore(DRAFT_STORE, { keyPath: 'key' });
          s.createIndex('updatedAt', 'updatedAt');
        }
        if (!db.objectStoreNames.contains(QUEUE_STORE)) {
          const s = db.createObjectStore(QUEUE_STORE, { keyPath: 'id', autoIncrement: true });
          s.createIndex('createdAt', 'createdAt');
        }
        if (!db.objectStoreNames.contains(PHOTO_QUEUE_STORE)) {
          const s = db.createObjectStore(PHOTO_QUEUE_STORE, { keyPath: 'id', autoIncrement: true });
          s.createIndex('createdAt', 'createdAt');
          s.createIndex('status', 'status');
        }
      };
      req.onsuccess = () => resolve(req.result);
      req.onerror = () => reject(req.error);
    });
  }

  function tx(store, mode, fn) {
    return openDB().then(function (db) {
      return new Promise(function (resolve, reject) {
        const t = db.transaction(store, mode);
        const s = t.objectStore(store);
        let result;
        try {
          result = fn(s);
        } catch (e) {
          reject(e);
          return;
        }
        t.oncomplete = () => resolve(result && result.result !== undefined ? result.result : result);
        t.onerror = () => reject(t.error);
        t.onabort = () => reject(t.error);
      });
    });
  }

  // ---------- 草稿快取 ----------

  const Drafts = {
    save: function (key, data) {
      return tx(DRAFT_STORE, 'readwrite', function (s) {
        return s.put({ key: key, data: data, updatedAt: Date.now() });
      });
    },
    load: function (key) {
      return openDB().then(function (db) {
        return new Promise(function (resolve, reject) {
          const t = db.transaction(DRAFT_STORE, 'readonly');
          const req = t.objectStore(DRAFT_STORE).get(key);
          req.onsuccess = () => resolve(req.result ? req.result.data : null);
          req.onerror = () => reject(req.error);
        });
      });
    },
    remove: function (key) {
      return tx(DRAFT_STORE, 'readwrite', function (s) { return s.delete(key); });
    },
    all: function () {
      return openDB().then(function (db) {
        return new Promise(function (resolve, reject) {
          const t = db.transaction(DRAFT_STORE, 'readonly');
          const req = t.objectStore(DRAFT_STORE).getAll();
          req.onsuccess = () => resolve(req.result || []);
          req.onerror = () => reject(req.error);
        });
      });
    }
  };

  // ---------- 照片 Blob 佇列 ----------

  const PhotoQueue = {
    add: function (record) {
      return openDB().then(function (db) {
        return new Promise(function (resolve, reject) {
          const t = db.transaction(PHOTO_QUEUE_STORE, 'readwrite');
          const req = t.objectStore(PHOTO_QUEUE_STORE).add(record);
          req.onsuccess = () => resolve(req.result);
          req.onerror = () => reject(req.error);
        });
      });
    },
    list: function () {
      return openDB().then(function (db) {
        return new Promise(function (resolve, reject) {
          const t = db.transaction(PHOTO_QUEUE_STORE, 'readonly');
          const req = t.objectStore(PHOTO_QUEUE_STORE).getAll();
          req.onsuccess = () => resolve(req.result || []);
          req.onerror = () => reject(req.error);
        });
      });
    },
    remove: function (id) {
      return tx(PHOTO_QUEUE_STORE, 'readwrite', function (s) { return s.delete(id); });
    },
    update: function (id, changes) {
      return openDB().then(function (db) {
        return new Promise(function (resolve, reject) {
          const t = db.transaction(PHOTO_QUEUE_STORE, 'readwrite');
          const store = t.objectStore(PHOTO_QUEUE_STORE);
          const req = store.get(id);
          req.onsuccess = function () {
            const rec = req.result;
            if (!rec) { resolve(null); return; }
            Object.keys(changes).forEach(function (key) { rec[key] = changes[key]; });
            const put = store.put(rec);
            put.onsuccess = () => resolve(rec);
            put.onerror = () => reject(put.error);
          };
          req.onerror = () => reject(req.error);
        });
      });
    }
  };

  // ---------- 上傳佇列（離線時暫存，恢復連線後重試） ----------

  const UploadQueue = {
    add: function (url, formData, meta) {
      return openDB().then(function (db) {
        return new Promise(function (resolve, reject) {
          const t = db.transaction(QUEUE_STORE, 'readwrite');
          const store = t.objectStore(QUEUE_STORE);
          const rec = {
            url: url,
            // FormData 無法結構化複製，改存欄位值
            fields: meta && meta.fields ? meta.fields : {},
            fileName: meta && meta.fileName ? meta.fileName : null,
            fileType: meta && meta.fileType ? meta.fileType : null,
            fileSize: meta && meta.fileSize ? meta.fileSize : null,
            createdAt: Date.now(),
            retryCount: 0
          };
          const req = store.add(rec);
          req.onsuccess = () => resolve(req.result);
          req.onerror = () => reject(req.error);
        });
      });
    },
    list: function () {
      return openDB().then(function (db) {
        return new Promise(function (resolve, reject) {
          const t = db.transaction(QUEUE_STORE, 'readonly');
          const req = t.objectStore(QUEUE_STORE).getAll();
          req.onsuccess = () => resolve(req.result || []);
          req.onerror = () => reject(req.error);
        });
      });
    },
    remove: function (id) {
      return tx(QUEUE_STORE, 'readwrite', function (s) { return s.delete(id); });
    },
    updateRetry: function (id, count) {
      return openDB().then(function (db) {
        return new Promise(function (resolve, reject) {
          const t = db.transaction(QUEUE_STORE, 'readwrite');
          const store = t.objectStore(QUEUE_STORE);
          const req = store.get(id);
          req.onsuccess = function () {
            const rec = req.result;
            if (!rec) { resolve(null); return; }
            rec.retryCount = count;
            store.put(rec).onsuccess = () => resolve(rec);
          };
          req.onerror = () => reject(req.error);
        });
      });
    }
  };

  // ---------- 連線狀態 ----------

  const Connection = {
    isOnline: function () { return navigator.onLine; },
    onChange: function (cb) {
      window.addEventListener('online', function () {
        cb(true);
        Retry.flush();
      });
      window.addEventListener('offline', function () { cb(false); });
    }
  };

  // ---------- 背景重試 ----------

  const Retry = {
    timer: null,
    schedule: function (delayMs) {
      if (this.timer) clearTimeout(this.timer);
      this.timer = setTimeout(function () { Retry.flush(); }, delayMs || 10000);
    },
    flush: function () {
      if (!Connection.isOnline()) return Promise.resolve(0);

      return UploadQueue.list().then(function (items) {
        let done = 0;
        let chain = Promise.resolve();

        items.forEach(function (item) {
          chain = chain.then(function () {
            return Retry.sendOne(item).then(function (ok) {
              if (ok) {
                return UploadQueue.remove(item.id).then(function () { done++; });
              }
              return UploadQueue.updateRetry(item.id, (item.retryCount || 0) + 1);
            }).catch(function () {
              return UploadQueue.updateRetry(item.id, (item.retryCount || 0) + 1);
            });
          });
        });

        return chain.then(function () {
          return PhotoQueue.list().then(function (photos) {
            let photoDone = 0;
            let photoChain = Promise.resolve();
            photos.forEach(function (photo) {
              photoChain = photoChain.then(function () {
                return Retry.sendPhoto(photo).then(function (ok) {
                  if (ok) photoDone++;
                });
              });
            });
            return photoChain.then(function () {
              const totalDone = done + photoDone;
              if (totalDone > 0) {
                console.log('[offline] 已重試上傳', totalDone, '筆');
                Retry.schedule(5000);
              } else if (items.length > 0 || photos.length > 0) {
                Retry.schedule(30000);
              }
              return totalDone;
            });
          });
        });
      }).catch(function (e) {
        console.warn('[offline] 重試失敗', e);
        return 0;
      });
    },
    sendOne: function (item) {
      var body = new URLSearchParams();
      Object.keys(item.fields || {}).forEach(function (k) {
        body.append(k, item.fields[k]);
      });
      return fetch(item.url, {
        method: 'POST',
        headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
        body: body.toString(),
        credentials: 'same-origin'
      }).then(function (r) {
        return r.ok;
      });
    },
    sendPhoto: function (item) {
      return PhotoQueue.update(item.id, {
        status: 'uploading',
        lastAttemptAt: Date.now()
      }).then(function () {
        const body = new FormData();
        Object.keys(item.fields || {}).forEach(function (key) {
          body.append(key, item.fields[key]);
        });
        body.append('photo', item.blob, item.fileName || 'photo');
        return fetch(item.url, {
          method: 'POST',
          headers: { 'Accept': 'application/json' },
          body: body,
          credentials: 'same-origin'
        }).then(function (response) {
          if (!response.ok) throw new Error('HTTP ' + response.status);
          if (new URL(response.url).pathname === '/login') {
            throw new Error('登入狀態已失效，請重新登入後重試');
          }
          return PhotoQueue.remove(item.id).then(function () {
            window.dispatchEvent(new CustomEvent('autorepair:photo-upload', {
              detail: { id: item.id, status: 'done', fileName: item.fileName }
            }));
            Retry.schedule(5000);
            return true;
          });
        });
      }).catch(function (error) {
        return PhotoQueue.update(item.id, {
          status: 'failed',
          retryCount: (item.retryCount || 0) + 1,
          lastError: error && error.message ? error.message : '照片上傳失敗'
        }).then(function () {
          window.dispatchEvent(new CustomEvent('autorepair:photo-upload', {
            detail: {
              id: item.id,
              status: 'failed',
              fileName: item.fileName,
              error: error && error.message ? error.message : '照片上傳失敗'
            }
          }));
          Retry.schedule(30000);
          return false;
        });
      });
    }
  };

  // ---------- 表單草稿自動綁定 ----------

  function autoBindDrafts() {
    var forms = document.querySelectorAll('form[method="POST"]');
    Array.prototype.forEach.call(forms, function (form) {
      var key = form.getAttribute('action') || location.pathname;

      // 還原草稿
      Drafts.load(key).then(function (data) {
        if (!data) return;
        Object.keys(data).forEach(function (name) {
          var field = form.querySelector('[name="' + name + '"]');
          if (field && !field.value && field.type !== 'file') {
            field.value = data[name];
          }
        });
      }).catch(function () {});

      // 儲存草稿
      form.addEventListener('change', function () {
        var data = {};
        Array.prototype.forEach.call(form.elements, function (el) {
          if (el.name && el.type !== 'file' && el.type !== 'submit') {
            data[el.name] = el.value;
          }
        });
        Drafts.save(key, data).catch(function () {});
      });

      // 提交成功後清除草稿
      form.addEventListener('submit', function () {
        Drafts.remove(key).catch(function () {});
      });
    });
  }

  function autoBindPhotoUploads() {
    var forms = document.querySelectorAll('form[enctype="multipart/form-data"]');
    Array.prototype.forEach.call(forms, function (form) {
      var fileInput = form.querySelector('input[type="file"][name="photo"]');
      if (!fileInput) return;
      var action = form.getAttribute('action') || location.pathname;
      var status = document.createElement('div');
      status.className = 'full msg';
      status.setAttribute('role', 'status');
      status.hidden = true;
      form.appendChild(status);
      var submitting = false;

      form.addEventListener('submit', function (event) {
        var file = fileInput.files && fileInput.files[0];
        if (!file) return;
        event.preventDefault();
        if (submitting) return;
        submitting = true;
        status.hidden = false;
        status.className = 'full msg';
        status.textContent = '照片已安全暫存，正在上傳…';

        var fields = {};
        Array.prototype.forEach.call(form.elements, function (el) {
          if (el.name && el.type !== 'file' && el.type !== 'submit' && el.type !== 'button') {
            fields[el.name] = el.value;
          }
        });
        var match = action.match(/\/work-orders\/(\d+)\/photos/);
        var record = {
          url: action,
          work_order_id: match ? Number(match[1]) : 0,
          item_id: Number(fields.item_id || 0),
          description: fields.description || fields.note || '',
          photo_type: fields.photo_type || 'damage',
          fields: fields,
          blob: file,
          fileName: file.name,
          mimeType: file.type,
          fileSize: file.size,
          createdAt: Date.now(),
          retryCount: 0,
          status: 'pending'
        };
        status.textContent = '正在壓縮並保存照片…';
        compressPhoto(file).then(function (blob) {
          record.blob = blob;
          record.mimeType = blob.type || file.type;
          record.fileSize = blob.size;
          return PhotoQueue.add(record);
        }).then(function (id) {
          record.id = id;
          return Retry.sendPhoto(record);
        }).then(function (ok) {
          if (ok) {
            status.className = 'full msg msg-ok';
            status.textContent = '照片上傳成功。';
            fileInput.value = '';
          } else {
            status.className = 'full msg msg-err';
            status.textContent = '照片已保存在本機佇列，連線恢復後會自動重試；請勿清除瀏覽器資料。';
          }
        }).catch(function (error) {
          status.className = 'full msg msg-err';
          status.textContent = '無法將照片保存至本機佇列：' +
            (error && error.message ? error.message : 'IndexedDB 寫入失敗');
        }).then(function () {
          submitting = false;
        });
      });
    });
  }

  function compressPhoto(file) {
    if (!file.type || !file.type.startsWith('image/') || file.type === 'image/gif' ||
        typeof window.createImageBitmap !== 'function') {
      return Promise.resolve(file);
    }
    return window.createImageBitmap(file).then(function (bitmap) {
      var scale = Math.min(1, 1920 / Math.max(bitmap.width, bitmap.height));
      var canvas = document.createElement('canvas');
      canvas.width = Math.max(1, Math.round(bitmap.width * scale));
      canvas.height = Math.max(1, Math.round(bitmap.height * scale));
      canvas.getContext('2d').drawImage(bitmap, 0, 0, canvas.width, canvas.height);
      bitmap.close();
      return new Promise(function (resolve, reject) {
        canvas.toBlob(function (blob) {
          if (!blob) {
            reject(new Error('照片壓縮失敗'));
            return;
          }
          resolve(blob.size < file.size ? blob : file);
        }, 'image/jpeg', 0.82);
      });
    }).catch(function (error) {
      console.warn('[offline] 照片壓縮失敗，保留原始照片', error);
      return file;
    });
  }

  // ---------- 離線提示 UI ----------

  function mountOfflineBanner() {
    var bar = document.createElement('div');
    bar.id = 'offline-banner';
    bar.style.cssText = [
      'position:fixed', 'bottom:0', 'left:0', 'right:0', 'z-index:9999',
      'padding:10px 16px', 'font-size:13.5px', 'font-weight:600',
      'text-align:center', 'display:none', 'transition:background .2s'
    ].join(';');
    document.body.appendChild(bar);

    function paint() {
      if (Connection.isOnline()) {
        bar.style.display = 'none';
      } else {
        bar.style.display = 'block';
        bar.style.background = '#b45309';
        bar.style.color = '#fef3c7';
        bar.textContent = '⚠ 離線中 — 資料將暫存於本機，恢復連線後自動上傳';
      }
    }

    Connection.onChange(paint);
    paint();

    window.addEventListener('autorepair:photo-upload', function (event) {
      var detail = event.detail || {};
      bar.style.display = 'block';
      if (detail.status === 'done') {
        bar.style.background = '#166534';
        bar.style.color = '#dcfce7';
        bar.textContent = '✓ 照片已上傳成功：' + (detail.fileName || '');
      } else {
        bar.style.background = '#991b1b';
        bar.style.color = '#fee2e2';
        bar.textContent = '照片上傳失敗，已保存在本機佇列等待重試：' +
          (detail.error || detail.fileName || '未知錯誤');
      }
      if (detail.status === 'done') {
        setTimeout(paint, 6000);
      }
    });

    // 每 15 秒檢查一次佇列數量並顯示
    setInterval(function () {
      if (!Connection.isOnline()) return;
      Promise.all([UploadQueue.list(), PhotoQueue.list()]).then(function (queues) {
        var items = queues[0];
        var photos = queues[1];
        if (items.length > 0 || photos.length > 0) {
          bar.style.display = 'block';
          bar.style.background = '#1d4ed8';
          bar.style.color = '#dbeafe';
          var failed = photos.filter(function (item) { return item.status === 'failed'; }).length;
          bar.textContent = '⟳ 待上傳 ' + (items.length + photos.length) + ' 筆資料' +
            (failed ? '，其中 ' + failed + ' 筆照片上次失敗，將繼續重試' : '') + '…';
        } else if (bar.style.background === 'rgb(29, 78, 216)') {
          paint();
        }
      }).catch(function () {});
    }, 15000);
  }

  // ---------- Service Worker ----------

  // 註冊後主動檢查更新。
  // 為什麼需要：瀏覽器對 service worker 腳本本身有 24 小時的快取，
  // 不主動觸發 update() 的話，換了 sw.js 或 app.css 之後舊 SW 會繼續
  // 攔截請求長達一天，使用者看到的一直是舊版（F5 無效，只有 Shift+F5 有用）。
  // 每次載入頁面都 update() 一次，成本極低（只比對腳本雜湊），
  // 但能保證新版本在下次重整就生效。
  function registerSW() {
    if (!('serviceWorker' in navigator)) return;
    navigator.serviceWorker.register('/sw.js').then(function (reg) {
      console.log('[sw] 已註冊', reg.scope);
      reg.update().catch(function () { /* 離線時更新失敗無所謂，下次再試 */ });
      watchUpdate(reg);
    }).catch(function (e) {
      console.warn('[sw] 註冊失敗', e);
    });
  }

  // 新版 SW 就緒時提示使用者重整。
  // 為什麼需要：新 SW install 完成後，舊的仍然控制著當前頁面（這是 SW 的設計，
  // 避免半新半舊的頁面），所以畫面不會自己變。若不提示，使用者會以為更新沒生效。
  // 新版只在「下次載入」生效，所以這裡給一個明確的一鍵重整按鈕。
  function watchUpdate(reg) {
    if (!reg || !reg.waiting) {
      if (reg) reg.addEventListener('updatefound', function () { watchUpdate(reg); });
      return;
    }
    showUpdateBar(reg.waiting);
  }

  function showUpdateBar(worker) {
    if (document.getElementById('sw-update-bar')) return;
    var bar = document.createElement('div');
    bar.id = 'sw-update-bar';
    bar.style.cssText = [
      'position:fixed', 'bottom:0', 'left:0', 'right:0', 'z-index:10000',
      'padding:10px 16px', 'font-size:13.5px', 'font-weight:600',
      'background:#1d4ed8', 'color:#dbeafe', 'text-align:center'
    ].join(';');
    bar.innerHTML =
      '<span>已有新版本，重新整理即可套用。</span> ' +
      '<button type="button" id="sw-update-btn" style="' +
      'margin-left:10px;padding:5px 14px;border:0;border-radius:6px;' +
      'background:#38bdf8;color:#0f172a;font-weight:700;cursor:pointer;font-size:13px' +
      '">立即更新</button>';
    document.body.appendChild(bar);
    document.getElementById('sw-update-btn').addEventListener('click', function () {
      worker.postMessage({ type: 'SKIP_WAITING' });
    });
    navigator.serviceWorker.addEventListener('controllerchange', function () {
      window.location.reload();
    });
  }

  // ---------- 初始化 ----------

  document.addEventListener('DOMContentLoaded', function () {
    registerSW();
    autoBindDrafts();
    autoBindPhotoUploads();
    mountOfflineBanner();
    Retry.schedule(3000);
  });

  // 對外 API
  window.AutoRepairOffline = {
    Drafts: Drafts,
    UploadQueue: UploadQueue,
    PhotoQueue: PhotoQueue,
    Connection: Connection,
    Retry: Retry,
    // 離線時嘗試上傳，失敗則入列
    post: function (url, fields) {
      var body = new URLSearchParams(fields);
      return fetch(url, {
        method: 'POST',
        headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
        body: body.toString(),
        credentials: 'same-origin'
      }).then(function (r) {
        if (!r.ok) throw new Error('HTTP ' + r.status);
        return r;
      }).catch(function (e) {
        return UploadQueue.add(url, null, { fields: fields }).then(function () {
          return { queued: true, error: e.message };
        });
      });
    }
  };

  console.log('[offline] AutoRepair 離線模組已載入');
})();
