import {
  createContext,
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import {
  ArrowRight,
  BookOpen,
  Copy,
  Flag,
  Plus,
  Search,
  X,
} from "lucide-react";
import { ContextReader } from "./ContextReader";
import { api } from "./api";
import {
  labels,
  type Project,
  type Run,
  type Checkpoint,
  type Attempt,
  type Idea,
} from "./types";
import { ReviewPanel, Models, type Act } from "./Panels";

export const OperationContext = createContext({ error: "", busy: false });

// 使用原生模态层承接键盘焦点；关闭只收起窗口，已填写的草稿继续留在组件中。
export function Modal({
  open,
  title,
  children,
  onClose,
  footer,
  wide = false,
  embeddedContext = false,
}: {
  open: boolean;
  title: string;
  children: ReactNode;
  onClose: () => void;
  footer?: ReactNode;
  wide?: boolean;
  embeddedContext?: boolean;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  const operation = useContext(OperationContext);
  useEffect(() => {
    const d = ref.current;
    if (!d) return;
    if (open && !d.open) {
      d.showModal();
      d.querySelector(".work-dialog-body")?.scrollTo(0, 0);
    }
    if (!open && d.open) d.close();
  }, [open]);
  return (
    <dialog
      ref={ref}
      className={`work-dialog context-dialog ${wide ? "wide-dialog" : ""} ${embeddedContext ? "review-dialog" : ""}`}
      onCancel={(e) => {
        e.preventDefault();
        onClose();
      }}
      aria-label={title}
    >
      <header className="work-dialog-head">
        <div>
          <span className="eyebrow">WeaveGrid · 工作台</span>
          <h2>{title}</h2>
        </div>
        <button aria-label={`关闭${title}`} onClick={onClose}>
          <X size={18} />
        </button>
      </header>
      <div
        className={
          embeddedContext ? "dialog-embedded-context" : "dialog-with-context"
        }
      >
        <div className="work-dialog-body">
          {operation.error && (
            <div role="alert" className="banner error">
              {operation.error}
            </div>
          )}
          {operation.busy && <p className="muted">处理中…</p>}
          {children}
        </div>
        {open && !embeddedContext && <ContextReader />}
      </div>
      {footer && <footer className="work-dialog-foot">{footer}</footer>}
    </dialog>
  );
}

export function LibraryDialog({
  open,
  projects,
  currentId,
  onClose,
  onSelect,
  onCreate,
  onChanged,
  act,
}: {
  open: boolean;
  projects: Project[];
  currentId?: string;
  onClose: () => void;
  onSelect: (id: string) => void;
  onCreate: () => void;
  onChanged: (p: Project) => void;
  act: Act;
}) {
  const [search, setSearch] = useState(""),
    [archived, setArchived] = useState(false),
    [names, setNames] = useState<Record<string, string>>({});
  const filtered = projects.filter(
    (p) => !!p.archived === archived && p.name.includes(search),
  );
  return (
    <Modal
      open={open}
      title="作品管理"
      onClose={onClose}
      wide
      footer={
        <>
          <span>作品的章节、笔记、运行与检查点各自独立。</span>
          <button className="primary" onClick={onCreate}>
            <Plus size={15} />
            新建作品
          </button>
        </>
      }
    >
      <div className="library-search">
        <Search size={17} />
        <input
          aria-label="搜索作品"
          placeholder="搜索作品名称"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <label className="check-label">
          <input
            type="checkbox"
            checked={archived}
            onChange={(e) => setArchived(e.target.checked)}
          />
          已归档
        </label>
      </div>
      <div className="library-grid">
        {filtered.map((p) => (
          <article
            className={`book-card ${p.id === currentId ? "current" : ""}`}
            key={p.id}
          >
            <div className="book-symbol">
              <BookOpen size={25} />
              <span>{p.id === currentId ? "当前作品" : "小说作品"}</span>
            </div>
            <input
              aria-label={`作品名称 ${p.name}`}
              value={names[p.id] ?? p.name}
              onChange={(e) => setNames({ ...names, [p.id]: e.target.value })}
            />
            <p>
              {p.chapters.length} 章 · 大纲{" "}
              {p.chapters.filter((c) => c.outline).length} · 正文{" "}
              {p.chapters.filter((c) => c.draft).length}
            </p>
            <small className="muted">
              创意 {p.ideas.length} · 参考资料 {p.references.length}
            </small>
            <div className="button-row">
              <button
                className="primary"
                onClick={() => {
                  onSelect(p.id);
                  onClose();
                }}
              >
                打开作品
                <ArrowRight size={14} />
              </button>
              <button
                disabled={!names[p.id]?.trim() || names[p.id] === p.name}
                onClick={() =>
                  act(
                    async () =>
                      onChanged(
                        await api<Project>(
                          `/projects/${p.id}`,
                          "PUT",
                          { name: names[p.id] },
                          p.revision,
                        ),
                      ),
                    "名称已更新",
                  )
                }
              >
                改名
              </button>
              <button
                onClick={() =>
                  act(
                    async () =>
                      onChanged(
                        await api<Project>(
                          `/projects/${p.id}/copy`,
                          "POST",
                          {},
                          p.revision,
                        ),
                      ),
                    "已创建独立副本",
                  )
                }
              >
                <Copy size={13} />
                复制
              </button>
              <button
                onClick={() =>
                  act(
                    async () =>
                      onChanged(
                        await api<Project>(
                          `/projects/${p.id}`,
                          "PUT",
                          { archived: !p.archived },
                          p.revision,
                        ),
                      ),
                    p.archived ? "已取消归档" : "作品已归档",
                  )
                }
              >
                {p.archived ? "取消归档" : "归档"}
              </button>
            </div>
          </article>
        ))}
      </div>
      {!filtered.length && <p className="muted">没有符合条件的作品。</p>}
    </Modal>
  );
}

export function CheckpointsDialog({
  open,
  p,
  run,
  onClose,
  onChanged,
  act,
}: {
  open: boolean;
  p: Project;
  run?: Run;
  onClose: () => void;
  onChanged: () => Promise<unknown>;
  act: Act;
}) {
  const [items, setItems] = useState<Checkpoint[]>([]),
    [name, setName] = useState(""),
    [note, setNote] = useState(""),
    [target, setTarget] = useState<Checkpoint>(),
    [error, setError] = useState("");
  const reload = async () =>
    setItems(await api<Checkpoint[]>(`/projects/${p.id}/checkpoints`));
  useEffect(() => {
    if (open) {
      setTarget(undefined);
    }
  }, [open, p.id]);
  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    api<Checkpoint[]>(`/projects/${p.id}/checkpoints`)
      .then((items) => {
        if (!cancelled) setItems(items);
      })
      .catch((e) => {
        if (!cancelled) setError(e.message);
      });
    return () => {
      cancelled = true;
    };
  }, [open, p.id, p.revision]);
  return (
    <Modal open={open} title="检查点与回退" onClose={onClose} wide>
      <p className="muted">
        每章大纲、正文首次保存成功时自动打点，不暂停写作。检查点保留整部作品的内容、创意、配置、统筹记忆与执行位置；回退后可单步或继续。也可以随时手动保存。
      </p>
      {error && <p className="banner error">{error}</p>}
      <div className="checkpoint-create">
        <label>
          检查点名称
          <input
            aria-label="检查点名称"
            placeholder="例如：第一轮大纲通过"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </label>
        <label>
          备注
          <textarea
            aria-label="检查点备注"
            rows={2}
            placeholder="记下这次想保留的状态"
            value={note}
            onChange={(e) => setNote(e.target.value)}
          />
        </label>
        <button
          className="primary"
          onClick={() =>
            act(async () => {
              await api(`/projects/${p.id}/checkpoints`, "POST", {
                name,
                note,
              });
              setName("");
              setNote("");
              await reload();
              await onChanged();
            }, "检查点已保存")
          }
        >
          <Flag size={15} />
          {run?.status === "running" ? "暂停并保存检查点" : "保存检查点"}
        </button>
      </div>
      {target && (
        <div className="restore-confirm">
          <b>回退到「{target.name}」</b>
          <p>
            运行中会先暂停。当前状态会先保存为“恢复前自动备份”。回退后从检查点继续，旧运行保留在记录中。
          </p>
          <div className="button-row">
            <button
              className="primary"
              onClick={() =>
                act(async () => {
                  if (run?.status === "running")
                    await api(`/runs/${run.id}/control`, "POST", {
                      action: "pause",
                    });
                  const latest = await api<Project>(`/projects/${p.id}`);
                  if (latest.revision !== p.revision) {
                    await onChanged();
                    throw new Error(
                      "暂停时内容已更新，请核对当前内容后再次确认回退",
                    );
                  }
                  await api(
                    `/projects/${p.id}/checkpoints/${target.id}/restore`,
                    "POST",
                    {},
                    p.revision,
                  );
                  setTarget(undefined);
                  await onChanged();
                  await reload();
                }, "已回退到检查点")
              }
            >
              确认回退
            </button>
            <button onClick={() => setTarget(undefined)}>取消</button>
          </div>
        </div>
      )}
      <div className="checkpoint-list">
        {items.map((c) => (
          <article key={c.id}>
            <Flag size={18} />
            <div>
              <h3>{c.name}</h3>
              <small>
                {c.automatic ? "自动检查点 · " : ""}
                {new Date(c.created).toLocaleString()} ·{" "}
                {c.mode
                  ? `${labels[c.mode]} · 第 ${c.chapter} 章 · ${c.node}`
                  : "尚未运行"}
              </small>
              {c.note && <p>{c.note}</p>}
            </div>
            <button onClick={() => setTarget(c)}>回退到此检查点</button>
          </article>
        ))}
      </div>
      {!items.length && (
        <p className="muted">
          还没有检查点。写完一章大纲或正文后会自动创建，也可以立即手动保存。
        </p>
      )}
      {run?.status === "running" && (
        <p className="muted">确认回退时会先暂停当前运行。</p>
      )}
    </Modal>
  );
}

