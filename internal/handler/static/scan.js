/* 拍照自動填入：行照/車牌 AI 辨識 + 零件條碼掃描。
   依賴：htmx.min.js（已載入）、後端 /api/vision/recognize、html5-qrcode.min.js。
   純 vanilla JS，離線可用。 */

(function () {
  "use strict";

  var II = {};
  try { II = JSON.parse(document.body.getAttribute("data-i18n-js") || "{}"); } catch (e) { II = {}; }

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
      btn.textContent = II["scan.scanning"];
      function reset() {
        btn.disabled = false;
        btn.textContent = oldText;
        fileInput.value = "";
      }

      var reader = new FileReader();
      reader.onload = function () {
        // T2.5：async=1 背景辨識＋每 2 秒 polling，按鈕顯示經過秒數。
        fetch("/api/vision/recognize?async=1", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ image: reader.result }),
        })
          .then(function (r) {
            return r.json();
          })
          .then(function (j) {
            if (!j.ok) {
              alert(II["scan.failed"] + (j.error || II["scan.unknown_error"]));
              reset();
              return;
            }
            if (j.job_id) {
              pollVisionJob(j.job_id, btn, II["scan.scanning"], function (s) {
                fillFields(s.reading, opts.fieldMap || {});
                reset();
              }, function (err) {
                alert(II["scan.failed"] + (err || II["scan.unknown_error"]));
                reset();
              });
              return;
            }
            // 舊版 server 無 async：同步結果直接填。
            fillFields(j.reading, opts.fieldMap || {});
            reset();
          })
          .catch(function () {
            alert(II["scan.request_failed"]);
            reset();
          });
      };
      reader.readAsDataURL(file);
    });
  };

  // pollVisionJob 每 2 秒查辨識進度；按鈕文字帶經過秒數讓使用者有感。
  function pollVisionJob(jobId, btn, label, onDone, onFail) {
    var t0 = Date.now();
    var timer = setInterval(function () {
      fetch("/api/vision/jobs/" + jobId)
        .then(function (r) {
          return r.json();
        })
        .then(function (s) {
          if (!s.ok) {
            clearInterval(timer);
            onFail(s.error);
            return;
          }
          btn.textContent = label + " " + Math.round((Date.now() - t0) / 1000) + "s";
          if (s.status === "done") {
            clearInterval(timer);
            onDone(s);
          } else if (s.status === "error") {
            clearInterval(timer);
            onFail(s.error);
          }
        })
        .catch(function () {
          /* 暫時網路失敗繼續 poll */
        });
    }, 2000);
  }

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
      alert(II["scan.no_fillable_fields"]);
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
      btn.textContent = II["scan.stop"];
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
          alert(II["scan.camera_failed"]);
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
      btn.textContent = II["scan.barcode_button"];
    }
  };
})();
