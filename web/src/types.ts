export interface User { id: string; username: string; role: 'admin' | 'user'; disabled: boolean; createdAt?: string }
export interface Host { id: string; userID?: string; name: string; address: string; port: number; username: string; credentialID: string; groupName: string; tags: string; notes: string; favorite: boolean; createdAt?: string; updatedAt?: string }
export interface Credential { id: string; name: string; kind: 'password' | 'key'; createdAt?: string }
export type SessionStatus = 'creating'|'attached'|'background'|'reconnecting'|'auth_required'|'unreachable'|'ended'|'ownership_error'|'host_key_required'
export interface TerminalSession { id: string; userID: string; hostID: string; credentialID: string; name: string; remoteUser: string; tmuxSocket: string; tmuxName: string; ownerMarker: string; status: SessionStatus; lastError: string; createdAt: string; updatedAt: string }
export interface Preferences { theme: 'dark'|'light'; fontSize: number; lineHeight: number; cursorStyle: 'block'|'underline'|'bar'; cursorBlink: boolean; pasteGuard: boolean }
export interface WorkspaceLayout { tabs: string[]; panes: string[]; active?: string; split?: 'single'|'horizontal'|'vertical'|'grid' }
export interface ApiErrorBody { code: string; message: string; fingerprint?: string }