export function ReviewDialog({
  open,
  p,
  run,
  onRun,
  chapter,
  setChapter,
  act,
  onClose,
  onContinue,
}: {
  open: boolean;
  p: Project;
  run?: Run;
  onRun: (r: Run) => void;
  chapter: number;
  setChapter: (c: number) => void;
  act: Act;
  onClose: () => void;
  onContinue: () => void;
}) {
  const canContinue =
    run &&
    ["paused", "waiting_input"].includes(run.status) &&
    !run.data.proposals?.some((q) => q.decision === "pending") &&
    (run.stage !== "H" || run.data.reviewApproved);
  return (
    <Modal
      open={open}
      title="审核与取舍"
      onClose={onClose}
      wide
      embeddedContext
      footer={
        <>
          <span>
            {run?.status === "waiting_input"
              ? "流程正在等待你处理；稍后处理只收起窗口。"
              : run?.message || "对照原文直接处理审核意见。"}
          </span>
          <button onClick={onClose}>
            {run && ["stopped", "completed"].includes(run.status)
              ? "关闭"
              : "稍后处理"}
          </button>
          {run?.status === "waiting_input" && (
            <button
              className="primary"
              disabled={!canContinue}
              onClick={onContinue}
            >
              处理完成，继续
            </button>
          )}
        </>
      }
    >
      <ReviewPanel
        p={p}
        run={run}
        onRun={onRun}
        chapter={chapter}
        setChapter={setChapter}
        act={act}
      />
    </Modal>
  );
}

