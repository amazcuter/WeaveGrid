// 展示的就是版本原文；仅排版保留段落，不另造审核内容或执行模型返回的HTML。
export function OutlineText({ text }: { text: string }) {
  const marker = /^##\s*伏笔提示\s*$/m;
  const match = marker.exec(text);
  if (!match) return <div className="reading-text">{text}</div>;
  const body = text
    .slice(0, match.index)
    .replace(/^\s*##\s*事件要点\s*\n/, "")
    .trim();
  const hints = text
    .slice(match.index + match[0].length)
    .trim()
    .split(/\n+/)
    .filter(Boolean);
  return (
    <div className="outline-document">
      <h4>事件要点</h4>
      <div className="reading-text">{body}</div>
      <div className="outline-hints">
        <h4>伏笔提示</h4>
        {hints.map((line, index) => {
          const hint = /^[-*]\s*(.*?[）)])\s*[：:]\s*(.*)$/.exec(line.trim());
          return (
            <div className="outline-hint" key={index}>
              {hint ? (
                <>
                  <strong>{hint[1]}</strong>
                  <p>{hint[2]}</p>
                </>
              ) : (
                <p>{line.replace(/^[-*]\s*/, "")}</p>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}
