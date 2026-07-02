#!/usr/bin/env bash
#
# ClawBench 自动升级脚本
#
# 从 https://github.com/xulongzhe/clawbench/releases 拉取最新 release 版本进行升级
#
# 用法:
#   ./upgrade.sh                # 升级到最新版本
#   ./upgrade.sh --check        # 仅检查是否有新版本，不执行升级
#   ./upgrade.sh --force        # 强制升级（即使版本相同）
#   ./upgrade.sh --no-restart   # 升级后不自动重启服务
#   ./upgrade.sh --keep-backup   # 升级后保留备份（默认自动清理旧备份）
#

set -euo pipefail

# ======================== 配置 ========================

REPO="xulongzhe/clawbench"
GITHUB_API="https://api.github.com/repos/${REPO}"
INSTALL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DATA_DIR="${INSTALL_DIR}/.clawbench"
BACKUP_DIR="${DATA_DIR}/upgrade-backup"
VERSION_FILE="${DATA_DIR}/version"

# 检测 OS 和架构
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$ARCH" in
    x86_64)  ARCH="amd64" ;;
    aarch64) ARCH="arm64" ;;
    armv7l)  ARCH="arm64" ;;
esac
ASSET_NAME="clawbench-${OS}-${ARCH}.zip"

# 用户数据文件，升级时永远不覆盖（即使 release 包中包含）
PRESERVE_FILES=(
    "config/config.yaml"
)

# 需要从 release 包同步的目录/文件
# 格式: "源路径" — 从 release 包根目录复制到安装目录
SYNC_ENTRIES=(
    "clawbench"       # 主二进制（前端已通过 go:embed 内嵌）
    "agents"          # 嵌入式智能体二进制（opencode, pi 等）
    "scripts"         # 辅助脚本
    "config/config.example.yaml" # 示例配置
)

# ======================== 颜色输出 ========================

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info()    { echo -e "${BLUE}[INFO]${NC} $*"; }
log_success() { echo -e "${GREEN}[OK]${NC} $*"; }
log_warn()    { echo -e "${YELLOW}[WARN]${NC} $*"; }
log_error()   { echo -e "${RED}[ERROR]${NC} $*" >&2; }

# ======================== 工具函数 ========================

