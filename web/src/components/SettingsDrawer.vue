<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from "vue";
import { ElMessage, ElMessageBox } from "element-plus";
import {
  Bell,
  Check,
  Code2,
  Database,
  HardDriveDownload,
  KeyRound,
  LockKeyhole,
  LogOut,
  MonitorCog,
  Network,
  Pencil,
  Plus,
  ScrollText,
  ServerCog,
  Shield,
  ShieldCheck,
  Trash2,
  UserPlus,
  Users,
} from "@lucide/vue";
import { api, json } from "../api";
import { useAuthStore } from "../stores/auth";
import type {
  Host,
  Credential,
  LoginDevice,
  Preferences,
  SecurityPolicy,
  TerminalSession,
  User,
} from "../types";
import ToolsDrawer from "./ToolsDrawer.vue";
import CredentialsPanel from "./CredentialsPanel.vue";
import {
  accentPresets,
  findInterfaceTheme,
  findTerminalTheme,
  interfaceThemePresets,
  terminalThemePresets,
} from "../themePresets";

const props = defineProps<{
  modelValue: boolean;
  preferences: Preferences;
  hosts: Host[];
  credentials: Credential[];
  sessions: TerminalSession[];
}>();
const emit = defineEmits<{
  "update:modelValue": [boolean];
  preferences: [Preferences];
  logout: [];
  insert: [string];
  execute: [string];
  batchExecute: [string, string[]];
  notificationOpen: [string];
  credentialSaved: [Credential];
  credentialDeleted: [string];
}>();
const auth = useAuthStore();
const tab = ref("terminal"),
  users = ref<User[]>([]),
  devices = ref<LoginDevice[]>([]),
  audits = ref<any[]>([]),
  stats = ref<any>({});
const userDialogOpen = ref(false),
  userPasswordDialogOpen = ref(false),
  savingUser = ref(false),
  savingUserPassword = ref(false),
  userForm = ref({
    id: "",
    username: "",
    password: "",
    role: "user" as "admin" | "user",
    disabled: false,
  }),
  userPasswordForm = ref({ userID: "", username: "", password: "" }),
  passwordForm = ref({
    currentPassword: "",
    newPassword: "",
    confirmPassword: "",
  });
const lockPINConfigured = ref(false),
  lockPINSetupOpen = ref(false),
  savingLockPIN = ref(false),
  lockPINForm = ref({ pin: "", confirm: "" });
let preferenceSaveTimer: number | undefined;
let preferenceSaveInFlight = false;
let preferenceSaveQueued = false;
let preferenceSaveNotice = false;
let savedPreferenceSnapshot = "";
const totp = ref({ enabled: false, recoveryCodesRemaining: 0 }),
  totpSetup = ref<{ secret: string; uri: string } | null>(null),
  totpForm = ref({ password: "", code: "" }),
  recoveryCodes = ref<string[]>([]);
const policy = ref<SecurityPolicy>({
  passwordMinLength: 10,
  loginFailureThreshold: 5,
  lockMinutes: 15,
  rememberDays: 7,
  forceChangeOnCreate: true,
});
const terminalDefaults: Preferences = {
  theme: "dark",
  accentColor: "#72c58f",
  terminalTheme: "velin",
  fontSize: 14,
  lineHeight: 1.25,
  fontWeight: 400,
  letterSpacing: 0,
  foreground: "#d8ded9",
  background: "#111416",
  cursorColor: "#8fd6a7",
  cursorStyle: "block",
  cursorBlink: true,
  pasteGuard: true,
  visualBell: true,
  soundBell: false,
  browserNotifications: false,
  lockEnabled: false,
  autoLockMinutes: 15,
  lockOnShortcut: true,
};
function selectTerminalTheme(id: string) {
  const preset = findTerminalTheme(id);
  props.preferences.terminalTheme = preset.id;
  props.preferences.background = preset.background;
  props.preferences.foreground = preset.foreground;
  props.preferences.cursorColor = preset.cursor;
}
function selectInterfaceTheme(id: string) {
  const preset = findInterfaceTheme(id);
  props.preferences.theme = preset.id;
  props.preferences.accentColor = preset.accent;
}
function formatBytes(value: number) {
  if (!value) return "0 B";
  const units = ["B", "KiB", "MiB", "GiB"],
    index = Math.min(
      units.length - 1,
      Math.floor(Math.log(value) / Math.log(1024)),
    );
  return `${(value / 1024 ** index).toFixed(index ? 1 : 0)} ${units[index]}`;
}
function formatDuration(seconds: number) {
  const days = Math.floor(seconds / 86400),
    hours = Math.floor((seconds % 86400) / 3600),
    minutes = Math.floor((seconds % 3600) / 60);
  return days
    ? `${days} 天 ${hours} 小时`
    : hours
      ? `${hours} 小时 ${minutes} 分钟`
      : `${minutes} 分钟`;
}

