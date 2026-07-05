# 每日夜间发布

> Task ID: 3 | Cron: `00 02 * * *` | Agent: codebuddy

你是 ClawBench 项目的每日发布助手。请执行以下流程：

**项目根目录：** 运行 `cd` 到 Git 仓库根目录（即本文件所在仓库的根目录），后续所有命令均基于该目录执行。

## 1. 检查是否有新提交需要发布

**只基于远程 `origin/main` 已合并的提交发布，不推本地未验证的代码。** 所有代码改动现在走 PR 流程，本地工作区可能有未合入 main 的内容，不能直接推。

```bash
# 先拉取最新的远程 main
git fetch origin main

LATEST_TAG=$(git tag --sort=-v:refname | head -1)
echo "最新版本标签: $LATEST_TAG"
NEW_COMMITS=$(git log $LATEST_TAG..origin/main --oneline)

if [ -z "$NEW_COMMITS" ]; then
  echo "自 $LATEST_TAG 以来没有新提交，跳过发布。"
  exit 0
fi

echo "新提交:"
echo "$NEW_COMMITS"
```

如果没有新提交，直接结束，不需要发布。

## 2. 分析提交确定版本号

分析 `$NEW_COMMITS` 中的提交消息，按以下规则确定版本升级类型：

### 版本升级规则

**当前项目处于 `0.x.x` 阶段**，适用以下规则：

| 条件 | 升级类型 | 示例 |
|------|---------|------|
| 包含 `feat:` / `feature:` 提交 | **minor** 升级 | v0.20.0 → v0.21.0 |
| 包含 `BREAKING CHANGE` 或 `!:` 提交 | **minor** 升级（0.x 阶段仍升 minor） | v0.20.0 → v0.21.0 |
| 仅包含 `fix:` / `bugfix:` / `perf:` / `chore:` / `docs:` / `style:` / `refactor:` 提交，无任何 `feat:` | **patch** 升级 | v0.20.0 → v0.20.1 |

**重要说明：**
- 在 `0.x.x` 阶段，按 semver 规范，minor 版本本身就可以包含破坏性变更，因此 `BREAKING CHANGE` 不需要升 major
- 只有在项目 API 明确宣布稳定、准备发布 `1.0.0` 时才升 major，发布任务不会自动触发 major 升级
- `perf:` 性能优化等同于 `fix:`，属于 patch 级别（除非标注 breaking）
- `refactor:` 代码重构属于 patch 级别（除非标注 breaking 或伴随 feat）

### 版本号计算

从 `$LATEST_TAG` 提取版本号（去掉 v 前缀），按上述规则递增对应位，然后加回 v 前缀作为新标签。

## 3. 生成详细的 Release Notes

在打标签之前，先生成详细的版本发布说明。你需要分析自上一个版本以来的所有提交，并生成结构化的 Release Notes。

### 3.1 获取完整提交信息

```bash
PREV_TAG=$LATEST_TAG
git log $PREV_TAG..origin/main --format="%H%n%s%n%b%n---END---"
```

### 3.2 分析并分类提交

仔细阅读每个提交的 message 和 body，按以下类别分类：

- **🚀 新特性 (Features)**: 所有 `feat:` / `feature:` 开头的提交
  - 用简洁的中文描述每个特性做了什么（不要直接复制 commit message，要用人话说明用户能感受到的变化）
  - 如果提交 body 中有更详细的说明，提取关键信息

- **🐛 问题修复 (Bug Fixes)**: 所有 `fix:` / `bugfix:` 开头的提交
  - 说明修了什么问题，以及修复后的行为

- **⚡ 性能优化 (Performance)**: 所有 `perf:` 开头的提交
  - 说明优化了哪方面的性能

- **🔧 内部改进 (Internal)**: `refactor:` / `chore:` / `style:` / `ci:` 等
  - 只列出重要的重构，琐碎的（如依赖更新、格式调整）可以合并为一行

- **💥 破坏性变更 (Breaking Changes)**: 包含 `BREAKING CHANGE` 或 `!:` 的提交
  - 必须详细说明什么行为变了，用户需要怎么适配

### 3.3 生成 Release Notes 文本

按以下格式生成（中文），保存到临时文件：

```markdown
## 🚀 新特性

- **{功能名称}**: {描述用户能感受到的变化}（#{commit-hash 前7位}）
- ...

## 🐛 问题修复

- 修复了 {问题描述}，现在 {修复后行为}（#{commit-hash 前7位}）
- ...

## ⚡ 性能优化

- {优化描述}（#{commit-hash 前7位}）
- ...

## 🔧 内部改进

- {重要重构描述}；其他：{依赖更新、格式调整等合并描述}
- ...

## 💥 破坏性变更

- **{变更内容}**: {详细说明和迁移指引}
- ...

---

**完整变更日志**: https://github.com/xulongzhe/clawbench/compare/{上一个版本}...{新版本}
```

