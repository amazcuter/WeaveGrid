import { createContext, useContext, useState } from "react";
import { OutlineText } from "./OutlineText";
import { current, labels, type Project, type Run, type Attempt } from "./types";

export const CreativeContext = createContext<{
  p?: Project;
  run?: Run;
  chapter: number;
  attempts: Attempt[];
}>({ chapter: 1, attempts: [] });

// 上下文阅读位置独立于表单：翻阅前文不会切换审核对象，也不会重置正在输入的草稿。
export function ContextReader({
  initialChapter,
  compact = false,
}: {
  initialChapter?: number;
  compact?: boolean;
}) {
  const { p, run, chapter, attempts } = useContext(CreativeContext);
  const focus = initialChapter || chapter;
  const [selected, setSelected] = useState(focus);
  const [tab, setTab] = useState("chapter");
  if (!p)
    return (
      <aside className="context-reader empty-context">
        新作品尚无章节内容。采用结构后，这里可以随时查看本章、前后章节和参考资料。
      </aside>
    );
  const ch = Math.min(Math.max(selected, 1), p.chapters.length);
  const ideas = p.ideas.filter((i) => i.chapter === 0 || i.chapter === ch);
  const materials = (run?.data.materials || []).filter((c) => c.chapter === ch);
  const candidates = (run?.data.batch || []).filter((c) => c.chapter === ch);
  const latest = attempts.filter((a) => a.chapter === ch).at(-1);
  const versions = [current(p, ch, "outline"), current(p, ch, "draft")];
  const view = (
    <aside className="context-reader" aria-label="创作上下文">
      <header>
        <strong>创作上下文</strong>
        <small>
          {p.name} · {run ? labels[run.mode] : "作品资料"}
        </small>
      </header>
      <div className="context-navigation">
        <button
          aria-label="上下文上一章"
          disabled={ch <= 1}
          onClick={() => setSelected(ch - 1)}
        >
          上一章
        </button>
        <select
          aria-label="上下文章节"
          value={ch}
          onChange={(e) => setSelected(Number(e.target.value))}
        >
          {p.chapters.map((c) => (
            <option key={c.number} value={c.number}>
              第 {c.number} 章{c.number === focus ? " · 本章" : ""}
            </option>
          ))}
        </select>
        <button
          aria-label="上下文下一章"
          disabled={ch >= p.chapters.length}
          onClick={() => setSelected(ch + 1)}
        >
          下一章
        </button>
        <button onClick={() => setSelected(focus)}>回到本章</button>
      </div>
      <nav className="context-tabs" aria-label="上下文分类">
        {[
          ["chapter", "大纲与正文"],
          ["ideas", "人工创意"],
          ["materials", "资料与引用"],
          ["structure", "结构与关联"],
          ["candidate", "候选与问题"],
        ].map(([id, name]) => (
          <button key={id} aria-pressed={tab === id} onClick={() => setTab(id)}>
            {name}
          </button>
        ))}
      </nav>
      <div className="context-content">
        {tab === "chapter" &&
          versions.map((v, i) => (
            <section key={i}>
              <h3>
                第 {ch} 章 · {i === 0 ? "大纲（计划）" : "正文"}
              </h3>
              {v ? (
                <>
                  <small>当前采用版本 · {v.id.slice(0, 8)}</small>
                  {v.kind === "outline" ? (
                    <OutlineText text={v.text} />
                  ) : (
                    <pre>{v.text}</pre>
                  )}
                </>
              ) : (
                <p className="muted">尚未写入{i === 0 ? "大纲" : "正文"}。</p>
              )}
            </section>
          ))}
        {tab === "ideas" && (
          <>
            <h3>本章与全书创意</h3>
            {ideas.length ? (
              ideas.map((i) => (
                <section key={i.id}>
                  <small>
                    {i.chapter === 0 ? "全书" : "本章"} ·{" "}
                    {i.required ? "必留要求" : "可选想法"} ·{" "}
                    {i.active ? "启用" : "停用"}
                  </small>
                  <pre>{i.text}</pre>
                </section>
              ))
            ) : (
              <p className="muted">尚未填写创意。</p>
            )}
          </>
        )}
        {tab === "materials" && (
          <>
            <h3>本章检索引用</h3>
            {materials.length ? (
              materials.map((c, i) => (
                <section key={i}>
                  <small>
                    {labels[c.kind]} · 版本 {c.version.slice(0, 8)} · 第{" "}
                    {c.paragraph} 段
                    {p.versions.some((v) => v.id === c.version) &&
                    current(p, ch, c.kind)?.id !== c.version
                      ? " · 历史版本"
                      : ""}
                  </small>
                  <pre>{c.text}</pre>
                </section>
              ))
            ) : (
              <p className="muted">本次运行尚无本章检索引用。</p>
            )}
            <h3>作品参考资料</h3>
            {p.references.length ? (
              p.references.map((ref) => (
                <section key={ref.id}>
                  <h4>{ref.title}</h4>
                  <pre>{ref.text}</pre>
                </section>
              ))
            ) : (
              <p className="muted">尚未添加资料。</p>
            )}
          </>
        )}
        {tab === "structure" && (
          <>
            <h3>第 {ch} 章的结构位置</h3>
            {p.structure.arcs
              .filter(([a, b]) => a <= ch && ch <= b)
              .map(([a, b]) => (
                <p key={a}>
                  剧情单元：第 {a}–{b} 章
                </p>
              ))}
            {p.structure.nodes
              .filter((n) => n.ch === ch)
              .map((n, i) => (
                <p key={i}>转折：{n.kind}</p>
              ))}
            <h3>初始结构连线</h3>
            {p.structure.lines
              .filter(([a, b]) => a === ch || b === ch)
              .map(([a, b]) => (
                <p key={`${a}-${b}`}>
                  第 {a} 章 → 第 {b} 章
                </p>
              ))}
            <h3>伏笔与提醒</h3>
            {p.structure.threads
              .filter(
                (t) =>
                  t.plant === ch || t.payoff === ch || t.reminds?.includes(ch),
              )
              .map((t) => (
                <section key={t.id}>
                  <p>
                    {t.id}：{t.plant} → {t.payoff}；提醒：
                    {t.reminds?.join("、") || "无"}
                  </p>
                  <div className="button-row">
                    <button
                      onClick={() => {
                        setSelected(t.plant);
                        setTab("chapter");
                      }}
                    >
                      看第 {t.plant} 章大纲
                    </button>
                    <button
                      onClick={() => {
                        setSelected(t.payoff);
                        setTab("chapter");
                      }}
                    >
                      看第 {t.payoff} 章大纲
                    </button>
                  </div>
                </section>
              ))}
            <h3>创作中发现的关联</h3>
            {p.links
              .filter((l) => l.valid && (l.from === ch || l.to === ch))
              .map((l) => (
                <section key={l.id}>
                  <small>
                    第 {l.from} 章 → 第 {l.to} 章
                  </small>
                  <pre>{l.reason}</pre>
                </section>
              ))}
          </>
        )}
        {tab === "candidate" && (
          <>
            <h3>本次候选（尚未采用）</h3>
            {candidates.length ? (
              candidates.map((c, i) => (
                <section key={i}>
                  <small>
                    {labels[c.kind]} · {c.reason}
                  </small>
                  {c.kind === "outline" ? (
                    <OutlineText text={c.text} />
                  ) : (
                    <pre>{c.text}</pre>
                  )}
                </section>
              ))
            ) : (
              <p className="muted">暂无本章候选。</p>
            )}
            {latest && (
              <details>
                <summary>
                  最近执行输出 · {latest.node} · {labels[latest.status]}
                </summary>
                <pre>{latest.output || latest.error || "暂无输出"}</pre>
              </details>
            )}
            {run?.data.proposals
              ?.filter(
                (q) =>
                  !["stopped", "completed"].includes(run.status) &&
                  q.chapter === ch &&
                  q.decision === "pending",
              )
              .map((q) => (
                <section key={q.id}>
                  <h4>{q.problem}</h4>
                  <pre>{q.instruction}</pre>
                  {q.evidence?.map((c, i) => (
                    <blockquote key={i}>
                      第 {c.chapter} 章 · {c.version.slice(0, 8)}
                      <pre>{c.text}</pre>
                    </blockquote>
                  ))}
                </section>
              ))}
          </>
        )}
      </div>
    </aside>
  );
  return compact ? (
    <details className="compact-context">
      <summary>查看创作上下文 · 本章与前后章节、创意和资料</summary>
      {view}
    </details>
  ) : (
    view
  );
}
