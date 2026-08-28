export type MonitorCounters = {
  collectedAt: number;
  cpuTotal: number;
  cpuIdle: number;
  cpuCores: number;
  receivedBytes: number;
  sentBytes: number;
};

export type SSHLoginRecord = {
  time: string;
  status: "success" | "failed";
  user: string;
  address: string;
  method: string;
  message: string;
};

export type SSHMonitor = {
  source: string;
  available: boolean;
  successful: number;
  failed: number;
  activeSessions: number;
  records: SSHLoginRecord[];
};

export const monitorCountersCommand = `LC_ALL=C
cores=$(getconf _NPROCESSORS_ONLN 2>/dev/null || nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || printf 0)
printf 'cores\t%s\n' "$cores"
if [ -r /proc/stat ]; then
  awk '/^cpu / {total=0; for (i=2; i<=NF; i++) total+=$i; idle=$5+$6; printf "cpu\\t%.0f\\t%.0f\\n", total, idle; exit}' /proc/stat
else
  printf 'cpu\t0\t0\n'
fi
if [ -r /proc/net/dev ]; then
  awk 'NR>2 {name=$1; sub(/:$/, "", name); if (name != "lo") {rx+=$2; tx+=$10}} END {printf "network\\t%.0f\\t%.0f\\n", rx, tx}' /proc/net/dev
else
  printf 'network\t0\t0\n'
fi`;

export const sshMonitorCommand = `LC_ALL=C
logs=''
source_name=''
if command -v journalctl >/dev/null 2>&1; then
  logs=$(journalctl --no-pager --since '-24 hours' -n 1000 -o short-unix -u ssh.service -u sshd.service 2>/dev/null || true)
  if [ -n "$logs" ]; then source_name='journalctl'; fi
fi
if [ -z "$logs" ] && [ -r /var/log/auth.log ]; then
  logs=$(tail -n 1000 /var/log/auth.log 2>/dev/null || true)
  if [ -n "$logs" ]; then source_name='/var/log/auth.log'; fi
fi
if [ -z "$logs" ] && [ -r /var/log/secure ]; then
  logs=$(tail -n 1000 /var/log/secure 2>/dev/null || true)
  if [ -n "$logs" ]; then source_name='/var/log/secure'; fi
fi
printf '__SOURCE__\t%s\n' "$source_name"
printf '%s\n' "$logs"
printf '__ACTIVE__\n'
who 2>/dev/null || true`;

export function parseMonitorCounters(output: string, collectedAt = Date.now()): MonitorCounters {
  const values = new Map<string, string[]>();
  for (const line of output.split(/\r?\n/)) {
    const [key, ...fields] = line.split("\t");
    if (key && fields.length) values.set(key, fields);
  }
  const number = (key: string, index = 0) => {
    const parsed = Number(values.get(key)?.[index] || 0);
    return Number.isFinite(parsed) && parsed >= 0 ? parsed : 0;
  };
  return {
    collectedAt,
    cpuCores: number("cores"),
    cpuTotal: number("cpu"),
    cpuIdle: number("cpu", 1),
    receivedBytes: number("network"),
    sentBytes: number("network", 1),
  };
}

export function deriveCounterRates(previous: MonitorCounters | undefined, current: MonitorCounters) {
  if (!previous) return { cpuPercent: 0, receivedPerSecond: 0, sentPerSecond: 0 };
  const elapsed = Math.max(0, (current.collectedAt - previous.collectedAt) / 1000);
  const cpuTotal = current.cpuTotal - previous.cpuTotal;
  const cpuIdle = current.cpuIdle - previous.cpuIdle;
  const cpuPercent = cpuTotal > 0 ? ((cpuTotal - Math.max(0, cpuIdle)) / cpuTotal) * 100 : 0;
  const rate = (next: number, before: number) => elapsed > 0 && next >= before ? (next - before) / elapsed : 0;
  return {
    cpuPercent: Math.max(0, Math.min(100, cpuPercent)),
    receivedPerSecond: rate(current.receivedBytes, previous.receivedBytes),
    sentPerSecond: rate(current.sentBytes, previous.sentBytes),
  };
}

function recordTime(line: string, now: Date) {
  const unix = line.match(/^(\d{10})(?:\.\d+)?\s/);
  if (unix) return new Date(Number(unix[1]) * 1000).toISOString();
  const syslog = line.match(/^([A-Z][a-z]{2}\s+\d{1,2}\s+\d{2}:\d{2}:\d{2})\s/);
  if (!syslog) return "";
  const parsed = new Date(`${syslog[1]} ${now.getFullYear()}`);
  return Number.isNaN(parsed.getTime()) ? syslog[1] : parsed.toISOString();
}

export function parseSSHMonitor(output: string, now = new Date()): SSHMonitor {
  const [logOutput = "", activeOutput = ""] = output.split(/\r?\n__ACTIVE__\r?\n/, 2);
  const lines = logOutput.split(/\r?\n/);
  const sourceLine = lines.shift() || "";
  const source = sourceLine.startsWith("__SOURCE__\t") ? sourceLine.slice(11).trim() : "";
  const records: SSHLoginRecord[] = [];
  for (const line of lines) {
    const accepted = line.match(/Accepted\s+(\S+)\s+for\s+(\S+)\s+from\s+(\S+)/i);
    const failed = line.match(/Failed\s+(\S+)\s+for\s+(?:invalid user\s+)?(\S+)\s+from\s+(\S+)/i);
    const match = accepted || failed;
    if (!match) continue;
    records.push({
      time: recordTime(line, now),
      status: accepted ? "success" : "failed",
      user: match[2],
      address: match[3],
      method: match[1],
      message: line.replace(/^.*?sshd(?:\[\d+\])?:\s*/, ""),
    });
  }
  records.reverse();
  return {
    source,
    available: Boolean(source),
    successful: records.filter((item) => item.status === "success").length,
    failed: records.filter((item) => item.status === "failed").length,
    activeSessions: activeOutput.split(/\r?\n/).filter((line) => line.trim()).length,
    records,
  };
}