async function load() {
  const [loadedDevices, loadedAudits, loadedTOTP, lockPIN] = await Promise.all([
    api<LoginDevice[]>("/api/auth/devices"),
    api<any[]>("/api/audit"),
    api<{ enabled: boolean; recoveryCodesRemaining: number }>("/api/auth/totp"),
    api<{ configured: boolean }>("/api/auth/lock-pin"),
  ]);
  devices.value = loadedDevices;
  audits.value = loadedAudits;
  totp.value = loadedTOTP;
  lockPINConfigured.value = lockPIN.configured;
  if (!lockPIN.configured) props.preferences.lockEnabled = false;
  if (auth.user?.role === "admin")
    [users.value, stats.value, policy.value] = await Promise.all([
      api<User[]>("/api/admin/users"),
      api<any>("/api/admin/stats"),
      api<SecurityPolicy>("/api/admin/security-policy"),
    ]);
}
function normalizePIN(value: string) {
  return value.replace(/\D/g, "").slice(0, 6);
}
function openLockPINSetup() {
  lockPINForm.value = { pin: "", confirm: "" };
  lockPINSetupOpen.value = true;
}
async function saveLockPIN() {
  const { pin, confirm } = lockPINForm.value;
  if (!/^\d{6}$/.test(pin))
    return ElMessage.warning("请输入 6 位数字 PIN");
  if (pin !== confirm) return ElMessage.warning("两次输入的 PIN 不一致");
  savingLockPIN.value = true;
  try {
    await api("/api/auth/lock-pin", {
      method: "PUT",
      body: json({ pin }),
    });
    lockPINConfigured.value = true;
    props.preferences.lockEnabled = true;
    lockPINSetupOpen.value = false;
    lockPINForm.value = { pin: "", confirm: "" };
    await persistPreferences(false);
    ElMessage.success("锁屏 PIN 已设置");
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : "PIN 保存失败");
  } finally {
    savingLockPIN.value = false;
  }
}
async function toggleLockFeature(value: string | number | boolean) {
  if (Boolean(value)) {
    if (lockPINConfigured.value) {
      props.preferences.lockEnabled = true;
      return;
    }
    openLockPINSetup();
    return;
  }
  try {
    await api("/api/auth/lock-pin", { method: "DELETE" });
    lockPINConfigured.value = false;
    props.preferences.lockEnabled = false;
    lockPINSetupOpen.value = false;
    lockPINForm.value = { pin: "", confirm: "" };
    await persistPreferences(false);
    ElMessage.success("锁屏已关闭");
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : "无法关闭锁屏");
  }
}
onMounted(load);
onBeforeUnmount(() => {
  clearTimeout(preferenceSaveTimer);
  if (preferenceSnapshot() !== savedPreferenceSnapshot)
    void persistPreferences(false);
});
function preferenceSnapshot() {
  return JSON.stringify(props.preferences);
}
function schedulePreferenceSave() {
  if (!props.modelValue) return;
  clearTimeout(preferenceSaveTimer);
  preferenceSaveTimer = window.setTimeout(
    () => void persistPreferences(false),
    400,
  );
}
async function persistPreferences(showNotice: boolean) {
  clearTimeout(preferenceSaveTimer);
  preferenceSaveTimer = undefined;
  const snapshot = preferenceSnapshot();
  if (snapshot === savedPreferenceSnapshot) {
    if (showNotice) ElMessage.success("设置已保存");
    return;
  }
  if (preferenceSaveInFlight) {
    preferenceSaveQueued = true;
    preferenceSaveNotice ||= showNotice;
    return;
  }
  preferenceSaveInFlight = true;
  let saved = false;
  try {
    await api("/api/preferences", {
      method: "PUT",
      body: snapshot,
    });
    savedPreferenceSnapshot = snapshot;
    saved = true;
    if (preferenceSnapshot() === snapshot)
      emit("preferences", JSON.parse(snapshot) as Preferences);
    if (showNotice) ElMessage.success("设置已保存");
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : "设置保存失败");
  } finally {
    preferenceSaveInFlight = false;
    const needsAnotherSave =
      preferenceSaveQueued ||
      (saved && preferenceSnapshot() !== savedPreferenceSnapshot);
    const notifyNext = preferenceSaveNotice;
    preferenceSaveQueued = false;
    preferenceSaveNotice = false;
    if (needsAnotherSave) void persistPreferences(notifyNext);
  }
}
async function savePreferences() {
  await persistPreferences(true);
}
async function resetPreferences() {
  if (lockPINConfigured.value) {
    try {
      await api("/api/auth/lock-pin", { method: "DELETE" });
      lockPINConfigured.value = false;
    } catch (error) {
      return ElMessage.error(
        error instanceof Error ? error.message : "无法关闭锁屏",
      );
    }
  }
  Object.assign(props.preferences, terminalDefaults);
  await savePreferences();
}
watch(
  () => props.preferences,
  () => schedulePreferenceSave(),
  { deep: true },
);
watch(
  () => props.modelValue,
  (open) => {
    if (open) {
      if (!savedPreferenceSnapshot)
        savedPreferenceSnapshot = preferenceSnapshot();
      return;
    }
    if (preferenceSnapshot() !== savedPreferenceSnapshot)
      void persistPreferences(false);
  },
);
async function toggleBrowserNotifications(value: boolean) {
  if (value && "Notification" in window) {
    const permission = await Notification.requestPermission();
    if (permission !== "granted") {
      props.preferences.browserNotifications = false;
      return ElMessage.warning("浏览器通知权限未授予");
    }
  }
}
async function revoke(id: string) {
  const device = devices.value.find((item) => item.id === id);
  await api(`/api/auth/devices/${id}`, { method: "DELETE" });
  devices.value = devices.value.filter((v) => v.id !== id);
  if (device?.current) emit("logout");
}
async function revokeAll() {
  try {
    await ElMessageBox.confirm(
      "这会撤销当前账户在所有浏览器中的登录，正在运行的远程终端任务不会终止。",
      "退出全部设备",
      {
        confirmButtonText: "全部退出",
        cancelButtonText: "取消",
        type: "warning",
      },
    );
    await api("/api/auth/devices/revoke-all", { method: "POST" });
    emit("logout");
  } catch {}
}
function openCreateUser() {
  userForm.value = {
    id: "",
    username: "",
    password: "",
    role: "user",
    disabled: false,
  };
  userDialogOpen.value = true;
}
function openEditUser(user: User) {
  userForm.value = {
    id: user.id,
    username: user.username,
    password: "",
    role: user.role,
    disabled: user.disabled,
  };
  userDialogOpen.value = true;
}
async function saveUser() {
  const form = userForm.value;
  if (form.username.trim().length < 3)
    return ElMessage.warning("用户名至少 3 位");
  if (!form.id && form.password.length < policy.value.passwordMinLength)
    return ElMessage.warning(`密码至少 ${policy.value.passwordMinLength} 位`);
  savingUser.value = true;
  try {
    const user = await api<User>(
      form.id ? `/api/admin/users/${form.id}` : "/api/admin/users",
      {
        method: form.id ? "PATCH" : "POST",
        body: json(
          form.id
            ? {
                username: form.username.trim(),
                role: form.role,
                disabled: form.disabled,
              }
            : {
                username: form.username.trim(),
                password: form.password,
                role: form.role,
              },
        ),
      },
    );
    const index = users.value.findIndex((item) => item.id === user.id);
    if (index >= 0) users.value.splice(index, 1, user);
    else users.value.push(user);
    if (auth.user?.id === user.id) Object.assign(auth.user, user);
    userDialogOpen.value = false;
    ElMessage.success(form.id ? "用户已更新" : "用户已创建");
  } catch (e) {
    ElMessage.error(e instanceof Error ? e.message : "保存失败");
  } finally {
    savingUser.value = false;
  }
}
function openUserPassword(user: User) {
  if (user.id === auth.user?.id) {
    tab.value = "devices";
    return ElMessage.info("当前账户请在账户与设备中修改登录密码");
  }
  userPasswordForm.value = {
    userID: user.id,
    username: user.username,
    password: "",
  };
  userPasswordDialogOpen.value = true;
}
async function saveUserPassword() {
  const form = userPasswordForm.value;
  if (form.password.length < policy.value.passwordMinLength)
    return ElMessage.warning(`密码至少 ${policy.value.passwordMinLength} 位`);
  savingUserPassword.value = true;
  try {
    const user = await api<User>(`/api/admin/users/${form.userID}`, {
      method: "PATCH",
      body: json({ password: form.password }),
    });
    const index = users.value.findIndex((item) => item.id === user.id);
    if (index >= 0) users.value.splice(index, 1, user);
    userPasswordDialogOpen.value = false;
    ElMessage.success("密码已修改，该用户的已有登录已撤销");
  } catch (e) {
    ElMessage.error(e instanceof Error ? e.message : "密码修改失败");
  } finally {
    savingUserPassword.value = false;
  }
}
async function deleteUser(user: User) {
  try {
    await ElMessageBox.confirm(
      `删除用户“${user.username}”及其主机、凭据和工作区？存在活动远程会话时系统会拒绝删除。`,
      "删除用户",
      { confirmButtonText: "删除", cancelButtonText: "取消", type: "warning" },
    );
    await api(`/api/admin/users/${user.id}`, { method: "DELETE" });
    users.value = users.value.filter((item) => item.id !== user.id);
    ElMessage.success("用户已删除");
  } catch (e: any) {
    if (e !== "cancel" && e !== "close")
      ElMessage.error(e instanceof Error ? e.message : "删除失败");
  }
}
async function savePolicy() {
  try {
    policy.value = await api<SecurityPolicy>("/api/admin/security-policy", {
      method: "PUT",
      body: json(policy.value),
    });
    ElMessage.success("登录安全策略已保存");
  } catch (e) {
    ElMessage.error(e instanceof Error ? e.message : "保存失败");
  }
}
async function backup() {
  const result = await api<{ file: string; sha256: string; verified: boolean }>(
    "/api/admin/backup",
    { method: "POST" },
  );
  ElMessage.success(
    `备份已创建并通过完整性验证：${result.file} · ${result.sha256.slice(0, 12)}…`,
  );
}
async function changePassword() {
  const p = passwordForm.value;
  if (p.newPassword !== p.confirmPassword)
    return ElMessage.warning("两次输入的新密码不一致");
  try {
    await api("/api/auth/password", {
      method: "POST",
      body: json({
        currentPassword: p.currentPassword,
        newPassword: p.newPassword,
      }),
    });
    passwordForm.value = {
      currentPassword: "",
      newPassword: "",
      confirmPassword: "",
    };
    if (auth.user) auth.user.forcePasswordChange = false;
    ElMessage.success("密码已修改");
  } catch (e) {
    ElMessage.error(e instanceof Error ? e.message : "修改失败");
  }
}
async function beginTOTP() {
  try {
    totpSetup.value = await api<{ secret: string; uri: string }>(
      "/api/auth/totp/setup",
      { method: "POST" },
    );
    totpForm.value = { password: "", code: "" };
    recoveryCodes.value = [];
  } catch (e) {
    ElMessage.error(e instanceof Error ? e.message : "无法生成双因素认证设置");
  }
}
async function enableTOTP() {
  if (!totpForm.value.password || !totpForm.value.code)
    return ElMessage.warning("请输入当前密码和验证器动态码");
  try {
    const result = await api<{ enabled: boolean; recoveryCodes: string[] }>(
      "/api/auth/totp/enable",
      { method: "POST", body: json(totpForm.value) },
    );
    totp.value = {
      enabled: true,
      recoveryCodesRemaining: result.recoveryCodes.length,
    };
    recoveryCodes.value = result.recoveryCodes;
    totpSetup.value = null;
    totpForm.value = { password: "", code: "" };
    ElMessage.success("双因素认证已启用，请立即保存恢复码");
  } catch (e) {
    ElMessage.error(e instanceof Error ? e.message : "启用失败");
  }
}
async function disableTOTP() {
  try {
    const password = await ElMessageBox.prompt(
      "请输入当前登录密码。",
      "关闭双因素认证",
      {
        confirmButtonText: "下一步",
        cancelButtonText: "取消",
        inputType: "password",
        inputValidator: (v) => Boolean(v) || "请输入当前密码",
      },
    );
    const code = await ElMessageBox.prompt(
      "请输入验证器中的 6 位动态码。",
      "确认关闭",
      {
        confirmButtonText: "关闭",
        cancelButtonText: "取消",
        inputValidator: (v) => /^\d{6}$/.test(v.trim()) || "请输入 6 位动态码",
      },
    );
    await api("/api/auth/totp", {
      method: "DELETE",
      body: json({ password: password.value, code: code.value.trim() }),
    });
    totp.value = { enabled: false, recoveryCodesRemaining: 0 };
    totpSetup.value = null;
    recoveryCodes.value = [];
    ElMessage.success("双因素认证已关闭");
  } catch (e: any) {
    if (e !== "cancel" && e !== "close")
      ElMessage.error(e instanceof Error ? e.message : "关闭失败");
  }
}
async function copyValue(value: string, label: string) {
  try {
    await navigator.clipboard.writeText(value);
    ElMessage.success(`${label}已复制`);
  } catch {
    ElMessage.warning("浏览器未允许复制，请手动选择文本");
  }
}
function downloadRecoveryCodes() {
  const content = `Velin Web SSH 恢复码\n账号：${auth.user?.username || ""}\n生成时间：${new Date().toLocaleString()}\n\n${recoveryCodes.value.join("\n")}\n`;
  const link = document.createElement("a");
  link.href = URL.createObjectURL(
    new Blob([content], { type: "text/plain;charset=utf-8" }),
  );
  link.download = "velin-recovery-codes.txt";
  link.click();
  URL.revokeObjectURL(link.href);
}
function forwardBatch(text: string, sessionIDs: string[]) {
  emit("batchExecute", text, sessionIDs);
}
</script>

