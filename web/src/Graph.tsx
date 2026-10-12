import { ContextReader } from "./ContextReader";
import ForeshadowTimeline from "./ForeshadowTimeline";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ReactFlow,
  Background,
  MiniMap,
  Handle,
  Position,
  MarkerType,
  BaseEdge,
  getSmoothStepPath,
  applyNodeChanges,
  useUpdateNodeInternals,
  useViewport,
  useNodesInitialized,
  useReactFlow,
  NodeToolbar,
  type NodeProps,
  type Node,
  type Edge,
  type EdgeProps,
  type ReactFlowInstance,
} from "@xyflow/react";
import {
  Check,
  ChevronRight,
  Circle,
  LoaderCircle,
  UserRound,
  Workflow as FlowIcon,
  Maximize2,
  Minimize2,
  RotateCcw,
  Download,
  X,
  LayoutGrid,
} from "lucide-react";
import type { FlowNode, Project, Run } from "./types";
import { api } from "./api";
import {
  compactPositions,
  parseLayout,
  routeGroupedConnections,
  type Direction,
  type Point,
  type LayoutFile,
  type Route,
  type Side,
} from "./graph-layout";
import "@xyflow/react/dist/style.css";

type StageData = {
  title: string;
  subtitle: string;
  code: string;
  status: string;
  count: string;
  ai: boolean;
  human?: boolean;
  current?: boolean;
  kind: "stage" | "step" | "chapter" | "end";
  expand?: () => void;
  ports?: {
    id: string;
    side: Side;
    offset: number;
    type: "source" | "target";
  }[];
  [key: string]: unknown;
};
type StageNode = Node<StageData>;
type RoutedEdge = Edge<{ route?: Route }, "orthogonal">;
type Spec = {
  nodes: {
    id: string;
    data: StageData;
    parentId?: string;
    width?: number;
    height?: number;
  }[];
  edges: (Edge & { feedback?: boolean })[];
};
const sides = {
  left: Position.Left,
  right: Position.Right,
  top: Position.Top,
  bottom: Position.Bottom,
};
const stateNames: Record<string, string> = {
  running: "执行中",
  succeeded: "已完成",
  completed: "已完成",
  waiting_input: "待人工",
  paused: "已暂停",
  failed: "出错",
  stopped: "已终止",
  interrupted: "已中断",
};

// 必须在 React Flow 内等节点及父分组完成测量，再聚焦绝对位置。
// 只改视口，不改布局；双帧避开首次全景适配，刷新后也能落到当前小节点。
function ExecutionFocus({
  enabled,
  node,
  runKey,
  status,
}: {
  enabled: boolean;
  node?: string;
  runKey: string;
  status?: string;
}) {
  const initialized = useNodesInitialized();
  const { fitView } = useReactFlow();
  useEffect(() => {
    if (
      !enabled ||
      !initialized ||
      !node ||
      ["completed", "stopped"].includes(status || "")
    )
      return;
    let second = 0;
    const first = requestAnimationFrame(() => {
      second = requestAnimationFrame(() => {
        void fitView({
          nodes: [{ id: node }],
          padding: 1.2,
          minZoom: 0.75,
          maxZoom: 1.05,
          duration: 260,
        });
      });
    });
    return () => {
      cancelAnimationFrame(first);
      cancelAnimationFrame(second);
    };
  }, [enabled, initialized, node, runKey, status, fitView]);
  return null;
}

