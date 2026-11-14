// Run with Node's test runner and Playwright on the module path; see README.md.
// When an account reports more than five rings (e.g. Antigravity models read
// apart from their group, each its own pool beside Gemini's and Claude & GPT's),
// keeping .pq-rings in the card's right column squeezed the account name/reset
// line to nothing and pushed the rings past the card and the panel's right
// edge. Past five rings (.pq-wrap), the rings move below the header lines and
// wrap inside the card, while five or fewer stay beside the name at 440px.
// English and Chinese, Chromium and WebKit, at 440px and 360px; API mocked.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const at = (h) => new Date(Date.now() + h * 36e5).toISOString();
const model = (name, family, pool, used, h) => ({ name, family, pool, used, resetsAt: at(h) });
const pool = (pool, name, used, h) => ({ name, pool, used, resetsAt: at(h) });

const agMany = {
  provider: "antigravity", name: "Antigravity", icon: "antigravity-color", user: "ada@example.com", plan: "Google AI Pro",
  windows: [
    model("Claude Sonnet 4.6", "Claude", "Claude & GPT", 2, 3),
    model("Gemini 4 Argon", "Gemini", "Gemini 4 Argon", 0, 150),
    model("Gemini 4 Pro", "Gemini", "Gemini 4 Pro", 2, 5),
    model("Gemini 3.1 Flash", "Gemini", "Gemini 3.1 Flash", 2, 5),
    model("Gemini 3.8 Flash", "Gemini", "gemini-3.8-flash", 2, 5),
    model("Gemini 3.5 Flash", "Gemini", "gemini-3.5-flash", 2, 5),
    model("Gemini 3 Flash", "Gemini", "Gemini 3 Flash", 2, 5),
    model("Gemini 3.1 Pro", "Gemini", "Gemini 3.1 Pro", 2, 5),
    model("Gemini 3.1 Flash Lite", "Gemini", "Gemini 3.1 Flash Lite", 2, 5),
    model("Gemini 3 Pro", "Gemini", "Gemini 3 Pro", 2, 5),
    pool("Gemini", "7 days", 0, 100), pool("Gemini", "5 hours", 0, 2),
    pool("Claude & GPT", "7 days", 2, 90), pool("Claude & GPT", "5 hours", 2, 3),
    pool("Gemini 4 Argon", "7 days", 0, 150),
    pool("Gemini 4 Pro", "5 hours", 2, 5),
    pool("Gemini 3.1 Flash", "5 hours", 2, 5),
    pool("gemini-3.8-flash", "5 hours", 2, 5),
    pool("gemini-3.5-flash", "5 hours", 2, 5),
    pool("Gemini 3 Flash", "5 hours", 2, 5),
    pool("Gemini 3.1 Pro", "5 hours", 2, 5),
    pool("Gemini 3.1 Flash Lite", "5 hours", 2, 5),
    pool("Gemini 3 Pro", "5 hours", 2, 5),
  ],
};

const agFive = {
  provider: "antigravity", name: "Antigravity", icon: "antigravity-color", user: "bob@example.com", plan: "Google AI Pro",
  windows: [
    model("Claude Sonnet 4.6", "Claude", "Claude & GPT", 30, 3),
    model("Gemini 3 Flash", "Gemini", "Gemini", 0, 2),
    model("Gemini 4 Argon", "Gemini", "Gemini 4 Argon", 47, 150),
    pool("Gemini", "7 days", 1, 100), pool("Gemini", "5 hours", 0, 2),
    pool("Claude & GPT", "7 days", 40, 90), pool("Claude & GPT", "5 hours", 30, 3),
    pool("Gemini 4 Argon", "7 days", 47, 150),
  ],
};

const quotas = [agMany, agFive];

function serve(lang) {
  const settings = { theme: "light", lang, tray: "panel", quotaLeft: true, currency: "usd" };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings });
    if (url.pathname === "/api/settings") return json(settings);
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/usage/quotas") return json(quotas);
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    for (const width of [440, 360]) {
      test(`${engine} ${lang} ${width}px: many quota rings wrap inside the tray card without overflowing`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(() => browser.close());
        const page = await (await browser.newContext({ viewport: { width, height: 640 }, reducedMotion: "reduce" })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang));
        await page.goto("http://magpie.test/?mode=panel");
        await page.locator('#ptabs [data-ptab="usage"]').click();
        await page.locator(".pq-group .pq-more").click();
        const cards = page.locator(".pq-group .pq-card");
        await cards.first().locator(".pq-ring").first().waitFor();
        assert.equal(await cards.count(), 2);

        const many = cards.nth(0);
        const m = await many.evaluate((c) => {
          const cr = c.getBoundingClientRect();
          const u = c.querySelector(".pq-user").getBoundingClientRect();
          const sub = c.querySelector(".pq-sub").getBoundingClientRect();
          const rings = [...c.querySelectorAll(".pq-ring")].map((r) => r.getBoundingClientRect());
          const v = document.querySelector("#view-agents");
          return {
            count: rings.length,
            userW: u.width,
            subW: sub.width,
            ringsLeft: Math.min(...rings.map((r) => r.left)),
            ringsRight: Math.max(...rings.map((r) => r.right)),
            cardLeft: cr.left,
            cardRight: cr.right,
            rows: new Set(rings.map((r) => Math.round(r.top))).size,
            cardScrollW: c.scrollWidth,
            cardClientW: c.clientWidth,
            viewScrollW: v.scrollWidth,
            viewClientW: v.clientWidth,
            docScrollW: document.documentElement.scrollWidth,
            innerW: innerWidth,
          };
        });
        assert.equal(m.count, 13);
        assert(m.userW >= 80, `account name keeps room: ${JSON.stringify(m)}`);
        assert(m.subW >= 80, `reset line keeps room: ${JSON.stringify(m)}`);
        assert(m.ringsLeft >= m.cardLeft - 0.5 && m.ringsRight <= m.cardRight + 0.5, `all rings stay inside the card: ${JSON.stringify(m)}`);
        assert(m.rows >= 2, `13 rings wrap onto multiple rows: ${JSON.stringify(m)}`);
        assert(m.cardScrollW <= m.cardClientW + 1, `card does not overflow: ${JSON.stringify(m)}`);
        assert(m.viewScrollW <= m.viewClientW + 1, `view does not scroll sideways: ${JSON.stringify(m)}`);
        assert(m.docScrollW <= m.innerW + 1, `document does not scroll sideways: ${JSON.stringify(m)}`);

        // 5 rings at 440px still stay beside the name on one row
        if (width === 440) {
          const five = cards.nth(1);
          const f = await five.evaluate((c) => {
            const cr = c.getBoundingClientRect();
            const u = c.querySelector(".pq-user").getBoundingClientRect();
            const rings = [...c.querySelectorAll(".pq-ring")].map((r) => r.getBoundingClientRect());
            return {
              count: rings.length,
              userW: u.width,
              rows: new Set(rings.map((r) => Math.round(r.top))).size,
              ringsRight: Math.max(...rings.map((r) => r.right)),
              cardRight: cr.right,
              sideBySide: Math.min(...rings.map((r) => r.top)) <= u.bottom,
            };
          });
          assert.equal(f.count, 5);
          assert.equal(f.rows, 1);
          assert(f.sideBySide, `5 rings stay beside the name at 440px: ${JSON.stringify(f)}`);
          assert(f.userW >= 80);
          assert(f.ringsRight <= f.cardRight + 0.5);
        }
        assert.deepEqual(errors, []);
        await page.context().close();
      });
    }
  }
}
