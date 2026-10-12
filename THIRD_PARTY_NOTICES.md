# 参考与第三方组件

WeaveGrid 自有代码采用 PolyForm Noncommercial 1.0.0，见 `LICENSE` / `NOTICE`；商业用途见 `COMMERCIAL.md`。**这一许可不重新许可、限制或替代下面的第三方组件。**

## 思路参考与生成器

本项目参考 [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness) 的角色配置与 `compaction-basic` 记忆压缩思路。其许可为 MIT，版权为 Copyright (c) 2026 DeepSeek；本项目没有打包其运行时、执行引擎或插件源码。

经纬生成器由项目权利人提供并确认可以随项目发布，按项目非商业许可提供。在保留原始随机序列的基础上，应用适配入口增加末章回收入度补齐、按回收入度分段、结构清理和提醒补点。

## 主要运行依赖

以下为当前锁文件中的版本及安装包自带声明，不是整个依赖树的替代清单。更新依赖后须重新生成原始许可集合。

| 组件 | 版本 | 许可 | 来源 |
| --- | --- | --- | --- |
| React / React DOM | 19.3.0 | MIT | [React](https://github.com/facebook/react) |
| React Flow (`@xyflow/react`) | 12.12.0 | MIT | [xyflow](https://github.com/xyflow/xyflow) |
| elkjs | 0.11.1 | EPL-2.0 | [版本源码](https://github.com/kieler/elkjs/tree/0.11.1) |
| Lucide React | 0.468.0 | ISC，部分 Feather 衍生内容为 MIT | [Lucide](https://github.com/lucide-icons/lucide) |
| modernc.org/sqlite | 1.60.1 | BSD-3-Clause；内含组件另见其声明 | [modernc SQLite](https://gitlab.com/cznic/sqlite) |

React Flow 的界面署名继续保留。elkjs 是独立布局依赖，其 EPL-2.0 及相关来源说明必须保留；项目不将它标为 PolyForm 或 MIT。获取该锁定版本也可使用 npm registry 的 `elkjs@0.11.1` 发布包及其仓库源码。依赖内部的额外声明，例如 SQLite、sqlite-vec 和 libc 的第三方声明，均由收集脚本保留。

## 构建依赖与分发

开发工具还包括 Vite（MIT）、TypeScript（Apache-2.0）、Prettier（MIT）、Go 和 Python；各自分发包及传递依赖的声明继续适用。本源码包不包含这些工具本体。

`make build` / `make notices` 运行 `scripts/collect_licenses.py`，从已安装且与锁文件一致的 npm 运行依赖、可执行文件实际导入的 Go 模块以及 Go 标准库收集原始 LICENSE / COPYING / NOTICE / PATENTS 文件，输出 `bin/THIRD_PARTY_LICENSES.txt`。缺失声明或版本不一致时失败，不用一张概览表冒充完整声明。

分发可执行文件时同时附上生成的许可集合、项目 `LICENSE`、`NOTICE`、本文件与源码获取方式。不同构建平台或依赖变化后应重新生成；自行修改、再分发第三方组件时另外核对其要求。
