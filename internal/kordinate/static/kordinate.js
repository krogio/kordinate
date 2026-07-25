// kordinate.js — lazy loading for the customer 360's two slow regions.
//
// The timeline fans out to six microservices behind a VPN and the balance call
// hits three products; rendering either server-side would hold the whole page
// on the slowest one. Both endpoints return JSON with per-source errors, and
// partial failure is the NORMAL case, not the exception — so both renderers
// name what failed. A partial view that looks complete is how an agent gives a
// customer the wrong answer.
(function () {
    "use strict";

    var ICONS = {
        send: '<path d="M22 2 11 13"/><path d="M22 2 15 22l-4-9-9-4z"/>',
        wallet: '<rect x="2" y="6" width="20" height="13" rx="2"/><path d="M2 10h20"/><circle cx="17" cy="14" r="1.4"/>',
        card: '<rect x="1" y="4" width="22" height="16" rx="2"/><line x1="1" y1="10" x2="23" y2="10"/>',
        deposit: '<path d="M12 3v12"/><path d="m7 10 5 5 5-5"/><path d="M4 19h16"/>',
        document: '<path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/>',
        note: '<path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/>',
        flow: '<path d="M6 3v6a3 3 0 0 0 3 3h6a3 3 0 0 1 3 3v6"/><polyline points="15 3 18 3 18 6"/><polyline points="21 15 18 18 15 15"/>',
        device: '<rect x="6" y="2" width="12" height="20" rx="2"/><line x1="10" y1="18" x2="14" y2="18"/>',
        voucher: '<path d="M3 8a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2v3a2 2 0 0 0 0 4v3a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-3a2 2 0 0 0 0-4z"/><line x1="12" y1="8" x2="12" y2="16"/>',
        eye: '<path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/>',
        clock: '<circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/>'
    };

    // Money formatting mirrors the server's FormatZAR so an amount doesn't
    // change shape depending on whether it arrived in HTML or JSON.
    function zar(v) {
        var neg = v < 0;
        var n = Math.abs(v).toFixed(2);
        var parts = n.split(".");
        var grouped = parts[0].replace(/\B(?=(\d{3})+(?!\d))/g, ",");
        return (neg ? "-R" : "R") + grouped + "." + parts[1];
    }

    function money(currency, v) {
        if (!currency || currency === "ZAR") return zar(v);
        return currency + " " + Math.abs(v).toFixed(2);
    }

    function el(tag, cls, text) {
        var n = document.createElement(tag);
        if (cls) n.className = cls;
        if (text != null) n.textContent = text;
        return n;
    }

    function icon(name) {
        var body = ICONS[name] || ICONS.clock;
        var svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
        svg.setAttribute("viewBox", "0 0 24 24");
        svg.setAttribute("fill", "none");
        svg.setAttribute("stroke", "currentColor");
        svg.setAttribute("stroke-width", "2");
        svg.setAttribute("stroke-linecap", "round");
        svg.setAttribute("stroke-linejoin", "round");
        svg.setAttribute("aria-hidden", "true");
        // Icon paths are our own constants, never server data.
        svg.innerHTML = body;
        return svg;
    }

    function relTime(iso) {
        var t = new Date(iso);
        if (isNaN(t.getTime())) return "";
        var d = (Date.now() - t.getTime()) / 1000;
        if (d < 60) return "just now";
        if (d < 3600) return Math.floor(d / 60) + "m ago";
        if (d < 86400) return Math.floor(d / 3600) + "h ago";
        if (d < 86400 * 30) return Math.floor(d / 86400) + "d ago";
        return t.toLocaleDateString("en-ZA", { day: "numeric", month: "short", year: "numeric" });
    }

    function dayHeading(iso) {
        var t = new Date(iso);
        if (isNaN(t.getTime())) return "Undated";
        var today = new Date();
        var sameDay = function (a, b) {
            return a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate();
        };
        if (sameDay(t, today)) return "Today";
        var yday = new Date(today.getTime() - 86400000);
        if (sameDay(t, yday)) return "Yesterday";
        return t.toLocaleDateString("en-ZA", { weekday: "short", day: "numeric", month: "long", year: "numeric" });
    }

    function dayKey(iso) {
        var t = new Date(iso);
        return isNaN(t.getTime()) ? "undated" : t.toISOString().slice(0, 10);
    }

    function getJSON(url) {
        return fetch(url, { headers: { Accept: "application/json" }, credentials: "same-origin" })
            .then(function (r) {
                if (!r.ok) throw new Error("HTTP " + r.status);
                return r.json();
            });
    }

    // ---------- Balances ----------
    //
    // Three distinct states per product, and conflating any two of them starts a
    // support ticket:
    //   a number      -> the customer holds this product
    //   null, no error-> the customer does not hold it            ("—")
    //   null + error  -> we could not find out                     (warning)
    function renderBalances(box, data) {
        box.textContent = "";
        var errors = (data && data.errors) || {};
        var products = [
            { key: "wallet", label: "Wallet (ZAR)" },
            { key: "card", label: "Card" },
            { key: "usdc", label: "USD savings" }
        ];

        products.forEach(function (p) {
            var tile = el("div", "kdn-bal");
            tile.appendChild(el("div", "kdn-bal-label", p.label));

            var raw = data ? data[p.key] : null;
            var err = errors[p.key];

            if (typeof raw === "number") {
                tile.appendChild(el("div", "kdn-bal-value", p.key === "usdc" ? money("USD", raw) : zar(raw)));
            } else if (err) {
                tile.classList.add("failed");
                var v = el("div", "kdn-bal-value absent", "unknown");
                tile.appendChild(v);
                tile.appendChild(el("div", "kdn-bal-note", "Lookup failed — " + err));
            } else {
                tile.appendChild(el("div", "kdn-bal-value absent", "—"));
                tile.appendChild(el("div", "kdn-bal-note", "No such product for this customer"));
            }
            box.appendChild(tile);
        });
    }

    function loadBalances() {
        var box = document.getElementById("kdn-balances");
        if (!box) return;
        var guid = box.dataset.guid;
        if (!guid) return;

        var url = "/kordinate/customer/balances?guid=" + encodeURIComponent(guid) +
            (box.dataset.msisdn ? "&msisdn=" + encodeURIComponent(box.dataset.msisdn) : "");

        getJSON(url).then(function (data) {
            renderBalances(box, data);
        }).catch(function (e) {
            box.textContent = "";
            var warn = el("div", "flash flash-error",
                "Balances could not be loaded (" + e.message + "). Do not quote a balance to the customer from this screen.");
            warn.style.gridColumn = "1/-1";
            box.appendChild(warn);
        }).then(function () {
            box.setAttribute("aria-busy", "false");
        });
    }

    // ---------- Timeline ----------
    function degradedBanner(errors) {
        var names = Object.keys(errors || {});
        if (!names.length) return null;

        var box = el("div", "flash flash-error");
        var head = el("strong", null,
            "This timeline is incomplete — " + names.length + " source" + (names.length > 1 ? "s" : "") + " failed.");
        box.appendChild(head);
        box.appendChild(document.createTextNode(" Events from these services are missing entirely, so treat what you see as a partial history:"));
        var ul = el("ul");
        ul.style.margin = "8px 0 0";
        ul.style.paddingLeft = "20px";
        names.forEach(function (n) {
            var li = el("li");
            li.appendChild(el("strong", null, n));
            li.appendChild(document.createTextNode(" — " + errors[n]));
            ul.appendChild(li);
        });
        box.appendChild(ul);
        return box;
    }

    function eventRow(ev) {
        var row = el("div", "kdn-ev");

        var ico = el("div", "kdn-ev-icon");
        ico.appendChild(icon(ev.icon));
        row.appendChild(ico);

        var body = el("div");
        body.appendChild(el("div", "kdn-ev-title", ev.title || "(untitled event)"));

        var meta = el("div", "kdn-ev-meta");
        if (ev.at) {
            var time = el("time", null, relTime(ev.at));
            time.setAttribute("datetime", ev.at);
            meta.appendChild(time);
        }
        if (ev.source) meta.appendChild(el("span", "kdn-src", ev.source));
        if (ev.status) meta.appendChild(el("span", null, ev.status));
        if (ev.detail) meta.appendChild(el("span", null, ev.detail));
        if (ev.actor) meta.appendChild(el("span", null, ev.actor));
        if (ev.ref) meta.appendChild(el("span", "kdn-mono", ev.ref));
        body.appendChild(meta);
        row.appendChild(body);

        var amt = el("div", "kdn-ev-amt");
        if (typeof ev.amount === "number") {
            amt.textContent = money(ev.currency, ev.amount);
            if (ev.amount < 0) amt.classList.add("neg");
        }
        row.appendChild(amt);

        return row;
    }

    function renderTimeline(box, banner, data) {
        box.textContent = "";
        if (banner) {
            banner.textContent = "";
            var b = degradedBanner(data && data.errors);
            if (b) {
                banner.appendChild(b);
                banner.hidden = false;
            } else {
                banner.hidden = true;
            }
        }

        var events = (data && data.events) || [];
        if (!events.length) {
            var empty = el("div", "empty-state");
            empty.appendChild(el("p", null,
                data && data.errors && Object.keys(data.errors).length
                    ? "No events could be retrieved — see the failures above."
                    : "No activity in this window."));
            empty.appendChild(el("p", "field-hint",
                "The default window is the last 90 days. Widen it with the from/to query parameters."));
            box.appendChild(empty);
            return;
        }

        // Group by day, preserving the server's newest-first ordering.
        var currentKey = null;
        events.forEach(function (ev) {
            var k = dayKey(ev.at);
            if (k !== currentKey) {
                currentKey = k;
                box.appendChild(el("h3", "kdn-day", dayHeading(ev.at)));
            }
            box.appendChild(eventRow(ev));
        });
    }

    function loadTimeline() {
        var box = document.getElementById("kdn-timeline");
        if (!box) return;
        var banner = document.getElementById("kdn-timeline-degraded");
        var guid = box.dataset.guid;
        if (!guid) return;

        var url = "/kordinate/customer/timeline?guid=" + encodeURIComponent(guid);
        if (box.dataset.from) url += "&from=" + encodeURIComponent(box.dataset.from);
        if (box.dataset.to) url += "&to=" + encodeURIComponent(box.dataset.to);

        getJSON(url).then(function (data) {
            renderTimeline(box, banner, data);
        }).catch(function (e) {
            // A total failure is worse than a degraded one and must not read as
            // "this customer has no history".
            box.textContent = "";
            box.appendChild(el("div", "flash flash-error",
                "The activity timeline could not be loaded (" + e.message + "). This is NOT an empty history — reload, or check the service status."));
        }).then(function () {
            box.setAttribute("aria-busy", "false");
        });
    }

    loadBalances();
    loadTimeline();
})();