<template>
  <el-dialog
    :model-value="modelValue"
    class="settings-dialog"
    title="设置"
    width="min(900px, calc(100vw - 28px))"
    append-to-body
    @open="
      tab = 'terminal';
      load();
    "
    @update:model-value="emit('update:modelValue', $event)"
  >
    <div class="settings-layout">
      <nav class="settings-menu" aria-label="设置分类">
        <button
          :class="{ active: tab === 'terminal' }"
          @click="tab = 'terminal'"
        >
          <MonitorCog :size="17" /><span>外观与终端</span>
        </button>
        <button :class="{ active: tab === 'devices' }" @click="tab = 'devices'">
          <Shield :size="17" /><span>账户与设备</span>
        </button>
        <button
          :class="{ active: tab === 'credentials' }"
          @click="tab = 'credentials'"
        >
          <KeyRound :size="17" /><span>凭据管理</span>
        </button>
        <button :class="{ active: tab === 'audit' }" @click="tab = 'audit'">
          <ScrollText :size="17" /><span>审计记录</span>
        </button>
        <button
          v-if="auth.user?.role === 'admin'"
          :class="{ active: tab === 'users' }"
          @click="tab = 'users'"
        >
          <Users :size="17" /><span>用户管理</span>
        </button>
        <button
          v-if="auth.user?.role === 'admin'"
          :class="{ active: tab === 'admin' }"
          @click="tab = 'admin'"
        >
          <ServerCog :size="17" /><span>系统管理</span>
        </button>
        <span class="settings-menu-divider">工具</span>
        <button
          :class="{ active: tab === 'snippets' }"
          @click="tab = 'snippets'"
        >
          <Code2 :size="17" /><span>命令片段</span>
        </button>
        <button
          :class="{ active: tab === 'forwards' }"
          @click="tab = 'forwards'"
        >
          <Network :size="17" /><span>端口转发</span>
        </button>
        <button :class="{ active: tab === 'data' }" @click="tab = 'data'">
          <Database :size="17" /><span>数据</span>
        </button>
        <button
          :class="{ active: tab === 'notifications' }"
          @click="tab = 'notifications'"
        >
          <Bell :size="17" /><span>通知</span>
        </button>
      </nav>
      <section
        class="settings-content"
        :class="{
          'has-detail-tabs': [
            'terminal',
            'devices',
            'audit',
            'users',
            'admin',
          ].includes(tab),
        }"
      >
        <CredentialsPanel
          v-if="tab === 'credentials'"
          :credentials="credentials"
          @saved="emit('credentialSaved', $event)"
          @deleted="emit('credentialDeleted', $event)"
        />
        <el-tabs
          v-if="
            ['terminal', 'devices', 'audit', 'users', 'admin'].includes(tab)
          "
          v-model="tab"
          class="settings-tabs settings-detail-tabs"
        >
          <el-tab-pane label="终端" name="terminal">
            <div class="settings-section">
              <h3>界面外观</h3>
              <div
                class="interface-theme-grid"
                role="radiogroup"
                aria-label="界面主题"
              >
                <button
                  v-for="preset in interfaceThemePresets"
                  :key="preset.id"
                  class="interface-theme-card"
                  :class="{ active: preferences.theme === preset.id }"
                  :aria-checked="preferences.theme === preset.id"
                  role="radio"
                  @click="selectInterfaceTheme(preset.id)"
                >
                  <span
                    class="interface-theme-preview"
                    :style="{
                      backgroundColor: preset.colors.bg,
                      borderColor: preset.colors.lineStrong,
                    }"
                  >
                    <i :style="{ backgroundColor: preset.colors.sidebar }"></i>
                    <b :style="{ backgroundColor: preset.colors.tabbar }"></b>
                    <em
                      :style="{ backgroundColor: preset.colors.surface2 }"
                    ></em>
                    <small :style="{ backgroundColor: preset.accent }"></small>
                  </span>
                  <span>
                    <strong>{{ preset.name }}</strong>
                    <small>{{ preset.description }}</small>
                  </span>
                  <Check v-if="preferences.theme === preset.id" :size="15" />
                </button>
              </div>
              <div class="setting-row palette-setting">
                <span>强调色</span>
                <div
                  class="accent-palette"
                  role="radiogroup"
                  aria-label="强调色"
                >
                  <el-tooltip
                    v-for="preset in accentPresets"
                    :key="preset.id"
                    :content="preset.name"
                  >
                    <button
                      class="accent-swatch"
                      :class="{
                        active: preferences.accentColor === preset.value,
                      }"
                      :style="{ backgroundColor: preset.value }"
                      :aria-label="preset.name"
                      :aria-checked="preferences.accentColor === preset.value"
                      role="radio"
                      @click="preferences.accentColor = preset.value"
                    >
                      <Check
                        v-if="preferences.accentColor === preset.value"
                        :size="15"
                      />
                    </button>
                  </el-tooltip>
                </div>
              </div>
            </div>
            <div class="settings-section">
              <h3>终端外观</h3>
              <div
                class="terminal-theme-grid"
                role="radiogroup"
                aria-label="终端配色"
              >
                <button
                  v-for="preset in terminalThemePresets"
                  :key="preset.id"
                  class="terminal-theme-card"
                  :class="{ active: preferences.terminalTheme === preset.id }"
                  :style="{
                    backgroundColor: preset.background,
                    color: preset.foreground,
                  }"
                  :aria-checked="preferences.terminalTheme === preset.id"
                  role="radio"
                  @click="selectTerminalTheme(preset.id)"
                >
                  <span class="terminal-theme-preview">
                    <i :style="{ backgroundColor: preset.colors[1] }"></i>
                    <i :style="{ backgroundColor: preset.colors[2] }"></i>
                    <i :style="{ backgroundColor: preset.colors[3] }"></i>
                    <i :style="{ backgroundColor: preset.colors[4] }"></i>
                  </span>
                  <span>{{ preset.name }}</span>
                  <Check
                    v-if="preferences.terminalTheme === preset.id"
                    :size="14"
                  />
                </button>
              </div>
              <div class="setting-row">
                <span>字号</span
                ><el-slider
                  v-model="preferences.fontSize"
                  :min="10"
                  :max="24"
                  show-input
                />
              </div>
              <div class="setting-row">
                <span>行高</span
                ><el-slider
                  v-model="preferences.lineHeight"
                  :min="1"
                  :max="2"
                  :step="0.05"
                  show-input
                />
              </div>
              <div class="setting-row">
                <span>字重</span
                ><el-slider
                  v-model="preferences.fontWeight"
                  :min="300"
                  :max="700"
                  :step="100"
                  show-input
                />
              </div>
              <div class="setting-row">
                <span>字间距</span
                ><el-slider
                  v-model="preferences.letterSpacing"
                  :min="0"
                  :max="3"
                  :step="0.25"
                  show-input
                />
              </div>
              <div class="setting-row">
                <span>光标</span
                ><el-select v-model="preferences.cursorStyle"
                  ><el-option label="方块" value="block" /><el-option
                    label="竖线"
                    value="bar" /><el-option label="下划线" value="underline"
                /></el-select>
              </div>
              <div class="setting-row">
                <span>光标闪烁</span
                ><el-switch v-model="preferences.cursorBlink" />
              </div>
              <div class="setting-row">
                <span>视觉响铃</span
                ><el-switch v-model="preferences.visualBell" />
              </div>
              <div class="setting-row">
                <span>声音响铃</span
                ><el-switch v-model="preferences.soundBell" />
              </div>
              <div class="setting-row">
                <span>浏览器通知</span
                ><el-switch
                  v-model="preferences.browserNotifications"
                  @change="toggleBrowserNotifications"
                />
              </div>
              <div class="setting-row">
                <span>多行粘贴保护</span
                ><el-switch v-model="preferences.pasteGuard" />
              </div>
              <div class="row-actions settings-actions">
                <el-button @click="resetPreferences">恢复默认</el-button
                ><el-button type="primary" @click="savePreferences"
                  >保存设置</el-button
                >
              </div>
            </div>
          </el-tab-pane>
          <el-tab-pane label="登录设备" name="devices">
            <div class="settings-section">
              <h3>工作区锁屏</h3>
              <div class="setting-row">
                <span>启用锁屏</span
                ><el-switch
                  :model-value="preferences.lockEnabled && lockPINConfigured"
                  @change="toggleLockFeature"
                />
              </div>
              <template v-if="preferences.lockEnabled && lockPINConfigured">
                <div class="setting-row">
                  <span>空闲自动锁屏</span
                  ><el-select v-model="preferences.autoLockMinutes">
                  <el-option label="不按空闲锁屏" :value="0" />
                  <el-option label="1 分钟" :value="1" />
                  <el-option label="5 分钟" :value="5" />
                  <el-option label="15 分钟" :value="15" />
                  <el-option label="30 分钟" :value="30" />
                  <el-option label="1 小时" :value="60" />
                  </el-select>
                </div>
                <div class="setting-row">
                  <span>Win/Meta + L</span
                  ><el-switch v-model="preferences.lockOnShortcut" />
                </div>
                <div class="setting-row">
                  <span>锁屏 PIN</span
                  ><el-button text @click="openLockPINSetup">修改 PIN</el-button>
                </div>
              </template>
              <div v-if="lockPINSetupOpen" class="lock-pin-setup">
                <el-input
                  :model-value="lockPINForm.pin"
                  maxlength="6"
                  inputmode="numeric"
                  type="password"
                  show-password
                  autocomplete="new-password"
                  placeholder="设置 6 位数字 PIN"
                  @update:model-value="lockPINForm.pin = normalizePIN($event)"
                />
                <el-input
                  :model-value="lockPINForm.confirm"
                  maxlength="6"
                  inputmode="numeric"
                  type="password"
                  show-password
                  autocomplete="new-password"
                  placeholder="再次输入 PIN"
                  @update:model-value="lockPINForm.confirm = normalizePIN($event)"
                />
                <div class="row-actions">
                  <el-button @click="lockPINSetupOpen = false">取消</el-button>
                  <el-button type="primary" :loading="savingLockPIN" @click="saveLockPIN">
                    保存 PIN
                  </el-button>
                </div>
              </div>
              <p class="setting-note">
                仅在空闲超时、手动锁屏或捕获到 Win/Meta + L 时锁定；切换页面不会锁定。
              </p>
            </div>
            <div class="settings-section">
              <h3>修改登录密码</h3>
              <div class="inline-form password-form">
                <el-input
                  v-model="passwordForm.currentPassword"
                  type="password"
                  show-password
                  placeholder="当前密码"
                /><el-input
                  v-model="passwordForm.newPassword"
                  type="password"
                  show-password
                  :placeholder="`新密码，至少 ${policy.passwordMinLength} 位`"
                /><el-input
                  v-model="passwordForm.confirmPassword"
                  type="password"
                  show-password
                  placeholder="确认新密码"
                /><el-button @click="changePassword">修改</el-button>
              </div>
            </div>
            <div class="settings-section totp-section">
              <div class="security-heading">
                <div>
                  <h3>双因素认证</h3>
                  <p>
                    登录时使用验证器动态码，恢复码可在无法访问验证器时使用一次。
                  </p>
                </div>
                <span
                  class="security-state"
                  :class="{ enabled: totp.enabled }"
                  >{{ totp.enabled ? "已启用" : "未启用" }}</span
                >
              </div>
              <template v-if="totp.enabled"
                ><p class="muted">
                  剩余恢复码：{{ totp.recoveryCodesRemaining }} 个
                </p>
                <el-button type="danger" plain @click="disableTOTP"
                  >关闭双因素认证</el-button
                ></template
              >
              <template v-else-if="totpSetup"
                ><div class="totp-secret">
                  <span>验证器密钥</span><code>{{ totpSetup.secret }}</code
                  ><el-button text @click="copyValue(totpSetup.secret, '密钥')"
                    >复制</el-button
                  >
                </div>
                <div class="totp-secret">
                  <span>配置 URI</span
                  ><code class="totp-uri">{{ totpSetup.uri }}</code
                  ><el-button text @click="copyValue(totpSetup.uri, '配置 URI')"
                    >复制</el-button
                  >
                </div>
                <div class="inline-form totp-confirm">
                  <el-input
                    v-model="totpForm.password"
                    type="password"
                    show-password
                    placeholder="当前密码"
                  /><el-input
                    v-model="totpForm.code"
                    maxlength="6"
                    inputmode="numeric"
                    placeholder="6 位动态码"
                  /><el-button
                    type="primary"
                    :icon="ShieldCheck"
                    @click="enableTOTP"
                    >确认启用</el-button
                  >
                </div></template
              >
              <el-button
                v-else
                :icon="ShieldCheck"
                type="primary"
                @click="beginTOTP"
                >设置双因素认证</el-button
              >
              <div v-if="recoveryCodes.length" class="recovery-panel">
                <strong>恢复码仅显示这一次</strong>
                <p>每个恢复码只能使用一次，请保存在安全位置。</p>
                <div class="recovery-grid">
                  <code v-for="code in recoveryCodes" :key="code">{{
                    code
                  }}</code>
                </div>
                <div class="row-actions">
                  <el-button
                    @click="copyValue(recoveryCodes.join('\n'), '恢复码')"
                    >复制全部</el-button
                  ><el-button
                    :icon="HardDriveDownload"
                    @click="downloadRecoveryCodes"
                    >下载文本</el-button
                  >
                </div>
              </div>
            </div>
            <div class="settings-section">
              <h3>登录设备</h3>
              <div class="list-stack">
                <div v-for="d in devices" :key="d.id" class="data-row">
                  <div>
                    <strong
                      >{{ d.userAgent || "未知设备" }}
                      <span v-if="d.current" class="current-device"
                        >当前设备</span
                      ></strong
                    ><small
                      >{{ d.ip }} · 最后活动
                      {{ new Date(d.lastSeenAt).toLocaleString() }} · 到期
                      {{ new Date(d.expiresAt).toLocaleString() }}</small
                    >
                  </div>
                  <el-button :icon="Trash2" text @click="revoke(d.id)"
                    >撤销</el-button
                  >
                </div>
              </div>
              <el-button :icon="LogOut" type="danger" plain @click="revokeAll"
                >退出全部设备</el-button
              >
            </div>
          </el-tab-pane>
          <el-tab-pane label="审计" name="audit"
            ><div class="list-stack">
              <div v-for="a in audits" :key="a.id" class="data-row">
                <div>
                  <strong>{{ a.event_type }}</strong
                  ><small
                    >{{ a.resource_type }} {{ a.resource_id }} ·
                    {{ new Date(a.created_at).toLocaleString() }}</small
                  >
                </div>
                <span class="muted">{{ a.ip }}</span>
              </div>
            </div></el-tab-pane
          >
          <el-tab-pane
            v-if="auth.user?.role === 'admin'"
            label="用户"
            name="users"
          >
            <div class="tool-heading user-management-heading">
              <div>
                <strong>用户列表</strong>
                <small>{{ users.length }} 个账户</small>
              </div>
              <el-button :icon="UserPlus" type="primary" @click="openCreateUser"
                >添加用户</el-button
              >
            </div>
            <div v-if="users.length" class="list-stack user-list">
              <div v-for="u in users" :key="u.id" class="user-row">
                <div class="user-identity">
                  <span class="user-initial">{{
                    u.username.slice(0, 1).toUpperCase()
                  }}</span>
                  <div>
                    <strong>{{ u.username }}</strong>
                    <small>
                      {{ u.role === "admin" ? "管理员" : "普通用户" }}
                      · {{ u.disabled ? "已禁用" : "正常" }}
                      <template v-if="u.forcePasswordChange">
                        · 登录后需改密</template
                      >
                    </small>
                  </div>
                </div>
                <span class="user-state" :class="{ disabled: u.disabled }">{{
                  u.disabled ? "已禁用" : "已启用"
                }}</span>
                <div class="row-actions user-row-actions">
                  <el-button :icon="Pencil" text @click="openEditUser(u)"
                    >编辑</el-button
                  >
                  <el-button
                    :icon="LockKeyhole"
                    text
                    @click="openUserPassword(u)"
                    >修改密码</el-button
                  >
                  <el-button
                    v-if="u.id !== auth.user?.id"
                    :icon="Trash2"
                    text
                    type="danger"
                    @click="deleteUser(u)"
                    >删除</el-button
                  >
                </div>
              </div>
            </div>
            <div v-else class="empty-small">
              <Users :size="28" /><span>暂无用户</span>
            </div>
          </el-tab-pane>
          <el-tab-pane
            v-if="auth.user?.role === 'admin'"
            label="管理"
            name="admin"
          >
            <div class="stats-grid admin-stats">
              <div>
                <strong>{{ stats.users || 0 }}</strong
                ><span>用户 · {{ stats.activeUsers || 0 }} 活跃</span>
              </div>
              <div>
                <strong>{{ stats.hosts || 0 }}</strong
                ><span>主机</span>
              </div>
              <div>
                <strong>{{ stats.sessions || 0 }}</strong
                ><span>活动 SSH 会话</span>
              </div>
              <div>
                <strong>{{ stats.websockets || 0 }}</strong
                ><span>WebSocket</span>
              </div>
              <div>
                <strong>{{ formatBytes(stats.databaseBytes || 0) }}</strong
                ><span>数据库 · {{ stats.auditEvents || 0 }} 条审计</span>
              </div>
              <div>
                <strong>{{ stats.backups || 0 }}</strong
                ><span>备份</span>
              </div>
            </div>
            <div class="system-meta">
              <span>运行 {{ formatDuration(stats.uptimeSeconds || 0) }}</span
              ><span>{{ stats.goVersion }}</span
              ><span>部署 {{ stats.deploymentID }}</span
              ><span v-if="stats.latestBackupAt"
                >最近备份
                {{ new Date(stats.latestBackupAt).toLocaleString() }}</span
              >
            </div>
            <div class="settings-section">
              <h3>登录安全策略</h3>
              <div class="setting-row">
                <span>密码最小长度</span
                ><el-input-number
                  v-model="policy.passwordMinLength"
                  :min="10"
                  :max="128"
                />
              </div>
              <div class="setting-row">
                <span>失败锁定阈值</span
                ><el-input-number
                  v-model="policy.loginFailureThreshold"
                  :min="3"
                  :max="20"
                />
              </div>
              <div class="setting-row">
                <span>锁定分钟数</span
                ><el-input-number
                  v-model="policy.lockMinutes"
                  :min="1"
                  :max="1440"
                />
              </div>
              <div class="setting-row">
                <span>保持登录天数</span
                ><el-input-number
                  v-model="policy.rememberDays"
                  :min="1"
                  :max="90"
                />
              </div>
              <div class="setting-row">
                <span>新用户强制改密</span
                ><el-switch v-model="policy.forceChangeOnCreate" />
              </div>
              <el-button :icon="ShieldCheck" type="primary" @click="savePolicy"
                >保存安全策略</el-button
              >
            </div>
            <el-button :icon="HardDriveDownload" @click="backup"
              >创建数据库备份</el-button
            >
          </el-tab-pane>
        </el-tabs>
        <ToolsDrawer
          v-if="['snippets', 'forwards', 'data', 'notifications'].includes(tab)"
          :section="tab"
          :hosts="hosts"
          :sessions="sessions"
          @insert="emit('insert', $event)"
          @execute="emit('execute', $event)"
          @batch-execute="forwardBatch"
          @notification-open="emit('notificationOpen', $event)"
        />
      </section>
    </div>
  </el-dialog>
  <el-dialog
    v-model="userDialogOpen"
    class="user-dialog"
    :title="userForm.id ? '编辑用户' : '添加用户'"
    width="min(520px, calc(100vw - 28px))"
    append-to-body
  >
    <el-form label-position="top">
      <el-form-item label="用户名">
        <el-input v-model="userForm.username" autocomplete="off" />
      </el-form-item>
      <el-form-item v-if="!userForm.id" label="初始密码">
        <el-input
          v-model="userForm.password"
          type="password"
          show-password
          autocomplete="new-password"
          :placeholder="`至少 ${policy.passwordMinLength} 位`"
        />
      </el-form-item>
      <el-form-item label="角色">
        <el-segmented
          v-model="userForm.role"
          :disabled="userForm.id === auth.user?.id"
          :options="[
            { label: '普通用户', value: 'user' },
            { label: '管理员', value: 'admin' },
          ]"
        />
      </el-form-item>
      <el-form-item v-if="userForm.id" label="账户状态">
        <div class="user-status-control">
          <el-switch
            v-model="userForm.disabled"
            :disabled="userForm.id === auth.user?.id"
            active-text="禁用"
            inactive-text="启用"
          />
          <small>禁用后该用户无法继续登录。</small>
        </div>
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="userDialogOpen = false">取消</el-button>
      <el-button type="primary" :loading="savingUser" @click="saveUser"
        >保存</el-button
      >
    </template>
  </el-dialog>
  <el-dialog
    v-model="userPasswordDialogOpen"
    class="user-password-dialog"
    title="修改用户密码"
    width="min(480px, calc(100vw - 28px))"
    append-to-body
  >
    <el-form label-position="top">
      <el-form-item label="用户">
        <el-input :model-value="userPasswordForm.username" disabled />
      </el-form-item>
      <el-form-item label="新密码">
        <el-input
          v-model="userPasswordForm.password"
          type="password"
          show-password
          autocomplete="new-password"
          :placeholder="`至少 ${policy.passwordMinLength} 位`"
          @keyup.enter="saveUserPassword"
        />
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="userPasswordDialogOpen = false">取消</el-button>
      <el-button
        type="primary"
        :icon="LockKeyhole"
        :loading="savingUserPassword"
        @click="saveUserPassword"
        >修改密码</el-button
      >
    </template>
  </el-dialog>
</template>
