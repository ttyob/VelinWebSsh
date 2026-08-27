export const terminalOutputSettleDelay = 5_000;

export type TerminalAttentionEvent = "bell" | "settled" | "clear";
export type TerminalAttentionNotice = Exclude<TerminalAttentionEvent, "clear">;
export type TabAttentionKind =
  | "required"
  | "bell"
  | "ended"
  | "settled";

export interface TerminalAttentionInput {
  name: string;
  status: string;
  notice?: TerminalAttentionNotice;
}

export interface TabAttention {
  kind: TabAttentionKind;
  label: string;
  count: number;
}

interface Candidate {
  kind: TabAttentionKind;
  label: string;
  priority: number;
}

function candidateFor(input: TerminalAttentionInput): Candidate | undefined {
  const prefix = input.name ? `${input.name}：` : "";
  if (input.status === "auth_required")
    return { kind: "required", label: `${prefix}等待 SSH 认证`, priority: 50 };
  if (input.status === "host_key_required")
    return { kind: "required", label: `${prefix}等待确认主机指纹`, priority: 50 };
  if (input.status === "ownership_error")
    return { kind: "required", label: `${prefix}终端所有权异常`, priority: 50 };
  if (input.status === "unreachable")
    return { kind: "required", label: `${prefix}连接不可达`, priority: 50 };
  if (input.notice === "bell")
    return {
      kind: "bell",
      label: `${prefix}终端响铃，可能需要响应`,
      priority: 40,
    };
  if (input.status === "ended")
    return { kind: "ended", label: `${prefix}会话已结束`, priority: 30 };
  if (input.notice === "settled")
    return {
      kind: "settled",
      label: `${prefix}输出已停止，等待查看`,
      priority: 20,
    };
  return undefined;
}

export function resolveTabAttention(
  sessions: TerminalAttentionInput[],
): TabAttention | undefined {
  const candidates = sessions
    .map(candidateFor)
    .filter((item): item is Candidate => Boolean(item))
    .sort((left, right) => right.priority - left.priority);
  const first = candidates[0];
  if (!first) return undefined;
  return {
    kind: first.kind,
    label:
      candidates.length > 1
        ? `${first.label}；标签内另有 ${candidates.length - 1} 个提醒`
        : first.label,
    count: candidates.length,
  };
}
