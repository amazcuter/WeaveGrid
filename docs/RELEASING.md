# 发布 WeaveGrid

## 首次公开：从干净源码包开始

私人开发目录可能有小说数据库、验收记录和带隐私的 Git 历史。不要直接将其远端改为公开，也不要对整个工作目录打包。

```sh
make source-check
make source-audit-git
make source-export
```

`source-audit-git` 检查已跟踪文件与所有可达历史对象，若报告私人文件必须先处理；它不会删文件、改写历史或更改暂存区。仅 `.gitignore` / `.gitattributes` 不会让旧提交消失。

`source-export` 从 `scripts/public-files.txt` 明确列出的**当前工作区文件**生成 `dist/weavegrid-source.tar.gz`，无需提前暂存或提交。它不包含 `.git`、`data/`、`.env`、`artifacts/`、`TEST_REPORT.md`、`node_modules`、现有二进制或生成的界面；会拒绝符号链接、缺失文件、已存在的输出文件及常见密钥命中。包中有逐文件 SHA-256 清单；不保留本机用户名、UID 或绝对路径。

已有同名包时使用新名字，例如 `make source-export SOURCE_ARCHIVE=dist/weavegrid-source-0.1.0.tar.gz`。不要在原工作区解压覆盖文件。将压缩包解压到一个新的空目录，核对内容、许可和署名，**在该新目录初始化新的公开仓库**。原来的私人 Git 历史留在本机；不要复制 `.git`。若必须保留历史，先制定并核对历史脱敏方案，再另行处理。

## 发布检查表

- [ ] 项目名 WeaveGrid、署名 amazcutere + DeepSeek + GPT；AI 辅助说明不暗示官方背书。
- [ ] `LICENSE` 为 PolyForm Noncommercial 1.0.0，`NOTICE` 版权行 amazcutere；不将项目标为 MIT 或 OSI 开源。
- [ ] 原始经纬生成器权利人已允许随项目发布；后续外来代码仍须核对来源及商业再许可权限。
- [ ] `COMMERCIAL.md` 说明商业合作流程；发布页有可用的维护者联系渠道，正式商业协议经专业审阅。
- [ ] `source-check` 通过，并人工检查文档、夹具与所有新文件；模式扫描不能发现所有私人情节或未知形式密钥。
- [ ] 不推送旧私人历史；`source-audit-git` 的发现有明确处理结果。
- [ ] 在干净目录 `make build`；按本次实际执行情况记录 `make test` 结果，不沿用旧报告。
- [ ] GitHub CI 已从真实仓库运行成功；配置文件存在不代表 CI 已通过。
- [ ] 发布仓库开启私密安全报告，并核对默认分支保护及协作权限。
- [ ] 准备公开版本号和变更说明；模型服务可用性、费用与隐私边界已说明。

## 源码与二进制分开

源码包不带依赖或本地数据。安装依赖时保留其原始许可；第三方概览在 `THIRD_PARTY_NOTICES.md`。

`make build` 会从实际使用的 Go 模块、前端运行依赖和 Go 工具链收集原始许可，写入 `bin/THIRD_PARTY_LICENSES.txt`。如果发布可执行文件，必须同时分发该文件、项目 `LICENSE`、`NOTICE`、`COMMERCIAL.md`、`THIRD_PARTY_NOTICES.md` 和对应版本源码入口。不要仅上传单个可执行文件后丢弃第三方声明。

ELK/elkjs 使用 EPL-2.0，不受项目的非商业许可替代；保留其声明及对应版本源码获取地址。若修改第三方组件，应另行核对并履行该组件的分发要求。

本仓库的 CI 只进行构建、模拟测试与源码检查，不发布、推送、创建 Release 或调用真实模型。远端发布始终是维护者的显式操作。
