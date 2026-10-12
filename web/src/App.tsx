import { useCallback, useEffect, useRef, useState } from "react";
import {
  Activity,
  StepForward,
  Flag,
  Library,
  ArrowDownUp,
  BookOpen,
  ChevronDown,
  ChevronRight,
  FileText,
  GitBranch,
  History,
  LoaderCircle,
  Maximize2,
  Pause,
  Play,
  Plus,
  Settings2,
  Square,
  Workflow as WorkflowIcon,
  X,
  CheckCircle2,
  AlertCircle,
  PanelRightClose,
  Download,
  Search,
  Moon,
  Sun,
  Bot,
} from "lucide-react";
import Graph from "./Graph";
import { CreativeContext } from "./ContextReader";
import { api } from "./api";
import { useTheme } from "./theme";
import {
  labels,
  current,
  type Project,
  type Run,
  type Attempt,
  type RunEvent,
  type Workflow,
} from "./types";
import {
  NewProjectDialog,
  Inspector,
  Editor,
  ReviewPanel,
  Models,
  HistoryPanel,
} from "./Panels";

import {
  OperationContext,
  Modal,
  LibraryDialog,
  CheckpointsDialog,
  ReviewDialog,
  InputDialog,
  CreativeInputDialog,
  ModelDialog,
} from "./Dialogs";

