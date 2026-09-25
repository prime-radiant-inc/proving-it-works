// The movie browse overlay, added to every document the filmed page loads.
// It draws the cursor the movie shows, tracks when the page last changed so
// verbs can wait for it to settle, and finds the elements verbs act on.
// Running it twice in one document does nothing the second time, and it
// does nothing inside frames: the movie has one cursor, the top page's.
(() => {
  if (window.__movie || window.top !== window) return;
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

  // hasWords reports whether text contains want as whole words, so
  // "Saved" is found in "Saved 2" but not in "Unsaved".
  const hasWords = (text, want) => {
    const word = /[\p{L}\p{N}]/u;
    for (let at = text.indexOf(want); at >= 0; at = text.indexOf(want, at + 1)) {
      const end = at + want.length;
      const cleanStart = at === 0 || !word.test(text[at - 1]) || !word.test(want[0]);
      const cleanEnd = end === text.length || !word.test(text[end]) || !word.test(want[want.length - 1]);
      if (cleanStart && cleanEnd) return true;
    }
    return false;
  };

  // reachable reports whether a click at el's centre would land on el; an
  // element scrolled out of view may be, so it counts.
  const reachable = (el) => {
    const r = el.getBoundingClientRect();
    const x = r.left + r.width / 2;
    const y = r.top + r.height / 2;
    if (x < 0 || y < 0 || x >= innerWidth || y >= innerHeight) return true;
    const hit = document.elementFromPoint(x, y);
    return !hit || hit === el || el.contains(hit);
  };

  // byText finds the element text=Label means. To act on (click, type,
  // choose), a person means a button, link, or field before any other
  // element. To see (wait), any element showing the text will do. Either
  // way exact matches come before whole-word ones, and within a kind of
  // match an element a click can reach comes before one covered by, say,
  // a modal.
  const byText = (text, purpose) => {
    const want = low(text);
    const fields = () => [...document.querySelectorAll(interactive)].filter((el) => !ours(el) && visible(el));
    const everything = () => [...document.body.querySelectorAll("*")].filter((el) => !ours(el) && visible(el));
    const named = (test) => fields().filter((el) => names(el).some((n) => test(n.toLowerCase())));
    const showing = (test) => deepest(everything().filter((el) => test(low(el.innerText)) || names(el).some((n) => test(n.toLowerCase()))));
    const exact = (n) => n === want;
    const words = (n) => hasWords(n, want);
    const tiers = purpose === "act" ? [() => named(exact), () => named(words), () => showing(exact), () => showing(words)] : [() => showing(exact), () => showing(words)];
    for (const tier of tiers) {
      const found = tier();
      if (found.length) return found.find(reachable) || found[0];
    }
    return null;
  };

  // find returns the element a TARGET names: text=Label, or a CSS selector.
  const find = (target, purpose) => {
    if (target.startsWith("text=")) return byText(target.slice(5), purpose);
    let els;
    try {
      els = [...document.querySelectorAll(target)];
    } catch (e) {
      throw new Error(`${target} is neither a CSS selector nor text=Label`);
    }
    return els.filter(visible).find(reachable) || els.find(visible) || null;
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
      if (n.length <= 60 && byText(n, "act") === el) return `text=${n}`;
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
      const el = find(target, "act");
      if (!el) return { error: "missing" };
      await scrollTo(el);
      const r = el.getBoundingClientRect();
      const x = Math.min(Math.max(r.left + r.width / 2, 1), innerWidth - 2);
      const y = Math.min(Math.max(r.top + r.height / 2, 1), innerHeight - 2);
      const hit = document.elementFromPoint(x, y);
      if (hit && hit !== el && !el.contains(hit)) return { error: "covered", by: describe(hit), what: describe(el) };
      return { x, y, what: describe(el) };
    },

    // reveal scrolls the target into view and describes it, or returns ""
    // when nothing shows it yet.
    reveal: async (target) => {
      const el = find(target, "see");
      if (!el) return "";
      await scrollTo(el);
      return describe(el);
    },

    // caretToEnd puts the focused field's caret after its text, or with
    // all, selects the text so typing replaces it. It returns false when
    // the field offers no way to place the caret, as email and number
    // fields do not, and the End key has to.
    caretToEnd: (all) => {
      const el = document.activeElement;
      if (!el) return true;
      if (el.isContentEditable) {
        const range = document.createRange();
        range.selectNodeContents(el);
        if (!all) range.collapse(false);
        getSelection().removeAllRanges();
        getSelection().addRange(range);
        return true;
      }
      if (all && el.select) {
        el.select();
        return true;
      }
      try {
        el.setSelectionRange(el.value.length, el.value.length);
        return true;
      } catch (e) {
        return false;
      }
    },

    // choose picks the option labelled label in the select target, as
    // the select's own popup would; headless Chrome draws no popup.
    choose: (target, label) => {
      const el = find(target, "act");
      if (!el || el.tagName !== "SELECT") return `${target} is not a select`;
      const option = [...el.options].find((o) => low(o.text) === low(label));
      if (!option) return `${describe(el)} has no option ${label}; its options: ${[...el.options].map((o) => norm(o.text)).join(", ")}`;
      option.selected = true; // not el.value: options may share a value
      el.dispatchEvent(new Event("input", { bubbles: true }));
      el.dispatchEvent(new Event("change", { bubbles: true }));
      return "";
    },

    glide,
    pulse,

    // targets lists what a verb can act on, for page and for a miss: the
    // first 60, and how many more there are.
    targets: () => {
      const all = [...document.querySelectorAll(interactive)].filter((el) => !ours(el) && visible(el));
      const value = (el) =>
        el.tagName === "SELECT" ? norm(el.selectedOptions[0] ? el.selectedOptions[0].text : "")
          : /^(INPUT|TEXTAREA)$/.test(el.tagName) && !/^(button|submit|reset|checkbox|radio|password)$/i.test(el.type) ? el.value
          : null;
      return {
        more: Math.max(0, all.length - 60),
        list: all.slice(0, 60).map((el) => ({
          kind: kind(el),
          target: handle(el),
          href: el.tagName === "A" ? el.getAttribute("href") : "",
          value: value(el),
        })),
      };
    },

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