**规则：**
- 如果某个分类没有内容，整个分类段落省略（不要输出空分类）
- 分类顺序固定：新特性 → 问题修复 → 性能优化 → 内部改进 → 破坏性变更
- 每个条目末尾附上 commit hash 前7位方便追溯
- 描述用中文，要具体、有价值，不要写"更新了代码"这种废话
- 内部改进中琐碎的提交可以合并描述，不要一个一个列

## 4. 同步本地 main 并创建标签

确保本地 main 与远程同步，然后基于 `origin/main` 打标签：

```bash
git checkout main
git pull origin main
```

注意：不要管工作区中未提交的文件，只处理已合入 main 的内容。**不要执行 `git push origin main` 推送本地代码**——所有代码改动走 PR 流程，发布任务只管打标签。

## 5. 创建并推送标签触发 Release

```bash
git tag $NEW_TAG
git push origin $NEW_TAG
```

## 6. 检查 GitHub Actions 流水线

```bash
sleep 10
RUN_ID=$(gh run list --workflow=release.yml --limit=1 --json databaseId -q .[0].databaseId)
echo "Run ID: $RUN_ID"
gh run watch $RUN_ID --exit-status
```

## 7. 如果流水线失败

1. 查看失败日志：`gh run view $RUN_ID --log-failed`
2. 分析失败原因
3. 如果是构建配置问题（如版本号、依赖），通过 PR 流程修复，**不要直接推 main**
4. 如果是标签问题（如版本号打错），删除标签重打：
   ```bash
   git push origin :refs/tags/$NEW_TAG
   git tag -d $NEW_TAG
   # 修正版本号后重新打标签
   git tag $NEW_TAG
   git push origin $NEW_TAG
   ```
5. 重新监控流水线直到成功

## 8. 更新 Release Notes

流水线成功后，用步骤 3 生成的 Release Notes 替换 GitHub 自动生成的发布说明：

```bash
gh release edit $NEW_TAG --notes-file /tmp/release-notes-$NEW_TAG.md
```

验证更新结果：
```bash
gh release view $NEW_TAG
```

确认 Release Notes 包含结构化的特性/修复分类说明，而不是只有默认的 "Full Changelog" 链接。

## 9. 验证发布产物

```bash
gh release view $NEW_TAG
```

确认产物文件都存在：
- clawbench-linux-amd64.zip
- clawbench-windows-amd64.zip
- clawbench-darwin-arm64.zip
- clawbench-darwin-amd64.zip
- clawbench-android.apk

## 10. 本地部署：绿色版（Docker，端口 20300）

将 Linux amd64 绿色版下载到本地，部署到 Docker 容器中，端口映射 20300。

### 10.1 清理端口占用

**绝对不要使用 `pkill` 或 `killall`**，必须按端口精确杀进程。也绝对不要碰端口 20000（用户主服务）。

```bash
# 检查端口 20300 是否被占用，如果是则杀掉占用进程
PID_20300=$(lsof -i :20300 -t 2>/dev/null || true)
if [ -n "$PID_20300" ]; then
  echo "端口 20300 被进程 $PID_20300 占用，正在杀掉..."
  kill $PID_20300
  sleep 1
fi
```

### 10.2 下载绿色版

```bash
DEPLOY_DIR="/tmp/clawbench-deploy-green"
rm -rf "$DEPLOY_DIR"
mkdir -p "$DEPLOY_DIR"

# 从 GitHub Release 下载 Linux amd64 绿色版
gh release download $NEW_TAG --pattern "clawbench-linux-amd64.zip" --dir "$DEPLOY_DIR"

cd "$DEPLOY_DIR"
unzip clawbench-linux-amd64.zip
chmod +x clawbench/clawbench
```

### 10.3 停止并移除旧容器（如果存在）

```bash
docker stop clawbench-green 2>/dev/null && docker rm clawbench-green 2>/dev/null || true
```

### 10.4 构建并启动 Docker 容器

```bash
# 准备 Dockerfile（复用项目根目录的 Dockerfile，但改端口）
cd "$DEPLOY_DIR/clawbench"

cat > Dockerfile <<'DOCKERFILE'
FROM ubuntu:24.04
RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates curl bash && \
    rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY clawbench .
RUN mkdir -p /data/.clawbench
EXPOSE 20300
ENTRYPOINT ["./clawbench", "--port", "20300", "--data-dir", "/data/.clawbench"]
DOCKERFILE

docker build -t clawbench-green:latest .
docker run -d \
  --name clawbench-green \
  -p 20300:20300 \
  -v clawbench-green-data:/data \
  clawbench-green:latest
```

### 10.5 等待启动并获取密码

```bash
sleep 3
GREEN_PASS=$(docker exec clawbench-green cat /data/.clawbench/auto-password 2>/dev/null || docker exec clawbench-green cat /app/.clawbench/auto-password 2>/dev/null || echo "未找到自动密码")
echo "绿色版 (port 20300) 密码: $GREEN_PASS"
```

## 11. 本地部署：NPM 版（Docker，端口 20500）

通过 npm 安装 clawbench，部署在 Docker 容器中，端口映射 20500。

### 11.1 清理端口占用