const nav = [
  ["flow", "流程", WorkflowIcon],
  ["relations", "经纬", GitBranch],
  ["editor", "写作", BookOpen],
  ["review", "审核", CheckCircle2],
  ["models", "模型", Settings2],
  ["history", "记录", History],
] as const;
export default function App() {
  const [theme, setTheme] = useTheme();
  const [projects, setProjects] = useState<Project[]>([]),
    [p, setP] = useState<Project>(),
    [workflow, setWorkflow] = useState<Workflow>(),
    [runs, setRuns] = useState<Run[]>([]),
    [run, setRun] = useState<Run>(),
    [attempts, setAttempts] = useState<Attempt[]>([]);
  const [view, setView] = useState("flow"),
    [mode, setMode] = useState("backward"),
    [selected, setSelected] = useState("A"),
    [expanded, setExpanded] = useState(""),
    [chapter, setChapter] = useState(1),
    [kind, setKind] = useState("outline");
  const [dialog, setDialog] = useState("");
  const [retryMessage, setRetryMessage] = useState("");
  const waitingShown = useRef("");
  const [connected, setConnected] = useState(false);
  const [create, setCreate] = useState(false),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [notice, setNotice] = useState(""),
    [events, setEvents] = useState<RunEvent[]>([]),
    [statuses, setStatuses] = useState<Record<string, string>>({}),
    [logOpen, setLogOpen] = useState(false),
    [inspectorOpen, setInspectorOpen] = useState(true);
  const activeProject = useRef("");
  const operationPending = useRef(false);
  const refreshTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const refresh = useCallback(async (id: string, resetMode = false) => {
    const [project, rs] = await Promise.all([
      api<Project>(`/projects/${id}`),
      api<Run[]>(`/runs?project=${id}`),
    ]);
    if (activeProject.current !== id) return;
    setP(project);
    setProjects((ps) => ps.map((x) => (x.id === id ? project : x)));
    setRuns(rs);
    if (rs[0]) {
      setRun((old) =>
        old?.id === rs[0].id && old.revision > rs[0].revision ? old : rs[0],
      );
      if (resetMode) setMode(rs[0].mode);
    } else {
      setRun(undefined);
      setAttempts([]);
    }
  }, []);
  useEffect(() => {
    Promise.all([api<Project[]>("/projects"), api<Workflow>("/workflow")])
      .then(([ps, w]) => {
        setConnected(true);
        setProjects(ps);
        setWorkflow(w);
        const first = ps.find((p) => !p.archived) || ps[0];
        if (first) {
          activeProject.current = first.id;
          refresh(first.id, true).catch((e) => setError(e.message));
        } else setCreate(true);
      })
      .catch((e) => setError(e.message));
  }, [refresh]);
  const selectProject = (id: string) => {
    activeProject.current = id;
    setRun(undefined);
    setDialog("");
    setRetryMessage("");
    waitingShown.current = "";
    setEvents([]);
    setStatuses({});
    setAttempts([]);
    setChapter(1);
    setExpanded("");
    refresh(id, true).catch((e) => setError(e.message));
  };
  useEffect(() => {
    if (!run?.id) return;
    const id = run.id;
    setEvents([]);
    setStatuses({});
    api<Attempt[]>(`/runs/${id}/attempts`)
      .then(setAttempts)
      .catch((e) => setError(e.message));
    // 首次连接读取当前快照；浏览器重连会携带事件序号，从中断处继续。
    const es = new EventSource(`/api/runs/${id}/events?tail=1`);
    es.onopen = () => setConnected(true);
    es.onerror = () => setConnected(false);
    es.onmessage = (msg) => {
      const event = JSON.parse(msg.data) as RunEvent;
      setEvents((old) => [...old, event].slice(-200));
      if (event.type === "run") {
        const r = event.data as Run;
        setRun((old) =>
          old?.id === r.id && old.revision > r.revision ? old : r,
        );
        setRuns((old) =>
          old.map((x) => (x.id === r.id && x.revision <= r.revision ? r : x)),
        );
      } else if (event.type === "retry") {
        setRetryMessage((event.data as { message: string }).message);
      } else if (event.type === "node") {
        setRetryMessage("");
        const d = event.data as {
          node: string;
          status: string;
          chapter: number;
          cycle: number;
        };
        setRun((old) =>
          old?.status === "running" && d.cycle >= old.cycle
            ? { ...old, node: d.node, chapter: d.chapter }
            : old,
        );
        setStatuses((old) => ({
          ...old,
          [d.node]: d.status,
          [d.node[0]]: d.status,
        }));
      } else if (event.type === "attempt") {
        setRetryMessage("");
        const a = event.data as Attempt;
        setAttempts((old) =>
          old.some((x) => x.id === a.id)
            ? old.map((x) => (x.id === a.id ? a : x))
            : [...old, a],
        );
      } else if (event.type === "stream") {
        const d = event.data as {
          attemptId: string;
          node: string;
          text: string;
        };
        setAttempts((old) => {
          const a = old.find((x) => x.id === d.attemptId);
          if (a)
            return old.map((x) =>
              x.id === d.attemptId
                ? { ...x, output: d.text, status: "running" }
                : x,
            );
          return old;
        });
        if (!refreshTimer.current)
          refreshTimer.current = setTimeout(() => {
            api<Attempt[]>(`/runs/${id}/attempts`)
              .then(setAttempts)
              .catch(() => {});
            refreshTimer.current = null;
          }, 300);
      } else if (event.type === "project") {
        const pid = (event.data as { projectId: string }).projectId;
        refresh(pid).catch((e) => setError(e.message));
      }
    };
    return () => {
      es.close();
      if (refreshTimer.current) {
        clearTimeout(refreshTimer.current);
        refreshTimer.current = null;
      }
    };
  }, [run?.id, refresh]);
  useEffect(() => {
    if (!notice) return;
    const t = setTimeout(() => setNotice(""), 4000);
    return () => clearTimeout(t);
  }, [notice]);
  useEffect(() => {
    if (!run || !["waiting_input", "failed"].includes(run.status)) {
      waitingShown.current = "";
      if (run?.status !== "running") setRetryMessage("");
      if (run && ["stopped", "completed"].includes(run.status)) {
        setDialog((current) =>
          ["review", "input", "creative-input", "failure"].includes(current)
            ? ""
            : current,
        );
      }
      return;
    }
    const key = `${run.id}:${run.status}:${run.epoch}:${run.nextNode}:${run.message}`;
    if (waitingShown.current === key) return;
    waitingShown.current = key;
    if (run.status === "failed") setDialog("failure");
    else if (run.stage === "C") setDialog("creative-input");
    else if (run.data.input && !run.data.input.answer) setDialog("input");
    else {
      setChapter(
        run.stage === "H"
          ? run.data.reviewChapters?.[0] || 1
          : run.data.proposals?.find((p) => p.decision === "pending")
              ?.chapter || run.chapter,
      );
      setDialog("review");
    }
  }, [run?.id, run?.status, run?.epoch, run?.nextNode, run?.message]);
  const act = async (fn: () => Promise<unknown>, message?: string) => {
    if (operationPending.current) return;
    operationPending.current = true;
    setBusy(true);
    setError("");
    try {
      await fn();
      if (message) setNotice(message);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      operationPending.current = false;
      setBusy(false);
    }
  };
  const saveProject = async (body: unknown) => {
    if (!p) return;
    const next = await api<Project>(
      `/projects/${p.id}`,
      "PUT",
      body,
      p.revision,
    );
    setP(next);
    setProjects((ps) => ps.map((x) => (x.id === next.id ? next : x)));
    setNotice("已保存；运行中的新输入将在安全点采用");
  };
  const afterProject = (next: Project) => {
    setP(next);
    setProjects((ps) =>
      ps.some((x) => x.id === next.id)
        ? ps.map((x) => (x.id === next.id ? next : x))
        : [next, ...ps],
    );
    activeProject.current = next.id;
  };
  const start = (singleStep = false) =>
    act(async () => {
      if (!p) return;
      if (mode === "forward" && p.chapters.some((c) => !c.outline)) {
        setDialog("prerequisites");
        return;
      }
      const r = await api<Run>("/runs", "POST", {
        projectId: p.id,
        mode,
        singleStep,
      });
      setRun(r);
      setRuns((rs) => [r, ...rs]);
      setSelected("A");
      setView("flow");
    });
  const sendControl = async (action: string, target = run) => {
    if (!target) return;
    const r = await api<Run>(`/runs/${target.id}/control`, "POST", { action });
    setRun((old) => (old?.id === r.id && old.revision > r.revision ? old : r));
    setAttempts(await api<Attempt[]>(`/runs/${r.id}/attempts`));
  };
  const control = (action: string) => {
    if (
      run?.status === "waiting_input" &&
      action !== "stop" &&
      !p?.settings.orchestratorEnabled
    ) {
      openWaiting();
      return;
    }
    return act(() => sendControl(action));
  };
  const continueDialog = () =>
    act(async () => {
      setDialog("");
      await sendControl(run?.singleStep ? "step" : "resume");
    });
  const libraryChanged = (next: Project) => {
    setProjects((ps) =>
      ps.some((p) => p.id === next.id)
        ? ps.map((p) => (p.id === next.id ? next : p))
        : [next, ...ps],
    );
    if (p?.id === next.id) setP(next);
  };
  const openWaiting = () =>
    setDialog(
      run?.stage === "C"
        ? "creative-input"
        : run?.data.input && !run.data.input.answer
          ? "input"
          : "review",
    );
  const actions = workflow?.actions[run?.status || "idle"] || [];
  const live = run && !["completed", "stopped"].includes(run.status);
  const done =
    p?.chapters.filter((c) => (mode === "forward" ? c.draft : c.outline))
      .length || 0;
  const waiting = live
    ? run.data.proposals?.filter((x) => x.decision === "pending").length || 0
    : 0;
  return (
    <CreativeContext.Provider
      value={{
        p,
        run,
        chapter: dialog === "review" ? chapter : run?.chapter || chapter,
        attempts,
      }}
    >
      <OperationContext.Provider value={{ error, busy }}>
        <div className="app">
          <aside className="rail">
            <div
              className="brand-mark"
              title="WeaveGrid · 经纬"
              aria-label="WeaveGrid · 经纬"
            >
              经<span>纬</span>
            </div>
            <nav>
              {nav.map(([id, label, Icon]) => (
                <button
                  key={id}
                  className={dialog === id || view === id ? "active" : ""}
                  onClick={() =>
                    id === "review" ? setDialog(id) : setView(id)
                  }
                  title={label}
                >
                  <Icon size={20} />
                  <span>{label}</span>
                  {id === "review" && waiting > 0 && <i />}
                </button>
              ))}
            </nav>
            <div className="rail-foot">
              本地
              <br />
              <span className="online-dot" />
              {connected ? "已连接" : "连接中"}
            </div>
          </aside>
          <aside className="project-sidebar">
            <button
              className="library-entry"
              onClick={() => setDialog("library")}
            >
              <Library size={17} />
              作品管理
            </button>
            <div className="project-heading">
              <span className="eyebrow">小说项目</span>
              <button
                className="icon-button"
                aria-label="新建项目"
                onClick={() => setCreate(true)}
              >
                <Plus size={17} />
              </button>
            </div>
            <select
              className="project-select"
              aria-label="选择项目"
              value={p?.id || ""}
              onChange={(e) => selectProject(e.target.value)}
            >
              {!p && <option>创建你的第一部小说</option>}
              {projects
                .filter((x) => !x.archived || x.id === p?.id)
                .map((x) => (
                  <option key={x.id} value={x.id}>
                    {x.name}
                  </option>
                ))}
            </select>
            <div className="project-stats">
              <span>{p?.structure.chapters || 0} 章</span>
              <span>{p?.structure.lines.length || 0} 组经纬</span>
            </div>
            <div className="section-rule" />
            <div className="sidebar-label">
              <span>章节进度</span>
              <span>
                {done} / {p?.chapters.length || 0}
              </span>
            </div>
            <div className="progress-track">
              <i
                style={{
                  width: `${(done / (p?.chapters.length || 1)) * 100}%`,
                }}
              />
            </div>
            <div className="chapter-list">
              {p?.chapters.map((c) => (
                <button
                  key={c.number}
                  className={`${chapter === c.number ? "selected" : ""} ${run?.chapter === c.number && run.status === "running" ? "executing" : ""}`}
                  onClick={() => {
                    setChapter(c.number);
                    if (view === "relations") setSelected(String(c.number));
                    else if (view !== "flow") setView("editor");
                  }}
                >
                  <span className="chapter-num">
                    {String(c.number).padStart(2, "0")}
                  </span>
                  <span>第 {c.number} 章</span>
                  <span
                    className={`chapter-dot ${c.draft ? "written" : c.outline ? "outlined" : ""}`}
                  />
                </button>
              ))}
            </div>
            <button
              className="participation-entry"
              onClick={() => setDialog("participation")}
            >
              需要我输入的节点
            </button>
            <div className="sidebar-bottom">
              <span>
                <span className="chapter-dot outlined" />
                大纲
              </span>
              <span>
                <span className="chapter-dot written" />
                正文
              </span>
              <button className="link" onClick={() => setView("history")}>
                历史版本
              </button>
            </div>
          </aside>
          <main className="main">
            <header className="topbar">
              <div>
                <span className="eyebrow">WeaveGrid · 创作工作台</span>
                <h1>
                  {nav.find((n) => n[0] === view)?.[1]}
                  <span className="title-slash">/</span>
                  <span>
                    {view === "flow"
                      ? "让故事有迹可循"
                      : view === "relations"
                        ? "看见每一次呼应"
                        : view === "editor"
                          ? "把想法写成故事"
                          : view === "review"
                            ? "决定故事的方向"
                            : "保持创作的连续性"}
                  </span>
                </h1>
              </div>
              <div className="top-actions">
                <button
                  className="icon-button"
                  aria-label={
                    theme === "dark" ? "切换日间模式" : "切换黑夜模式"
                  }
                  title={theme === "dark" ? "日间模式" : "黑夜模式"}
                  onClick={() => setTheme(theme === "dark" ? "light" : "dark")}
                >
                  {theme === "dark" ? <Sun size={18} /> : <Moon size={18} />}
                </button>
                <span className={`run-status ${run?.status || ""}`}>
                  <span
                    className={
                      run?.status === "running" ? "pulse-dot" : "status-dot"
                    }
                  />
                  {run ? labels[run.status] : "尚未开始"}
                </span>
                {view === "flow" && (
                  <button
                    className="icon-button"
                    aria-label="切换节点面板"
                    onClick={() => setInspectorOpen(!inspectorOpen)}
                  >
                    <PanelRightClose size={18} />
                  </button>
                )}
              </div>
            </header>
            <div className="run-toolbar">
              <div className="mode-switch">
                <button
                  disabled={!!live}
                  className={mode === "backward" ? "active" : ""}
                  onClick={() => {
                    setMode("backward");
                    setKind("outline");
                  }}
                >
                  <ArrowDownUp size={14} />
                  后向编纲
                </button>
                <button
                  disabled={!!live}
                  className={mode === "forward" ? "active" : ""}
                  onClick={() => {
                    setMode("forward");
                    setKind("draft");
                  }}
                >
                  前向写作
                </button>
              </div>
              <span className="run-position">
                {run
                  ? `第 ${run.chapter} 章 · ${run.node}${run.status === "paused" && run.nextNode ? " → " + run.nextNode : ""}`
                  : "结构已就绪，等待开始"}
                {p && <small>{p.structure.arcs.length} 个剧情单元</small>}
              </span>
              <div className="run-buttons">
                <button
                  className={
                    p?.settings.orchestratorEnabled
                      ? "orchestrator-toggle active"
                      : "orchestrator-toggle"
                  }
                  aria-pressed={!!p?.settings.orchestratorEnabled}
                  disabled={!p || busy}
                  title="自动处理创意、补充资料、取舍和审核；再次点击可交还人工处理"
                  onClick={() =>
                    act(async () => {
                      if (!p) return;
                      const updated = await api<Project>(
                        `/projects/${p.id}`,
                        "PUT",
                        {
                          settings: {
                            ...p.settings,
                            orchestratorEnabled:
                              !p.settings.orchestratorEnabled,
                          },
                        },
                        p.revision,
                      );
                      setP(updated);
                      setProjects((old) =>
                        old.map((item) =>
                          item.id === updated.id ? updated : item,
                        ),
                      );
                      if (
                        updated.settings.orchestratorEnabled &&
                        run?.status === "waiting_input"
                      ) {
                        setDialog("");
                        await refresh(updated.id);
                      }
                    })
                  }
                >
                  <Bot size={15} />
                  统筹 AI
                  {p?.settings.orchestratorEnabled ? " · 已开启" : " · 关闭"}
                </button>
                {!live ? (
                  <button
                    className="primary"
                    disabled={
                      busy || !p || p.archived || !actions.includes("start")
                    }
                    onClick={() => start()}
                  >
                    <Play size={14} />
                    开始{mode === "backward" ? "编纲" : "写作"}
                  </button>
                ) : (
                  <>
                    {run.status === "running" ? (
                      <button
                        disabled={busy || !actions.includes("pause")}
                        onClick={() => control("pause")}
                      >
                        <Pause size={14} />
                        暂停
                      </button>
                    ) : (
                      <button
                        className="primary"
                        disabled={busy || !actions.includes("resume")}
                        onClick={() => control("resume")}
                      >
                        <Play size={14} />
                        {run.status === "failed" ? "重试当前步骤" : "继续"}
                      </button>
                    )}
                    <button
                      disabled={busy || !actions.includes("stop")}
                      className="danger-subtle"
                      onClick={() => control("stop")}
                    >
                      <Square size={13} />
                      终止
                    </button>
                  </>
                )}
                <button
                  disabled={
                    busy ||
                    !p ||
                    p.archived ||
                    (!!live && !actions.includes("step"))
                  }
                  onClick={() => (live ? control("step") : start(true))}
                  title="执行一个内部步骤后暂停；模型节点会完成本次生成"
                >
                  <StepForward size={15} />
                  前进一个小节点
                </button>
                <button disabled={!p} onClick={() => setDialog("checkpoints")}>
                  <Flag size={15} />
                  检查点 / 回退
                </button>
                <button onClick={() => setDialog("review")}>
                  <CheckCircle2 size={15} />
                  审核{waiting > 0 && <b className="counter">{waiting}</b>}
                </button>
              </div>
            </div>
            {retryMessage && run?.status === "running" && (
              <div className="banner waiting">
                <LoaderCircle className="spin" size={15} />
                <span>{retryMessage}</span>
              </div>
            )}
            {run?.orchestratorGate && run.status === "running" && (
              <div className="banner waiting" role="status">
                <Bot size={16} />
                <span>
                  {run.node === "O2"
                    ? "统筹正在压缩记忆，保存近期决定与未完成事项"
                    : `统筹正在处理第 ${run.chapter} 章 · ${run.orchestratorGate}`}
                </span>
                <button onClick={() => setView("models")}>查看统筹记忆</button>
              </div>
            )}
            {error && (
              <div className="banner error">
                <AlertCircle size={16} />
                <span>{error}</span>
                <button onClick={() => setError("")} aria-label="关闭错误">
                  <X size={14} />
                </button>
              </div>
            )}
            {run?.error && !error && (
              <div className="banner error">
                <AlertCircle size={16} />
                <span>{run.error}</span>
              </div>
            )}
            {run?.status === "waiting_input" && (
              <div className="banner waiting">
                <UserIcon />
                <span>{run.message}</span>
                <button onClick={openWaiting}>
                  打开待办
                  <ChevronRight size={14} />
                </button>
              </div>
            )}
            {p && p.pendingEdits?.length > 0 && (
              <div className="banner waiting">
                <FileText size={14} />
                <span>
                  {p.pendingEdits.length}{" "}
                  处编辑已保存，将在下一章节定位或进度调度时采用。
                </span>
              </div>
            )}
            <div className="workspace-content">
              {!p || !workflow ? (
                <div className="empty-state">
                  <div className="large-symbol">
                    <GitBranch />
                  </div>
                  <h2>从一张经纬图开始</h2>
                  <p>生成章节结构，再让故事逐步生长。</p>
                  <button className="primary" onClick={() => setCreate(true)}>
                    <Plus size={16} />
                    创建小说项目
                  </button>
                </div>
              ) : (
                <>
                  <div
                    className={`content-stage ${view === "flow" || view === "relations" ? "canvas-stage" : ""}`}
                  >
                    {view === "flow" || view === "relations" ? (
                      <Graph
                        nodes={workflow.nodes}
                        flowEdges={workflow.edges}
                        project={p}
                        chapter={chapter}
                        run={run}
                        mode={mode}
                        selected={selected}
                        onSelect={(id) => {
                          setSelected(id);
                          if (view === "relations") setChapter(Number(id));
                          setInspectorOpen(true);
                        }}
                        expanded={expanded}
                        onExpand={setExpanded}
                        relation={view === "relations"}
                        statuses={statuses}
                      />
                    ) : view === "editor" ? (
                      <Editor
                        p={p}
                        chapter={chapter}
                        setChapter={setChapter}
                        kind={kind}
                        setKind={setKind}
                        onSaved={afterProject}
                        act={act}
                      />
                    ) : view === "review" ? (
                      <ReviewPanel
                        p={p}
                        run={run}
                        onRun={setRun}
                        chapter={chapter}
                        setChapter={setChapter}
                        act={act}
                      />
                    ) : view === "models" ? (
                      <Models
                        p={p}
                        attempts={attempts}
                        save={saveProject}
                        onSaved={afterProject}
                        act={act}
                      />
                    ) : (
                      <HistoryPanel
                        p={p}
                        runs={runs}
                        attempts={attempts}
                        onRun={async (r) => {
                          setRun(r);
                          setAttempts(
                            await api<Attempt[]>(`/runs/${r.id}/attempts`),
                          );
                        }}
                      />
                    )}
                  </div>
                  {inspectorOpen && view === "flow" && (
                    <aside className="inspector">
                      <Inspector
                        p={p}
                        workflow={workflow}
                        selected={selected}
                        setSelected={setSelected}
                        attempts={attempts}
                        run={run}
                        onSaved={afterProject}
                        act={act}
                        expand={(id) => setExpanded(id)}
                        onCreativeInput={() => setDialog("creative-input")}
                      />
                    </aside>
                  )}
                </>
              )}
            </div>
            <section className={`event-drawer ${logOpen ? "open" : ""}`}>
              <button
                className="event-handle"
                onClick={() => setLogOpen(!logOpen)}
              >
                <Activity size={14} />
                <b>执行动态</b>
                <span>{run?.message || "所有执行步骤都会在这里留下记录"}</span>
                <small>{events.length} 条事件</small>
                <ChevronDown size={14} />
              </button>
              {logOpen && (
                <div className="event-list">
                  {events
                    .slice()
                    .reverse()
                    .map((e) => (
                      <div key={e.seq}>
                        <time>{new Date(e.created).toLocaleTimeString()}</time>
                        <b>{e.type}</b>
                        <span>{eventDescription(e)}</span>
                      </div>
                    ))}
                </div>
              )}
            </section>
          </main>
          {notice && (
            <div className="toast">
              <CheckCircle2 size={16} />
              {notice}
            </div>
          )}
          {busy && (
            <div className="working-indicator">
              <LoaderCircle className="spin" size={15} />
              处理中
            </div>
          )}
          <Modal
            open={dialog === "participation"}
            title="需要你输入的节点"
            onClose={() => setDialog("")}
          >
            <p>每次需要你操作时会自动弹窗；关掉窗口可从“打开待办”重新进入。</p>
            <ul className="participation-list">
              <li>
                <b>C2 · 人工创意（每章必经）</b>
                <p>
                  填写或确认本章创意、必留要求与可选想法。没有创意也须由你明确确认。
                </p>
              </li>
              <li>
                <b>B8 · 补充必要资料（有缺口时）</b>
                <p>B7 发现只能由你决定的信息时，在此回答问题。</p>
              </li>
              <li>
                <b>F7 · 跨章修订取舍（有关键冲突时）</b>
                <p>
                  决定必留要求、关键创意或反复修订的问题，采用、拒绝或修改建议。
                </p>
              </li>
              <li>
                <b>H5 · 共同审核（到达审核点时）</b>
                <p>
                  正序查看原文、处理建议并确认审核完成。默认全书审核，也可启用剧情单元审核。
                </p>
              </li>
            </ul>
            <p className="muted">
              任意模型节点请求失败时会弹出错误处理窗口。文风准备子流程已移除。
            </p>
          </Modal>
          <LibraryDialog
            open={dialog === "library"}
            projects={projects}
            currentId={p?.id}
            onClose={() => setDialog("")}
            onSelect={selectProject}
            onCreate={() => {
              setDialog("");
              setCreate(true);
            }}
            onChanged={libraryChanged}
            act={act}
          />
          {p && (
            <>
              <CheckpointsDialog
                key={`checkpoints:${p.id}`}
                open={dialog === "checkpoints"}
                p={p}
                run={run}
                onClose={() => setDialog("")}
                onChanged={() => refresh(p.id, true)}
                act={act}
              />
              <ReviewDialog
                key={`review:${p.id}`}
                open={dialog === "review"}
                p={p}
                run={run}
                onRun={setRun}
                chapter={chapter}
                setChapter={setChapter}
                act={act}
                onClose={() => setDialog("")}
                onContinue={continueDialog}
              />
              <CreativeInputDialog
                key={`creative:${p.id}`}
                open={dialog === "creative-input"}
                p={p}
                run={run}
                onRun={setRun}
                onSaved={afterProject}
                onClose={() => setDialog("")}
                act={act}
                onContinue={(r) =>
                  sendControl(r.singleStep ? "step" : "resume", r)
                }
              />
              <InputDialog
                open={dialog === "input"}
                p={p}
                run={run}
                onRun={setRun}
                onSaved={afterProject}
                onClose={() => setDialog("")}
                act={act}
                onContinue={(r) =>
                  sendControl(r.singleStep ? "step" : "resume", r)
                }
              />
              <ModelDialog
                key={`model-dialog:${p.id}`}
                open={dialog === "models"}
                p={p}
                run={run}
                attempts={attempts}
                save={saveProject}
                onSaved={afterProject}
                act={act}
                onClose={() => setDialog("")}
                onContinue={continueDialog}
              />
              <Modal
                open={dialog === "failure"}
                title="当前步骤需要处理"
                onClose={() => setDialog("")}
                footer={
                  <>
                    <button onClick={() => setDialog("models")}>
                      修改模型配置
                    </button>
                    <button className="primary" onClick={continueDialog}>
                      重试当前步骤
                    </button>
                  </>
                }
              >
                <p>
                  第 {run?.chapter} 章 · {run?.node}
                </p>
                <p className="error-detail">{run?.error}</p>
                <p className="muted">
                  已完成的步骤和部分草稿已保留。重试会重新生成被中断的步骤。
                </p>
              </Modal>
              <Modal
                open={dialog === "prerequisites"}
                title="还需要章节大纲"
                onClose={() => setDialog("")}
                footer={
                  <>
                    <button
                      onClick={() => {
                        const c = p.chapters.find((c) => !c.outline);
                        setChapter(c?.number || 1);
                        setKind("outline");
                        setView("editor");
                        setDialog("");
                      }}
                    >
                      手动补充大纲
                    </button>
                    <button
                      className="primary"
                      onClick={() => {
                        setMode("backward");
                        setKind("outline");
                        setDialog("");
                      }}
                    >
                      切换到后向编纲
                    </button>
                  </>
                }
              >
                <p>写正文前，需要先补齐大纲。</p>
                <p>
                  缺少：
                  {p.chapters
                    .filter((c) => !c.outline)
                    .map((c) => `第 ${c.number} 章`)
                    .join("、")}
                </p>
              </Modal>
            </>
          )}
          {create && (
            <NewProjectDialog
              onClose={() => {
                setCreate(false);
              }}
              onCreate={async (next) => {
                afterProject(next);
                setCreate(false);
                setRun(undefined);
                setRuns([]);
                setAttempts([]);
                setView("flow");
                setChapter(1);
                setStatuses({});
                setEvents([]);
              }}
            />
          )}
        </div>
      </OperationContext.Provider>
    </CreativeContext.Provider>
  );
}
function UserIcon() {
  return <CheckCircle2 size={15} />;
}
function eventDescription(event: RunEvent) {
  const d = event.data as Record<string, unknown>;
  if (event.type === "checkpoint") return `已保存检查点：${d.name}`;
  if (event.type === "orchestrator") {
    try {
      return `统筹 · 第 ${d.chapter} 章 · ${JSON.parse(String(d.decision)).reason}`;
    } catch {
      return "统筹决定已保存";
    }
  }
  if (event.type === "run")
    return String(d.message || labels[String(d.status)] || d.stage);
  if (event.type === "node")
    return `${d.node} · ${labels[String(d.status)] || d.status}`;
  if (event.type === "retry") return String(d.message);
  if (event.type === "stream") return `${d.node} · 正在生成内容`;
  if (event.type === "attempt")
    return `${d.node} · ${labels[String(d.status)] || d.status}`;
  return "项目有效版本已更新";
}
