# 外链图片展示示例

用于验证「点外部链接」的行为：**图片类链接直接在应用内打开大图查看，不再弹"是否用浏览器打开"的确认框**；非图片链接保持原样（先确认，再交给浏览器）。

需要联网。逐个点下面的链接看反应即可。

---

## 一、应该直接弹大图（不出现确认框）

| 类型 | 链接 |
|---|---|
| PNG | [GitHub-Mark.png](https://github.githubassets.com/images/modules/logos_page/GitHub-Mark.png) |
| PNG（带查询参数） | [placehold 600x400?raw=1](https://placehold.co/600x400.png?raw=1) |
| JPG | [gstatic 2.jpg](https://www.gstatic.com/webp/gallery/2.jpg) |
| GIF | [giphy.gif](https://media.giphy.com/media/JIX9t2j0ZTN9S/giphy.gif) |
| WEBP | [gstatic 1.webp](https://www.gstatic.com/webp/gallery/1.webp) |
| SVG | [github.svg](https://simpleicons.org/icons/github.svg) |

裸地址（不带链接文字，直接贴出来）也算：

https://placehold.co/640x360.png

每个都应该直接进入大图查看：可以缩放、拖动、右上角下载、关掉。

---

## 二、仍然会弹确认框（不是图片，应用里显示不了）

| 类型 | 链接 |
|---|---|
| 网页 | [W3C 首页](https://www.w3.org/) |
| PDF | [dummy.pdf](https://www.w3.org/WAI/ER/tests/xhtml/testfiles/resources/pdf/dummy.pdf) |

还有一种：**地址里没有图片后缀**的图片接口（服务器其实返回的是图片，但地址长得不像），判不出来，也会走确认框——这是故意的保守判定，宁可多点一次，也不要把网页错当图片塞进大图。

- 无后缀的图片接口：[httpbin /image/png](https://httpbin.org/image/png)

---

## 三、对照：markdown 图片语法（本来就能点开大图）

下面这个是"内嵌图片"，不是链接，点击/点右上角查看按钮进大图，行为和上面第一组一致。

![远程 PNG](https://placehold.co/320x200.png)

---

## 四、已知的误判情况

如果某个链接后缀是 `.png`，但服务器实际返回的是网页或干脆打不开，大图里会显示"媒体加载失败"，关掉即可。例：

- [后缀是 jpg 但服务器返回网页](https://www.w3schools.com/images/lamp.jpg)

要彻底避免这种误判，得在点击时先请求一次看服务器到底返回什么，代价是点击有明显延迟，所以没这么做。
