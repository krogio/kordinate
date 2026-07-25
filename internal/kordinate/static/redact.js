// redact.js — the canvas redaction editor for a document image.
//
// The one thing that must not be got wrong here: coordinates are POSTed
// normalised (0..1) against the image's NATURAL size, never its displayed size.
// The agent draws on a scaled preview whose width depends on the viewport, the
// sidebar state and the device pixel ratio; the server burns the boxes into the
// full-resolution raster. Storing display pixels would silently misalign every
// box the moment either side changed — and a redaction that misses is worse than
// no redaction, because the operator believes the PII is covered.
//
// Normalising against the natural size makes the two independent: the same
// region is correct at any preview scale and for any derivative.
(function () {
    "use strict";

    var img = document.getElementById("kdn-docimg");
    var canvas = document.getElementById("kdn-redact-canvas");
    var form = document.getElementById("kdn-redact-form");
    if (!img || !canvas || !form) return;

    var regionsField = document.getElementById("kdn-redact-regions");
    var kindSelect = document.getElementById("kdn-redact-kind");
    var listBox = document.getElementById("kdn-redact-list");
    var applyBtn = document.getElementById("kdn-redact-apply");
    var clearBtn = document.getElementById("kdn-redact-clear");
    var proposeBtn = document.getElementById("kdn-redact-propose");

    var ctx = canvas.getContext("2d");

    // Regions are held in normalised space. Every conversion to and from pixels
    // goes through toDisplay/fromDisplay so there is exactly one place the
    // scaling can be wrong.
    var regions = [];
    var selected = -1;

    var MIN_NORM = 0.005; // reject a stray click that would redact nothing

    // ---------- geometry ----------

    function displaySize() {
        var r = img.getBoundingClientRect();
        return { w: r.width, h: r.height };
    }

    function toDisplay(reg) {
        var d = displaySize();
        return { x: reg.x * d.w, y: reg.y * d.h, w: reg.w * d.w, h: reg.h * d.h };
    }

    function fromDisplay(px) {
        var d = displaySize();
        if (!d.w || !d.h) return { x: 0, y: 0, w: 0, h: 0 };
        return { x: px.x / d.w, y: px.y / d.h, w: px.w / d.w, h: px.h / d.h };
    }

    function clamp01(v) { return v < 0 ? 0 : v > 1 ? 1 : v; }

    // Clamp a region inside the image. The server rejects out-of-bounds boxes,
    // so fixing it here keeps a drag past the edge from failing the submit.
    function clampRegion(reg) {
        reg.x = clamp01(reg.x);
        reg.y = clamp01(reg.y);
        if (reg.x + reg.w > 1) reg.w = 1 - reg.x;
        if (reg.y + reg.h > 1) reg.h = 1 - reg.y;
        return reg;
    }

    function pointerPos(e) {
        var r = canvas.getBoundingClientRect();
        return { x: e.clientX - r.left, y: e.clientY - r.top };
    }

    function hitTest(pt) {
        // Topmost first, so a box drawn over another is the one you grab.
        for (var i = regions.length - 1; i >= 0; i--) {
            var b = toDisplay(regions[i]);
            if (pt.x >= b.x && pt.x <= b.x + b.w && pt.y >= b.y && pt.y <= b.y + b.h) return i;
        }
        return -1;
    }

    // ---------- drawing ----------

    // The canvas backing store follows the displayed size times the device pixel
    // ratio, so boxes stay crisp; the drawing code works in CSS pixels.
    function resize() {
        var d = displaySize();
        if (!d.w || !d.h) return;
        var dpr = window.devicePixelRatio || 1;
        canvas.width = Math.round(d.w * dpr);
        canvas.height = Math.round(d.h * dpr);
        canvas.style.width = d.w + "px";
        canvas.style.height = d.h + "px";
        ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
        draw();
    }

    function draw(preview) {
        var d = displaySize();
        ctx.clearRect(0, 0, d.w, d.h);

        regions.forEach(function (reg, i) {
            var b = toDisplay(reg);
            // Opaque fill so the preview looks like the destructive result, not
            // like a translucent highlight.
            ctx.fillStyle = "rgba(0,0,0,0.88)";
            ctx.fillRect(b.x, b.y, b.w, b.h);
            ctx.lineWidth = i === selected ? 3 : 1.5;
            ctx.strokeStyle = i === selected ? "#e0a23c" : (reg.auto ? "#4cc38a" : "#ffffff");
            ctx.strokeRect(b.x, b.y, b.w, b.h);

            ctx.fillStyle = "#ffffff";
            ctx.font = "600 11px system-ui, sans-serif";
            ctx.fillText(String(i + 1), b.x + 4, b.y + 13);
        });

        if (preview) {
            ctx.setLineDash([5, 4]);
            ctx.lineWidth = 1.5;
            ctx.strokeStyle = "#e0a23c";
            ctx.strokeRect(preview.x, preview.y, preview.w, preview.h);
            ctx.setLineDash([]);
        }
    }

    // ---------- state sync ----------

    function labelFor(kind) {
        if (!kindSelect) return kind;
        for (var i = 0; i < kindSelect.options.length; i++) {
            if (kindSelect.options[i].value === kind) return kindSelect.options[i].textContent.trim();
        }
        return kind;
    }

    function sync() {
        if (regionsField) regionsField.value = JSON.stringify(regions);
        if (applyBtn) applyBtn.disabled = regions.length === 0;

        if (!listBox) return;
        listBox.textContent = "";
        if (!regions.length) {
            var li = document.createElement("li");
            var dt = document.createElement("dt");
            dt.className = "field-hint";
            dt.textContent = "No boxes drawn yet.";
            li.appendChild(dt);
            li.appendChild(document.createElement("dd"));
            listBox.appendChild(li);
            return;
        }

        regions.forEach(function (reg, i) {
            var li = document.createElement("li");

            var dt = document.createElement("dt");
            dt.textContent = (i + 1) + ". " + labelFor(reg.kind) + (reg.auto ? " (proposed)" : "");
            li.appendChild(dt);

            var dd = document.createElement("dd");
            var pick = document.createElement("select");
            pick.setAttribute("aria-label", "PII class for box " + (i + 1));
            if (kindSelect) {
                for (var k = 0; k < kindSelect.options.length; k++) {
                    var o = document.createElement("option");
                    o.value = kindSelect.options[k].value;
                    o.textContent = kindSelect.options[k].textContent;
                    if (o.value === reg.kind) o.selected = true;
                    pick.appendChild(o);
                }
            }
            pick.addEventListener("change", function () {
                reg.kind = pick.value;
                sync();
                draw();
            });
            dd.appendChild(pick);

            var del = document.createElement("button");
            del.type = "button";
            del.className = "btn btn-sm btn-danger";
            del.style.marginLeft = "8px";
            del.textContent = "Remove";
            del.setAttribute("aria-label", "Remove box " + (i + 1));
            del.addEventListener("click", function () {
                regions.splice(i, 1);
                if (selected >= regions.length) selected = -1;
                sync();
                draw();
            });
            dd.appendChild(del);

            li.appendChild(dd);
            listBox.appendChild(li);
        });
    }

    // ---------- pointer interaction ----------

    var mode = null; // "draw" | "move"
    var start = null;
    var moveOffset = null;

    canvas.addEventListener("pointerdown", function (e) {
        canvas.setPointerCapture(e.pointerId);
        canvas.focus();
        var pt = pointerPos(e);
        var hit = hitTest(pt);

        if (hit >= 0) {
            selected = hit;
            mode = "move";
            var b = toDisplay(regions[hit]);
            moveOffset = { dx: pt.x - b.x, dy: pt.y - b.y };
        } else {
            selected = -1;
            mode = "draw";
            start = pt;
        }
        draw();
    });

    canvas.addEventListener("pointermove", function (e) {
        if (!mode) return;
        var pt = pointerPos(e);

        if (mode === "draw") {
            draw({
                x: Math.min(start.x, pt.x), y: Math.min(start.y, pt.y),
                w: Math.abs(pt.x - start.x), h: Math.abs(pt.y - start.y)
            });
            return;
        }

        var reg = regions[selected];
        if (!reg) return;
        var b = toDisplay(reg);
        var moved = fromDisplay({ x: pt.x - moveOffset.dx, y: pt.y - moveOffset.dy, w: b.w, h: b.h });
        reg.x = clamp01(Math.min(moved.x, 1 - reg.w));
        reg.y = clamp01(Math.min(moved.y, 1 - reg.h));
        draw();
    });

    function endPointer(e) {
        if (mode === "draw" && start) {
            var pt = pointerPos(e);
            var box = fromDisplay({
                x: Math.min(start.x, pt.x), y: Math.min(start.y, pt.y),
                w: Math.abs(pt.x - start.x), h: Math.abs(pt.y - start.y)
            });
            // A zero-area box would redact nothing while looking like it did.
            if (box.w >= MIN_NORM && box.h >= MIN_NORM) {
                box.kind = kindSelect ? kindSelect.value : "other";
                box.auto = false;
                regions.push(clampRegion(box));
                selected = regions.length - 1;
            }
        }
        mode = null;
        start = null;
        moveOffset = null;
        sync();
        draw();
    }

    canvas.addEventListener("pointerup", endPointer);
    canvas.addEventListener("pointercancel", endPointer);

    // ---------- keyboard ----------
    //
    // The canvas is the only way to place a box, so it has to be operable
    // without a mouse: Tab cycles the selection, arrows nudge, Delete removes.
    canvas.addEventListener("keydown", function (e) {
        if (!regions.length) return;
        var step = e.shiftKey ? 0.02 : 0.005;

        switch (e.key) {
            case "Tab":
                e.preventDefault();
                selected = e.shiftKey
                    ? (selected <= 0 ? regions.length - 1 : selected - 1)
                    : (selected + 1) % regions.length;
                break;
            case "Delete":
            case "Backspace":
                if (selected < 0) return;
                e.preventDefault();
                regions.splice(selected, 1);
                selected = -1;
                sync();
                break;
            case "ArrowLeft": case "ArrowRight": case "ArrowUp": case "ArrowDown":
                if (selected < 0) return;
                e.preventDefault();
                var reg = regions[selected];
                if (e.key === "ArrowLeft") reg.x = clamp01(reg.x - step);
                if (e.key === "ArrowRight") reg.x = clamp01(Math.min(reg.x + step, 1 - reg.w));
                if (e.key === "ArrowUp") reg.y = clamp01(reg.y - step);
                if (e.key === "ArrowDown") reg.y = clamp01(Math.min(reg.y + step, 1 - reg.h));
                sync();
                break;
            default:
                return;
        }
        draw();
    });

    // ---------- controls ----------

    if (clearBtn) {
        clearBtn.addEventListener("click", function () {
            regions = [];
            selected = -1;
            sync();
            draw();
        });
    }

    // Server-proposed boxes are conventional field positions for a standard
    // document layout, not detected coordinates — they are a starting point the
    // agent drags into place, and stay marked auto so the audit record keeps the
    // distinction.
    if (proposeBtn) {
        proposeBtn.addEventListener("click", function () {
            var raw = proposeBtn.dataset.regions;
            if (!raw) return;
            try {
                var parsed = JSON.parse(raw);
                if (!Array.isArray(parsed)) return;
                parsed.forEach(function (r) {
                    if (typeof r.x !== "number" || typeof r.w !== "number") return;
                    regions.push(clampRegion({
                        x: r.x, y: r.y, w: r.w, h: r.h,
                        kind: r.kind || "other", note: r.note || "", auto: true
                    }));
                });
                sync();
                draw();
            } catch (err) {
                /* a malformed proposal is not worth breaking the editor over */
            }
        });
    }

    // The form POSTs normally (so the CSRF field and the confirm dialog apply);
    // this only guards against submitting an empty set.
    form.addEventListener("submit", function (e) {
        if (!regions.length) {
            e.preventDefault();
            return;
        }
        regionsField.value = JSON.stringify(regions);
    });

    // ---------- lifecycle ----------

    // The natural size is only known once the image has decoded, and the canvas
    // must match the RENDERED box — so size on load, and again whenever the
    // layout can change under it.
    if (img.complete && img.naturalWidth) {
        resize();
    } else {
        img.addEventListener("load", resize);
    }
    window.addEventListener("resize", resize);
    if (window.ResizeObserver) new ResizeObserver(resize).observe(img);

    sync();
})();