function Stage({ id, data, selected }: NodeProps<StageNode>) {
  const updateInternals = useUpdateNodeInternals();
  const portKey = JSON.stringify(data.ports);
  useEffect(() => {
    updateInternals(id);
  }, [id, portKey, updateInternals]);
  return (
    <div
      className={`flow-node ${data.kind} ${data.status} ${data.current ? "current-node" : ""} ${selected ? "selected" : ""}`}
      title={data.subtitle}
      onDoubleClick={
        data.expand
          ? (event) => {
              event.stopPropagation();
              data.expand?.();
            }
          : undefined
      }
    >
      {(data.ports || []).map((p) => (
        <Handle
          key={p.id}
          id={p.id}
          type={p.type}
          position={sides[p.side]}
          style={
            p.side === "left" || p.side === "right"
              ? { top: `${p.offset * 100}%` }
              : { left: `${p.offset * 100}%` }
          }
        />
      ))}
      {data.current && (
        <span className="current-node-label">
          当前 · {stateNames[data.status] || "所在节点"}
        </span>
      )}
      <div className="flow-node-heading">
        <span className="flow-code">{data.code}</span>
        <strong>{data.title}</strong>
        {data.status === "running" ? (
          <LoaderCircle size={15} className="spin" />
        ) : data.status === "succeeded" || data.status === "completed" ? (
          <Check size={15} />
        ) : data.status === "waiting_input" ? (
          <UserRound size={15} />
        ) : (
          <Circle size={10} />
        )}
      </div>
      <div className="flow-node-footer">
        <span>{stateNames[data.status] || data.count}</span>
        {data.expand ? (
          <button
            className="nodrag nopan"
            aria-label={`展开 ${data.code} 子流程`}
            onClick={(event) => {
              event.stopPropagation();
              data.expand?.();
            }}
          >
            展开 <ChevronRight size={12} />
          </button>
        ) : (
          <small>
            {data.kind === "step"
              ? data.human
                ? "人工"
                : data.ai
                  ? "模型"
                  : "程序"
              : ""}
          </small>
        )}
      </div>
    </div>
  );
}
function WorkflowGroup({ id, data, selected }: NodeProps<StageNode>) {
  const { zoom } = useViewport();
  const updateInternals = useUpdateNodeInternals();
  const portKey = JSON.stringify(data.ports);
  useEffect(() => {
    updateInternals(id);
  }, [id, portKey, updateInternals]);
  return (
    <div
      className={`flow-group ${selected ? "selected" : ""} ${data.status}`}
      onDoubleClick={(event) => {
        event.stopPropagation();
        data.expand?.();
      }}
    >
      {(data.ports || []).map((p) => (
        <Handle
          key={p.id}
          id={p.id}
          type={p.type}
          position={sides[p.side]}
          style={
            p.side === "left" || p.side === "right"
              ? { top: `${p.offset * 100}%` }
              : { left: `${p.offset * 100}%` }
          }
        />
      ))}
      {zoom < 0.45 && (
        <NodeToolbar isVisible position={Position.Top}>
          <span className="group-overview-label">
            {data.code} · {data.title}
          </span>
        </NodeToolbar>
      )}
      <div className="flow-group-header">
        <span className="flow-code">{data.code}</span>
        <strong>{data.title}</strong>
        <span>{stateNames[data.status] || data.count}</span>
        <button
          className="nodrag nopan"
          aria-label={`定位 ${data.code} 子流程`}
          onClick={(event) => {
            event.stopPropagation();
            data.expand?.();
          }}
        >
          定位
        </button>
      </div>
      <p>{data.subtitle}</p>
    </div>
  );
}
function OrthogonalEdge(props: EdgeProps<RoutedEdge>) {
  const route = props.data?.route;
  const first = route?.points[0],
    last = route?.points.at(-1);
  const settled =
    first &&
    last &&
    Math.abs(first.x - props.sourceX) < 4 &&
    Math.abs(first.y - props.sourceY) < 4 &&
    Math.abs(last.x - props.targetX) < 4 &&
    Math.abs(last.y - props.targetY) < 4;
  // 拖动时让端点立即跟随；松手后重新绕开所有节点。两种路径都只包含直角。
  const [fallback, lx, ly] = getSmoothStepPath({ ...props, borderRadius: 0 });
  const path = settled
    ? route.points.map((p, i) => `${i ? "L" : "M"} ${p.x} ${p.y}`).join(" ")
    : fallback;
  return (
    <>
      <path
        d={path}
        fill="none"
        stroke="var(--canvas-surface)"
        strokeWidth={7}
        style={{
          pointerEvents: "none",
          strokeDasharray: "none",
          animation: "none",
        }}
      />
      <BaseEdge
        id={props.id}
        path={path}
        markerEnd={props.markerEnd}
        style={props.style}
        label={props.label}
        labelX={settled ? route.label.x : lx}
        labelY={settled ? route.label.y : ly}
        labelStyle={{ fill: "var(--canvas-ink)", fontSize: 12, fontWeight: 500 }}
        labelBgStyle={{ fill: "var(--canvas-surface)", fillOpacity: 0.97 }}
        labelBgPadding={[6, 4]}
        labelBgBorderRadius={2}
      />
    </>
  );
}
const nodeTypes = { stage: Stage, workflowGroup: WorkflowGroup },
  edgeTypes = { orthogonal: OrthogonalEdge };

