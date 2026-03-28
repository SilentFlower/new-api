# GitHub CI: build 分支 Docker 镜像构建与推送

## Goal

为 `build` 分支创建专用的 GitHub Actions 工作流，自动构建多架构 Docker 镜像并推送到 GHCR。

## Requirements

- 仅在 `build` 分支 push 时触发，同时支持 `workflow_dispatch` 手动触发
- 构建 amd64 + arm64 双架构镜像（使用原生 runner）
- 仅推送到 GHCR（GitHub Container Registry）
- Tag 策略：
  - 单架构：`build-{commit短哈希}-{arch}`
  - 多架构 manifest：`build-{commit短哈希}`
  - 滚动 manifest：`build-latest`
- 不包含 cosign 签名
- 复用现有 Dockerfile，与 alpha 工作流结构一致

## Acceptance Criteria

- [ ] push 到 build 分支时自动触发构建
- [ ] 成功构建 amd64 和 arm64 镜像
- [ ] 镜像推送到 GHCR
- [ ] build-latest tag 正确指向最新构建
- [ ] workflow_dispatch 可手动触发

## Out of Scope

- Docker Hub 推送
- Cosign 签名
- 其他分支触发

## Technical Notes

- 基于 `docker-image-alpha.yml` 模板修改
- 使用相同的 action 版本和 pin hash
- Runner：amd64 用 `ubuntu-latest`，arm64 用 `ubuntu-24.04-arm`
