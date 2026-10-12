# WeaveGrid · 经纬

以章节之间的因果与伏笔，组织长篇小说的大纲、正文和审核。WeaveGrid 是运行在本机的中文 AI 小说创作工作台：你决定创作方向，模型负责写作，所有修改保留版本与执行记录。

**amazcutere + DeepSeek + GPT** — amazcutere 主导，DeepSeek 与 GPT 提供 AI 辅助。详见 [署名与致谢](CREDITS.md)。本项目与相关模型提供方无官方隶属或背书关系。

**源码可见，非商业使用；商业用途另行授权。** 本项目用于技术交流，不采用 MIT，也不宣称是 OSI 意义的开源。具体许可与例外以 [LICENSE](LICENSE) 为准，商业合作见 [COMMERCIAL.md](COMMERCIAL.md)。

## 能做什么

- 生成章节经纬结构，展示伏笔设定、途中提醒、收回与章节进度。
- 从结局向前编制大纲，再按正序写正文，跨章节核查与修订。
- 大纲、正文、统筹、核查、摘要五种独立模型角色；兼容 Chat Completions 服务。
- 可选统筹 AI 接管创意确认、补充问题、修订取舍与审核，传承记忆并自动压缩。
- 调用或返回格式出错时最多重试三次；暂停、单步、继续、终止均保留进度。
- 每章大纲/正文首次保存后自动打检查点，可查看版本、回退并导出小说。
- 完整工作流画布、日间/黑夜主题、本机 SQLite 存储。

## 快速开始

环境：Linux（主要开发环境）或 macOS；Windows 使用 WSL2。需要 Go **1.27.1+**、Node.js **22.12+**、npm、Python **3.10+**、Bash 和 Make。Node 版本参考 [.node-version](.node-version)。macOS 尚无完整验收记录；原生 Windows 入口不支持 Unix 文件锁。

下载源码并进入项目目录：

```sh
make build
./启动.sh
```

打开 [本机工作台](http://127.0.0.1:8731)。第一次使用创建项目，在“模型”中配置接口、模型名称和凭据来源，然后开始后向编纲。已有二进制时，启动脚本不会再次安装依赖；修改源码后须重新 `make build`。

构建产物为 `bin/weavegrid`。也可以直接运行并指定独立目录：

```sh
./bin/weavegrid --addr 127.0.0.1:8731 --data data
```

## 模型与隐私

默认大纲、正文和统筹使用 OpenCode Go 的 `kimi-k3`，核查、摘要使用 `deepseek-v4.1-flash`。这些只是可修改的配置，不包含订阅、密钥或服务额度。使用前请确认你的提供方支持所填模型；生成会产生调用费用。

凭据来源支持 `opencode-go`、`env:环境变量名` 和无认证服务的 `none`。`opencode-go` 优先读取环境变量 `OPENCODE_API_KEY`，再读取本机 OpenCode 凭据；其他服务可使用 `env:WEAVEGRID_API_KEY`。只填写引用，不把真实密钥粘贴到模型名称、提示词或小说内容中。参见 [.env.example](.env.example)；程序**不会自动加载 `.env`**。

数据默认保存在 `data/novelflow.db`，为兼容旧版保留数据库名、部分 `NOVELFLOW_*` 环境变量与布局格式。改名不会迁移、删除或覆盖已有小说。

本机存储不代表离线推理：使用远程模型/向量服务时，相关小说原文、创意、记忆和引用会发送给所配置的提供方。此应用没有多用户认证，**不要将端口直接暴露到公网或不可信局域网**。备份前停止服务并复制整个数据目录，包括 SQLite WAL 文件。更多边界见 [SECURITY.md](SECURITY.md)。

## 文档与开发

- [完整使用手册](docs/USER_GUIDE.md)：工作流、创作关卡、模型配置、检查点和接口。
- [贡献指南](CONTRIBUTING.md)：开发命令、提交规范及验证方式。
- [发布检查表](docs/RELEASING.md)：公开文件清单、安全导出与历史审查。
- [第三方组件](THIRD_PARTY_NOTICES.md)：许可、来源及分发说明。

前端在 `web/src/`，Go 后端在 `internal/workbench/`，独立结构生成器在 `internal/workbench/generator/`。公开测试使用合成材料；默认不调用真实模型。

```sh
make test          # 本地自动测试；不需要模型密钥
make source-check  # 检查源码导出清单与常见敏感信息
make source-export # 生成不带 Git 历史、数据库或私有记录的源码压缩包
```

AI 生成结果及审核仍需创作者判断；通过格式校验不等于文学质量、一致性或内容权利已得到保证。

## 许可

项目代码（包括经纬生成器）采用 [PolyForm Noncommercial 1.0.0](LICENSE)，版权署名 **amazcutere**，分发时保留 [NOTICE](NOTICE)。该许可允许其定义内的非商业用途；未被许可覆盖的商业用途必须先取得 amazcutere 的单独书面授权。第三方依赖保留各自许可，不受本项目新增商业限制。
