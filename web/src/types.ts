export interface User {
  id: string;
  username: string;
  role: "admin" | "user";
  disabled: boolean;
  forcePasswordChange?: boolean;
  sessionLocked?: boolean;
  createdAt?: string;
}
export interface LoginDevice {
  id: string;
  userAgent: string;
  ip: string;
  createdAt: string;
  lastSeenAt: string;
  expiresAt: string;
  current: boolean;
}
export interface SecurityPolicy {
  passwordMinLength: number;
  loginFailureThreshold: number;
  lockMinutes: number;
  rememberDays: number;
  forceChangeOnCreate: boolean;
}
export interface Snippet {
  id: string;
  userID?: string;
  name: string;
  groupName: string;
  tags: string;
  command: string;
  description: string;
  createdAt?: string;
  updatedAt?: string;
}
export interface AppNotification {
  id: string;
  sessionID: string;
  title: string;
  kind: string;
  read: boolean;
  createdAt: string;
}
export interface PortForward {
  id: string;
  hostID: string;
  name: string;
  kind: "local" | "remote" | "dynamic";
  listenAddress: string;
  listenPort: number;
  targetHost: string;
  targetPort: number;
  status: string;
  lastError: string;
  bytesIn: number;
  bytesOut: number;
}
export interface WebService {
  id: string;
  hostID: string;
  name: string;
  proxyMode: "path" | "host_port";
  listenPort: number;
  targetURL: string;
  upstreamHost: string;
  skipTLSVerify: boolean;
  createdAt?: string;
  updatedAt?: string;
}
export interface Host {
  id: string;
  userID?: string;
  name: string;
  address: string;
  port: number;
  username: string;
  credentialID: string;
  groupName: string;
  tags: string;
  notes: string;
  initialDirectory: string;
  connectTimeout: number;
  keepaliveInterval: number;
  maxRetries: number;
  terminalType: string;
  platform?: "linux" | "windows" | "macos" | "bsd" | "unix" | "";
  distribution?: string;
  lastStatus?: string;
  lastLatencyMs?: number;
  lastConnectedAt?: string;
  createdAt?: string;
  updatedAt?: string;
}
export interface Credential {
  id: string;
  name: string;
  kind: "password" | "key";
  createdAt?: string;
}
export type SessionStatus =
  | "creating"
  | "attached"
  | "background"
  | "reconnecting"
  | "auth_required"
  | "unreachable"
  | "ended"
  | "ownership_error"
  | "host_key_required";
export interface TerminalSession {
  id: string;
  userID: string;
  hostID: string;
  credentialID: string;
  name: string;
  remoteUser: string;
  tmuxSocket: string;
  tmuxName: string;
  ownerMarker: string;
  status: SessionStatus;
  lastError: string;
  createdAt: string;
  updatedAt: string;
}
export interface Preferences {
  theme: "dark" | "vscode-dark" | "graphite" | "light";
  accentColor: string;
  terminalTheme: string;
  fontSize: number;
  lineHeight: number;
  fontWeight: number;
  letterSpacing: number;
  foreground: string;
  background: string;
  cursorColor: string;
  cursorStyle: "block" | "underline" | "bar";
  cursorBlink: boolean;
  pasteGuard: boolean;
  visualBell: boolean;
  soundBell: boolean;
  browserNotifications: boolean;
  lockEnabled: boolean;
  autoLockMinutes: number;
  lockOnShortcut: boolean;
}

export const terminalFontFamily =
  '"JetBrains Mono", "Cascadia Mono", "Cascadia Code", Menlo, Consolas, "Sarasa Mono SC", "Noto Sans Mono CJK SC", "Source Han Mono SC", "Microsoft YaHei", monospace';
export interface PaneLeaf {
  type: "leaf";
  id: string;
  sessionID: string;
}
export interface PaneSplit {
  type: "split";
  id: string;
  direction: "horizontal" | "vertical";
  ratio: number;
  first: PaneNode;
  second: PaneNode;
}
export type PaneNode = PaneLeaf | PaneSplit;
export interface WorkspaceLayout {
  tabs: string[];
  active?: string;
  trees?: Record<string, PaneNode>;
  focused?: Record<string, string>;
  maximized?: Record<string, string>;
  panes?: string[];
  split?: "single" | "horizontal" | "vertical" | "grid";
  watchedSessionIDs?: string[];
  pinnedSessionIDs?: string[];
}
export interface ApiErrorBody {
  code: string;
  message: string;
  fingerprint?: string;
}