// 表单只接收人的输入，关闭后保留草稿；提交同时保存创意内容和本次不可变交付。
export function CreativeInputDialog({
  open,
  p,
  run,
  onRun,
  onSaved,
  onClose,
  act,
  onContinue,
}: {
  open: boolean;
  p: Project;
  run?: Run;
  onRun: (r: Run) => void;
  onSaved: (p: Project) => void;
  onClose: () => void;
  act: Act;
  onContinue: (r: Run) => Promise<void>;
}) {
  const input = run?.data.creativeInput;
  const [ideas, setIdeas] = useState<Idea[]>([]);
  const [revision, setRevision] = useState(p.revision);
  const [empty, setEmpty] = useState(false);
  const initialize = () => {
    setIdeas(
      p.ideas
        .filter((i) => i.chapter === 0 || i.chapter === run?.chapter)
        .map((i) => ({ ...i })),
    );
    setRevision(p.revision);
    setEmpty(false);
  };
  useEffect(initialize, [input?.id]);
  const update = (index: number, field: Partial<Idea>) =>
    setIdeas((old) =>
      old.map((i, n) => (n === index ? { ...i, ...field } : i)),
    );
  const ready =
    !!input &&
    !input.submittedAt &&
    run?.stage === "C" &&
    ["waiting_input", "paused"].includes(run.status);
  const valid =
    ideas.every((i) => i.text.trim()) && (ideas.some((i) => i.active) || empty);
  const submit = (resume: boolean) =>
    act(async () => {
      if (!input || !run) return;
      const result = await api<{ project: Project; run: Run }>(
        `/runs/${run.id}/creative-input`,
        "POST",
        {
          id: input.id,
          ideas,
          emptyConfirmed: empty,
          projectRevision: revision,
        },
        run.revision,
      );
      onSaved(result.project);
      onRun(result.run);
      if (resume) {
        onClose();
        await onContinue(result.run);
      }
    }, "人工创意已原样保存");
  return (
    <Modal
      open={open}
      title={`C · 第 ${run?.chapter || 1} 章人工创意`}
      onClose={onClose}
      wide
      footer={
        <>
          <span>本章每次创作都等待你确认；提交内容原样交给后续创作。</span>
          <button onClick={onClose}>稍后填写</button>
          {ready ? (
            <>
              <button disabled={!valid} onClick={() => submit(false)}>
                提交并停留
              </button>
              <button
                className="primary"
                disabled={!valid}
                onClick={() => submit(true)}
              >
                提交创意并继续
              </button>
            </>
          ) : (
            input?.submittedAt && (
              <button
                className="primary"
                onClick={() =>
                  act(async () => {
                    onClose();
                    if (run) await onContinue(run);
                  })
                }
              >
                继续执行
              </button>
            )
          )}
        </>
      }
    >
      <p>
        {p.name} · {labels[run?.mode || "backward"]}
      </p>
      <p className="muted">
        请填写本章的情节、人物行动、必留要求或可选想法。已有创意也需要你确认；这里不会由模型补写。
      </p>
      {input?.submittedAt ? (
        <>
          <h3>
            {input.source === "orchestrator"
              ? "已提交的统筹创意"
              : "已提交的人工输出"}
          </h3>
          {input.ideas.map((i) => (
            <section key={i.id}>
              <small>
                {i.required ? "必留要求" : "可选想法"} ·{" "}
                {i.chapter === 0 ? "全书" : "本章"}
              </small>
              <pre className="prose-output">{i.text}</pre>
            </section>
          ))}
          {input.emptyConfirmed && (
            <p>
              {input.source === "orchestrator"
                ? "统筹确认本章交由写作模型自由构思。"
                : "你已明确确认本章不提供创意。"}
            </p>
          )}
        </>
      ) : (
        <>
          {ideas.map((idea, index) => (
            <section className="creative-input-card" key={index}>
              <div className="button-row wrap">
                <select
                  aria-label={`创意 ${index + 1} 范围`}
                  value={idea.chapter}
                  onChange={(e) =>
                    update(index, { chapter: Number(e.target.value) })
                  }
                >
                  <option value={run?.chapter}>本章</option>
                  <option value={0}>全书通用</option>
                </select>
                <label>
                  <input
                    type="checkbox"
                    checked={idea.required}
                    onChange={(e) =>
                      update(index, { required: e.target.checked })
                    }
                  />
                  必留要求
                </label>
                <label>
                  <input
                    type="checkbox"
                    checked={idea.active}
                    onChange={(e) =>
                      update(index, { active: e.target.checked })
                    }
                  />
                  本次采用
                </label>
                <button
                  onClick={() =>
                    setIdeas((old) => old.filter((_, n) => n !== index))
                  }
                >
                  删除这条
                </button>
              </div>
              <textarea
                aria-label={`人工创意 ${index + 1}`}
                rows={5}
                value={idea.text}
                onChange={(e) => update(index, { text: e.target.value })}
                placeholder="写下你的创意；内容会原样提交。"
              />
            </section>
          ))}
          {!ideas.length && (
            <p className="empty-inline">还没有创意，请添加并填写。</p>
          )}
          <div className="button-row wrap">
            <button
              className="primary"
              onClick={() =>
                setIdeas((old) => [
                  ...old,
                  {
                    id: "",
                    chapter: run?.chapter || 1,
                    text: "",
                    required: true,
                    active: true,
                  },
                ])
              }
            >
              <Plus size={15} />
              添加一条创意
            </button>
            <button onClick={initialize}>重新载入已保存创意</button>
          </div>
          {!ideas.some((i) => i.active) && (
            <label className="empty-choice">
              <input
                type="checkbox"
                checked={empty}
                onChange={(e) => setEmpty(e.target.checked)}
              />
              我明确确认：本章不提供人工创意
            </label>
          )}
        </>
      )}
    </Modal>
  );
}

