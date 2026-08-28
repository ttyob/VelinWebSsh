export type GitChange = {
  index: string;
  worktree: string;
  path: string;
  originalPath?: string;
};

export type GitTreeNode = {
  id: string;
  label: string;
  directory: boolean;
  children?: GitTreeNode[];
  change?: GitChange;
};

export type GitStatus = {
  branch: string;
  tracking: string;
  ahead: number;
  behind: number;
  changes: GitChange[];
};

export type GitCommit = {
  hash: string;
  shortHash: string;
  author: string;
  date: string;
  subject: string;
};

export type GitBranch = {
  current: boolean;
  name: string;
  hash: string;
  subject: string;
  remote: boolean;
  upstream: string;
  tracking: string;
};

export type GitRemote = {
  name: string;
  url: string;
};

export type GitDiffRow = {
  leftNumber?: number;
  rightNumber?: number;
  left: string;
  right: string;
  kind: "context" | "add" | "remove" | "change";
};

export function isStaged(change: GitChange) {
  return change.index !== " " && change.index !== "?";
}

export function parseGitStatus(output: string): GitStatus {
  const records = output.split("\0").filter(Boolean);
  const header = records.find((line) => line.startsWith("## "))?.slice(3) || "";
  let branch = header.split("...")[0].replace(/^No commits yet on /, "").trim() || "HEAD";
  if (/^HEAD \(no branch\)$/.test(branch)) branch = "HEAD";
  let tracking = "";
  let ahead = 0;
  let behind = 0;
  const trackingMatch = header.match(/^(.+?)\.\.\.(\S+)(?: \[(.*?)\])?$/);
  if (trackingMatch) {
    branch = trackingMatch[1];
    tracking = trackingMatch[2];
    const details = trackingMatch[3] || "";
    ahead = Number(details.match(/ahead (\d+)/)?.[1] || 0);
    behind = Number(details.match(/behind (\d+)/)?.[1] || 0);
  }
  const changes: GitChange[] = [];
  for (let index = 0; index < records.length; index++) {
    const record = records[index];
    if (record.startsWith("## ") || record.length < 3) continue;
    const change: GitChange = {
      index: record[0],
      worktree: record[1],
      path: record.slice(3),
    };
    if ((change.index === "R" || change.index === "C" || change.worktree === "R" || change.worktree === "C") && records[index + 1]) {
      change.originalPath = records[++index];
    }
    changes.push(change);
  }
  return { branch, tracking, ahead, behind, changes };
}

export function buildGitChangeTree(changes: GitChange[]): GitTreeNode[] {
  const roots: GitTreeNode[] = [];
  const directories = new Map<string, GitTreeNode>();
  for (const change of changes) {
    const parts = change.path.split("/");
    let children = roots;
    let directoryPath = "";
    for (const part of parts.slice(0, -1)) {
      directoryPath = directoryPath ? `${directoryPath}/${part}` : part;
      let directory = directories.get(directoryPath);
      if (!directory) {
        directory = { id: `dir:${directoryPath}`, label: part, directory: true, children: [] };
        directories.set(directoryPath, directory);
        children.push(directory);
      }
      children = directory.children!;
    }
    children.push({
      id: `file:${change.path}`,
      label: parts.at(-1) || change.path,
      directory: false,
      change,
    });
  }
  const sortNodes = (nodes: GitTreeNode[]) => {
    nodes.sort((left, right) => Number(right.directory) - Number(left.directory) || left.label.localeCompare(right.label));
    for (const node of nodes) if (node.children) sortNodes(node.children);
  };
  sortNodes(roots);
  return roots;
}

export function parseUnifiedDiff(output: string): GitDiffRow[] {
  const rows: GitDiffRow[] = [];
  let leftNumber = 0;
  let rightNumber = 0;
  let inHunk = false;
  let removed: Array<{ number: number; text: string }> = [];
  let added: Array<{ number: number; text: string }> = [];

  const flushChanges = () => {
    const count = Math.max(removed.length, added.length);
    for (let index = 0; index < count; index++) {
      const left = removed[index];
      const right = added[index];
      rows.push({
        leftNumber: left?.number,
        rightNumber: right?.number,
        left: left?.text || "",
        right: right?.text || "",
        kind: left && right ? "change" : left ? "remove" : "add",
      });
    }
    removed = [];
    added = [];
  };

  for (const line of output.split("\n")) {
    if (line.startsWith("diff --git ")) {
      flushChanges();
      inHunk = false;
      continue;
    }
    const hunk = line.match(/^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/);
    if (hunk) {
      flushChanges();
      leftNumber = Number(hunk[1]);
      rightNumber = Number(hunk[2]);
      inHunk = true;
      continue;
    }
    if (!inHunk || line === "" || line === "\\ No newline at end of file") continue;
    if (line.startsWith("-")) {
      removed.push({ number: leftNumber++, text: line.slice(1) });
      continue;
    }
    if (line.startsWith("+")) {
      added.push({ number: rightNumber++, text: line.slice(1) });
      continue;
    }
    flushChanges();
    const text = line.startsWith(" ") ? line.slice(1) : line;
    rows.push({ leftNumber: leftNumber++, rightNumber: rightNumber++, left: text, right: text, kind: "context" });
  }
  flushChanges();
  return rows;
}

export function parseGitCommits(output: string): GitCommit[] {
  return output.split(/\r?\n/).filter(Boolean).map((line) => {
    const [hash = "", shortHash = "", author = "", date = "", ...subject] = line.split("\t");
    return { hash, shortHash, author, date, subject: subject.join("\t") };
  }).filter((item) => item.hash);
}

export function parseGitBranches(output: string): GitBranch[] {
  return output.split(/\r?\n/).filter(Boolean).map((line) => {
    const [head = "", ref = "", hash = "", upstream = "", tracking = "", ...subject] = line.split("\t");
    const remote = ref.startsWith("refs/remotes/");
    const name = ref.replace(/^refs\/(heads|remotes)\//, "");
    return {
      current: head.trim() === "*",
      name,
      hash,
      subject: subject.join("\t"),
      remote,
      upstream,
      tracking: tracking.replace(/^\[|\]$/g, ""),
    };
  }).filter((item) => item.name && !item.name.endsWith("/HEAD"));
}

export function parseGitRemotes(output: string): GitRemote[] {
  const values = new Map<string, string>();
  for (const line of output.split(/\r?\n/).filter(Boolean)) {
    const match = line.match(/^(\S+)\s+(\S+)\s+\((fetch|push)\)$/);
    if (match && !values.has(match[1])) values.set(match[1], match[2]);
  }
  return [...values].map(([name, url]) => ({ name, url }));
}
