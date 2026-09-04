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

export type SSHSession = {
  user: string;
  terminal: string;
  pid: number;
  loginTime: string;
  idle: string;
  address: string;
};

export type SSHMonitor = {
  source: string;
  available: boolean;
  successful: number;
  failed: number;
  activeSessions: number;
  sessions: SSHSession[];
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
fi
printf '__LOGS__\n'
printf '%s\n' "$logs"`;

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
if [ -z "$logs" ] && command -v logread >/dev/null 2>&1; then
  logs=$(logread 2>/dev/null | tail -n 1000 || true)
  if [ -n "$logs" ]; then source_name='logread'; fi
fi
printf '__SOURCE__\t%s\n' "$source_name"
printf '__ACTIVE__\n'
active=$(who -u 2>/dev/null || true)
[ -n "$active" ] || active=$(who 2>/dev/null || true)
if [ -n "$active" ]; then
  printf '%s\n' "$active"
elif command -v netstat >/dev/null 2>&1 || command -v ss >/dev/null 2>&1; then
  now=$(date '+%Y-%m-%d %H:%M' 2>/dev/null || true)
  connections=''
  if command -v netstat >/dev/null 2>&1; then
    connections=$(netstat -tnp 2>/dev/null | awk '$6 == "ESTABLISHED" && $4 ~ /:22$/ && $7 ~ /dropbear/ { split($7, p, "/"); print p[1] "\t" $5 }')
  fi
  if [ -z "$connections" ] && command -v ss >/dev/null 2>&1; then
    connections=$(ss -tnp 2>/dev/null | awk '$1 == "ESTAB" && $4 ~ /:22$/ { pid=""; for (i=1; i<=NF; i++) if ($i ~ /pid=[0-9]+/) { pid=$i; sub(/^.*pid=/, "", pid); sub(/[^0-9].*$/, "", pid) } if (pid != "") print pid "\t" $5 }')
  fi
  if [ -z "$connections" ] && command -v netstat >/dev/null 2>&1; then
    connections=$(netstat -tn 2>/dev/null | awk '$6 == "ESTABLISHED" && $4 ~ /:22$/ { print "0\t" $5 }')
  fi
  if [ -z "$connections" ] && command -v ss >/dev/null 2>&1; then
    connections=$(ss -tn 2>/dev/null | awk '$1 == "ESTAB" && $4 ~ /:22$/ { print "0\t" $5 }')
  fi
  if [ -z "$connections" ]; then
    connections=$(ps w 2>/dev/null | awk 'NR > 1 && $0 ~ /[d]ropbear/ && $1 ~ /^[0-9]+$/ { print $1 "\t-" }')
  fi
  printf '%s\n' "$connections" |
    while IFS='	' read -r pid peer; do
      [ -n "$pid" ] || continue
      proc=$(ps w 2>/dev/null | awk -v pid="$pid" '$1 == pid { print; exit }')
      user=$(printf '%s\n' "$proc" | awk '{ print $2 }')
      [ -n "$user" ] || user='?'
      terminal=$(readlink "/proc/$pid/fd/0" 2>/dev/null | sed 's#^.*/##')
      [ -n "$terminal" ] || terminal='-'
      if [ "$pid" != "0" ] && [ "$peer" = "-" ] && { [ "$terminal" = "null" ] || [ "$terminal" = "zero" ] || [ "$terminal" = "-" ]; }; then
        continue
      fi
      address=$(printf '%s\n' "$peer" | sed 's/^\[//; s/\]:[0-9][0-9]*$//; s/:[0-9][0-9]*$//')
      [ -n "$address" ] || address='-'
      printf '%s %s %s 00:00 0 %s (%s)\n' "$user" "$terminal" "$now" "$pid" "$address"
    done
fi
printf '__LOGS__\n'
printf '%s\n' "$logs"`;

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
  const activeMarker = output.match(/\r?\n__ACTIVE__\r?\n/);
  const logsMarker = output.match(/\r?\n__LOGS__\r?\n/);
  let logOutput = output;
  let activeOutput = "";
  let hasActiveSection = false;
  if (activeMarker && logsMarker && (activeMarker.index || 0) < (logsMarker.index || 0)) {
    const activeStart = (activeMarker.index || 0) + activeMarker[0].length;
    const logsStart = (logsMarker.index || 0) + logsMarker[0].length;
    activeOutput = output.slice(activeStart, logsMarker.index);
    logOutput = output.slice(0, activeMarker.index).concat("\n", output.slice(logsStart));
    hasActiveSection = true;
  } else {
    const sections = output.split(/\r?\n__ACTIVE__\r?\n/, 2);
    logOutput = sections[0] || "";
    activeOutput = sections[1] || "";
    hasActiveSection = sections.length > 1;
  }
  const lines = logOutput.split(/\r?\n/);
  const sourceLine = lines.shift() || "";
  const source = sourceLine.startsWith("__SOURCE__\t") ? sourceLine.slice(11).trim() : "";
  const records: SSHLoginRecord[] = [];
  for (const line of lines) {
    const accepted = line.match(/Accepted\s+(\S+)\s+for\s+(\S+)\s+from\s+(\S+)/i);
    const failed = line.match(/Failed\s+(\S+)\s+for\s+(?:invalid user\s+)?(\S+)\s+from\s+(\S+)/i);
    const dropbearAccepted = line.match(/(?:password|pubkey|publickey)\s+auth\s+succeeded\s+for\s+["']?([^\s"']+)["']?\s+from\s+(\S+)/i);
    const dropbearFailed = line.match(/(?:password|pubkey|publickey)\s+auth\s+failed\s+for\s+["']?([^\s"']+)["']?\s+from\s+(\S+)/i);
    const match = accepted || failed || dropbearAccepted || dropbearFailed;
    if (!match) continue;
    const isAccepted = Boolean(accepted || dropbearAccepted);
    const user = accepted || failed ? match[2] : match[1];
    const address = normalizeSSHAddress(accepted || failed ? match[3] : match[2]);
    records.push({
      time: recordTime(line, now),
      status: isAccepted ? "success" : "failed",
      user,
      address,
      method: accepted || failed ? match[1] : line.match(/(password|pubkey|publickey)\s+auth/i)?.[1] || "SSH",
      message: line.replace(/^.*?sshd(?:\[\d+\])?:\s*/, ""),
    });
  }
  records.reverse();
  const sessionLines =
    hasActiveSection
      ? (activeOutput ? activeOutput.split(/\r?\n/) : []).concat(lines)
      : output.split(/\r?\n/);
  const sessions = sessionLines
    .map((line) => parseSSHSession(line))
    .filter((item): item is SSHSession => Boolean(item))
    .filter(
      (item, index, all) =>
        all.findIndex(
          (other) =>
            other.user === item.user &&
            other.terminal === item.terminal &&
            other.loginTime === item.loginTime &&
            other.pid === item.pid &&
            other.address === item.address,
        ) === index,
    );
  return {
    source,
    available: Boolean(source),
    successful: records.filter((item) => item.status === "success").length,
    failed: records.filter((item) => item.status === "failed").length,
    activeSessions: sessions.length,
    sessions,
    records,
  };
}

function normalizeSSHAddress(value: string) {
  const trimmed = value.replace(/^\[/, "").replace(/\](:\d+)?$/, "");
  return trimmed.replace(/^(\d{1,3}(?:\.\d{1,3}){3}):\d+$/, "$1");
}

function parseSSHSession(line: string): SSHSession | undefined {
  const fields = line.trim().split(/\s+/);
  if (fields.length < 4 || !/^\d{4}-\d{2}-\d{2}$/.test(fields[2]) || !/^\d{2}:\d{2}/.test(fields[3])) return;
  const user = fields[0];
  const terminal = fields[1];
  const loginTime = `${fields[2]}T${fields[3].slice(0, 5)}:00`;
  let pid = 0;
  let idle = "";
  for (const field of fields.slice(4)) {
    if (!idle && !/^\d+$/.test(field) && !field.startsWith("(")) idle = field;
    if (/^\d+$/.test(field)) {
      pid = Number(field);
      break;
    }
  }
  const addressMatch = line.match(/\(([^)]*)\)\s*$/);
  const address = addressMatch?.[1] || "";
  return {
    user,
    terminal,
    pid: Number.isSafeInteger(pid) ? pid : 0,
    loginTime,
    idle,
    address,
  };
}