```bash
PID_20500=$(lsof -i :20500 -t 2>/dev/null || true)
if [ -n "$PID_20500" ]; then
  echo "端口 20500 被进程 $PID_20500 占用，正在杀掉..."
  kill $PID_20500
  sleep 1
fi
```

### 11.2 停止并移除旧容器（如果存在）

```bash
docker stop clawbench-npm 2>/dev/null && docker rm clawbench-npm 2>/dev/null || true
```

### 11.3 构建并启动 NPM 版 Docker 容器

```bash
NPM_DIR="/tmp/clawbench-deploy-npm"
rm -rf "$NPM_DIR"
mkdir -p "$NPM_DIR"

# NPM 版本号（去掉 v 前缀）
NPM_VER="${NEW_TAG#v}"

cat > "$NPM_DIR/Dockerfile" <<DOCKERFILE
FROM node:24-slim
RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates && \
    rm -rf /var/lib/apt/lists/*
WORKDIR /app
RUN npm config set registry https://registry.npmmirror.com && \
    npm install @xulongzhe/clawbench@$NPM_VER
RUN mkdir -p /data/.clawbench
EXPOSE 20500
CMD ["npx", "clawbench", "--port", "20500", "--data-dir", "/data/.clawbench"]
DOCKERFILE

cd "$NPM_DIR"
docker build -t clawbench-npm:latest .
docker run -d \
  --name clawbench-npm \
  -p 20500:20500 \
  -v clawbench-npm-data:/data \
  clawbench-npm:latest
```

### 11.4 等待启动并获取密码

```bash
sleep 3
NPM_PASS=$(docker exec clawbench-npm cat /data/.clawbench/auto-password 2>/dev/null || docker exec clawbench-npm cat /app/.clawbench/auto-password 2>/dev/null || echo "未找到自动密码")
echo "NPM 版 (port 20500) 密码: $NPM_PASS"
```

## 12. 本地部署：Docker 镜像版（端口 20400）

拉取 GitHub Container Registry 上发布的 Docker 镜像，端口映射 20400。

### 12.1 清理端口占用

```bash
PID_20400=$(lsof -i :20400 -t 2>/dev/null || true)
if [ -n "$PID_20400" ]; then
  echo "端口 20400 被进程 $PID_20400 占用，正在杀掉..."
  kill $PID_20400
  sleep 1
fi
```

### 12.2 停止并移除旧容器（如果存在）

```bash
docker stop clawbench-image 2>/dev/null && docker rm clawbench-image 2>/dev/null || true
```

### 12.3 拉取镜像并启动

```bash
docker pull ghcr.io/xulongzhe/clawbench:$NEW_TAG
docker run -d \
  --name clawbench-image \
  -p 20400:20000 \
  -v clawbench-image-data:/data \
  ghcr.io/xulongzhe/clawbench:$NEW_TAG
```

注意：容器内部服务监听 20000，通过 `-p 20400:20000` 映射到宿主机 20400。

### 12.4 等待启动并获取密码

```bash
sleep 3
IMG_PASS=$(docker exec clawbench-image cat /data/.clawbench/auto-password 2>/dev/null || docker exec clawbench-image cat /app/.clawbench/auto-password 2>/dev/null || echo "未找到自动密码")
echo "Docker 镜像版 (port 20400) 密码: $IMG_PASS"
```

## 13. 打印部署摘要

三个实例都启动完成后，打印密码供人工测试：

```
╔══════════════════════════════════════════════════════════╗
║  ClawBench $NEW_TAG 部署完成                              ║
╠══════════════════════════════════════════════════════════╣
║  绿色版 (Docker)   →  http://localhost:20300              ║
║  密码: $GREEN_PASS                                        ║
╠══════════════════════════════════════════════════════════╣
║  Docker 镜像版     →  http://localhost:20400              ║
║  密码: $IMG_PASS                                          ║
╠══════════════════════════════════════════════════════════╣
║  NPM 版 (Docker)   →  http://localhost:20500              ║
║  密码: $NPM_PASS                                          ║
╚══════════════════════════════════════════════════════════╝

请手动测试以上三个实例，确认功能正常。
```

## 重要注意事项

- **只基于 `origin/main` 已合并的提交发布**，不推本地未验证的代码
- 工作区未提交的文件不要管，只处理已合入 main 的内容
- **不要执行 `git push origin main`**——代码改动走 PR 流程，发布任务只负责打标签和更新 Release Notes
- 不要修改 Go 版本、Node 版本等构建配置（除非流水线因版本问题失败）
- 如果多次重试仍失败，记录错误信息后结束，不要无限循环
- 使用 gh CLI 操作 GitHub，确保 gh 已认证
- Release Notes 必须在流水线成功后更新，替换 GitHub 自动生成的简略说明
- Release Notes 要对用户有价值：说明"做了什么"而不是"改了哪些文件"
- **杀进程时绝对不要用 `pkill` 或 `killall`**，必须用 `lsof -i :PORT -t | xargs kill` 按端口精确杀
- **绝对不要碰端口 20000**——那是用户的主服务
- Docker 镜像版容器内监听 20000，宿主机映射到 20400；绿色版和 NPM 版容器内直接监听各自端口
