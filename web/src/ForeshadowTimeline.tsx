import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ArrowLeftRight,
  ChevronLeft,
  ChevronRight,
  Crosshair,
  Maximize2,
  Minimize2,
  Waypoints,
  X,
} from "lucide-react";
import type { Project, Run } from "./types";
import {
  buildTimeline,
  chapterX,
  connectionGeometry,
  TIMELINE_PADDING,
  visibleChapterRange,
} from "./foreshadow-timeline";
import "./ForeshadowTimeline.css";

const TOP = 128;
const GUTTER = 112;

export default function ForeshadowTimeline({
  project,
  run,
  chapter,
  onSelect,
  focus,
  setFocus,
}: {
  project: Project;
  run?: Run;
  chapter: number;
  onSelect: (id: string) => void;
  focus: boolean;
  setFocus: (value: boolean) => void;
}) {
  const model = useMemo(() => buildTimeline(project), [project]);
  const total = model.chapters.length;
  const viewport = useRef<HTMLDivElement>(null);
  const drag = useRef<{ x: number; scroll: number; moved: boolean } | null>(
    null,
  );
  const [spacing, setSpacing] = useState(56);
  const [size, setSize] = useState({ width: 800, height: 490 });
  const [scroll, setScroll] = useState(0);
  const [hovered, setHovered] = useState<string | null>(null);
  const [pinned, setPinned] = useState<string | null>(null);
  const [relatedOnly, setRelatedOnly] = useState(false);
  const [showLinks, setShowLinks] = useState(true);
  const height = Math.max(260, size.height - 2);
  const bottom = height - 64;
  const width = Math.max(
    size.width - GUTTER,
    total * spacing + TIMELINE_PADDING * 2,
  );
  const range = visibleChapterRange(
    scroll,
    size.width - GUTTER,
    spacing,
    total,
  );
  const selected = model.chapters[chapter - 1];
  const active = model.connections.find(
    (line) => line.id === (hovered || pinned),
  );
  const liveChapter =
    run && !["completed", "stopped"].includes(run.status) ? run.chapter : 0;
  const relevant = (line: (typeof model.connections)[number]) =>
    line.plant === chapter ||
    line.payoff === chapter ||
    line.reminds.includes(chapter);
  const lines = useMemo(
    () =>
      model.connections.map((line) => ({
        ...line,
        geometry: connectionGeometry(line, spacing, TOP, bottom),
      })),
    [model, spacing, bottom],
  );

  useEffect(() => {
    const element = viewport.current;
    if (!element) return;
    const measure = () =>
      setSize({ width: element.clientWidth, height: element.clientHeight });
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    measure();
    // 普通鼠标滚轮也可沿章节横向浏览；保留触控板原生横向滚动。
    const wheel = (event: WheelEvent) => {
      if (
        event.ctrlKey ||
        event.metaKey ||
        Math.abs(event.deltaX) >= Math.abs(event.deltaY) ||
        element.scrollWidth <= element.clientWidth
      )
        return;
      // 矮窗口仍允许向下查看图中内容，Shift + 滚轮始终横向移动。
      if (element.scrollHeight > element.clientHeight + 20 && !event.shiftKey)
        return;
      const delta =
        event.deltaY *
        (event.deltaMode === 1
          ? 18
          : event.deltaMode === 2
            ? element.clientWidth
            : 1);
      event.preventDefault();
      element.scrollLeft += delta;
    };
    element.addEventListener("wheel", wheel, { passive: false });
    return () => {
      observer.disconnect();
      element.removeEventListener("wheel", wheel);
    };
  }, []);

  const locate = useCallback(
    (number: number, behavior: ScrollBehavior = "smooth") => {
      const element = viewport.current;
      if (element)
        element.scrollTo({
          left: chapterX(number, spacing) - (element.clientWidth - GUTTER) / 2,
          behavior,
        });
    },
    [spacing],
  );
  useEffect(() => {
    locate(chapter);
  }, [chapter, locate]);
  useEffect(() => {
    if (!focus) return;
    const escape = (event: KeyboardEvent) => {
      if (event.key === "Escape") setFocus(false);
    };
    window.addEventListener("keydown", escape);
    return () => window.removeEventListener("keydown", escape);
  }, [focus, setFocus]);

  function selectChapter(number: number) {
    setPinned(null);
    onSelect(String(Math.max(1, Math.min(total, number))));
  }
  const chapterLabel = (number: number) => {
    const ch = model.chapters[number - 1];
    return `第 ${number} 章，大纲${ch.outline ? "已完成" : "未完成"}，正文${ch.draft ? "已写完" : "未写完"}，设定 ${ch.plants} 条，收回 ${ch.payoffs} 条，提醒 ${ch.reminders} 处`;
  };
  const lineLabel = (line: (typeof model.connections)[number]) =>
    line.added
      ? `新增关联：第 ${line.plant} 章 → 第 ${line.payoff} 章 · ${line.label}`
      : `${line.label}：第 ${line.plant} 章设定 → 第 ${line.payoff} 章收回${line.reminds.length ? ` · 第 ${line.reminds.join("、")} 章提醒` : ""}`;
  const pageScroll = (direction: number) =>
    viewport.current?.scrollBy({
      left: direction * size.width * 0.75,
      behavior: "smooth",
    });

  return (
    <section
      className={`graph-shell foreshadow-timeline ${focus ? "graph-focused" : ""}`}
      aria-label="章节经纬图"
    >
      <div className="graph-toolbar">
        <div className="graph-title">
          <Waypoints size={18} />
          <strong>章节经纬图</strong>
          <span className="timeline-total">
            {total} 章 · {model.threadCount} 条伏笔
          </span>
        </div>
        <div className="graph-tools">
          <label className="timeline-spacing">
            章节间距{" "}
            <select
              aria-label="章节间距"
              value={spacing}
              onChange={(event) => setSpacing(Number(event.target.value))}
            >
              <option value={36}>紧凑</option>
              <option value={56}>标准</option>
              <option value={84}>宽松</option>
            </select>
          </label>
          <button onClick={() => locate(chapter)}>
            <Crosshair size={14} />
            定位选中章
          </button>
          <button
            className="icon-button"
            aria-label={focus ? "退出专注画布" : "专注画布"}
            onClick={() => setFocus(!focus)}
          >
            {focus ? <Minimize2 size={16} /> : <Maximize2 size={16} />}
          </button>
        </div>
      </div>
      <div className="timeline-summary">
        <span>
          <i className="timeline-swatch outline" />
          大纲已完成 <b>{model.outlineCount}</b>
          <small>/ {total} 章</small>
        </span>
        <span>
          <i className="timeline-swatch draft" />
          正文已写完 <b>{model.draftCount}</b>
          <small>/ {total} 章</small>
        </span>
        <span className="timeline-summary-note">色带逐章标记实际进度</span>
      </div>
      <div className="timeline-legend">
        <span>
          <i className="timeline-swatch thread" />
          伏笔连线
        </span>
        <span>
          <i className="timeline-swatch turning" />
          转折章
        </span>
        <span>
          <i className="timeline-swatch reminder" />
          途中提醒
        </span>
        <span>柱越高，该章设定 / 收回越密集</span>
        <div className="timeline-filters">
          <label>
            <input
              type="checkbox"
              checked={relatedOnly}
              onChange={(event) => {
                setRelatedOnly(event.target.checked);
                setPinned(null);
                setHovered(null);
              }}
            />
            只看选中章
          </label>
          {model.connections.some((line) => line.added) && (
            <label>
              <input
                type="checkbox"
                checked={showLinks}
                onChange={(event) => {
                  setShowLinks(event.target.checked);
                  setPinned(null);
                }}
              />
              新增关联（虚线）
            </label>
          )}
        </div>
      </div>
      <div className="timeline-chart">
        <div
          className="timeline-scroll"
          ref={viewport}
          role="region"
          aria-label="伏笔双轴时间线，可左右滚动"
          tabIndex={0}
          onScroll={(event) => setScroll(event.currentTarget.scrollLeft)}
          onKeyDown={(event) => {
            if (event.target !== event.currentTarget) return;
            if (event.key === "ArrowRight" || event.key === "ArrowLeft") {
              event.preventDefault();
              pageScroll(event.key === "ArrowRight" ? 1 : -1);
            }
            if (event.key === "Home" || event.key === "End") {
              event.preventDefault();
              locate(event.key === "Home" ? 1 : total);
            }
          }}
          onPointerDown={(event) => {
            if (event.button !== 0 || event.pointerType !== "mouse") return;
            drag.current = {
              x: event.clientX,
              scroll: event.currentTarget.scrollLeft,
              moved: false,
            };
          }}
          onPointerMove={(event) => {
            const state = drag.current;
            if (!state || event.buttons !== 1) return;
            if (Math.abs(event.clientX - state.x) > 5) {
              state.moved = true;
              event.currentTarget.setPointerCapture(event.pointerId);
              event.currentTarget.scrollLeft =
                state.scroll + state.x - event.clientX;
              setHovered(null);
            }
          }}
          onPointerUp={(event) => {
            if (event.currentTarget.hasPointerCapture(event.pointerId))
              event.currentTarget.releasePointerCapture(event.pointerId);
          }}
          onPointerCancel={() => {
            drag.current = null;
          }}
          onClickCapture={(event) => {
            if (drag.current?.moved) {
              event.preventDefault();
              event.stopPropagation();
            }
            drag.current = null;
          }}
          onPointerLeave={() => {
            if (!drag.current?.moved) drag.current = null;
            setHovered(null);
          }}
        >
          <div
            className="timeline-sheet"
            style={{ width: width + GUTTER, height }}
          >
            <div
              className="timeline-axis-labels"
              style={{ width: GUTTER, height }}
              aria-hidden="true"
            >
              <span style={{ top: 10 }}>剧情单元</span>
              <span style={{ top: 28 }}>
                <i className="timeline-swatch outline" />
                大纲完成
              </span>
              <span style={{ top: 44 }}>
                <i className="timeline-swatch draft" />
                正文写完
              </span>
              <strong className="plant-label" style={{ top: TOP - 10 }}>
                伏笔设定轴<small>由此埋下伏笔</small>
              </strong>
              <ArrowLeftRight
                className="timeline-direction"
                size={20}
                style={{ top: (TOP + bottom) / 2 }}
              />
              <strong className="payoff-label" style={{ top: bottom - 10 }}>
                伏笔收回轴<small>在此回应伏笔</small>
              </strong>
            </div>
            <svg
              className="timeline-svg"
              width={width}
              height={height}
              aria-label="上轴设定，下轴收回，每章一个刻度"
            >
              <g aria-hidden="true">
                {project.structure.arcs.map(([start, end], index) => (
                  <g key={`${start}-${end}`}>
                    <rect
                      x={chapterX(start, spacing) - spacing / 2}
                      y={8}
                      width={(end - start + 1) * spacing - 2}
                      height={18}
                      rx={4}
                      className={`timeline-arc arc-${index % 2}`}
                    />
                    <text
                      x={Math.max(
                        chapterX(start, spacing) - spacing / 2 + 12,
                        Math.min(scroll + 14, chapterX(end, spacing) - 65),
                      )}
                      y={21}
                      className="timeline-arc-name"
                    >
                      单元 {index + 1}
                    </text>
                  </g>
                ))}
                {model.chapters
                  .slice(
                    Math.max(0, range.first - 3),
                    Math.min(total, range.last + 3),
                  )
                  .map((ch) => {
                    const x = chapterX(ch.number, spacing);
                    return (
                      <g key={ch.number}>
                        <line
                          x1={x}
                          x2={x}
                          y1={66}
                          y2={bottom + 58}
                          className="timeline-gridline"
                        />
                        <rect
                          x={x - spacing / 2 + 2}
                          y={34}
                          width={spacing - 4}
                          height={10}
                          rx={3}
                          className={`timeline-progress ${ch.outline ? "outline" : "pending"}`}
                        >
                          <title>
                            第 {ch.number} 章大纲
                            {ch.outline ? "已完成" : "未完成"}
                          </title>
                        </rect>
                        <rect
                          x={x - spacing / 2 + 2}
                          y={50}
                          width={spacing - 4}
                          height={10}
                          rx={3}
                          className={`timeline-progress ${ch.draft ? "draft" : "pending"}`}
                        >
                          <title>
                            第 {ch.number} 章正文
                            {ch.draft ? "已写完" : "未写完"}
                          </title>
                        </rect>
                        {ch.plants > 0 && (
                          <rect
                            x={x - 7}
                            y={TOP - 8 - (20 * ch.plants) / model.maxDensity}
                            width={14}
                            height={(20 * ch.plants) / model.maxDensity}
                            rx={2}
                            className="timeline-density plant"
                          />
                        )}
                        {ch.payoffs > 0 && (
                          <rect
                            x={x - 7}
                            y={bottom + 8}
                            width={14}
                            height={(20 * ch.payoffs) / model.maxDensity}
                            rx={2}
                            className="timeline-density payoff"
                          />
                        )}
                        <text
                          x={x}
                          y={TOP - 37}
                          className="timeline-count plant"
                        >
                          {ch.plants || "·"}
                        </text>
                        <text
                          x={x}
                          y={bottom + 42}
                          className="timeline-count payoff"
                        >
                          {ch.payoffs || "·"}
                        </text>
                      </g>
                    );
                  })}
                <line
                  x1={TIMELINE_PADDING}
                  x2={width - TIMELINE_PADDING}
                  y1={TOP}
                  y2={TOP}
                  className="timeline-axis plant"
                />
                <line
                  x1={TIMELINE_PADDING}
                  x2={width - TIMELINE_PADDING}
                  y1={bottom}
                  y2={bottom}
                  className="timeline-axis payoff"
                />
                {selected && (
                  <rect
                    x={chapterX(chapter, spacing) - spacing / 2 + 1}
                    y={65}
                    width={spacing - 2}
                    height={bottom - 4}
                    rx={6}
                    className="timeline-selection"
                  />
                )}
                {liveChapter > 0 && (
                  <g>
                    <line
                      x1={chapterX(liveChapter, spacing)}
                      x2={chapterX(liveChapter, spacing)}
                      y1={65}
                      y2={bottom + 61}
                      className="timeline-live"
                    />
                    <text
                      x={chapterX(liveChapter, spacing) + 6}
                      y={bottom - 16}
                      className="timeline-live-label"
                    >
                      当前执行
                    </text>
                  </g>
                )}
              </g>
              <g className="timeline-connections">
                {lines
                  .filter(
                    (line) =>
                      (showLinks || !line.added) &&
                      (!relatedOnly || relevant(line)) &&
                      Math.max(line.plant, line.payoff) >= range.first - 2 &&
                      Math.min(line.plant, line.payoff) <= range.last + 2,
                  )
                  .sort(
                    (a, b) =>
                      Number(a.id === active?.id) - Number(b.id === active?.id),
                  )
                  .map((line) => (
                    <g
                      key={line.id}
                      role="button"
                      tabIndex={0}
                      aria-label={lineLabel(line)}
                      aria-pressed={pinned === line.id}
                      className={`timeline-connection ${line.added ? "added" : ""} ${line.id === active?.id ? "active" : ""} ${active && line.id !== active.id ? "dimmed" : ""}`}
                      onPointerEnter={() => {
                        if (!drag.current?.moved) setHovered(line.id);
                      }}
                      onPointerLeave={() => setHovered(null)}
                      onClick={() =>
                        setPinned(pinned === line.id ? null : line.id)
                      }
                      onFocus={() => setHovered(line.id)}
                      onBlur={() => setHovered(null)}
                      onKeyDown={(event) => {
                        if (event.key === "Enter" || event.key === " ") {
                          event.preventDefault();
                          setPinned(pinned === line.id ? null : line.id);
                        }
                      }}
                    >
                      <title>{lineLabel(line)}</title>
                      <path
                        d={line.geometry.path}
                        className="timeline-line-hit"
                      />
                      <path d={line.geometry.path} className="timeline-line" />
                      <circle
                        cx={line.geometry.x1}
                        cy={TOP}
                        r={2.6}
                        className="timeline-endpoint"
                      />
                      <circle
                        cx={line.geometry.x2}
                        cy={bottom}
                        r={2.6}
                        className="timeline-endpoint"
                      />
                      {line.geometry.reminders.map((point) => (
                        <circle
                          key={point.chapter}
                          cx={point.x}
                          cy={point.y}
                          r={3.6}
                          className="timeline-reminder"
                        >
                          <title>
                            {line.label} · 第 {point.chapter} 章提醒
                          </title>
                        </circle>
                      ))}
                    </g>
                  ))}
              </g>
              {model.chapters
                .slice(
                  Math.max(0, range.first - 3),
                  Math.min(total, range.last + 3),
                )
                .map((ch) => {
                  const x = chapterX(ch.number, spacing);
                  return (
                    <g
                      key={ch.number}
                      className={`timeline-chapter ${ch.turning ? "turning" : ""} ${chapter === ch.number ? "selected" : ""}`}
                      role="button"
                      tabIndex={0}
                      aria-label={chapterLabel(ch.number)}
                      aria-pressed={chapter === ch.number}
                      onClick={() => selectChapter(ch.number)}
                      onKeyDown={(event) => {
                        if (event.key === "Enter" || event.key === " ") {
                          event.preventDefault();
                          selectChapter(ch.number);
                        }
                      }}
                    >
                      <title>
                        {chapterLabel(ch.number)}
                        {ch.turning ? ` · 转折：${ch.turning}` : ""}
                      </title>
                      <rect
                        x={x - spacing / 2 + 1}
                        y={65}
                        width={spacing - 2}
                        height={TOP - 56}
                        className="timeline-tick-hit"
                        rx={4}
                      />
                      <rect
                        x={x - spacing / 2 + 1}
                        y={bottom - 9}
                        width={spacing - 2}
                        height={70}
                        className="timeline-tick-hit"
                        rx={4}
                      />
                      <text
                        x={x}
                        y={TOP - 56}
                        className="timeline-chapter-number"
                      >
                        {ch.number}
                      </text>
                      <text
                        x={x}
                        y={bottom + 58}
                        className="timeline-chapter-number"
                      >
                        {ch.number}
                      </text>
                      <line
                        x1={x}
                        x2={x}
                        y1={TOP - 6}
                        y2={TOP + 6}
                        className="timeline-tick"
                      />
                      <line
                        x1={x}
                        x2={x}
                        y1={bottom - 6}
                        y2={bottom + 6}
                        className="timeline-tick"
                      />
                    </g>
                  );
                })}
              {model.threadCount === 0 && (
                <text
                  x={scroll + (size.width - GUTTER) / 2}
                  y={(TOP + bottom) / 2}
                  className="timeline-empty"
                >
                  暂无伏笔，章节进度仍会显示在上方
                </text>
              )}
            </svg>
          </div>
        </div>
      </div>
      <div className="timeline-detail" aria-live="polite">
        {active ? (
          <>
            <span className="timeline-detail-label">
              {active.added ? "新增关联" : "伏笔"}
            </span>
            <span className="timeline-detail-text">{lineLabel(active)}</span>
            <button
              className="link"
              onClick={() => {
                onSelect(String(active.plant));
                locate(active.plant);
              }}
            >
              定位起点
            </button>
            <button
              className="link"
              onClick={() => {
                onSelect(String(active.payoff));
                locate(active.payoff);
              }}
            >
              定位终点
            </button>
            {pinned && (
              <button
                className="icon-button"
                aria-label="取消选中伏笔"
                onClick={() => {
                  setPinned(null);
                  setHovered(null);
                }}
              >
                <X size={14} />
              </button>
            )}
          </>
        ) : selected ? (
          <>
            <strong>第 {chapter} 章</strong>
            <span>
              大纲{selected.outline ? "已完成" : "未完成"} · 正文
              {selected.draft ? "已写完" : "未写完"}
            </span>
            <span className="timeline-detail-counts">
              设定 <b>{selected.plants}</b> · 收回 <b>{selected.payoffs}</b> ·
              提醒 <b>{selected.reminders}</b>
            </span>
            <small>点击刻度选章，悬停连线查看呼应</small>
          </>
        ) : (
          <span>点击章节刻度查看进度与伏笔数量</span>
        )}
      </div>
      <div className="timeline-overview-row">
        <div className="timeline-overview-label">
          <strong>全书密度</strong>
          <small>设定 + 收回 + 提醒</small>
        </div>
        <svg
          className="timeline-overview"
          viewBox={`0 0 ${Math.max(1, total)} 40`}
          preserveAspectRatio="none"
          role="slider"
          tabIndex={0}
          aria-label="全书伏笔密度导航"
          aria-valuemin={1}
          aria-valuemax={total}
          aria-valuenow={chapter}
          aria-valuetext={`第 ${chapter} 章`}
          onClick={(event) => {
            const box = event.currentTarget.getBoundingClientRect();
            const ch =
              Math.floor(((event.clientX - box.left) / box.width) * total) + 1;
            selectChapter(ch);
          }}
          onKeyDown={(event) => {
            if (
              ["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)
            ) {
              event.preventDefault();
              selectChapter(
                event.key === "Home"
                  ? 1
                  : event.key === "End"
                    ? total
                    : chapter + (event.key === "ArrowLeft" ? -1 : 1),
              );
            }
          }}
        >
          {model.chapters.map((ch) => {
            const activity = ch.plants + ch.payoffs + ch.reminders;
            return (
              <g key={ch.number}>
                <rect
                  x={ch.number - 1 + 0.12}
                  y={30 - (26 * activity) / model.maxActivity}
                  width={0.76}
                  height={Math.max(1, (26 * activity) / model.maxActivity)}
                  className="timeline-overview-bar"
                />
                <rect
                  x={ch.number - 1 + 0.12}
                  y={33}
                  width={0.76}
                  height={3}
                  className={`timeline-progress ${ch.outline ? "outline" : "pending"}`}
                />
                <rect
                  x={ch.number - 1 + 0.12}
                  y={38}
                  width={0.76}
                  height={2}
                  className={`timeline-progress ${ch.draft ? "draft" : "pending"}`}
                />
                <title>{chapterLabel(ch.number)}</title>
              </g>
            );
          })}
          <rect
            x={range.first - 1}
            y={0.8}
            width={Math.max(1, range.last - range.first + 1)}
            height={38.4}
            className="timeline-overview-window"
          />
          <line
            x1={chapter - 0.5}
            x2={chapter - 0.5}
            y1={1}
            y2={39}
            className="timeline-overview-current"
          />
        </svg>
        <span className="timeline-overview-end">{total} 章</span>
      </div>
      <div className="timeline-footer">
        <span>
          <ArrowLeftRight size={13} />
          拖动、滚轮或触控板左右浏览 · 点击全书密度快速定位
        </span>
        <div>
          <button
            className="icon-button"
            aria-label="向前浏览章节"
            disabled={scroll <= 0}
            onClick={() => pageScroll(-1)}
          >
            <ChevronLeft size={16} />
          </button>
          <span>
            第 {range.first}–{range.last} 章 / 共 {total} 章
          </span>
          <button
            className="icon-button"
            aria-label="向后浏览章节"
            disabled={scroll >= width + GUTTER - size.width - 1}
            onClick={() => pageScroll(1)}
          >
            <ChevronRight size={16} />
          </button>
        </div>
      </div>
    </section>
  );
}
