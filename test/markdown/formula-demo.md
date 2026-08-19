# 数学公式示例

本文档展示了各种数学公式的 LaTeX 写法。

## 基本运算

- 加法：$a + b = c$
- 减法：$x - y = z$
- 乘法：$2 \times 3 = 6$ 或 $2 \cdot 3 = 6$
- 除法：$\frac{a}{b} = c$
- 指数：$x^2 = 4$ 或 $e^{x}$
- 根号：$\sqrt{9} = 3$ 或 $\sqrt[n]{a}$

## 代数公式

### 二次方程
二次方程求根公式：
$$x = \frac{-b \pm \sqrt{b^2 - 4ac}}{2a}$$

### 对数公式
$$\log_a(xy) = \log_a x + \log_a y$$
$$a^{\log_a x} = x$$

## 微积分

### 导数
$$f'(x) = \lim_{h \to 0} \frac{f(x + h) - f(x)}{h}$$

### 积分
不定积分：
$$\int x^n \, dx = \frac{x^{n+1}}{n+1} + C$$

定积分：
$$\int_{0}^{1} x^2 \, dx = \left[\frac{x^3}{3}\right]_{0}^{1} = \frac{1}{3}$$

### 微分方程
欧拉公式：
$$e^{i\pi} + 1 = 0$$

## 矩阵

$$
A = \begin{pmatrix}
a_{11} & a_{12} \\
a_{21} & a_{22}
\end{pmatrix}
$$

矩阵乘法：
$$
\begin{pmatrix}
a & b \\
c & d
\end{pmatrix}
\begin{pmatrix}
x \\
y
\end{pmatrix}
=
\begin{pmatrix}
ax + by \\
cx + dy
\end{pmatrix}
$$

## 概率与统计

### 正态分布
$$f(x) = \frac{1}{\sigma\sqrt{2\pi}} e^{-\frac{(x-\mu)^2}{2\sigma^2}}$$

### 贝叶斯定理
$$P(A|B) = \frac{P(B|A) \cdot P(A)}{P(B)}$$

## 几何

### 勾股定理
$$a^2 + b^2 = c^2$$

### 圆的面积与周长
$$A = \pi r^2$$
$$C = 2\pi r$$

## 物理公式

### 牛顿第二定律
$$F = ma$$

### 质能方程
$$E = mc^2$$

### 万有引力
$$F = G\frac{m_1 m_2}{r^2}$$

## 高级数学

### 泰勒级数
$$f(x) = \sum_{n=0}^{\infty} \frac{f^{(n)}(a)}{n!}(x-a)^n$$

### 傅里叶级数
$$f(x) = \frac{a_0}{2} + \sum_{n=1}^{\infty} \left[a_n \cos(nx) + b_n \sin(nx)\right]$$

### 欧拉恒等式
$$e^{ix} = \cos x + i \sin x$$

## 求和与乘积

$$\sum_{i=1}^{n} i = \frac{n(n+1)}{2}$$

$$\prod_{i=1}^{n} i = n!$$

## 集合论

$$A \cup B = \{x | x \in A \text{ or } x \in B\}$$

$$A \cap B = \{x | x \in A \text{ and } x \in B\}$$

## 上下标混合（Issue #384 回归测试）

以下公式包含同时带有上标和下标的变量，是 Issue #384 的核心复现场景。
修复前，`_` 被 marked 当作强调语法解析，产生 `<em>` 标签导致渲染错乱。

### 行内公式

- 单下标（原本正常）：$x_{i}$、$y_{j}$
- 单上标（原本正常）：$x^{2}$、$e^{x}$
- 上标+下标（修复前会错乱）：$a^{0}_{i}$、$b^{0}_{j}$、$\tau^{0}_{i}$、$y^{0}_{i}$
- 多个上下标混合：$a^{0}_{i} + b^{0}_{j}$ — 修复前此处会出现 `<em>`
- 复杂集合：$\mathcal{B}=\{(x_{i},\tau^{0}_{i},y^{0}_{i})\}_{i=1}^{n}$

### 显示公式

$$\mathcal{B}=\{(x_{i},\tau^{0}_{i},y^{0}_{i})\}_{i=1}^{n},\qquad \mathcal{I}^{-}=\{i:y^{0}_{i}=0\},\quad \mathcal{I}^{+}=\{i:y^{0}_{i}=1\}$$

$$\sum_{i=1}^{n} \tau^{0}_{i} \cdot y^{0}_{i} = \sum_{j \in \mathcal{I}^{+}} \tau^{0}_{j}$$

### 星号 + 下标混合（强调攻击）

公式中同时出现 `*` 和 `_`，修复前会被 marked 同时解析为 `<strong>` 和 `<em>`：

$$a * b_{i} + c^{0}_{j}$$

$$P(A|B) = \frac{P(B|A) \cdot P(A)}{P(B)} \quad \text{where } a^{*}_{i} > 0$$

### `\(...\)` 和 `\[...\]` 定界符

\[\mathcal{L}=\sum_{i=1}^{n} \tau^{0}_{i} \cdot y^{0}_{i}\]

行内：\(\tau^{0}_{i}\) 和 \(y^{0}_{i}\) 的值。

### 代码块内的 `$`（不应被提取为数学公式）

```
这里 $a^{0}_{i}$ 不是数学公式，是代码
$x_{i} + y_{j}$ 也是代码
```

行内代码：`$a_{i}$` 和 `$$x^2$$` 也不是数学公式。

---

提示：这些公式使用 KaTeX 渲染，支持完整的 LaTeX 数学语法。
