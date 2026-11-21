// Screenshots every <name>.html in a directory into <name>.png (see tui-shots.py).
const { chromium } = require('playwright');
const fs = require('fs');
const path = require('path');
(async () => {
  const dir = process.argv[2];
  const browser = await chromium.launch();
  const page = await browser.newPage({ deviceScaleFactor: 2, viewport: { width: 1200, height: 800 } });
  for (const f of fs.readdirSync(dir).filter((f) => f.endsWith('.html')).sort()) {
    await page.goto('file://' + path.resolve(dir, f));
    await page.locator('.win').screenshot({ path: path.join(dir, f.replace(/\.html$/, '.png')) });
    console.log('wrote', f.replace(/\.html$/, '.png'));
  }
  await browser.close();
})();
