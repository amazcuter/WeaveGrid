import { ContextReader } from "./ContextReader";
import { OutlineText } from "./OutlineText";
import { useEffect, useRef, useState } from "react";
import {
  ArrowLeft,
  ArrowRight,
  BookOpen,
  Check,
  CheckCircle2,
  ChevronRight,
  Copy,
  Download,
  FileText,
  GitBranch,
  History,
  Layers3,
  LoaderCircle,
  Plus,
  RefreshCw,
  Save,
  Search,
  Settings2,
  Sparkles,
  Trash2,
  X,
} from "lucide-react";
import { api } from "./api";
import {
  current,
  labels,
  type Project,
  type Structure,
  type Workflow,
  type Attempt,
  type Run,
  type Idea,
  type Reference,
  type Settings,
  type Citation,
  type Proposal,
  type PromptVersion,
} from "./types";

export type Act = (
  fn: () => Promise<unknown>,
  message?: string,
) => Promise<void>;
type ProjectProps = { p: Project; act: Act };
const formatTime = (s: string) =>
  s
    ? new Date(s).toLocaleString("zh-CN", {
        month: "short",
        day: "numeric",
        hour: "2-digit",
        minute: "2-digit",
      })
    : "—";
export function NewProjectDialog({
  onClose,
  onCreate,
  origin,
  replace = false,
}: {
  onClose: () => void;
  onCreate: (p: Project) => void;
  origin?: Project;
  replace?: boolean;
}) {
  const [name, setName] = useState(
      origin ? origin.name + (replace ? "" : " · 新结构") : "我的新小说",
    ),
    [chapters, setChapters] = useState(origin?.structure.chapters || 50),
    [seed, setSeed] = useState(origin?.structure.seed ?? 7),
    [mode, setMode] = useState("generate"),
    [raw, setRaw] = useState(""),
    [preview, setPreview] = useState<Structure>(),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  const generate = async () => {
    setBusy(true);
    setError("");
    try {
      setPreview(
        await api<Structure>("/structure/preview", "POST", {
          chapters,
          seed,
          ...(mode === "import" ? { raw: JSON.parse(raw) } : {}),
        }),
      );
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };
  useEffect(() => {
    generate();
  }, []);
  const create = async () => {
    if (!preview) return;
    setBusy(true);
    try {
      onCreate(
        replace && origin
          ? await api<Project>(
              `/projects/${origin.id}/structure`,
              "PUT",
              preview,
              origin.revision,
            )
          : await api<Project>("/projects", "POST", {
              name,
              structure: preview,
            }),
      );
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };
  const used = new Set(preview?.lines.flat());
  const exportPreview = () => {
    if (!preview) return;
    const url = URL.createObjectURL(
      new Blob([JSON.stringify(preview, null, 2)], {
        type: "application/json",
      }),
    );
    const a = document.createElement("a");
    a.href = url;
    a.download = `经纬结构-${preview.chapters}章-${preview.seed}.json`;
    a.click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  };
  return (
    <div className="modal-backdrop">
      <div className="new-project-modal">
        <div className="modal-header">
          <div>
            <span className="eyebrow">新故事的起点</span>
            <h2>编织你的故事结构</h2>
            <p>先确定章节之间的呼应，把具体情节留给创作。</p>
          </div>
          <button
            className="icon-button"
            aria-label="关闭新建项目"
            onClick={onClose}
          >
            <X size={20} />
          </button>
        </div>
        <ContextReader compact />
        <div className="modal-columns">
          <div className="setup-form">
            <label>
              小说名称
              <input
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="给故事取个名字"
              />
            </label>
            <div className="segmented">
              <button
                className={mode === "generate" ? "active" : ""}
                onClick={() => {
                  setMode("generate");
                  setPreview(undefined);
                }}
              >
                自动生成
              </button>
              <button
                className={mode === "import" ? "active" : ""}
                onClick={() => {
                  setMode("import");
                  setPreview(undefined);
                }}
              >
                导入 JSON
              </button>
            </div>
            <div className="field-row">
              <label>
                章节数
                <input
                  type="number"
                  min={mode === "generate" ? 3 : 1}
                  max={2000}
                  value={chapters}
                  onChange={(e) => {
                    setChapters(Number(e.target.value));
                    setPreview(undefined);
                  }}
                />
              </label>
              {mode === "generate" && (
                <label>
                  随机种子
                  <input
                    type="number"
                    value={seed}
                    onChange={(e) => {
                      setSeed(Number(e.target.value));
                      setPreview(undefined);
                    }}
                  />
                </label>
              )}
            </div>
            {mode === "import" ? (
              <>
                <label>
                  结构文件
                  <input
                    type="file"
                    accept=".json"
                    onChange={async (e) => {
                      const f = e.target.files?.[0];
                      if (f) {
                        setRaw(await f.text());
                        setPreview(undefined);
                      }
                    }}
                  />
                </label>
                <textarea
                  aria-label="结构JSON"
                  rows={9}
                  value={raw}
                  onChange={(e) => {
                    setRaw(e.target.value);
                    setPreview(undefined);
                  }}
                  placeholder="粘贴完整结构或 [[1,10],[2,9]]"
                />
              </>
            ) : (
              <div className="explanation">
                <GitBranch size={20} />
                <p>
                  每章回收的独立伏笔超过 4
                  条时，在该章结束一个剧情单元；全书末章至少回收 5
                  条，不足时自动补齐。相同章节数和种子会生成相同结构，具体事件由你和创作模型决定。
                </p>
              </div>
            )}
            <div className="button-row">
              <button disabled={busy} onClick={generate}>
                {busy ? (
                  <LoaderCircle className="spin" size={15} />
                ) : (
                  <RefreshCw size={15} />
                )}
                生成预览
              </button>
              {mode === "generate" && (
                <button
                  onClick={() => {
                    setSeed(Math.floor(Math.random() * 1000000));
                    setPreview(undefined);
                  }}
                >
                  换个种子
                </button>
              )}
            </div>
          </div>
          <div className="structure-preview">
            {preview ? (
              <>
                <div className="preview-heading">
                  <span className="eyebrow">结构预览</span>
                  <span className="tag">仅结构</span>
                </div>
                <div className="metric-grid">
                  <div>
                    <strong>{preview.chapters}</strong>
                    <span>完整章节</span>
                  </div>
                  <div>
                    <strong>{preview.lines.length}</strong>
                    <span>章节关系</span>
                  </div>
                  <div>
                    <strong>{preview.threads.length}</strong>
                    <span>伏笔位置</span>
                  </div>
                </div>
                <div className="chapter-mosaic">
                  {Array.from(
                    { length: Math.min(preview.chapters, 200) },
                    (_, i) => i + 1,
                  ).map((c) => (
                    <span
                      key={c}
                      title={`第 ${c} 章${used.has(c) ? "" : " · 自由剧情章"}`}
                      className={
                        preview.nodes.some((n) => n.ch === c)
                          ? "turning"
                          : used.has(c)
                            ? "connected"
                            : ""
                      }
                    >
                      {String(c).padStart(2, "0")}
                    </span>
                  ))}
                </div>
                <div className="legend">
                  <span>
                    <i className="teal" />
                    转折节点
                  </span>
                  <span>
                    <i className="mint" />
                    关联章节
                  </span>
                  <span>
                    <i />
                    自由剧情章
                  </span>
                </div>
                <h4>剧情单元</h4>
                <div className="arc-list">
                  {preview.arcs.length ? (
                    preview.arcs.map(([a, b], i) => (
                      <span key={a}>
                        <small>{String(i + 1).padStart(2, "0")}</small>
                        {a} — {b} 章
                      </span>
                    ))
                  ) : (
                    <p className="muted">导入的连线未提供剧情单元。</p>
                  )}
                </div>
                <p className="muted small">
                  {preview.chapters - used.size} 个无连线章节同样参与创作。
                  {preview.fixedReminders.length > 0 &&
                    `已为 ${preview.fixedReminders.length} 条长跨度伏笔补入提醒位置。`}
                </p>
                <details>
                  <summary>转折节点（{preview.nodes.length}）</summary>
                  {preview.nodes.map((n, i) => (
                    <div className="connection-row" key={i}>
                      <b>第 {n.ch} 章</b>
                      <span>{n.kind}</span>
                      <small>单元 {n.arc}</small>
                    </div>
                  ))}
                </details>
                <details>
                  <summary>伏笔起止与提醒（{preview.threads.length}）</summary>
                  {preview.threads.map((t) => (
                    <div className="connection-row" key={t.id}>
                      <b>{t.id}</b>
                      <span>
                        {t.plant} → {t.payoff}
                      </span>
                      <small>
                        {t.reminds.length
                          ? `提醒：${t.reminds.join("、")}`
                          : "无需提醒"}
                      </small>
                    </div>
                  ))}
                </details>
                <button onClick={exportPreview}>
                  <Download size={14} />
                  导出当前预览
                </button>
              </>
            ) : (
              <div className="empty-state compact">
                <GitBranch size={35} />
                <p>生成预览后，确认采用结构</p>
              </div>
            )}
          </div>
        </div>
        {error && <div className="inline-error">{error}</div>}
        <footer className="modal-footer">
          <span>
            {replace
              ? "尚未开始创作，可采用新结构"
              : origin
                ? "已有创作或运行记录，将保存为独立新项目"
                : "新项目的大纲、正文和创意均为空"}
          </span>
          <button
            className="primary"
            disabled={!preview || busy}
            onClick={create}
          >
            {replace ? "采用新结构" : "采用结构并创建"}
            <ArrowRight size={16} />
          </button>
        </footer>
      </div>
    </div>
  );
}

export function Inspector({
  p,
  workflow,
  selected,
  setSelected,
  attempts,
  run,
  onSaved,
  act,
  expand,
  onCreativeInput,
}: ProjectProps & {
  workflow: Workflow;
  selected: string;
  setSelected: (id: string) => void;
  attempts: Attempt[];
  run?: Run;
  onSaved: (p: Project) => void;
  expand: (id: string) => void;
  onCreativeInput: () => void;
}) {
  const [tab, setTab] = useState("state"),
    [text, setText] = useState(""),
    [preview, setPreview] = useState(""),
    [attemptID, setAttemptID] = useState("");
  const parent =
    workflow.nodes.find((n) => n.id === selected[0]) || workflow.nodes[0];
  const step = parent.steps.find((s) => s.id === selected);
  const history = p.prompts[selected] || [];
  useEffect(() => {
    setText(history.at(-1)?.text ?? workflow.defaults[selected] ?? "");
    setPreview("");
    setAttemptID("");
  }, [selected, p.id, history.length, workflow]);
  const filtered = attempts.filter((a) =>
    selected.length === 1 ? a.node.startsWith(selected) : a.node === selected,
  );
  const chosen = filtered.find((a) => a.id === attemptID) || filtered.at(-1);
  const isAI = step ? step.ai : parent.steps.some((s) => s.ai);
  return (
    <>
      <div className="inspector-title">
        <span className="node-token">{selected}</span>
        <div>
          <span className="eyebrow">节点详情</span>
          <h2>{step?.name || parent.name}</h2>
        </div>
      </div>
      <div className="inspector-tabs">
        <button
          className={tab === "state" ? "active" : ""}
          onClick={() => setTab("state")}
        >
          执行状态
        </button>
        <button
          className={tab === "prompt" ? "active" : ""}
          onClick={() => setTab("prompt")}
        >
          提示词
        </button>
      </div>
      <div className="inspector-body">
        <label className="small">
          查看节点
          <select
            value={selected}
            onChange={(e) => setSelected(e.target.value)}
          >
            <option value={parent.id}>{parent.id} · 节点公共提示词</option>
            {parent.steps.map((s) => (
              <option key={s.id} value={s.id}>
                {s.id} · {s.name}
                {s.ai ? " · AI" : ""}
              </option>
            ))}
          </select>
        </label>
        {tab === "prompt" && parent.id === "C" ? (
          <p>人工创意由你在输入弹窗填写并确认，不调用模型。</p>
        ) : tab === "prompt" ? (
          <>
            <p className="muted small">
              {isAI
                ? "子步骤提示词与所属节点公共提示词共同生效。修改保留历史，在下一次请求时采用。"
                : "此程序步骤不调用模型。编号校验和状态转移由程序执行。"}
            </p>
            <div className="variable-tags">
              {Object.keys(workflow.variables).map((v) => (
                <button key={v} onClick={() => setText((t) => t + ` {{${v}}}`)}>
                  {v}
                </button>
              ))}
            </div>
            <textarea
              className="prompt-editor"
              aria-label="节点提示词"
              value={text}
              onChange={(e) => setText(e.target.value)}
              placeholder="填写此节点的任务要求"
            />
            <div className="button-row wrap">
              <button
                className="primary"
                onClick={() =>
                  act(
                    async () =>
                      onSaved(
                        await api<Project>(
                          `/projects/${p.id}/prompts/${selected}`,
                          "PUT",
                          { text },
                          p.revision,
                        ),
                      ),
                    "提示词已保存",
                  )
                }
              >
                <Save size={14} />
                保存
              </button>
              <button
                onClick={() => setText(workflow.defaults[selected] || "")}
              >
                恢复默认
              </button>
              <button
                onClick={() =>
                  act(async () =>
                    setPreview(
                      JSON.stringify(
                        await api(
                          `/projects/${p.id}/prompts/${selected}/preview`,
                          "POST",
                          {
                            text,
                            chapter: run?.chapter || 1,
                            mode: run?.mode || "backward",
                            runId: run?.id,
                          },
                        ),
                        null,
                        2,
                      ),
                    ),
                  )
                }
              >
                预览请求
              </button>
            </div>
            {preview && <pre className="output-preview">{preview}</pre>}
            <h3>历史版本</h3>
            {history
              .slice()
              .reverse()
              .map((h) => (
                <button
                  key={h.id}
                  className="history-row"
                  onClick={() => setText(h.text)}
                >
                  <History size={13} />
                  <span>{formatTime(h.created)}</span>
                  <small>载入</small>
                </button>
              ))}
          </>
        ) : (
          <>
            <div className="node-summary">
              <span
                className={`tag ${run?.stage === parent.id ? "green" : ""}`}
              >
                {run?.stage === parent.id ? labels[run.status] : "等待执行"}
              </span>
              <p>{parent.description}</p>
              <button className="text-button" onClick={() => expand(parent.id)}>
                定位 {parent.steps.length} 个内部步骤
                <ChevronRight size={14} />
              </button>
            </div>
            {parent.id === "C" ? (
              <>
                <h3>人工输入与输出</h3>
                {run?.stage === "C" && (
                  <button className="primary" onClick={onCreativeInput}>
                    {run.data.creativeInput?.submittedAt
                      ? "查看已提交创意"
                      : "填写本章创意"}
                  </button>
                )}
                {run?.creativeInputs?.length ? (
                  [...run.creativeInputs].reverse().map((input) => (
                    <section className="human-output" key={input.id}>
                      <h4>
                        第 {input.chapter} 章 ·{" "}
                        {input.source === "orchestrator"
                          ? "统筹提交"
                          : "人工提交"}
                      </h4>
                      <small>{formatTime(input.submittedAt)}</small>
                      {input.ideas.map((i) => (
                        <div key={i.id}>
                          <small>
                            {i.required ? "必留要求" : "可选想法"} ·{" "}
                            {i.chapter === 0 ? "全书" : "本章"}
                            {!i.active ? " · 未采用" : ""}
                          </small>
                          <pre className="prose-output">{i.text}</pre>
                        </div>
                      ))}
                      {input.emptyConfirmed && (
                        <p>
                          {input.source === "orchestrator"
                            ? "统筹确认本章交由写作模型自由构思。"
                            : "你已明确确认本章不提供创意。"}
                        </p>
                      )}
                    </section>
                  ))
                ) : (
                  <p className="muted">
                    等待你的输入，提交后这里显示原样输出。
                  </p>
                )}
              </>
            ) : filtered.length > 0 ? (
              <>
                <h3>
                  请求记录 <span>{filtered.length}</span>
                </h3>
                <select
                  aria-label="选择执行记录"
                  value={chosen?.id || ""}
                  onChange={(e) => setAttemptID(e.target.value)}
                >
                  {filtered
                    .slice()
                    .reverse()
                    .map((a) => (
                      <option key={a.id} value={a.id}>
                        第{a.chapter}章 · {a.node} · {labels[a.status]} ·{" "}
                        {formatTime(a.created)}
                      </option>
                    ))}
                </select>
                {chosen && (
                  <>
                    <div className="attempt-meta">
                      <span>{chosen.modelConfig.model}</span>
                      <span>
                        {chosen.usage?.total || 0} tokens ·{" "}
                        {Math.max(
                          0,
                          Math.round(
                            ((chosen.finished
                              ? Date.parse(chosen.finished)
                              : Date.now()) -
                              Date.parse(chosen.created)) /
                              1000,
                          ),
                        )}{" "}
                        秒
                      </span>
                    </div>
                    {chosen.error && (
                      <p className="inline-error">{chosen.error}</p>
                    )}
                    <h3>输出内容</h3>
                    <pre className="prose-output">
                      {chosen.output || "正在等待模型输出…"}
                    </pre>
                    <details>
                      <summary>
                        输入材料与出处（{chosen.inputs?.length || 0}）
                      </summary>
                      <Citations items={chosen.inputs || []} />
                    </details>
                    <details>
                      <summary>实际提示词 · {chosen.promptVersion}</summary>
                      <pre className="output-preview">{chosen.prompt}</pre>
                    </details>
                    {chosen.reasoning && (
                      <details>
                        <summary>模型提供的推理输出</summary>
                        <pre className="output-preview">{chosen.reasoning}</pre>
                      </details>
                    )}
                  </>
                )}
              </>
            ) : (
              <div className="empty-state compact">
                <ActivityIcon />
                <p>执行后，这里将显示真实输入、输出与版本记录。</p>
              </div>
            )}
          </>
        )}
      </div>
    </>
  );
}
function ActivityIcon() {
  return <Layers3 size={28} />;
}
function Citations({ items }: { items: Citation[] }) {
  return (
    <div className="citations">
      {items.map((c, i) => (
        <div key={i}>
          <small>
            第 {c.chapter} 章 · {labels[c.kind]} · 段落 {c.paragraph}
            <span className="tag">{c.plan ? "计划" : "原文"}</span>
          </small>
          <p>{c.text}</p>
          <code>{c.version}</code>
        </div>
      ))}
    </div>
  );
}

export function Editor({
  p,
  chapter,
  setChapter,
  kind,
  setKind,
  onSaved,
  act,
}: ProjectProps & {
  chapter: number;
  setChapter: (n: number) => void;
  kind: string;
  setKind: (k: string) => void;
  onSaved: (p: Project) => void;
}) {
  const v = current(p, chapter, kind);
  const pending = p.pendingEdits?.find(
    (x) => x.chapter === chapter && x.kind === kind,
  );
  const [text, setText] = useState(""),
    [base, setBase] = useState(""),
    [version, setVersion] = useState(""),
    [query, setQuery] = useState(""),
    [results, setResults] = useState<Citation[]>([]);
  const dirty = useRef(false);
  const editingKey = useRef("");
  useEffect(() => {
    const key = `${p.id}:${chapter}:${kind}`;
    // 后台采用新版本时，保留正在输入的文字；旧基准由后端冲突校验保护。
    if (editingKey.current === key && dirty.current) return;
    editingKey.current = key;
    dirty.current = false;
    setText(pending?.text ?? v?.text ?? "");
    setBase(v?.id || "");
    setVersion(v?.id || "");
  }, [p.id, chapter, kind, v?.id, pending?.id]);
  const history = p.versions
    .filter((x) => x.chapter === chapter && x.kind === kind)
    .slice()
    .reverse();
  return (
    <div className="page editor-page">
      <div className="page-heading">
        <div>
          <span className="eyebrow">章节编辑</span>
          <h2>第 {String(chapter).padStart(2, "0")} 章</h2>
        </div>
        <div className="button-row">
          <button
            disabled={chapter <= 1}
            onClick={() => setChapter(chapter - 1)}
            aria-label="上一章"
          >
            <ArrowLeft size={16} />
          </button>
          <button
            disabled={chapter >= p.chapters.length}
            onClick={() => setChapter(chapter + 1)}
            aria-label="下一章"
          >
            <ArrowRight size={16} />
          </button>
          <a
            className="button"
            href={`/api/projects/${p.id}/export?kind=${kind}`}
          >
            <Download size={15} />
            导出全书
          </a>
          <a
            className="button"
            href={`/api/projects/${p.id}/export?kind=${kind}&format=txt`}
          >
            TXT
          </a>
        </div>
      </div>
      <div className="editor-toolbar">
        <div className="segmented">
          <button
            className={kind === "outline" ? "active" : ""}
            onClick={() => setKind("outline")}
          >
            章节大纲
          </button>
          <button
            className={kind === "draft" ? "active" : ""}
            onClick={() => setKind("draft")}
          >
            正文
          </button>
        </div>
        <select
          aria-label="内容历史版本"
          value={version}
          onChange={(e) => {
            setVersion(e.target.value);
            const h = history.find((x) => x.id === e.target.value);
            if (h) {
              dirty.current = true;
              setText(h.text);
            }
          }}
        >
          <option value="">未生成</option>
          {history.map((h) => (
            <option key={h.id} value={h.id}>
              {h.id === v?.id ? "当前采用 · " : ""}
              {formatTime(h.created)}
            </option>
          ))}
        </select>
        <span className="muted small">{Array.from(text).length} 字</span>
        <button
          className="primary"
          onClick={() =>
            act(async () => {
              const updated = await api<Project>(
                `/projects/${p.id}/edits`,
                "POST",
                { chapter, kind, text, base },
                p.revision,
              );
              dirty.current = false;
              onSaved(updated);
            }, "已保存章节编辑")
          }
        >
          <Save size={14} />
          {version && version !== v?.id ? "以此版本保存" : "保存修改"}
        </button>
      </div>
      <textarea
        className="manuscript"
        aria-label="章节内容"
        placeholder="这一章还没有内容。开始工作流自动生成，或在这里写下你的版本。"
        value={text}
        onChange={(e) => {
          dirty.current = true;
          setText(e.target.value);
        }}
      />
      {base !== (v?.id || "") && (
        <div className="banner waiting">
          <span>后台已有新版本，当前编辑仍保留。请对照后更新编辑基准。</span>
          <button onClick={() => setBase(v?.id || "")}>
            保留编辑并更新基准
          </button>
        </div>
      )}
      <div className="editor-note">
        <span>
          {pending
            ? "有待采用编辑 · 下一安全点生效"
            : v
              ? "当前有效版本 " + v.id.slice(0, 8)
              : "尚未生成内容"}
        </span>
        <span>修改保留旧版本，可随时回看</span>
      </div>
      {v && text !== v.text && (
        <details className="search-section" open>
          <summary>与当前采用版本对照</summary>
          <div className="diff-grid">
            <div>
              <h4>当前采用</h4>
              <pre>{v.text}</pre>
            </div>
            <div>
              <h4>待保存内容</h4>
              <pre>{text}</pre>
            </div>
          </div>
        </details>
      )}
      <details className="search-section">
        <summary>
          <Search size={14} />
          查找全书原文
        </summary>
        <div className="button-row">
          <input
            aria-label="检索关键词"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="人物、物品或事件关键词"
          />
          <button
            onClick={() =>
              act(async () =>
                setResults(
                  await api<Citation[]>(
                    `/projects/${p.id}/search?q=${encodeURIComponent(query)}`,
                  ),
                ),
              )
            }
          >
            检索
          </button>
        </div>
        <Citations items={results} />
      </details>
    </div>
  );
}

export function Ideas({
  p,
  save,
  act,
}: ProjectProps & { save: (body: unknown) => Promise<void> }) {
  const [items, setItems] = useState<Idea[]>(p.ideas),
    [batch, setBatch] = useState("");
  const baseline = useRef(JSON.stringify(p.ideas));
  useEffect(() => {
    const previous = baseline.current;
    baseline.current = JSON.stringify(p.ideas);
    setItems((old) => (JSON.stringify(old) === previous ? p.ideas : old));
  }, [p.ideas]);
  return (
    <div className="page">
      <PageTitle
        eyebrow="创意笔记"
        title="创意与必留要求"
        text="提前按章录入。必留要求会交给生成与核查步骤，可选想法允许自由取舍。"
      />
      <div className="button-row">
        <button
          onClick={() =>
            setItems([
              ...items,
              { id: "", chapter: 0, text: "", required: false, active: true },
            ])
          }
        >
          <Plus size={15} />
          添加点子
        </button>
        <button
          className="primary"
          onClick={() =>
            act(async () => {
              baseline.current = JSON.stringify(items);
              await save({ ideas: items });
            }, "创意已保存")
          }
        >
          <Save size={14} />
          保存全部
        </button>
      </div>
      <div className="ideas-list">
        {items.map((item, i) => (
          <div
            className={`idea-card ${!item.active ? "inactive" : ""}`}
            key={item.id || `new-${i}`}
          >
            <div className="idea-meta">
              <label>
                章节
                <select
                  value={item.chapter}
                  onChange={(e) =>
                    setItems(
                      items.map((x, j) =>
                        j === i ? { ...x, chapter: Number(e.target.value) } : x,
                      ),
                    )
                  }
                >
                  <option value={0}>全书</option>
                  {p.chapters.map((c) => (
                    <option key={c.number} value={c.number}>
                      第 {c.number} 章
                    </option>
                  ))}
                </select>
              </label>
              <label className="check-label">
                <input
                  type="checkbox"
                  checked={item.required}
                  onChange={(e) =>
                    setItems(
                      items.map((x, j) =>
                        j === i ? { ...x, required: e.target.checked } : x,
                      ),
                    )
                  }
                />
                必留要求
              </label>
              <label className="check-label">
                <input
                  type="checkbox"
                  checked={item.active}
                  onChange={(e) =>
                    setItems(
                      items.map((x, j) =>
                        j === i ? { ...x, active: e.target.checked } : x,
                      ),
                    )
                  }
                />
                采用
              </label>
              <button
                className="icon-button"
                aria-label="删除点子"
                onClick={() => setItems(items.filter((_, j) => j !== i))}
              >
                <Trash2 size={15} />
              </button>
            </div>
            <textarea
              aria-label={`创意内容${i + 1}`}
              rows={3}
              value={item.text}
              onChange={(e) =>
                setItems(
                  items.map((x, j) =>
                    j === i ? { ...x, text: e.target.value } : x,
                  ),
                )
              }
              placeholder="一个场景、一句台词，或不想失去的情节…"
            />
          </div>
        ))}
      </div>
      <div className="card">
        <h3>批量录入</h3>
        <p className="muted small">
          每行格式：章号 | 必留或可选 | 点子内容。章号 0 表示全书。
        </p>
        <textarea
          aria-label="批量创意"
          rows={4}
          value={batch}
          onChange={(e) => setBatch(e.target.value)}
          placeholder={
            "10 | 必留 | 主角不能在这一章知道真相\n20 | 可选 | 重逢发生在旧车站"
          }
        />
        <button
          onClick={() => {
            const added = batch
              .split("\n")
              .filter(Boolean)
              .map((line) => {
                const [ch, flag, ...content] = line.split("|");
                return {
                  id: "",
                  chapter: Number(ch.trim()),
                  required: flag?.trim() === "必留",
                  active: true,
                  text: content.join("|").trim(),
                };
              });
            setItems([...items, ...added]);
            setBatch("");
          }}
        >
          加入列表
        </button>
      </div>
    </div>
  );
}
export function References({
  p,
  save,
  act,
}: ProjectProps & { save: (body: unknown) => Promise<void> }) {
  const [items, setItems] = useState<Reference[]>(p.references);
  const baseline = useRef(JSON.stringify(p.references));
  useEffect(() => {
    const previous = baseline.current;
    baseline.current = JSON.stringify(p.references);
    setItems((old) => (JSON.stringify(old) === previous ? p.references : old));
  }, [p.references]);
  return (
    <div className="page">
      <PageTitle
        eyebrow="资料笔记"
        title="故事的参考资料"
        text="人物设定、背景资料和写作约定会作为参考材料提供给模型。"
      />
      <div className="button-row">
        <button
          onClick={() => setItems([...items, { id: "", title: "", text: "" }])}
        >
          <Plus size={15} />
          添加资料
        </button>
        <label className="button file-button">
          <FileText size={14} />
          导入文本
          <input
            type="file"
            accept=".md,.txt"
            onChange={async (e) => {
              const f = e.target.files?.[0];
              if (f)
                setItems([
                  ...items,
                  { id: "", title: f.name, text: await f.text() },
                ]);
            }}
          />
        </label>
        <button
          className="primary"
          onClick={() =>
            act(async () => {
              baseline.current = JSON.stringify(items);
              await save({ references: items });
            }, "资料已保存")
          }
        >
          <Save size={14} />
          保存资料
        </button>
        <a
          className="button"
          href={`/api/projects/${p.id}/export?kind=structure`}
        >
          <Download size={14} />
          导出经纬结构
        </a>
      </div>
      {items.map((item, i) => (
        <div className="card reference-card" key={item.id || i}>
          <div className="button-row">
            <input
              aria-label={`资料标题${i + 1}`}
              placeholder="资料标题"
              value={item.title}
              onChange={(e) =>
                setItems(
                  items.map((x, j) =>
                    j === i ? { ...x, title: e.target.value } : x,
                  ),
                )
              }
            />
            <button
              aria-label="删除资料"
              onClick={() => setItems(items.filter((_, j) => j !== i))}
            >
              <Trash2 size={14} />
            </button>
          </div>
          <textarea
            aria-label={`资料内容${i + 1}`}
            rows={8}
            placeholder="粘贴资料内容"
            value={item.text}
            onChange={(e) =>
              setItems(
                items.map((x, j) =>
                  j === i ? { ...x, text: e.target.value } : x,
                ),
              )
            }
          />
        </div>
      ))}
    </div>
  );
}

export function ReviewPanel({
  p,
  run,
  onRun,
  chapter,
  setChapter,
  act,
}: ProjectProps & {
  run?: Run;
  onRun: (r: Run) => void;
  chapter: number;
  setChapter: (n: number) => void;
}) {
  const [instructions, setInstructions] = useState<Record<string, string>>({}),
    [manual, setManual] = useState("");
  const [readingView, setReadingView] = useState("candidate");
  const readingScroll = useRef<HTMLDivElement>(null);
  const liveReview = !!run && !["stopped", "completed"].includes(run.status);
  const allProposals = liveReview ? run?.data.proposals || [] : [];
  // 已提交的取舍留在执行记录中，待办区不重复陈列旧决定。
  const proposals = allProposals.filter((item) => item.decision === "pending");
  // 已处理但尚未执行的取舍可以撤回；已执行的不可更改，维持原有约束。
  const withdrawnable = allProposals.filter(
    (item) =>
      !item.applied &&
      (item.decision === "accepted" || item.decision === "rejected"),
  );
  const awaitingRevision = allProposals.filter(
    (item) => item.decision === "accepted" && !item.applied,
  ).length;
  const [kind, setReadingKind] = useState(
    run?.mode === "forward" ? "draft" : "outline",
  );
  // 前向写作也可能提出大纲修订；审核原文必须与提议的内容类型一致。
  useEffect(() => {
    const pending = liveReview
      ? run?.data.proposals?.find((item) => item.decision === "pending")
      : undefined;
    setReadingKind(
      pending?.kind || (run?.mode === "forward" ? "draft" : "outline"),
    );
  }, [run?.id, run?.status, run?.stage]);
  const v = current(p, chapter, kind);
  const change = run?.data.batch?.find(
    (b) => b.chapter === chapter && b.kind === kind,
  );
  const activeView =
    !change && ["candidate", "compare"].includes(readingView)
      ? "adopted"
      : readingView;
  useEffect(() => {
    readingScroll.current?.scrollTo(0, 0);
  }, [chapter, kind, activeView]);
  const canDecide =
    run &&
    ["F", "H"].includes(run.stage) &&
    ["paused", "waiting_input"].includes(run.status);
  const decide = (proposal: Proposal, decision: string) =>
    act(async () => {
      if (!run) return;
      onRun(
        await api<Run>(
          `/runs/${run.id}/decisions`,
          "POST",
          {
            decisions: [
              {
                id: proposal.id,
                decision,
                instruction: instructions[proposal.id] ?? proposal.instruction,
              },
            ],
          },
          run.revision,
        ),
      );
    });
  return (
    <div className="page review-page">
      <div className="review-grid">
        <div className="reading-column">
          <div className="reading-toolbar">
            <button
              aria-label="审核上一章"
              disabled={chapter === 1}
              onClick={() => setChapter(chapter - 1)}
            >
              <ArrowLeft size={14} />
            </button>
            <select
              aria-label="通读章节"
              value={chapter}
              onChange={(e) => setChapter(Number(e.target.value))}
            >
              {p.chapters.map((c) => (
                <option key={c.number} value={c.number}>
                  第 {c.number} 章
                </option>
              ))}
            </select>
            <button
              aria-label="审核下一章"
              disabled={chapter === p.chapters.length}
              onClick={() => setChapter(chapter + 1)}
            >
              <ArrowRight size={14} />
            </button>
            <select
              aria-label="审核阅读类型"
              value={kind}
              onChange={(e) => setReadingKind(e.target.value)}
            >
              <option value="outline">大纲</option>
              <option value="draft">正文</option>
            </select>
          </div>
          <nav className="review-reading-tabs" aria-label="审核阅读内容">
            {[
              ["candidate", "候选修改"],
              ["adopted", "当前采用"],
              ["compare", "上下对照"],
              ["context", "章节上下文"],
            ].map(([id, name]) => (
              <button
                key={id}
                aria-pressed={activeView === id}
                disabled={!change && ["candidate", "compare"].includes(id)}
                onClick={() => setReadingView(id)}
              >
                {name}
              </button>
            ))}
          </nav>
          {/* 每篇文本独占阅读宽度；对照上下排列，避免弹窗内再次分栏。 */}
          <div
            className="review-reading-scroll"
            ref={readingScroll}
            hidden={activeView === "context"}
          >
            {(activeView === "adopted" || activeView === "compare") && (
              <article className="review-document" aria-label="当前采用文本">
                <header>
                  <h3>
                    当前采用 · 第 {chapter} 章
                    {kind === "outline" ? "大纲" : "正文"}
                  </h3>
                  <small>
                    {v ? `版本 ${v.id.slice(0, 8)}` : "尚未采用内容"}
                  </small>
                </header>
                {kind === "outline" ? (
                  <OutlineText text={v?.text || "本章尚未生成。"} />
                ) : (
                  <div className="reading-text">
                    {v?.text || "本章尚未生成。"}
                  </div>
                )}
              </article>
            )}
            {change &&
              (activeView === "candidate" || activeView === "compare") && (
                <article
                  className="review-document candidate-document"
                  aria-label="候选修改文本"
                >
                  <header>
                    <h3>
                      候选修改 · 第 {chapter} 章
                      {kind === "outline" ? "大纲" : "正文"}
                    </h3>
                    <small>{change.reason || "本轮生成"} · 尚未采用</small>
                  </header>
                  {kind === "outline" ? (
                    <OutlineText text={change.text} />
                  ) : (
                    <div className="reading-text">{change.text}</div>
                  )}
                </article>
              )}
          </div>
          {/* 切换阅读方式不卸载上下文，保留翻阅位置和审核输入。 */}
          <div className="review-context" hidden={activeView !== "context"}>
            <ContextReader initialChapter={chapter} />
          </div>
        </div>
        <div className="decisions-column" role="region" aria-label="审核意见">
          <header className="review-decisions-heading">
            <h3>
              待处理意见 <span>{proposals.length}</span>
            </h3>
            <small>已处理的决定已收起</small>
          </header>
          {!proposals.length && (
            <div className="card">
              <CheckCircle2 size={24} />
              <p>
                {run?.status === "stopped"
                  ? "本次运行已终止，没有待处理审核项。"
                  : "当前没有待处理的问题。"}
              </p>
              <small className="muted">
                {!liveReview
                  ? "历史意见仍保留在执行记录中，可继续阅读已有内容。"
                  : awaitingRevision
                    ? `已接收 ${awaitingRevision} 条修改要求，继续后合并修订并核验；现在尚未改入候选。`
                    : run?.stage === "H"
                      ? "最终审核仍需要你通读并确认完成。"
                      : "继续后完成剩余核查与保存。"}
              </small>
            </div>
          )}
          {!!proposals.length && awaitingRevision > 0 && (
            <p className="muted">
              已接收 {awaitingRevision} 条修改要求，继续后执行并核验。
            </p>
          )}
          {proposals.map((prop) => (
            <div className={`proposal-card ${prop.decision}`} key={prop.id}>
              <div className="proposal-top">
                <button
                  className="link"
                  onClick={() => {
                    setChapter(prop.chapter);
                    setReadingKind(prop.kind);
                    setReadingView("candidate");
                  }}
                >
                  第 {prop.chapter} 章
                </button>
                <span className="tag">
                  {prop.critical ? "人工取舍" : "常规修订"}
                </span>
                <small>{labels[prop.decision]}</small>
              </div>
              <h4>{prop.problem}</h4>
              {prop.reason && <p className="muted small">{prop.reason}</p>}
              <label className="review-instruction">
                修订要求
                <textarea
                  aria-label="修订要求"
                  rows={5}
                  disabled={prop.applied}
                  value={instructions[prop.id] ?? prop.instruction}
                  onChange={(e) =>
                    setInstructions({
                      ...instructions,
                      [prop.id]: e.target.value,
                    })
                  }
                />
              </label>
              <details>
                <summary>原文证据（{prop.evidence?.length || 0}）</summary>
                <Citations items={prop.evidence || []} />
              </details>
              <div className="button-row">
                <button
                  className="primary"
                  disabled={!canDecide || prop.applied}
                  onClick={() => decide(prop, "accepted")}
                >
                  <Check size={13} />
                  采用
                </button>
                <button
                  disabled={!canDecide || prop.applied}
                  onClick={() => decide(prop, "rejected")}
                >
                  拒绝
                </button>
                {prop.applied && <small>已执行</small>}
              </div>
            </div>
          ))}
          {!!withdrawnable.length && (
            <section className="decided-group" aria-label="已处理的决定">
              <header className="review-decisions-heading">
                <h4>
                  已处理 <span>{withdrawnable.length}</span>
                </h4>
                <small>未执行的可以撤回待定</small>
              </header>
              {withdrawnable.map((prop) => (
                <div className="proposal-card decided" key={prop.id}>
                  <div className="proposal-top">
                    <button
                      className="link"
                      onClick={() => {
                        setChapter(prop.chapter);
                        setReadingKind(prop.kind);
                        setReadingView("candidate");
                      }}
                    >
                      第 {prop.chapter} 章
                    </button>
                    <span className="tag">
                      {prop.critical ? "人工取舍" : "常规修订"}
                    </span>
                    <small>{labels[prop.decision]}</small>
                  </div>
                  <h4>{prop.problem}</h4>
                  <div className="button-row">
                    <button
                      disabled={!canDecide}
                      onClick={() => decide(prop, "pending")}
                    >
                      撤回为待处理
                    </button>
                  </div>
                </div>
              ))}
            </section>
          )}
          {liveReview && (
            <div className="card">
              <h4>提出人工修改</h4>
              <p className="muted small">应用于当前阅读的第 {chapter} 章。</p>
              <textarea
                aria-label="人工修改意见"
                rows={3}
                value={manual}
                onChange={(e) => setManual(e.target.value)}
                placeholder="说明你希望修改的情节或表达"
              />
              <button
                disabled={!canDecide || !manual.trim()}
                onClick={() =>
                  act(async () => {
                    if (!run) return;
                    onRun(
                      await api<Run>(
                        `/runs/${run.id}/decisions`,
                        "POST",
                        { manual: [{ chapter, kind, instruction: manual }] },
                        run.revision,
                      ),
                    );
                    setManual("");
                  })
                }
              >
                加入本轮修改
              </button>
            </div>
          )}
          {liveReview && run?.stage === "H" && (
            <button
              className="primary wide"
              disabled={
                !canDecide || proposals.some((x) => x.decision === "pending")
              }
              onClick={() =>
                act(
                  async () =>
                    onRun(
                      await api<Run>(
                        `/runs/${run.id}/decisions`,
                        "POST",
                        { completeReview: true },
                        run.revision,
                      ),
                    ),
                  "审核决定已记录，点击继续执行",
                )
              }
            >
              <CheckCircle2 size={16} />
              {run.data.reviewApproved ? "本轮审核已确认" : "确认本轮审核完成"}
            </button>
          )}
          {liveReview && (
            <p className="muted small">处理完决定后，点击“处理完成，继续”。</p>
          )}
        </div>
      </div>
    </div>
  );
}

export function Models({
  p,
  save,
  act,
}: ProjectProps & {
  attempts: Attempt[];
  save: (body: unknown) => Promise<void>;
  onSaved: (p: Project) => void;
}) {
  const [settings, setSettings] = useState<Settings>(
      structuredClone(p.settings),
    ),
    [role, setRole] = useState("outline");
  const baseline = useRef(JSON.stringify(p.settings));
  useEffect(() => {
    const previous = baseline.current;
    baseline.current = JSON.stringify(p.settings);
    setSettings((old) =>
      JSON.stringify(old) === previous ? structuredClone(p.settings) : old,
    );
  }, [p.settings]);
  const model = settings.models[role];
  const setField = (key: string, value: unknown) =>
    setSettings({
      ...settings,
      models: { ...settings.models, [role]: { ...model, [key]: value } },
    });
  return (
    <div className="page">
      <PageTitle
        eyebrow="模型配置"
        title="为每种工作选择合适的模型"
        text="大纲、正文与统筹默认使用 Kimi K3；核查和摘要可独立配置。配置保存后在下一次请求生效。"
      />
      <div className="settings-grid">
        <div className="card">
          <div className="segmented">
            {[
              ["outline", "大纲"],
              ["draft", "正文"],
              ["orchestrator", "统筹"],
              ["check", "核查"],
              ["summary", "摘要"],
            ].map(([id, label]) => (
              <button
                key={id}
                className={role === id ? "active" : ""}
                onClick={() => setRole(id)}
              >
                {label}
              </button>
            ))}
          </div>
          <label>
            模型接口
            <input
              aria-label="模型接口"
              value={model.baseUrl}
              onChange={(e) => setField("baseUrl", e.target.value)}
            />
          </label>
          <label>
            模型名称
            <input
              value={model.model}
              onChange={(e) => setField("model", e.target.value)}
            />
          </label>
          <label>
            凭据来源
            <input
              value={model.credential}
              onChange={(e) => setField("credential", e.target.value)}
              placeholder="opencode-go / none / env:变量名"
            />
            <small className="muted">
              opencode-go 使用本机已有凭据；本地无认证服务填写 none。
            </small>
          </label>
          <div className="field-row">
            <label>
              输出上限
              <input
                type="number"
                min={64}
                value={model.maxTokens}
                onChange={(e) => setField("maxTokens", Number(e.target.value))}
              />
            </label>
            <label>
              创作温度
              <input
                type="number"
                min={0}
                max={2}
                step={0.1}
                value={model.temperature}
                onChange={(e) =>
                  setField("temperature", Number(e.target.value))
                }
              />
            </label>
          </div>
          <div className="field-row">
            <label>
              上下文字数上限
              <input
                type="number"
                value={model.contextChars}
                onChange={(e) =>
                  setField("contextChars", Number(e.target.value))
                }
              />
            </label>
            <label>
              请求超时（秒）
              <input
                type="number"
                value={model.timeoutSeconds}
                onChange={(e) =>
                  setField("timeoutSeconds", Number(e.target.value))
                }
              />
            </label>
          </div>
          <label className="check-label">
            <input
              type="checkbox"
              checked={model.thinking}
              disabled={!model.model.toLowerCase().includes("deepseek")}
              onChange={(e) => setField("thinking", e.target.checked)}
            />
            启用模型思考（DeepSeek）
          </label>
          {model.model.toLowerCase().includes("kimi-k3") && (
            <label>
              Kimi K3 思考强度
              <select
                value={model.reasoningEffort || "high"}
                onChange={(event) =>
                  setField("reasoningEffort", event.target.value)
                }
              >
                <option value="low">低 · 更快</option>
                <option value="high">高 · 默认</option>
                <option value="max">最高</option>
              </select>
              <small className="muted">
                K3 始终使用思考模式，输出上限包含思考内容。
              </small>
            </label>
          )}
          <button
            onClick={() =>
              setSettings({
                ...settings,
                models: Object.fromEntries(
                  Object.keys(settings.models).map((k) => [k, { ...model }]),
                ),
              })
            }
          >
            将当前配置用于全部角色
          </button>
          <button
            className="wide"
            onClick={() => {
              const models = { ...settings.models };
              for (const key of ["outline", "draft", "orchestrator"]) {
                models[key] = {
                  ...models[key],
                  model: "kimi-k3",
                  reasoningEffort: "high",
                  maxTokens: Math.max(16384, models[key].maxTokens),
                  baseUrl: "https://opencode.ai/zen/go/v1",
                  credential: "opencode-go",
                };
              }
              setSettings({ ...settings, models });
            }}
          >
            大纲、正文与统筹使用 Kimi K3
          </button>
        </div>
        <div>
          <div className="card orchestrator-settings">
            <h3>自动统筹</h3>
            <label className="check-label">
              <input
                type="checkbox"
                checked={!!settings.orchestratorEnabled}
                onChange={(event) =>
                  setSettings({
                    ...settings,
                    orchestratorEnabled: event.target.checked,
                  })
                }
              />
              让统筹 AI 接管创作弹窗
            </label>
            <p className="muted small">
              自动确认创意、回答补充问题、处理修订取舍并完成审核。保留用户要求，逐项记录判断理由；暂停和单步仍由你控制。达到重试上限会保留现场并显示原因。
            </p>
            <label>
              给统筹的长期要求
              <textarea
                rows={4}
                maxLength={2000}
                value={settings.orchestratorGuidance || ""}
                onChange={(event) =>
                  setSettings({
                    ...settings,
                    orchestratorGuidance: event.target.value,
                  })
                }
                placeholder="例如：从每章的叙事目的出发安排事件；不要重复相邻章节的情节；保留人物选择的代价。"
              />
            </label>
            <p className="muted small">
              记忆随作品保存，跨章节、切换运行方向和服务重启后继承；接近预算时自动压缩较早记录，保留最近决定。原始记录可在“记录”查看。
            </p>
            <details className="orchestrator-memory">
              <summary>
                统筹记忆 · {p.orchestratorMemory?.sequence || 0} 次决定 ·{" "}
                {p.orchestratorMemory?.compactions || 0} 次压缩
              </summary>
              <pre>
                {p.orchestratorMemory?.summary ||
                  "尚无压缩摘要，启动统筹后自动积累。"}
              </pre>
              {[...(p.orchestratorMemory?.recent || [])]
                .reverse()
                .map((entry) => {
                  let decision: {
                    reason?: string;
                    memory?: string;
                    creativeDirection?: string;
                    answer?: string;
                    decisions?: {
                      id: string;
                      decision: string;
                      reason: string;
                    }[];
                  } = {};
                  try {
                    decision = JSON.parse(entry.decision);
                  } catch {
                    decision.reason = entry.decision;
                  }
                  return (
                    <article key={entry.id} className="orchestrator-entry">
                      <strong>
                        第 {entry.chapter} 章 · {entry.gate}
                      </strong>
                      <small className="muted">
                        {" "}
                        · {formatTime(entry.created)}
                      </small>
                      <p>{decision.reason}</p>
                      <p>{decision.memory}</p>
                      {(decision.creativeDirection || decision.answer) && (
                        <pre>
                          {decision.creativeDirection || decision.answer}
                        </pre>
                      )}
                      {decision.decisions?.map((item) => (
                        <p key={item.id}>
                          <span className="tag">
                            {item.decision === "accepted" ? "采用" : "拒绝"}
                          </span>{" "}
                          {item.reason}
                        </p>
                      ))}
                    </article>
                  );
                })}
            </details>
          </div>
          <div className="card">
            <h3>推进与审核</h3>
            <label className="check-label">
              <input
                type="checkbox"
                checked={settings.arcReviews}
                onChange={(e) =>
                  setSettings({ ...settings, arcReviews: e.target.checked })
                }
              />
              剧情单元完成后安排审核
            </label>
            <p className="muted small">
              全书最终审核始终启用。两个方向都按整个单元完成触发。
            </p>
            <label>
              独立核查并发
              <input
                type="number"
                min={1}
                max={4}
                value={settings.checkConcurrency}
                onChange={(e) =>
                  setSettings({
                    ...settings,
                    checkConcurrency: Number(e.target.value),
                  })
                }
              />
            </label>
          </div>
          <div className="card">
            <h3>语义检索（可选）</h3>
            <p className="muted small">留空时使用关键词检索。</p>
            <label>
              向量接口
              <input
                placeholder="http://127.0.0.1:8000/v1"
                value={settings.embeddingUrl}
                onChange={(e) =>
                  setSettings({ ...settings, embeddingUrl: e.target.value })
                }
              />
            </label>
            <label>
              向量模型
              <input
                value={settings.embeddingModel}
                onChange={(e) =>
                  setSettings({ ...settings, embeddingModel: e.target.value })
                }
              />
            </label>
            <label>
              凭据引用
              <input
                placeholder="none"
                value={settings.embeddingCredential}
                onChange={(e) =>
                  setSettings({
                    ...settings,
                    embeddingCredential: e.target.value,
                  })
                }
              />
            </label>
          </div>
        </div>
      </div>
      <div className="sticky-save">
        <button
          className="primary"
          onClick={() =>
            act(
              () =>
                (async () => {
                  baseline.current = JSON.stringify(settings);
                  await save({ settings });
                })(),
              "模型与执行配置已保存",
            )
          }
        >
          <Save size={15} />
          保存全部配置
        </button>
      </div>
    </div>
  );
}

export function HistoryPanel({
  p,
  runs,
  attempts,
  onRun,
}: {
  p: Project;
  runs: Run[];
  attempts: Attempt[];
  onRun: (r: Run) => void;
}) {
  const [selected, setSelected] = useState<Attempt>();
  return (
    <div className="page">
      <PageTitle
        eyebrow="EXECUTION HISTORY"
        title="每一步，都有来处"
        text="查看运行记录、实际提示词、输入版本与模型输出。"
      />
      <div className="history-grid">
        <div>
          <h3>运行记录</h3>
          {runs.map((r) => (
            <button className="run-card" key={r.id} onClick={() => onRun(r)}>
              <strong>{labels[r.mode]}</strong>
              <span
                className={`tag ${r.status === "completed" ? "green" : ""}`}
              >
                {labels[r.status]}
              </span>
              <small>
                {formatTime(r.created)} · 第 {r.chapter} 章 · {r.node}
              </small>
            </button>
          ))}
          <div className="card">
            <h3>项目版本</h3>
            <p>
              {p.versions.length} 个内容版本 · {p.reviews.length} 次审核
            </p>
            <small className="muted">
              进入章节编辑器可载入历史版本并保存为新的当前版本。
            </small>
          </div>
        </div>
        <div>
          <h3>模型请求（{attempts.length}）</h3>
          <div className="attempt-list">
            {attempts
              .slice()
              .reverse()
              .map((a) => (
                <button
                  key={a.id}
                  className="attempt-row"
                  onClick={() => setSelected(a)}
                >
                  <span className="node-token small-token">{a.node}</span>
                  <span>
                    第 {a.chapter} 章
                    <small>
                      {a.modelConfig.model} · {formatTime(a.created)}
                    </small>
                  </span>
                  <span>{labels[a.status]}</span>
                  <span>{a.usage?.total || 0} tokens</span>
                </button>
              ))}
          </div>
        </div>
      </div>
      {selected && (
        <div className="modal-backdrop">
          <div className="record-modal">
            <div className="modal-header">
              <h2>
                {selected.node} · 第 {selected.chapter} 章
              </h2>
              <button
                aria-label="关闭执行记录"
                onClick={() => setSelected(undefined)}
              >
                <X size={17} />
              </button>
            </div>
            <ContextReader initialChapter={selected.chapter} compact />
            <h3>完整请求</h3>
            <pre className="output-preview">{selected.prompt}</pre>
            <h3>输出</h3>
            <pre className="prose-output">{selected.output}</pre>
            {selected.error && <p className="inline-error">{selected.error}</p>}
          </div>
        </div>
      )}
    </div>
  );
}
function PageTitle({
  eyebrow,
  title,
  text,
}: {
  eyebrow: string;
  title: string;
  text: string;
}) {
  return (
    <div className="page-heading">
      <div>
        <span className="eyebrow">{eyebrow}</span>
        <h2>{title}</h2>
        <p className="muted">{text}</p>
      </div>
    </div>
  );
}
