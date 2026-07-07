# 每日发布后部署

> Task ID: 33 | Cron: `30 02 * * *` | Agent: codebuddy

你是 ClawBench 项目的部署助手。在每日夜间发布（Task 3）完成后，将最新版本部署到本地三个 Docker 实例供测试。

**项目根目录：** 运行 `cd` 到 Git 仓库根目录（即本文件所在仓库的根目录），后续所有命令均基于该目录执行。

## 1. 获取最新版本标签

```bash
git fetch origin --tags
NEW_TAG=$(git tag --sort=-v:refname | head -1)
echo "最新版本标签: $NEW_TAG"
```

## 2. 确认发布产物已就绪

```bash
# 检查 GitHub Release 是否存在该标签的产物
ASSETS=$(gh release view $NEW_TAG --json assets --jq '.assets | length' 2>/dev/null || echo "0")
if [ "$ASSETS" -lt 1 ]; then
  echo "发布产物尚未就绪，等待 60 秒后重试..."
  sleep 60
  ASSETS=$(gh release view $NEW_TAG --json assets --jq '.assets | length' 2>/dev/null || echo "0")
  if [ "$ASSETS" -lt 1 ]; then
    echo "发布产物仍未就绪，跳过本次部署。"
    exit 0
  fi
fi
echo "发布产物已就绪 ($ASSETS 个文件)"
```

## 3. 本地部署：绿色版（Docker，端口 20300）

将 Linux amd64 绿色版下载到本地，部署到 Docker 容器中，端口映射 20300。

### 3.1 清理端口占用

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

### 3.2 下载绿色版

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

### 3.3 停止并移除旧容器（如果存在）

```bash
docker stop clawbench-green 2>/dev/null && docker rm clawbench-green 2>/dev/null || true
```

### 3.4 构建并启动 Docker 容器

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

### 3.5 等待启动并获取密码

```bash
sleep 3
GREEN_PASS=$(docker exec clawbench-green cat /data/.clawbench/auto-password 2>/dev/null || docker exec clawbench-green cat /app/.clawbench/auto-password 2>/dev/null || echo "未找到自动密码")
echo "绿色版 (port 20300) 密码: $GREEN_PASS"
```

## 4. 本地部署：NPM 版（Docker，端口 20500）

通过 npm 安装 clawbench，部署在 Docker 容器中，端口映射 20500。

### 4.1 清理端口占用

```bash
PID_20500=$(lsof -i :20500 -t 2>/dev/null || true)
if [ -n "$PID_20500" ]; then
  echo "端口 20500 被进程 $PID_20500 占用，正在杀掉..."
  kill $PID_20500
  sleep 1
fi
```

### 4.2 停止并移除旧容器（如果存在）

```bash
docker stop clawbench-npm 2>/dev/null && docker rm clawbench-npm 2>/dev/null || true
```

### 4.3 构建并启动 NPM 版 Docker 容器

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
RUN npm install @xulongzhe/clawbench@$NPM_VER @xulongzhe/clawbench-linux-x64@$NPM_VER
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

### 4.4 等待启动并获取密码

```bash
sleep 3
NPM_PASS=$(docker exec clawbench-npm cat /data/.clawbench/auto-password 2>/dev/null || docker exec clawbench-npm cat /app/.clawbench/auto-password 2>/dev/null || echo "未找到自动密码")
echo "NPM 版 (port 20500) 密码: $NPM_PASS"
```

## 5. 本地部署：Docker 镜像版（端口 20400）

拉取 GitHub Container Registry 上发布的 Docker 镜像，端口映射 20400。

### 5.1 清理端口占用

```bash
PID_20400=$(lsof -i :20400 -t 2>/dev/null || true)
if [ -n "$PID_20400" ]; then
  echo "端口 20400 被进程 $PID_20400 占用，正在杀掉..."
  kill $PID_20400
  sleep 1
fi
```

### 5.2 停止并移除旧容器（如果存在）

```bash
docker stop clawbench-image 2>/dev/null && docker rm clawbench-image 2>/dev/null || true
```

### 5.3 拉取镜像并启动

```bash
docker pull ghcr.io/clawbench-dev/clawbench:$NEW_TAG
docker run -d \
  --name clawbench-image \
  -p 20400:20000 \
  -v clawbench-image-data:/data \
  ghcr.io/clawbench-dev/clawbench:$NEW_TAG
```

注意：容器内部服务监听 20000，通过 `-p 20400:20000` 映射到宿主机 20400。

### 5.4 等待启动并获取密码

```bash
sleep 3
IMG_PASS=$(docker exec clawbench-image cat /data/.clawbench/auto-password 2>/dev/null || docker exec clawbench-image cat /app/.clawbench/auto-password 2>/dev/null || echo "未找到自动密码")
echo "Docker 镜像版 (port 20400) 密码: $IMG_PASS"
```

## 6. 打印部署摘要

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

- 本任务依赖每日夜间发布（Task 3）完成，cron 设为 `30 02`（发布任务 `00 02` 后 30 分钟）
- **杀进程时绝对不要用 `pkill` 或 `killall`**，必须用 `lsof -i :PORT -t | xargs kill` 按端口精确杀
- **绝对不要碰端口 20000**——那是用户的主服务
- Docker 镜像版容器内监听 20000，宿主机映射到 20400；绿色版和 NPM 版容器内直接监听各自端口
- NPM 安装使用官方 registry `https://registry.npmjs.org`（不使用国内镜像，因 npmmirror 同步延迟可能导致平台包 404）
- NPM 安装需显式安装平台包 `@xulongzhe/clawbench-linux-x64`，主包不会自动安装可选依赖
- Docker 镜像的 GHCR 地址是 `ghcr.io/clawbench-dev/clawbench`（GitHub 仓库的 organization 是 `clawbench-dev`）