function makeSpec(
  definitions: FlowNode[],
  flowEdges: Record<string, [string, string, string?][]>,
  project: Project,
  mode: string,
  expanded: string,
  relation: boolean,
): Spec {
  if (relation)
    return {
      nodes: project.chapters.map((c) => ({
        id: String(c.number),
        data: {
          title: `第 ${String(c.number).padStart(2, "0")} 章`,
          code: String(c.number).padStart(2, "0"),
          kind: "chapter",
          ai: false,
          status: "",
          subtitle:
            project.structure.nodes.find((n) => n.ch === c.number)?.kind ||
            "自由剧情章",
          count: c.draft ? "正文已写" : c.outline ? "大纲已编" : "待创作",
        },
      })),
      edges: [
        ...project.structure.lines.map(([a, b]) => ({
          id: `s${a}-${b}`,
          source: String(a),
          target: String(b),
          label: String(
            project.structure.threads.filter(
              (t) => t.plant === a && t.payoff === b,
            ).length,
          ),
        })),
        ...(project.links || [])
          .filter((l) => l.valid)
          .map((l) => ({
            id: l.id,
            source: String(l.from),
            target: String(l.to),
            label: "新关联",
            feedback: true,
          })),
      ],
    };
  const groups: Spec["nodes"] = definitions.map((n) => ({
    id: n.id,
    width: 936,
    height: Math.ceil(n.steps.length / 3) * 184 + 120,
    data: {
      title: n.name,
      subtitle: n.description,
      code: n.id,
      kind: "stage",
      status: "",
      ai: n.steps.some((s) => s.ai),
      count: `${n.steps.length} 个步骤`,
    },
  }));
  const children: Spec["nodes"] = definitions.flatMap((n) =>
    n.steps.map((step) => ({
      id: step.id,
      parentId: n.id,
      data: {
        title: step.name,
        subtitle: step.human
          ? "需要人工输入或确认"
          : step.ai
            ? "调用模型，可独立配置提示词"
            : "程序按规则执行",
        code: step.id,
        kind: "step",
        status: "",
        ai: step.ai,
        human: step.human,
        count: step.human ? "等待你输入" : step.ai ? "独立提示词" : "自动执行",
      },
    })),
  );
  const internal = definitions.flatMap((n) =>
    (flowEdges[n.id] || []).map(([a, b, label]) => ({
      id: `${n.id}${a}-${n.id}${b}`,
      source: n.id + a,
      target: n.id + b,
      label,
      feedback: Number(b) < Number(a),
    })),
  );
  const main: [string, string, string?, boolean?][] = [
    ["A", "J", "读取进度"],
    ["B", "C"],
    ["C", "D", "后向编纲"],
    ["C", "E", "前向写作"],
    ["D", "F"],
    ["E", "F"],
    ["F", "G"],
    ["G", "J"],
    ["J", "H", "到达审核点"],
    ["J", "done", "全部完成"],
    ["J", "B", "继续创作", true],
    ["H", "F", "选定修改", true],
    ["H", "J", "审核完成", true],
  ];
  return {
    nodes: [
      ...groups,
      ...children,
      {
        id: "done",
        data: {
          title: "当前模式完成",
          subtitle: "全部章节和最终审核均完成",
          code: "✓",
          kind: "end",
          status: "",
          ai: false,
          count: "大纲 / 正文",
        },
      },
    ],
    edges: [
      ...internal,
      ...main.map(([source, target, label, feedback]) => ({
        id: source + "-" + target,
        source,
        target,
        label,
        feedback,
      })),
    ],
  };
}

function unifiedPositions(
  spec: Spec,
  direction: Direction,
): Record<string, Point> {
  const groups = spec.nodes.filter((n) => !n.parentId);
  const columns =
    direction === "RIGHT" ? groups.length : direction === "DOWN" ? 1 : 3;
  const positions: Record<string, Point> = {};
  groups.forEach((group, i) => {
    positions[group.id] = {
      x: (i % columns) * 1150,
      y: Math.floor(i / columns) * 1100,
    };
  });
  for (const group of groups) {
    const children = spec.nodes.filter((n) => n.parentId === group.id);
    const local = compactPositions(children.map((n) => n.id));
    for (const child of children)
      positions[child.id] = {
        x: local[child.id].x + 80,
        y: local[child.id].y + 112,
      };
  }
  return positions;
}

