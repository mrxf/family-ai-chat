#!/usr/bin/env bash
# 发布新版本：改版本号 → 提交 → 打 tag → 推送。GitHub Actions 看到 v* tag 后自动打包并创建 Release。
#   scripts/release.sh                       → 补丁号 +1（1.2.3 → 1.2.4）
#   scripts/release.sh minor                 → 1.2.3 → 1.3.0
#   scripts/release.sh major                 → 1.2.3 → 2.0.0
#   scripts/release.sh 2.0.0                 → 指定版本
#   scripts/release.sh current               → 不改版本号，直接给当前版本打 tag（比如第一次发布）
#   scripts/release.sh patch -m "更新说明"    → 更新说明会显示在 Release 页面和家人手机的更新弹窗里
#
# 版本号唯一来源：app/pubspec.yaml 的 version: 1.2.3+10203
#   versionCode 由版本号推出（1.2.3 → 10203），本地和 CI 打的包一致，升级不会被系统当成降级。
# 网络需要代理时：cc-proxy scripts/release.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PUBSPEC="$ROOT/app/pubspec.yaml"
cd "$ROOT"

fail() { printf '\n  ✗ %s\n\n' "$1" >&2; exit 1; }

ARG="patch" NOTES=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    -m|--notes) NOTES="${2:-}"; shift 2 ;;
    -h|--help) sed -n '2,12p' "$0"; exit 0 ;;
    *) ARG="$1"; shift ;;
  esac
done

[[ -z "$(git status --porcelain)" ]] || fail "还有没提交的改动，先提交或暂存（git stash）再发布"
BRANCH="$(git rev-parse --abbrev-ref HEAD)"
[[ "$BRANCH" == main ]] || fail "当前在 $BRANCH 分支，请切到 main 再发布"

CUR="$(grep -E '^version:' "$PUBSPEC" | sed -E 's/version:[[:space:]]*([0-9]+\.[0-9]+\.[0-9]+).*/\1/')"
[[ "$CUR" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail "app/pubspec.yaml 里的版本号格式不对：$CUR"
IFS=. read -r MAJ MIN PAT <<<"$CUR"

case "$ARG" in
  current) NEXT="$CUR" ;;
  patch)   NEXT="$MAJ.$MIN.$((PAT + 1))" ;;
  minor)   NEXT="$MAJ.$((MIN + 1)).0" ;;
  major)   NEXT="$((MAJ + 1)).0.0" ;;
  *) [[ "$ARG" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail "用法：scripts/release.sh [patch|minor|major|current|x.y.z] [-m 更新说明]"; NEXT="$ARG" ;;
esac

TAG="v$NEXT"
[[ -z "$(git tag --list "$TAG")" ]] || fail "tag $TAG 已经存在。要重新发布同一个版本，先删掉：git tag -d $TAG && git push origin :refs/tags/$TAG"

IFS=. read -r A B C <<<"$NEXT"
(( B < 100 && C < 100 )) || fail "次版本号和补丁号要小于 100（versionCode = 主*10000 + 次*100 + 补丁）"
CODE=$((A * 10000 + B * 100 + C))
LINE="version: $NEXT+$CODE"

if [[ "$(grep -E '^version:' "$PUBSPEC")" != "$LINE" ]]; then
  sed -i.bak -E "s/^version:.*/$LINE/" "$PUBSPEC" && rm -f "$PUBSPEC.bak"
  git add "$PUBSPEC"
  git commit -q -m "release: $TAG"
  echo "版本号：$CUR → $NEXT（versionCode $CODE）"
fi

git tag -a "$TAG" -m "${NOTES:-$TAG}"
git push origin HEAD "refs/tags/$TAG"

REMOTE="$(git remote get-url origin | sed -E 's#\.git$##; s#^git@github\.com:#https://github.com/#')"
printf '\n  ✓ 已推送 %s，打包进度：%s/actions\n    完成后在这里下载：%s/releases/tag/%s\n\n' "$TAG" "$REMOTE" "$REMOTE" "$TAG"
