// 重试沿用同一个幂等标识，断线重发不会重复创建作品、运行或检查点。
export async function api<T>(
  path: string,
  method = "GET",
  body?: unknown,
  revision?: number,
): Promise<T> {
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
  };
  if (method !== "GET") headers["Idempotency-Key"] = crypto.randomUUID();
  if (revision !== undefined) headers["If-Match"] = String(revision);
  const encoded = body === undefined ? undefined : JSON.stringify(body);
  for (let retry = 0; retry <= 3; retry++) {
    let res: Response;
    try {
      res = await fetch("/api" + path, { method, headers, body: encoded });
    } catch (error) {
      if (retry === 3)
        throw new Error("无法连接工作台，已重试 3 次。请检查后台是否运行。", {
          cause: error,
        });
      await delay(retry);
      continue;
    }
    if ([429, 502, 503, 504].includes(res.status) && retry < 3) {
      await res.body?.cancel();
      await delay(retry);
      continue;
    }
    let text: string;
    try {
      text = await res.text();
    } catch (error) {
      if (retry === 3)
        throw new Error("工作台响应中断，已重试 3 次。", { cause: error });
      await delay(retry);
      continue;
    }
    let result: { error?: string };
    try {
      result = JSON.parse(text);
    } catch {
      throw new Error(
        `工作台返回了无法识别的响应（${res.status}），请确认已启动新版后台。`,
      );
    }
    if (!res.ok) throw new Error(result.error || `请求失败 ${res.status}`);
    return result as T;
  }
  throw new Error("请求重试已耗尽");
}
const delay = (retry: number) =>
  new Promise((resolve) => setTimeout(resolve, 500 * 2 ** retry));