export function InputDialog({
  open,
  p,
  run,
  onRun,
  onSaved,
  onClose,
  act,
  onContinue,
}: {
  open: boolean;
  p: Project;
  run?: Run;
  onRun: (r: Run) => void;
  onSaved: (p: Project) => void;
  onClose: () => void;
  act: Act;
  onContinue: (r: Run) => Promise<void>;
}) {
  const [answer, setAnswer] = useState("");
  useEffect(
    () => setAnswer(run?.data.input?.answer || ""),
    [run?.data.input?.id],
  );
  return (
    <Modal
      open={open}
      title="需要你补充"
      onClose={onClose}
      footer={
        <>
          <span>回答会保存为这部作品的参考资料。</span>
          <button onClick={onClose}>稍后回答</button>
          <button
            className="primary"
            disabled={!answer.trim()}
            onClick={() =>
              act(async () => {
                if (!run?.data.input) return;
                const result = await api<{ project: Project; run: Run }>(
                  `/runs/${run.id}/input`,
                  "POST",
                  { id: run.data.input.id, answer },
                  run.revision,
                );
                onSaved(result.project);
                onRun(result.run);
                onClose();
                await onContinue(result.run);
              }, "回答已保存")
            }
          >
            保存回答并继续
          </button>
        </>
      }
    >
      <p>
        {p.name} · 第 {run?.chapter} 章
      </p>
      <p className="muted">{run?.message}</p>
      <ol className="input-questions">
        {run?.data.input?.questions.map((q, i) => (
          <li key={i}>{q}</li>
        ))}
      </ol>
      <textarea
        autoFocus
        aria-label="补充回答"
        rows={8}
        value={answer}
        onChange={(e) => setAnswer(e.target.value)}
        placeholder="按问题补充信息；没有特定要求时，也可以明确写“交由作者自由决定”。"
      />
    </Modal>
  );
}

export function ModelDialog({
  open,
  p,
  run,
  attempts,
  save,
  onSaved,
  act,
  onClose,
  onContinue,
}: {
  open: boolean;
  p: Project;
  run?: Run;
  attempts: Attempt[];
  save: (b: unknown) => Promise<void>;
  onSaved: (p: Project) => void;
  act: Act;
  onClose: () => void;
  onContinue: () => void;
}) {
  return (
    <Modal
      open={open}
      title="模型配置"
      onClose={onClose}
      wide
      footer={
        <>
          <span>{run?.message || "保存配置后，从当前步骤继续。"}</span>
          <button onClick={onClose}>稍后处理</button>
          <button className="primary" onClick={onContinue}>
            已处理，继续
          </button>
        </>
      }
    >
      <Models
        p={p}
        attempts={attempts}
        save={save}
        onSaved={onSaved}
        act={act}
      />
    </Modal>
  );
}
