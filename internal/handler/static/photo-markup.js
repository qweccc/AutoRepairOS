(function () {
  "use strict";

  var csrfToken = JSON.parse(document.body.getAttribute("hx-headers") || "{}")[
    "X-CSRF-Token"
  ];
  var markLabels = {
    scratch: "刮痕",
    dent: "凹痕",
    crack: "裂紋",
    rust: "鏽蝕",
    light_damage: "燈具損壞",
    panel_repair: "鈑金修復",
  };

  function requestJSON(url, method, payload) {
    return fetch(url, {
      method: method,
      headers: {
        "Content-Type": "application/json",
        "X-CSRF-Token": csrfToken,
      },
      body: payload ? JSON.stringify(payload) : undefined,
    }).then(function (response) {
      if (!response.ok) throw new Error("HTTP " + response.status);
      return response;
    });
  }

  function fitCanvas(canvas, width, height) {
    var scale = window.devicePixelRatio || 1;
    canvas.width = Math.max(1, Math.round(width * scale));
    canvas.height = Math.max(1, Math.round(height * scale));
    canvas.style.width = width + "px";
    canvas.style.height = height + "px";
    var ctx = canvas.getContext("2d");
    ctx.setTransform(scale, 0, 0, scale, 0, 0);
    return ctx;
  }

  function drawArrow(ctx, x1, y1, x2, y2) {
    var angle = Math.atan2(y2 - y1, x2 - x1);
    var head = 12;
    ctx.beginPath();
    ctx.moveTo(x1, y1);
    ctx.lineTo(x2, y2);
    ctx.lineTo(
      x2 - head * Math.cos(angle - Math.PI / 6),
      y2 - head * Math.sin(angle - Math.PI / 6),
    );
    ctx.moveTo(x2, y2);
    ctx.lineTo(
      x2 - head * Math.cos(angle + Math.PI / 6),
      y2 - head * Math.sin(angle + Math.PI / 6),
    );
    ctx.stroke();
  }

  function drawAnnotation(ctx, annotation, width, height) {
    var x1 = annotation.x1 * width;
    var y1 = annotation.y1 * height;
    var x2 = annotation.x2 * width;
    var y2 = annotation.y2 * height;
    ctx.strokeStyle = annotation.color || "#ff0000";
    ctx.fillStyle = annotation.color || "#ff0000";
    ctx.lineWidth = 3;
    if (annotation.annotation_type === "rect") {
      ctx.strokeRect(x1, y1, x2 - x1, y2 - y1);
    } else if (annotation.annotation_type === "arrow") {
      drawArrow(ctx, x1, y1, x2, y2);
    } else if (annotation.annotation_type === "text") {
      ctx.font = "bold 16px sans-serif";
      ctx.fillText(annotation.note || "標記", x1, y1);
    } else {
      ctx.beginPath();
      ctx.moveTo(x1, y1);
      ctx.lineTo(x2, y2);
      ctx.stroke();
    }
  }

  function initPhotoMarkers() {
    document
      .querySelectorAll("[data-photo-marker-wrap]")
      .forEach(function (wrap) {
        var canvas = wrap.querySelector("[data-photo-marker-canvas]");
        var image = wrap.querySelector("[data-photo-marker-img]");
        var photoID = canvas.getAttribute("data-photo-id");
        var workOrderID = Number(
          wrap.parentElement.getAttribute("data-work-order-id"),
        );
        var toggle = wrap.parentElement.querySelector(
          "[data-photo-marker-toggle]",
        );
        var controls = wrap.parentElement.querySelector(
          "[data-photo-marker-controls]",
        );
        var form = controls.querySelector("[data-photo-annotation-form]");
        var list = wrap.parentElement.querySelector(
          "[data-photo-annotation-list]",
        );
        var annotations = [];
        var drawing = false;
        var start = null;
        var current = null;

        function redraw() {
          var width = image.clientWidth;
          var height = image.clientHeight;
          if (!width || !height) return;
          var ctx = fitCanvas(canvas, width, height);
          ctx.clearRect(0, 0, width, height);
          annotations.forEach(function (annotation) {
            drawAnnotation(ctx, annotation, width, height);
          });
          if (drawing && current) {
            drawAnnotation(
              ctx,
              {
                annotation_type: form.querySelector('[name="tool"]').value,
                x1: start.x,
                y1: start.y,
                x2: current.x,
                y2: current.y,
                color: form.querySelector('[name="color"]').value,
                note: form.querySelector('[name="note"]').value,
              },
              width,
              height,
            );
          }
        }

        function loadAnnotations() {
          return fetch("/api/photos/" + photoID + "/annotations")
            .then(function (response) {
              if (!response.ok) throw new Error("HTTP " + response.status);
              return response.json();
            })
            .then(function (items) {
              annotations = items;
              redraw();
              list.replaceChildren();
              items.forEach(function (item) {
                var row = document.createElement("div");
                var label = document.createElement("span");
                label.textContent =
                  item.annotation_type + (item.note ? "： " + item.note : "");
                var button = document.createElement("button");
                button.type = "button";
                button.className = "btn btn-d";
                button.textContent = "刪除";
                button.addEventListener("click", function () {
                  requestJSON("/api/annotations/" + item.id + "/delete", "POST")
                    .then(loadAnnotations)
                    .catch(showError);
                });
                row.style.cssText =
                  "display:flex;gap:8px;align-items:center;margin:4px 0";
                row.appendChild(label);
                row.appendChild(button);
                list.appendChild(row);
              });
            });
        }

        function position(event) {
          var rect = canvas.getBoundingClientRect();
          return {
            x: Math.max(
              0,
              Math.min(1, (event.clientX - rect.left) / rect.width),
            ),
            y: Math.max(
              0,
              Math.min(1, (event.clientY - rect.top) / rect.height),
            ),
          };
        }

        toggle.addEventListener("click", function () {
          var editing = controls.style.display === "none";
          controls.style.display = editing ? "block" : "none";
          canvas.style.pointerEvents = editing ? "auto" : "none";
          canvas.style.cursor = editing ? "crosshair" : "default";
          toggle.textContent = editing ? "結束標記" : "標記";
          if (editing) redraw();
        });
        canvas.addEventListener("pointerdown", function (event) {
          if (controls.style.display === "none") return;
          drawing = true;
          start = position(event);
          current = start;
          canvas.setPointerCapture(event.pointerId);
          redraw();
        });
        canvas.addEventListener("pointermove", function (event) {
          if (!drawing) return;
          current = position(event);
          redraw();
        });
        canvas.addEventListener("pointerup", function (event) {
          if (!drawing) return;
          current = position(event);
          drawing = false;
          var fields = form.elements;
          form.querySelector('[name="type"]').value =
            form.querySelector('[name="tool"]').value;
          fields.x1.value = start.x;
          fields.y1.value = start.y;
          fields.x2.value = current.x;
          fields.y2.value = current.y;
          redraw();
        });
        form.addEventListener("submit", function (event) {
          event.preventDefault();
          if (!start || !current) {
            alert("請先在照片上繪製標記");
            return;
          }
          var payload = {
            type: form.querySelector('[name="type"]').value,
            work_order_id: workOrderID,
            x1: Number(form.elements.x1.value),
            y1: Number(form.elements.y1.value),
            x2: Number(form.elements.x2.value),
            y2: Number(form.elements.y2.value),
            color: form.elements.color.value,
            note: form.elements.note.value,
          };
          requestJSON(
            "/api/photos/" + photoID + "/annotations",
            "POST",
            payload,
          )
            .then(function () {
              start = null;
              current = null;
              form.elements.note.value = "";
              return loadAnnotations();
            })
            .catch(showError);
        });
        image.addEventListener("load", redraw);
        window.addEventListener("resize", redraw);
        loadAnnotations().catch(showError);
      });
  }

  function initWalkaround() {
    var canvas = document.getElementById("walkaround-canvas");
    var svg = document.getElementById("walkaround-svg");
    var list = document.getElementById("walkaround-mark-list");
    if (!canvas || !svg || !list) return;
    var woID = canvas.getAttribute("data-wo-id");
    var typeSelect = document.querySelector('select[name="mark_type"]');
    var noteInput = document.getElementById("walkaround-note");
    var status = document.getElementById("walkaround-status");
    var marks = [];

    function redraw() {
      var rect = canvas.getBoundingClientRect();
      if (!rect.width || !rect.height) return;
      var ctx = fitCanvas(canvas, rect.width, rect.height);
      ctx.clearRect(0, 0, rect.width, rect.height);
      marks.forEach(function (mark) {
        var x = mark.x * rect.width;
        var y = mark.y * rect.height;
        ctx.fillStyle = "#f43f5e";
        ctx.strokeStyle = "#fff";
        ctx.lineWidth = 2;
        ctx.beginPath();
        ctx.arc(x, y, 9, 0, Math.PI * 2);
        ctx.fill();
        ctx.stroke();
        ctx.fillStyle = "#fff";
        ctx.font = "bold 11px sans-serif";
        ctx.textAlign = "center";
        ctx.textBaseline = "middle";
        ctx.fillText(String(mark.id), x, y);
      });
    }

    function loadMarks() {
      return fetch("/work-orders/" + woID + "/walkaround-marks")
        .then(function (response) {
          if (!response.ok) throw new Error("HTTP " + response.status);
          return response.json();
        })
        .then(function (items) {
          marks = items;
          redraw();
          list.replaceChildren();
          items.forEach(function (mark) {
            var row = document.createElement("div");
            var label = document.createElement("span");
            label.textContent =
              (markLabels[mark.mark_type] || mark.mark_type) +
              "（" +
              Math.round(mark.x * 100) +
              "%, " +
              Math.round(mark.y * 100) +
              "%）" +
              (mark.note ? "： " + mark.note : "");
            var button = document.createElement("button");
            button.type = "button";
            button.className = "btn btn-d";
            button.textContent = "刪除";
            button.addEventListener("click", function () {
              requestJSON(
                "/work-orders/" +
                  woID +
                  "/walkaround-marks/" +
                  mark.id +
                  "/delete",
                "POST",
              )
                .then(loadMarks)
                .catch(showError);
            });
            row.style.cssText =
              "display:flex;gap:8px;align-items:center;margin:4px 0";
            row.appendChild(label);
            row.appendChild(button);
            list.appendChild(row);
          });
        });
    }

    canvas.addEventListener("click", function (event) {
      var rect = canvas.getBoundingClientRect();
      var payload = {
        mark_type: typeSelect.value,
        x: Math.max(0, Math.min(1, (event.clientX - rect.left) / rect.width)),
        y: Math.max(0, Math.min(1, (event.clientY - rect.top) / rect.height)),
        note: noteInput.value,
      };
      requestJSON("/work-orders/" + woID + "/walkaround-marks", "POST", payload)
        .then(function () {
          noteInput.value = "";
          status.textContent = "舊傷標記已新增";
          return loadMarks();
        })
        .catch(function (error) {
          status.textContent = "新增失敗：" + error.message;
        });
    });
    window.addEventListener("resize", redraw);
    loadMarks().catch(function (error) {
      status.textContent = "讀取舊傷標記失敗：" + error.message;
    });
  }

  function showError(error) {
    alert("標記操作失敗：" + error.message);
  }

  initPhotoMarkers();
  initWalkaround();
})();