type Props = {
  nodes: FlowNode[];
  flowEdges: Record<string, [string, string, string?][]>;
  project: Project;
  chapter: number;
  run?: Run;
  mode: string;
  selected: string;
  onSelect: (id: string) => void;
  expanded: string;
  onExpand: (id: string) => void;
  relation: boolean;
  statuses: Record<string, string>;
};
export default function Graph(props: Props) {
  const [focus, setFocus] = useState(false);
  if (props.relation)
    return (
      <ForeshadowTimeline
        key={props.project.id}
        project={props.project}
        run={props.run}
        chapter={props.chapter}
        onSelect={props.onSelect}
        focus={focus}
        setFocus={setFocus}
      />
    );
  const graph = props.relation ? "relations" : "workflow:all";
  // 切换图时重建画布；选中节点、流式输出和运行状态变化绝不重建布局。
  return (
    <Diagram
      key={`${props.project.id}:${graph}`}
      {...props}
      graph={graph}
      focus={focus}
      setFocus={setFocus}
    />
  );
}

function Diagram({
  nodes: definitions,
  flowEdges,
  project,
  run,
  mode,
  selected,
  onSelect,
  expanded,
  onExpand,
  relation,
  statuses,
  graph,
  focus,
  setFocus,
}: Props & {
  graph: string;
  focus: boolean;
  setFocus: (value: boolean) => void;
}) {
  const spec = makeSpec(
    definitions,
    flowEdges,
    project,
    mode,
    expanded,
    relation,
  );
  const remoteKey = relation ? `relations:${project.id}` : graph;
  const storageKey = `novelflow:layout:v1:${remoteKey}`;
  const legacyKey = `novelflow:layout:v1:${project.id}:${graph}`;
  const initial = useMemo(() => {
    try {
      const text =
        localStorage.getItem(storageKey) || localStorage.getItem(legacyKey);
      return text
        ? parseLayout(
            text,
            graph,
            spec.nodes.map((n) => n.id),
          )
        : undefined;
    } catch {
      return undefined;
    }
  }, [storageKey]);
  const [direction, setDirection] = useState<Direction>(
    initial?.direction || (relation ? "DOWN" : "compact"),
  );
  const [nodes, setNodes] = useState<StageNode[]>([]),
    [settled, setSettled] = useState<StageNode[]>([]);
  const [flow, setFlow] = useState<ReactFlowInstance<StageNode, RoutedEdge>>();
  const [layoutError, setLayoutError] = useState(""),
    [saved, setSaved] = useState(initial ? "已恢复布局" : "拖动后自动保存");
  const [showMap, setShowMap] = useState(false);
  const [layoutDialog, setLayoutDialog] = useState(false),
    [layoutText, setLayoutText] = useState("");
  const [dialogError, setDialogError] = useState("");
  const savedLayout = useRef(initial);
  const remoteRevision = useRef(0),
    remoteText = useRef("");
  const writeQueue = useRef(Promise.resolve());
  const [hydrated, setHydrated] = useState(false);
  useEffect(() => {
    let alive = true;
    api<{ revision: number; layout: LayoutFile | null }>(
      `/layouts/${encodeURIComponent(remoteKey)}`,
    )
      .then((record) => {
        if (!alive) return;
        remoteRevision.current = record.revision;
        if (record.layout) {
          const restored = parseLayout(
            JSON.stringify(record.layout),
            graph,
            spec.nodes.map((n) => n.id),
          );
          savedLayout.current = restored;
          remoteText.current = JSON.stringify(restored);
          setDirection(restored.direction);
        }
      })
      .catch(() => {
        if (alive) setSaved("暂用浏览器备份，连接恢复后继续保存");
      })
      .finally(() => {
        if (alive) setHydrated(true);
      });
    return () => {
      alive = false;
    };
  }, [remoteKey]);
  const nodeState = useRef(nodes);
  nodeState.current = nodes;
  const [arrange, setArrange] = useState(0);
  const topology = JSON.stringify({
    ids: spec.nodes.map((n) => n.id),
    edges: spec.edges.map((e) => [e.source, e.target, e.id]),
  });
  const dimensions = { width: relation ? 164 : 196, height: 88 };
  const fit = useCallback(() => {
    flow?.fitView({ padding: 0.15, maxZoom: 1.1, duration: 180 });
  }, [flow]);
  const save = (next: StageNode[], nextDirection = direction) => {
    if (relation) return; // 经纬页只读，不写入章节关系或布局。
    const file: LayoutFile = {
      format: "novelflow-layout",
      version: 1,
      graph,
      direction: nextDirection,
      positions: Object.fromEntries(next.map((n) => [n.id, n.position])),
    };
    savedLayout.current = file;
    try {
      localStorage.setItem(storageKey, JSON.stringify(file));
      setSaved("正在保存布局…");
    } catch {
      setSaved("浏览器备份不可用，正在保存到工作台…");
    }
    // 串行写入并携带版本，快速连续拖动不会让较早的位置覆盖最后一次移动。
    const text = JSON.stringify(file);
    writeQueue.current = writeQueue.current.then(async () => {
      if (text === remoteText.current) {
        setSaved("布局已保存到工作台");
        return;
      }
      try {
        const record = await api<{ revision: number; layout: LayoutFile }>(
          `/layouts/${encodeURIComponent(remoteKey)}`,
          "PUT",
          file,
          remoteRevision.current,
        );
        remoteRevision.current = record.revision;
        remoteText.current = text;
        if (savedLayout.current === file) setSaved("布局已保存到工作台");
      } catch (e) {
        setSaved(
          `已保留浏览器备份；保存未完成：${e instanceof Error ? e.message : "连接失败"}`,
        );
      }
    });
  };
  useEffect(() => {
    if (!hydrated) return;
    let alive = true;
    setLayoutError("");
    const commit = (positions: Record<string, Point>) => {
      if (!alive) return;
      const next: StageNode[] = spec.nodes.map((n) => ({
        ...n,
        type: n.data.kind === "stage" ? "workflowGroup" : "stage",
        ...dimensions,
        width: n.width || dimensions.width,
        height: n.height || dimensions.height,
        parentId: n.parentId,
        extent: n.parentId ? "parent" : undefined,
        zIndex: n.data.kind === "stage" ? 0 : 2,
        position: positions[n.id] || { x: 0, y: 0 },
        ariaLabel: `${n.id} ${n.data.title}`,
        ariaRole: "group",
      }));
      setNodes(next);
      setSettled(next);
      save(next, direction);
    };
    if (
      savedLayout.current?.direction === direction &&
      spec.nodes.every((n) => savedLayout.current?.positions[n.id])
    )
      commit(savedLayout.current.positions);
    else if (!relation) commit(unifiedPositions(spec, direction));
    else if (direction === "compact")
      commit(
        compactPositions(
          spec.nodes.map((n) => n.id),
          relation ? 6 : 3,
        ),
      );
    else {
      setNodes([]);
      setSettled([]);
      import("elkjs/lib/elk.bundled.js")
        .then(({ default: ELK }) =>
          new ELK().layout({
            id: "root",
            layoutOptions: {
              "elk.algorithm": "layered",
              "elk.direction": direction,
              "elk.spacing.nodeNode": "70",
              "elk.layered.spacing.nodeNodeBetweenLayers": "100",
              "elk.layered.considerModelOrder.strategy": "NODES_AND_EDGES",
            },
            children: spec.nodes.map((n) => ({ id: n.id, ...dimensions })),
            edges: spec.edges.map((e) => ({
              id: e.id,
              sources: [e.source],
              targets: [e.target],
            })),
          }),
        )
        .then((result) =>
          commit(
            Object.fromEntries(
              (result.children || []).map((n) => [
                n.id,
                { x: n.x || 0, y: n.y || 0 },
              ]),
            ),
          ),
        )
        .catch(() => {
          if (alive) {
            setLayoutError("自动布局加载失败，已按顺序排列");
            commit(
              compactPositions(
                spec.nodes.map((n) => n.id),
                relation ? 6 : 3,
              ),
            );
          }
        });
    }
    return () => {
      alive = false;
    };
    // 只依赖拓扑；执行状态、章节内容和选择变化不能把用户摆好的节点移走。
  }, [topology, direction, arrange, hydrated]);
  useEffect(() => {
    if (nodes.length && flow) {
      const frame = requestAnimationFrame(fit);
      return () => cancelAnimationFrame(frame);
    }
  }, [flow, !!nodes.length, direction, arrange, focus]);
  useEffect(() => {
    if (!focus) return;
    const escape = (e: KeyboardEvent) => {
      if (e.key === "Escape") setFocus(false);
    };
    window.addEventListener("keydown", escape);
    return () => window.removeEventListener("keydown", escape);
  }, [focus]);
  const routes = useMemo(
    () =>
      routeGroupedConnections(
        settled.map((n) => ({
          id: n.id,
          parentId: n.parentId,
          ...n.position,
          width: n.width!,
          height: n.height!,
        })),
        spec.edges,
      ),
    [settled, topology],
  );
  const liveRun = run?.mode === mode ? run : undefined;
  const [followExecution, setFollowExecution] = useState(true);
  const drawn = nodes.map((n) => {
    const definition = spec.nodes.find((s) => s.id === n.id)!.data;
    let status = relation || (run && !liveRun) ? "" : statuses[n.id] || "";
    if (
      !relation &&
      liveRun &&
      (liveRun.node === n.id ||
        (n.id.length === 1 && liveRun.node.startsWith(n.id)))
    )
      status = liveRun.status;
    if (n.id === "done")
      status = liveRun?.status === "completed" ? "completed" : "";
    return {
      ...n,
      selected: selected === n.id,
      data: {
        ...definition,
        status,
        current:
          !relation &&
          liveRun?.node === n.id &&
          !["completed", "stopped"].includes(liveRun.status),
        ports: spec.edges.flatMap((e) => {
          const r = routes[e.id];
          if (!r) return [];
          const ports: NonNullable<StageData["ports"]> = [];
          if (e.source === n.id)
            ports.push({
              id: `out-${e.id}`,
              side: r.sourceSide,
              offset: r.sourceOffset,
              type: "source",
            });
          if (e.target === n.id)
            ports.push({
              id: `in-${e.id}`,
              side: r.targetSide,
              offset: r.targetOffset,
              type: "target",
            });
          return ports;
        }),
        expand:
          definition.kind === "stage"
            ? () => {
                onExpand(n.id);
                onSelect(n.id);
              }
            : undefined,
      },
    };
  });
  const edges: RoutedEdge[] = spec.edges.map((e) => {
    const route = routes[e.id],
      active =
        !relation &&
        liveRun?.status === "running" &&
        (e.source === liveRun.node || e.source === liveRun.stage);
    const color = active
      ? "#157b60"
      : e.feedback
        ? relation
          ? "#8754aa"
          : "#af743c"
        : "#607b70";
    return {
      ...e,
      type: "orthogonal",
      zIndex: 1,
      data: { route },
      sourceHandle: `out-${e.id}`,
      targetHandle: `in-${e.id}`,
      style: {
        stroke: color,
        strokeWidth: active ? 2.6 : 1.8,
        strokeDasharray: e.feedback ? "6 4" : undefined,
      },
      markerEnd: { type: MarkerType.ArrowClosed, color, width: 18, height: 18 },
      animated: active,
    };
  });
  const title = relation ? "章节经纬图" : "完整工作流";
  const locateGroup = useCallback(
    (id: string) => {
      const node = nodes.find((n) => n.id === id);
      if (node)
        flow?.fitView({
          nodes: [node],
          padding: 0.12,
          maxZoom: 1,
          duration: 220,
        });
    },
    [flow, nodes],
  );
  useEffect(() => {
    if (expanded && !relation && nodes.length && flow) {
      locateGroup(expanded);
      onExpand("");
    }
  }, [expanded, relation, !!nodes.length, flow]);
  const openLayout = () => {
    setLayoutText(
      JSON.stringify(
        {
          format: "novelflow-layout",
          version: 1,
          graph,
          direction,
          positions: Object.fromEntries(nodes.map((n) => [n.id, n.position])),
        },
        null,
        2,
      ),
    );
    setDialogError("");
    setLayoutDialog(true);
  };
  const importLayout = () => {
    try {
      const file = parseLayout(
        layoutText,
        graph,
        nodes.map((n) => n.id),
      );
      const next = nodes.map((n) => ({ ...n, position: file.positions[n.id] }));
      save(next, file.direction);
      setDirection(file.direction);
      setNodes(next);
      setSettled(next);
      setArrange((v) => v + 1);
      setLayoutDialog(false);
    } catch (e) {
      setDialogError(e instanceof Error ? e.message : "布局无法读取");
    }
  };
  const downloadLayout = () => {
    try {
      const file = parseLayout(
        layoutText,
        graph,
        nodes.map((n) => n.id),
      );
      const url = URL.createObjectURL(
        new Blob([JSON.stringify(file, null, 2)], { type: "application/json" }),
      );
      const a = document.createElement("a");
      a.href = url;
      a.download = `经纬布局-${graph.replaceAll(":", "-")}.json`;
      a.click();
      setTimeout(() => URL.revokeObjectURL(url), 1000);
    } catch (e) {
      setDialogError(e instanceof Error ? e.message : "布局无法读取");
    }
  };
  return (
    <div className={`graph-shell ${focus ? "graph-focused" : ""}`}>
      <div className="graph-toolbar">
        <div className="graph-title">
          <FlowIcon size={17} />
          <strong>{title}</strong>
          <button
            className="icon-button"
            title={focus ? "退出专注画布" : "专注画布"}
            aria-label={focus ? "退出专注画布" : "专注画布"}
            onClick={() => setFocus(!focus)}
          >
            {focus ? <Minimize2 size={16} /> : <Maximize2 size={16} />}
          </button>
        </div>
        {!relation && (
          <div className="graph-tools">
            <select
              aria-label="排列方向"
              value={direction}
              onChange={(e) => {
                savedLayout.current = undefined;
                setDirection(e.target.value as Direction);
                setSaved("拖动后自动保存");
              }}
            >
              <option value="compact">紧凑排布</option>
              <option value="RIGHT">从左到右</option>
              <option value="DOWN">从上到下</option>
            </select>
            <button
              onClick={() => {
                savedLayout.current = undefined;
                try {
                  localStorage.removeItem(storageKey);
                } catch {}
                setArrange((v) => v + 1);
                setSaved("已恢复自动排布");
              }}
              title="重新排列当前图，清除当前图的手动位置"
            >
              <RotateCcw size={13} />
              自动排布
            </button>
            <button onClick={openLayout} disabled={!nodes.length}>
              <LayoutGrid size={13} />
              布局方案
            </button>
            <button onClick={() => save(nodes)} disabled={!nodes.length}>
              保存位置
            </button>
          </div>
        )}
      </div>
      <div className="graph-guidance">
        <span>
          {relation
            ? "实线：初始结构 · 紫色虚线：新增关联"
            : "内部步骤全部展开 · 拖动节点或整组流程，位置自动保存"}
        </span>
        {!relation && (
          <select
            aria-label="定位子流程"
            value=""
            onChange={(e) => locateGroup(e.target.value)}
          >
            <option value="">定位子流程…</option>
            {definitions.map((n) => (
              <option key={n.id} value={n.id}>
                {n.id} · {n.name}
              </option>
            ))}
          </select>
        )}
        {!relation && (
          <label className="follow-execution">
            <input
              type="checkbox"
              checked={followExecution}
              onChange={(e) => setFollowExecution(e.target.checked)}
            />
            跟随执行
          </label>
        )}
        {liveRun && !relation && (
          <button
            className="link"
            onClick={() => {
              if (expanded !== liveRun.stage) onExpand(liveRun.stage);
              onSelect(liveRun.node);
              const node = nodes.find((n) => n.id === liveRun.node);
              if (node)
                flow?.fitView({
                  nodes: [node],
                  maxZoom: 1.2,
                  padding: 0.5,
                  duration: 200,
                });
            }}
          >
            定位当前
          </button>
        )}
      </div>
      <div className="graph-viewport">
        {nodes.length === 0 && (
          <div className="graph-loading">
            <LoaderCircle size={18} className="spin" />
            正在排列流程…
          </div>
        )}
        <ReactFlow<StageNode, RoutedEdge>
          ariaLabelConfig={{
            "controls.zoomIn.ariaLabel": "放大",
            "controls.zoomOut.ariaLabel": "缩小",
            "controls.fitView.ariaLabel": "适应画布",
            "controls.ariaLabel": "画布控制",
            "minimap.ariaLabel": "缩略图",
          }}
          nodes={drawn}
          edges={edges}
          nodeTypes={nodeTypes}
          edgeTypes={edgeTypes}
          onInit={setFlow}
          onNodesChange={(changes) =>
            setNodes((old) => applyNodeChanges(changes, old))
          }
          onNodeDragStop={(_, moved) => {
            const next = nodeState.current.map((n) =>
              n.id === moved.id ? { ...n, position: moved.position } : n,
            );
            setNodes(next);
            setSettled(next);
            save(next);
          }}
          onNodeClick={(_, n) => {
            if (n.data.kind !== "end") onSelect(n.id);
            if (n.data.kind === "step" || n.data.kind === "chapter")
              setFocus(false);
          }}
          onNodeDoubleClick={(event, n) => {
            event.stopPropagation();
            if (n.data.kind === "stage") {
              onExpand(n.id);
              onSelect(n.id);
            }
          }}
          zoomOnDoubleClick={false}
          nodeDragThreshold={6}
          nodeClickDistance={6}
          minZoom={0.015}
          maxZoom={2}
          nodesDraggable={!relation}
          elevateNodesOnSelect={false}
          nodesConnectable={false}
          edgesReconnectable={false}
          deleteKeyCode={null}
          snapToGrid
          snapGrid={[12, 12]}
          fitView
          fitViewOptions={{ padding: 0.15, maxZoom: 1.1 }}
          proOptions={{ hideAttribution: false }}
        >
          <ExecutionFocus
            enabled={!relation && followExecution}
            node={liveRun?.node}
            runKey={`${liveRun?.id}:${liveRun?.cycle}`}
            status={liveRun?.status}
          />
          <Background color="var(--canvas-grid)" gap={24} size={1} />
          {showMap && (
            <MiniMap
              style={{ width: 116, height: 72 }}
              nodeColor={(n) =>
                n.data.status === "running" ? "#2b7c69" : "#c4d4c9"
              }
              maskColor="var(--canvas-mask)"
              pannable
              zoomable
            />
          )}
        </ReactFlow>
      </div>
      <div className="graph-zoom" aria-label="画布控制">
        <button
          aria-label="放大"
          onClick={() => flow?.zoomIn({ duration: 150 })}
        >
          ＋
        </button>
        <button
          aria-label="缩小"
          onClick={() => flow?.zoomOut({ duration: 150 })}
        >
          −
        </button>
        <button aria-label="适应画布" onClick={fit}>
          全景
        </button>
        <button
          aria-label="原始大小"
          onClick={() => flow?.zoomTo(1, { duration: 150 })}
        >
          1:1
        </button>
        <button aria-pressed={showMap} onClick={() => setShowMap(!showMap)}>
          缩略图
        </button>
        <span>
          {relation
            ? `${nodes.length} 章 · ${edges.length} 组关系`
            : "实线：推进 · 虚线：返回 / 修订"}
        </span>
      </div>
      <div className="graph-status" role="status">
        <span>{layoutError || (relation ? "只读章节关系图" : saved)}</span>
      </div>
      {layoutDialog && (
        <div className="modal-backdrop">
          <div
            className="modal graph-layout-dialog"
            role="dialog"
            aria-modal="true"
            aria-label="布局方案"
          >
            <div className="modal-header">
              <h2>布局方案</h2>
              <button
                className="icon-button"
                aria-label="关闭布局方案"
                onClick={() => setLayoutDialog(false)}
              >
                <X size={18} />
              </button>
            </div>
            <p>
              拖动后的排布会保存到工作台，并在浏览器保留备份。完整工作流在所有项目间共用，经纬图按项目保存。你可以导出给我，或粘贴布局重新采用。
            </p>
            <ContextReader compact />
            <label>
              当前图：{title}
              <textarea
                aria-label="布局内容"
                spellCheck={false}
                value={layoutText}
                onChange={(e) => setLayoutText(e.target.value)}
              />
            </label>
            {dialogError && (
              <p className="error-text" role="alert">
                {dialogError}
              </p>
            )}
            <div className="modal-footer">
              <button onClick={downloadLayout}>
                <Download size={15} />
                导出布局
              </button>
              <button className="primary" onClick={importLayout}>
                采用此布局
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
