// The movie browse overlay, added to every document the filmed page loads.
// It draws the cursor the movie shows, tracks when the page last changed so
// verbs can wait for it to settle, and finds the elements verbs act on.
// Running it twice in one document does nothing the second time.
(() => {
  if (window.__movie) return;
  const store = "__movie_cursor";
  let cursor = null;
  let changed = Date.now();

  const norm = (s) => (s || "").replace(/\s+/g, " ").trim();
  const low = (s) => norm(s).toLowerCase();
  const ours = (node) => cursor && (node === cursor || cursor.contains(node));

  new MutationObserver((records) => {
    if (records.some((r) => !ours(r.target))) changed = Date.now();
  }).observe(document, { subtree: true, childList: true, attributes: true, characterData: true });

  const remembered = () => {
    try {
      return JSON.parse(sessionStorage.getItem(store));
    } catch (e) {
      return null;
    }
  };
  const remember = (at) => {
    try {
      sessionStorage.setItem(store, JSON.stringify(at));
    } catch (e) {}
  };

  // An arrow cursor, tip at its top-left corner, drawn above everything and
  // invisible to the page's own hit testing.
  const ensureCursor = () => {
    if (cursor && cursor.isConnected) return cursor;
    cursor = document.createElement("div");
    cursor.setAttribute("aria-hidden", "true");
    cursor.style.cssText =
      "position:fixed;left:0;top:0;width:28px;height:28px;z-index:2147483647;" +
      "pointer-events:none;display:none;will-change:transform";
    // A blinking caret changes the picture twice a second forever, which
    // would make an idle page look busy; this caret holds still.
    cursor.innerHTML =
      "<style>*{caret-animation:manual !important}</style>" +
      '<svg width="28" height="28" viewBox="0 0 28 28" xmlns="http://www.w3.org/2000/svg">' +
      '<path d="M3 2 L3 22 L8.5 17 L12.5 26 L16 24.5 L12 15.5 L19.5 15.5 Z" ' +
      'fill="#111" stroke="#fff" stroke-width="1.8" stroke-linejoin="round"/></svg>';
    document.documentElement.appendChild(cursor);
    const at = remembered();
    if (at) place(at.x, at.y);
    return cursor;
  };

  let pos = null;
  const place = (x, y) => {
    pos = { x, y };
    remember(pos);
    const c = ensureCursor();
    c.style.transform = `translate(${x - 3}px, ${y - 2}px)`;
    c.style.display = "block";
  };

  const ease = (t) => (t < 0.5 ? 4 * t * t * t : 1 - Math.pow(-2 * t + 2, 3) / 2);

  // glide moves the cursor to x, y, taking longer for longer trips.
  const glide = (x, y) =>
    new Promise((resolve) => {
      ensureCursor();
      const from = pos || remembered() || { x: innerWidth / 2, y: innerHeight * 0.8 };
      const dist = Math.hypot(x - from.x, y - from.y);
      const ms = Math.min(700, Math.max(250, dist * 0.9));
      const start = performance.now();
      const step = (now) => {
        const t = Math.min(1, (now - start) / ms);
        const k = ease(t);
        place(from.x + (x - from.x) * k, from.y + (y - from.y) * k);
        if (t < 1) requestAnimationFrame(step);
        else resolve();
      };
      requestAnimationFrame(step);
    });

  // pulse draws a ring expanding from the cursor's tip (3, 2 within the
  // cursor): the click.
  const pulse = () => {
    if (!pos) return;
    const ring = document.createElement("div");
    ring.style.cssText =
      "position:absolute;left:-15px;top:-16px;width:36px;height:36px;" +
      "border-radius:50%;border:3px solid rgba(255,196,0,0.95);box-sizing:border-box;" +
      "z-index:2147483646;pointer-events:none";
    ensureCursor().appendChild(ring);
    ring
      .animate(
        [
          { transform: "scale(0.3)", opacity: 1 },
          { transform: "scale(1.4)", opacity: 0 },
        ],
        { duration: 450, easing: "ease-out" },
      )
      .finished.then(() => ring.remove());
  };

  const visible = (el) => {
    const r = el.getBoundingClientRect();
    if (r.width === 0 && r.height === 0) return false;
    const cs = getComputedStyle(el);
    return cs.visibility !== "hidden" && cs.display !== "none" && cs.opacity !== "0";
  };

  const interactive =
    "a[href],button,input:not([type=hidden]),textarea,select,summary," +
    '[role=button],[role=link],[role=tab],[role=menuitem],[role=checkbox],[role=radio],[role=switch],' +
    '[contenteditable=""],[contenteditable=true]';

  // names are the texts a person would call an element by.
  const names = (el) => {
    const out = [el.innerText, el.getAttribute("aria-label")];
    if (el.labels) for (const l of el.labels) out.push(l.innerText);
    out.push(el.getAttribute("placeholder"), el.getAttribute("title"));
    if (el.tagName === "INPUT" && /^(button|submit|reset)$/i.test(el.type)) out.push(el.value);
    return out.map(norm).filter(Boolean);
  };

  // deepest keeps the elements none of whose descendants are also kept.
  const deepest = (els) => els.filter((el) => !els.some((o) => o !== el && el.contains(o)));

  const byText = (text) => {
    const want = low(text);
    const fields = [...document.querySelectorAll(interactive)].filter(visible);
    const everything = () => [...document.body.querySelectorAll("*")].filter((el) => !ours(el) && visible(el));
    return (
      fields.find((el) => names(el).some((n) => n.toLowerCase() === want)) ||
      deepest(everything().filter((el) => low(el.innerText) === want))[0] ||
      fields.find((el) => names(el).some((n) => n.toLowerCase().includes(want))) ||
      deepest(everything().filter((el) => low(el.innerText).includes(want)))[0] ||
      null
    );
  };

  // find returns the element a TARGET names: text=Label, or a CSS selector.
  const find = (target) => {
    if (target.startsWith("text=")) return byText(target.slice(5));
    let els;
    try {
      els = [...document.querySelectorAll(target)];
    } catch (e) {
      throw new Error(`${target} is neither a CSS selector nor text=Label`);
    }
    return els.find(visible) || null;
  };

  // scrollTo scrolls el to the middle of the viewport, smoothly, as a
  // person would, unless it is already in full view.
  const scrollTo = (el) => {
    const r = el.getBoundingClientRect();
    if (r.top >= 0 && r.left >= 0 && r.bottom <= innerHeight && r.right <= innerWidth) return Promise.resolve();
    return new Promise((resolve) => {
      const done = () => {
        removeEventListener("scrollend", done);
        resolve();
      };
      addEventListener("scrollend", done);
      setTimeout(done, 1500); // a scroll with nowhere to go never ends
      el.scrollIntoView({ block: "center", inline: "center", behavior: "smooth" });
    });
  };

  const describe = (el) => {
    const text = names(el)[0] || "";
    const id = el.id ? `#${el.id}` : "";
    return `<${el.tagName.toLowerCase()}${id}>` + (text ? ` "${text.slice(0, 40)}"` : "");
  };

  const kind = (el) => {
    const tag = el.tagName.toLowerCase();
    if (tag === "a") return "link";
    if (tag === "input") return /^(button|submit|reset)$/i.test(el.type) ? "button" : `input:${el.type}`;
    return el.getAttribute("role") || tag;
  };

  // handle is how a verb can name el: text=Label when a label finds this
  // very element, else a CSS selector by id or name, else nothing.
  const handle = (el) => {
    for (const n of names(el)) {
      if (n.length <= 60 && byText(n) === el) return `text=${n}`;
    }
    if (el.id && document.querySelectorAll(`#${CSS.escape(el.id)}`).length === 1) return `#${CSS.escape(el.id)}`;
    const name = el.getAttribute("name");
    if (name) return `${el.tagName.toLowerCase()}[name="${name}"]`;
    return "";
  };

  window.__movie = {
    // quiet is how long ago, in ms, the page last changed.
    quiet: () => Date.now() - changed,

    // point scrolls the target into view and returns the point to click,
    // or why there is none.
    point: async (target) => {
      const el = find(target);
      if (!el) return { error: "missing" };
      await scrollTo(el);
      const r = el.getBoundingClientRect();
      const x = Math.min(Math.max(r.left + r.width / 2, 1), innerWidth - 2);
      const y = Math.min(Math.max(r.top + r.height / 2, 1), innerHeight - 2);
      const hit = document.elementFromPoint(x, y);
      if (hit && hit !== el && !el.contains(hit)) return { error: "covered", by: describe(hit), what: describe(el) };
      return { x, y, what: describe(el) };
    },

    visible: (target) => !!find(target),
    glide,
    pulse,

    // targets lists what a verb can act on, for page and for a miss.
    targets: () =>
      [...document.querySelectorAll(interactive)]
        .filter((el) => !ours(el) && visible(el))
        .slice(0, 60)
        .map((el) => ({ kind: kind(el), target: handle(el), href: el.tagName === "A" ? el.getAttribute("href") : "" })),

    // text is the page's visible text, one trimmed line per line, without
    // runs of blank lines.
    text: () =>
      (document.body ? document.body.innerText : "")
        .split("\n")
        .map(norm)
        .filter((line, i, all) => line || (i > 0 && all[i - 1]))
        .join("\n")
        .trim(),
  };

  if (document.documentElement) ensureCursor();
  else document.addEventListener("DOMContentLoaded", ensureCursor, { once: true });
})();
