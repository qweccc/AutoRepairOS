/* 拍照自動填入：行照/車牌 AI 辨識 + 零件條碼掃描。
   依賴：htmx.min.js（已載入）、後端 /api/vision/recognize、html5-qrcode.min.js。
   純 vanilla JS，離線可用。 */

(function () {
  "use strict";

  // ---- 行照/車牌拍照辨識 ----
  // 在指定表單插入「📷 拍照辨識行照」按鈕，選圖後 POST 到後端，回填欄位。
  // fieldMap: 後端 reading 欄位 -> 表單 input name 的對照。
  window.initPlateScan = function (opts) {
    var btn = document.getElementById(opts.buttonId);
    var fileInput = document.getElementById(opts.inputId);
    if (!btn || !fileInput) return;

    btn.addEventListener("click", function () {
      fileInput.click();
    });

    fileInput.addEventListener("change", function () {
      var file = fileInput.files && fileInput.files[0];
      if (!file) return;
      btn.disabled = true;
      var oldText = btn.textContent;
      btn.textContent = "辨識中…";

      var reader = new FileReader();
      reader.onload = function () {
        fetch("/api/vision/recognize", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ image: reader.result }),
        })
          .then(function (r) {
            return r.json();
          })
          .then(function (j) {
            if (!j.ok) {
              alert("辨識失敗：" + (j.error || "未知錯誤"));
              return;
            }
            fillFields(j.reading, opts.fieldMap || {});
          })
          .catch(function () {
            alert("辨識請求失敗（網路或伺服器錯誤）");
          })
          .finally(function () {
            btn.disabled = false;
            btn.textContent = oldText;
            fileInput.value = "";
          });
      };
      reader.readAsDataURL(file);
    });
  };

  function fillFields(reading, fieldMap) {
    var filled = 0;
    Object.keys(fieldMap).forEach(function (apiField) {
      var inputName = fieldMap[apiField];
      var val = reading[apiField];
      if (!val) return;
      var el = document.querySelector('[name="' + inputName + '"]');
      if (el && !el.value) {
        // 只填空欄，不覆蓋使用者已輸入
        el.value = val;
        filled++;
      }
    });
    if (filled === 0) {
      alert("已辨識但沒有可填入的空欄（或未辨識到資料）");
    }
  }

  // ---- 零件條碼掃描 ----
  // 用相機掃條碼，掃到回填到指定 input（通常是零件搜尋框或料號欄）。
  window.initBarcodeScan = function (opts) {
    var btn = document.getElementById(opts.buttonId);
    if (!btn || typeof Html5Qrcode === "undefined") return;

    var scanner = null;
    btn.addEventListener("click", function () {
      if (scanner) {
        stopScan();
        return;
      }
      var div = document.getElementById(opts.containerId);
      if (!div) return;
      div.style.display = "block";
      btn.textContent = "停止掃描";
      scanner = new Html5Qrcode(opts.containerId);
      scanner
        .start(
          { facingMode: "environment" },
          { fps: 10, qrbox: 250 },
          function (decodedText) {
            var target = document.getElementById(opts.targetId);
            if (target) {
              target.value = decodedText;
              target.dispatchEvent(new Event("input", { bubbles: true }));
              target.dispatchEvent(new Event("change", { bubbles: true }));
            }
            stopScan();
          },
          function () {
            /* 持續掃描，忽略單幀失敗 */
          },
        )
        .catch(function () {
          alert("無法啟動相機（請確認已授權相機權限）");
          stopScan();
        });
    });

    function stopScan() {
      if (scanner) {
        scanner
          .stop()
          .then(function () {
            scanner.clear();
          })
          .catch(function () {});
        scanner = null;
      }
      var div = document.getElementById(opts.containerId);
      if (div) div.style.display = "none";
      btn.textContent = "📷 掃條碼";
    }
  };
})();