# 检查依赖
check_deps() {
    local missing=()
    for cmd in curl unzip jq; do
        if ! command -v "$cmd" &>/dev/null; then
            missing+=("$cmd")
        fi
    done
    if [[ ${#missing[@]} -gt 0 ]]; then
        log_error "缺少必要依赖: ${missing[*]}"
        log_info "请安装后重试: apt install -y ${missing[*]}"
        exit 1
    fi
}

# 获取本地当前版本
get_local_version() {
    if [[ -f "$VERSION_FILE" ]]; then
        cat "$VERSION_FILE"
    else
        echo ""
    fi
}

# 保存本地版本
save_local_version() {
    local version="$1"
    mkdir -p "$(dirname "$VERSION_FILE")"
    echo "$version" > "$VERSION_FILE"
}

# 从 GitHub API 获取最新 release 信息
# 输出: tag_name
get_latest_release() {
    local response
    local http_code

    response=$(curl -sS -w "\n%{http_code}" \
        -H "Accept: application/vnd.github+json" \
        "${GITHUB_API}/releases/latest" 2>/dev/null) || true

    http_code=$(echo "$response" | tail -1)
    local body=$(echo "$response" | sed '$d')

    if [[ "$http_code" != "200" ]]; then
        log_error "无法获取最新 release 信息 (HTTP $http_code)"
        echo "$body" | grep -o '"message":"[^"]*"' || true
        exit 1
    fi

    echo "$body" | jq -r '.tag_name'
}

# 获取指定版本的下载 URL
get_download_url() {
    local tag="$1"
    echo "https://github.com/${REPO}/releases/download/${tag}/${ASSET_NAME}"
}

# 停止 clawbench 服务
stop_service() {
    log_info "停止 clawbench 服务..."
    # 通过 PID 文件或端口停止
    local pid_file="${DATA_DIR}/server.pid"
    if [[ -f "$pid_file" ]]; then
        local pid=$(cat "$pid_file")
        if kill -0 "$pid" 2>/dev/null; then
            kill "$pid"
            sleep 1
        fi
        rm -f "$pid_file"
    else
        # fallback: 通过端口停止
        local pids=$(lsof -ti :20000 2>/dev/null || true)
        if [[ -n "$pids" ]]; then
            for pid in $pids; do
                kill "$pid" 2>/dev/null || true
            done
            sleep 1
        fi
    fi
    # 确保端口已释放
    local waited=0
    while [[ $waited -lt 10 ]]; do
        if ! ss -tlnp 2>/dev/null | grep -q ":20000"; then
            break
        fi
        sleep 0.5
        waited=$((waited + 1))
    done
    log_success "服务已停止"
}

# 启动 clawbench 服务
start_service() {
    log_info "启动 clawbench 服务..."
    if [[ -x "${INSTALL_DIR}/clawbench" ]]; then
        (cd "${INSTALL_DIR}" && nohup ./clawbench > /dev/null 2>&1 &)
    else
        log_error "找不到 clawbench 二进制，请手动启动服务"
        exit 1
    fi
    log_success "服务已启动"
}

# 备份当前版本
backup_current() {
    log_info "备份当前版本..."

    # 清理旧备份
    if [[ -d "$BACKUP_DIR" ]]; then
        rm -rf "$BACKUP_DIR"
    fi
    mkdir -p "$BACKUP_DIR"

    # 备份所有需要同步的条目
    for entry in "${SYNC_ENTRIES[@]}"; do
        local src="${INSTALL_DIR}/${entry}"
        local dest_dir="${BACKUP_DIR}/$(dirname "$entry")"
        if [[ -e "$src" ]]; then
            mkdir -p "$dest_dir"
            cp -a "$src" "${BACKUP_DIR}/${entry}"
        fi
    done

    # 记录备份时的版本
    local current_version
    current_version=$(get_local_version)
    echo "${current_version:-unknown}" > "${BACKUP_DIR}/.backup_version"

    log_success "备份完成: $BACKUP_DIR"
}

# 回滚到备份版本
rollback() {
    log_error "升级失败，正在回滚..."

    if [[ ! -d "$BACKUP_DIR" ]]; then
        log_error "没有找到备份，无法回滚"
        exit 1
    fi

    for entry in "${SYNC_ENTRIES[@]}"; do
        local backup="${BACKUP_DIR}/${entry}"
        local dest="${INSTALL_DIR}/${entry}"
        if [[ -e "$backup" ]]; then
            rm -rf "$dest"
            mkdir -p "$(dirname "$dest")"
            cp -a "$backup" "$dest"
        fi
    done

    # 恢复版本号
    if [[ -f "${BACKUP_DIR}/.backup_version" ]]; then
        local backup_version
        backup_version=$(cat "${BACKUP_DIR}/.backup_version")
        if [[ "$backup_version" != "unknown" ]]; then
            save_local_version "$backup_version"
        fi
    fi

    log_success "回滚完成"
}

# 清理备份
cleanup_backup() {
    if [[ -d "$BACKUP_DIR" ]]; then
        rm -rf "$BACKUP_DIR"
        log_info "已清理升级备份"
    fi
}

# 检查路径是否在保护列表中
is_preserved() {
    local path="$1"
    local _pf
    for _pf in "${PRESERVE_FILES[@]}"; do
        if [[ "$path" == "$_pf" ]]; then
            return 0
        fi
    done
    return 1
}

# 同步目录：将 src_dir 的内容合并到 dest_dir
# - 新文件直接复制
# - 已存在的文件：检查是否被用户修改过，未修改则更新
# - 用户修改过的 .yaml 配置文件保留不动，但更新对应的 .example 文件
sync_directory() {
    local src_dir="$1"
    local dest_dir="$2"
    local rel_prefix="$3"  # 相对路径前缀，用于日志和保护列表判断

    local updated=0
    local added=0
    local preserved=0

    while IFS= read -r -d '' src_file; do
        local rel_path="${src_file#$src_dir/}"
        local dest_file="${dest_dir}/${rel_path}"
        local full_rel_path="${rel_prefix:+${rel_prefix}/}${rel_path}"

        # 检查是否在保护列表中
        if is_preserved "$full_rel_path"; then
            preserved=$((preserved + 1))
            continue
        fi

        if [[ -f "$dest_file" ]]; then
            # 文件已存在，比较内容
            local md5_src md5_dest
            md5_src=$(md5sum "$src_file" | awk '{print $1}')
            md5_dest=$(md5sum "$dest_file" | awk '{print $1}')

            if [[ "$md5_src" == "$md5_dest" ]]; then
                continue  # 内容相同，无需更新
            fi

            # .example 文件直接覆盖
            if [[ "$rel_path" == *.example ]]; then
                cp -a "$src_file" "$dest_file"
                updated=$((updated + 1))
                continue
            fi

            # .yaml 配置文件：对比对应的 .example 文件判断是否被用户修改
            if [[ "$rel_path" == *.yaml ]]; then
                local example_file="${dest_file%.yaml}.yaml.example"
                local example_src="${src_file%.yaml}.yaml.example"

                if [[ -f "$example_file" ]]; then
                    local md5_dest_example
                    md5_dest_example=$(md5sum "$example_file" | awk '{print $1}')
                    md5_dest=$(md5sum "$dest_file" | awk '{print $1}')

                    if [[ "$md5_dest" == "$md5_dest_example" ]]; then
                        # 本地 yaml 和 example 一致 → 用户没改过，可以更新
                        cp -a "$src_file" "$dest_file"
                        updated=$((updated + 1))
                    else
                        # 用户已自定义，保留不动
                        preserved=$((preserved + 1))
                        continue
                    fi
                else
                    # 没有 example 文件参考，保守地覆盖（非 agent 配置）
                    cp -a "$src_file" "$dest_file"
                    updated=$((updated + 1))
                fi
            else
                # 非 yaml 文件直接覆盖
                cp -a "$src_file" "$dest_file"
                updated=$((updated + 1))
            fi
        else
            # 新文件，直接复制
            mkdir -p "$(dirname "$dest_file")"
            cp -a "$src_file" "$dest_file"
            added=$((added + 1))
        fi
    done < <(find "$src_dir" -type f -print0)

    if [[ $added -gt 0 ]]; then
        log_info "  新增 ${added} 个文件"
    fi
    if [[ $updated -gt 0 ]]; then
        log_info "  更新 ${updated} 个文件"
    fi
    if [[ $preserved -gt 0 ]]; then
        log_info "  保留 ${preserved} 个用户自定义文件"
    fi
}

# 下载并解压新版本
download_and_extract() {
    local tag="$1"
    local url
    url=$(get_download_url "$tag")

    local tmp_dir
    tmp_dir=$(mktemp -d)
    local zip_file="${tmp_dir}/${ASSET_NAME}"

    log_info "下载 ${tag} ..."
    log_info "  URL: ${url}"

    local http_code
    http_code=$(curl -sS -w "%{http_code}" -L -o "$zip_file" "$url" 2>/dev/null) || true

    if [[ "$http_code" != "200" ]]; then
        log_error "下载失败 (HTTP $http_code)"
        log_error "请确认 ${ASSET_NAME} 在 release ${tag} 中存在"
        rm -rf "$tmp_dir"
        exit 1
    fi

    local file_size
    file_size=$(stat -c%s "$zip_file" 2>/dev/null || echo "0")
    log_success "下载完成 ($(numfmt --to=iec "$file_size" 2>/dev/null || echo "${file_size} bytes"))"

    # 解压
    log_info "解压文件..."
    if ! unzip -o -q "$zip_file" -d "$tmp_dir/extracted" 2>/dev/null; then
        log_error "解压失败"
        rm -rf "$tmp_dir"
        exit 1
    fi

    # 查找 release 包根目录
    # zip 包内可能有 "clawbench/" 子目录，需要定位到正确的根目录
    local extract_root="$tmp_dir/extracted"
    if [[ -d "${extract_root}/clawbench" && -f "${extract_root}/clawbench/clawbench" ]]; then
        # 包内有 "clawbench/" 子目录，且该目录下有主二进制
        extract_root="${extract_root}/clawbench"
    elif [[ -d "${extract_root}/clawbench-${OS}-${ARCH}" ]]; then
        extract_root="${extract_root}/clawbench-${OS}-${ARCH}"
    elif [[ $(find "$extract_root" -maxdepth 1 -type d | wc -l) -eq 2 ]]; then
        # 只有一个子目录
        local sub_dir
        sub_dir=$(find "$extract_root" -maxdepth 1 -type d | tail -1)
        if [[ "$sub_dir" != "$extract_root" ]]; then
            extract_root="$sub_dir"
        fi
    fi

    # 安装文件
    log_info "安装新版本文件..."
    for entry in "${SYNC_ENTRIES[@]}"; do
        local src="${extract_root}/${entry}"
        local dest="${INSTALL_DIR}/${entry}"

        if [[ ! -e "$src" ]]; then
            log_warn "  ${entry} 在发布包中未找到，跳过"
            continue
        fi

        if [[ -d "$src" ]]; then
            # 目录：增量合并
            if [[ -d "$dest" ]]; then
                sync_directory "$src" "$dest" "$entry"
            else
                # 本地不存在此目录，直接复制
                mkdir -p "$(dirname "$dest")"
                cp -a "$src" "$dest"
                log_info "  新增目录: ${entry}"
            fi
        elif [[ -f "$src" ]]; then
            # 文件
            local do_copy=true
            if [[ -f "$dest" ]]; then
                # 检查是否在保护列表中
                if is_preserved "$entry"; then
                    log_info "  保留用户配置: ${entry}"
                    do_copy=false
                else
                    local md5_src md5_dest
                    md5_src=$(md5sum "$src" | awk '{print $1}')
                    md5_dest=$(md5sum "$dest" | awk '{print $1}')
                    if [[ "$md5_src" == "$md5_dest" ]]; then
                        do_copy=false  # 内容相同，跳过
                    fi
                fi
            fi

            if [[ "$do_copy" == true ]]; then
                mkdir -p "$(dirname "$dest")"
                cp -a "$src" "$dest"
                # 确保可执行
                chmod +x "$dest" 2>/dev/null || true
                log_info "  更新: ${entry}"
            fi
        fi
    done

    # 确保 clawbench 二进制可执行
    chmod +x "${INSTALL_DIR}/clawbench" 2>/dev/null || true
    # 确保 scripts/ 下所有脚本可执行
    if [[ -d "${INSTALL_DIR}/scripts" ]]; then
        chmod +x "${INSTALL_DIR}/scripts/"*.sh 2>/dev/null || true
        chmod +x "${INSTALL_DIR}/scripts/"*.py 2>/dev/null || true
    fi

    # 清理临时文件
    rm -rf "$tmp_dir"

    log_success "文件安装完成"
}

# ======================== 主流程 ========================

main() {
    local check_only=false
    local force=false
    local no_restart=false
    local keep_backup=false

    # 解析参数
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --check)       check_only=true ;;
            --force)       force=true ;;
            --no-restart)  no_restart=true ;;
            --keep-backup) keep_backup=true ;;
            --data-dir)
                if [[ $# -lt 2 ]]; then
                    log_error "--data-dir 需要一个参数"
                    exit 1
                fi
                DATA_DIR="$2"
                BACKUP_DIR="${DATA_DIR}/upgrade-backup"
                VERSION_FILE="${DATA_DIR}/version"
                shift
                ;;
            --help|-h)
                echo "用法: $0 [选项]"
                echo ""
                echo "选项:"
                echo "  --check         仅检查是否有新版本，不执行升级"
                echo "  --force         强制升级（即使版本相同）"
                echo "  --no-restart    升级后不自动重启服务"
                echo "  --keep-backup   升级后保留备份文件"
                echo "  --data-dir DIR  指定数据目录（默认: INSTALL_DIR/.clawbench）"
                echo "  -h, --help      显示帮助信息"
                exit 0
                ;;
            *)
                log_error "未知参数: $1"
                exit 1
                ;;
        esac
        shift
    done

    echo ""
    echo "╔══════════════════════════════════╗"
    echo "║     ClawBench 自动升级脚本       ║"
    echo "╚══════════════════════════════════╝"
    echo ""
    log_info "安装目录: ${INSTALL_DIR}"
    log_info "数据目录: ${DATA_DIR}"
    log_info "平台: ${OS}-${ARCH}"
    log_info "资源文件: ${ASSET_NAME}"
    echo ""

    # 检查依赖
    check_deps

    # 获取本地版本
    local local_version
    local_version=$(get_local_version)
    if [[ -n "$local_version" ]]; then
        log_info "当前版本: ${local_version}"
    else
        log_warn "未检测到本地版本记录（首次升级或版本文件缺失）"
    fi

    # 获取最新版本
    log_info "正在检查最新版本..."
    local latest_version
    latest_version=$(get_latest_release)
    log_info "最新版本: ${latest_version}"
    echo ""

    # 仅检查模式
    if [[ "$check_only" == true ]]; then
        if [[ -z "$local_version" ]]; then
            log_warn "本地无版本记录，建议执行升级"
            exit 0
        fi
        if [[ "$local_version" == "$latest_version" ]]; then
            log_success "当前已是最新版本 (${local_version})"
        else
            log_warn "有新版本可用: ${local_version} -> ${latest_version}"
            exit 1  # exit code 1 表示有新版本
        fi
        exit 0
    fi

    # 版本比较
    if [[ "$local_version" == "$latest_version" && "$force" != true ]]; then
        log_success "当前已是最新版本 (${local_version})，无需升级"
        log_info "使用 --force 可强制重新安装"
        exit 0
    fi

    if [[ "$force" == true && "$local_version" == "$latest_version" ]]; then
        log_warn "强制重新安装当前版本 ${latest_version}"
    else
        log_info "将升级: ${local_version:-unknown} -> ${latest_version}"
    fi
    echo ""

    # 停止服务
    stop_service

    # 备份当前版本
    backup_current

    # 下载并安装新版本
    if ! download_and_extract "$latest_version"; then
        rollback
        if [[ "$no_restart" != true ]]; then
            start_service
        fi
        exit 1
    fi

    # 更新版本记录
    save_local_version "$latest_version"
    log_success "版本已更新: ${local_version:-unknown} -> ${latest_version}"

    # 清理备份
    if [[ "$keep_backup" != true ]]; then
        cleanup_backup
    else
        log_info "备份保留在: ${BACKUP_DIR}"
    fi

    # 重启服务
    if [[ "$no_restart" != true ]]; then
        echo ""
        start_service
    else
        log_info "跳过服务重启（--no-restart），请手动启动"
    fi

    echo ""
    log_success "升级完成! ${latest_version}"
}

main "$@"
