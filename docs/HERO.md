# 产品宣传图

ClawBench 产品宣传 Hero 图，展示手机端和平板端的实际使用效果。

![ClawBench 产品宣传图](screenshots/product_hero.png)

## 在线预览

在浏览器中打开 `screenshots/product_hero.html` 即可查看完整交互版宣传图。

## 素材

| 文件 | 说明 |
|------|------|
| `screenshots/product_hero.png` | 最终导出图（1920×1080，2x DPR） |
| `screenshots/product_hero.html` | 纯 HTML/CSS 版本，可编辑 |
| `screenshots/product_hero_assets/bg_tech.jpg` | MiniMax 生成的科技风背景 |
| `screenshots/product_hero_assets/phone_screenshot.jpg` | 手机端截图 |
| `screenshots/product_hero_assets/tablet_screenshot.jpg` | 平板端截图 |
| `screenshots/product_hero_assets/logo.png` | ClawBench Logo |

## 自定义

编辑 `product_hero.html` 中的以下部分：

- **设备尺寸**：`.tablet-screen` / `.phone-screen` 的 `width` 和 `height`
- **功能亮点**：右侧 `.feature-card` 区域
- **品牌信息**：左上角 `.brand` 区域
- **背景**：替换 `bg_tech.jpg` 或修改 `.bg-image` 的 `filter` 属性

修改后用以下命令重新导出：

```bash
cd docs
node -e "
const { chromium } = require('playwright');
(async () => {
  const browser = await chromium.launch();
  const page = await browser.newPage({
    viewport: { width: 1920, height: 1080 },
    deviceScaleFactor: 2,
  });
  await page.goto('file://' + process.cwd() + '/screenshots/product_hero.html');
  await page.waitForTimeout(2000);
  await page.screenshot({ path: 'screenshots/product_hero.png', fullPage: false });
  await browser.close();
  console.log('Done');
})();
"
```
