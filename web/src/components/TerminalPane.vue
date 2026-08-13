<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  watch,
} from "vue";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { SearchAddon } from "@xterm/addon-search";
import { WebLinksAddon } from "@xterm/addon-web-links";
import {
  ArrowDown,
  Search,
  Unplug,
  Wifi,
  Eye,
  Keyboard,
  KeyboardOff,
  X,
} from "@lucide/vue";
import { ElMessage, ElMessageBox } from "element-plus";
import { ApiError, api, json } from "../api";
import { terminalFontFamily } from "../types";
import type { Preferences, TerminalSession } from "../types";
import { terminalXtermTheme } from "../themePresets";
import { reconnectDelay } from "../reconnect";

const props = withDefaults(
  defineProps<{
    session: TerminalSession;
    preferences: Preferences;
    visible: boolean;
    closable?: boolean;
    mobileCtrl?: boolean;
    mobileAlt?: boolean;
    watched?: boolean;
  }>(),
  { closable: false, mobileCtrl: false, mobileAlt: false, watched: false },
);
const emit = defineEmits<{
  status: [id: string, status: string, message?: string];
  title: [id: string, title: string];
  directory: [id: string, path: string];
  close: [];
  modifiersUsed: [];
}>();
const container = ref<HTMLElement>();
const status = ref(props.session.status);
const statusMessage = ref(props.session.lastError || "");
const connected = ref(false);
const controller = ref(false);
const searchOpen = ref(false);
const searchText = ref("");
const bellActive = ref(false);
const hasNewOutput = ref(false);
const historyLocked = ref(false);
let terminal: Terminal | undefined;
let fit: FitAddon | undefined;
let search: SearchAddon | undefined;
let socket: WebSocket | undefined;
let observer: ResizeObserver | undefined;
let reconnectTimer: number | undefined;
let reconnectAttempts = 0;
let heartbeatTimer: number | undefined;
let disposed = false;
let clientID = "";
let replaying = false;
let controlPromptOpen = false;
let quietTimer: number | undefined;
let resizeFrame: number | undefined;
let resizeSettleTimer: number | undefined;
let resizeSocketTimer: number | undefined;
let bottomFrame: number | undefined;
let restoringScrollTimer: number | undefined;
let lastSentCols = 0;
let lastSentRows = 0;
let followOutput = true;
let scrollbarDragging = false;
let wheelScrollActive = false;
let restoringScroll = false;
let followedWrites = 0;
let wheelScrollTimer: number | undefined;
let viewport: HTMLElement | null = null;

const statusLabel = computed(
  () =>
    ({
      attached: "已连接",
      background: "后台运行",
      reconnecting: "重连中",
      auth_required: "等待认证",
      unreachable: "不可达",
      ended: "已结束",
      ownership_error: "所有权异常",
      creating: "连接中",
      host_key_required: "等待确认",
    })[status.value] || status.value,
);

function bytesToBase64(data: string) {
  const bytes = new TextEncoder().encode(data);
  let binary = "";
  for (const b of bytes) binary += String.fromCharCode(b);
  return btoa(binary);
}
function base64ToBytes(data: string) {
  const binary = atob(data);
  const out = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) out[i] = binary.charCodeAt(i);
  return out;
}

function isAtBottom() {
  if (!terminal) return true;
  const buffer = terminal.buffer.active;
  return buffer.baseY === buffer.viewportY;
}

function pauseOutputFollow() {
  historyLocked.value = true;
  followOutput = false;
  if (bottomFrame !== undefined) cancelAnimationFrame(bottomFrame);
}

function resumeOutputFollow() {
  historyLocked.value = false;
  followOutput = true;
  hasNewOutput.value = false;
  scrollToBottom();
}

function settleUserScroll() {
  requestAnimationFrame(() => {
    if (isAtBottom()) resumeOutputFollow();
    else pauseOutputFollow();
  });
}

function finishScrollbarDrag() {
  if (!scrollbarDragging) return;
  scrollbarDragging = false;
  settleUserScroll();
}

function scrollToBottom() {
  if (!terminal || !followOutput) return;
  if (bottomFrame !== undefined) cancelAnimationFrame(bottomFrame);
  bottomFrame = requestAnimationFrame(() => {
    if (!terminal || !followOutput) return;
    restoringScroll = true;
    terminal.scrollToBottom();
    clearTimeout(restoringScrollTimer);
    restoringScrollTimer = window.setTimeout(() => {
      restoringScroll = false;
    }, 130);
  });
}

function writeTerminal(data: Uint8Array, callback?: () => void) {
  if (!terminal) return callback?.();
  const keepBottom = followOutput && !historyLocked.value;
  if (historyLocked.value) hasNewOutput.value = true;
  followedWrites++;
  if (keepBottom) {
    followOutput = true;
  }
  terminal.write(data, () => {
    if (keepBottom) scrollToBottom();
    followedWrites = Math.max(0, followedWrites - 1);
    callback?.();
  });
}

async function ensureAttached() {
  status.value = "reconnecting";
  try {
    await api(`/api/sessions/${props.session.id}/restore`, {
      method: "POST",
      body: json({}),
    });
    connectSocket();
  } catch (error) {
    const message = error instanceof Error ? error.message : "恢复失败";
    if (message.toLowerCase().includes("credential required")) {
      status.value = "auth_required";
      statusMessage.value = "需要重新输入 SSH 凭据";
      try {
        const { value } = await ElMessageBox.prompt(
          "远程 tmux 任务仍在运行，请输入同一远程账号的 SSH 密码。",
          "恢复会话",
          {
            confirmButtonText: "重新附着",
            cancelButtonText: "稍后处理",
            inputType: "password",
            inputValidator: (v) => Boolean(v) || "请输入密码",
          },
        );
        await restoreWith({ secret: value });
        connectSocket();
        return;
      } catch {}
    } else if (
      error instanceof ApiError &&
      (error.body.code === "unknown_host_key" ||
        error.body.code === "host_key_changed")
    ) {
      try {
        await ElMessageBox.confirm(
          `远程主机指纹：\n${error.body.fingerprint}`,
          "确认主机指纹",
          {
            confirmButtonText: "信任并恢复",
            cancelButtonText: "取消",
            type: "warning",
          },
        );
        await restoreWith({ trustFingerprint: error.body.fingerprint || "" });
        connectSocket();
        return;
      } catch {}
    } else {
      status.value =
        props.session.status === "auth_required"
          ? "auth_required"
          : "unreachable";
      statusMessage.value = message;
	  scheduleReconnect();
    }
    emit("status", props.session.id, status.value, statusMessage.value);
  }
}

function scheduleReconnect() {
  if (disposed || status.value === "ended" || status.value === "auth_required") return;
  clearTimeout(reconnectTimer);
  const delay = reconnectDelay(reconnectAttempts);
  reconnectAttempts = Math.min(reconnectAttempts + 1, 5);
  reconnectTimer = window.setTimeout(ensureAttached, delay);
}
async function restoreWith(extra: {
  secret?: string;
  trustFingerprint?: string;
}) {
  await api(`/api/sessions/${props.session.id}/restore`, {
    method: "POST",
    body: json({
      secret: extra.secret || "",
      trustFingerprint: extra.trustFingerprint || "",
    }),
  });
}

function connectSocket() {
  if (disposed) return;
  clearTimeout(reconnectTimer);
  clearInterval(heartbeatTimer);
  socket?.close();
  const protocol = location.protocol === "https:" ? "wss:" : "ws:";
  const current = new WebSocket(
    `${protocol}//${location.host}/ws/sessions/${props.session.id}`,
  );
  socket = current;
  current.onopen = () => {
    if (socket !== current) return;
    lastSentCols = 0;
    lastSentRows = 0;
    connected.value = true;
	reconnectAttempts = 0;
    status.value = "attached";
    emit("status", props.session.id, "attached");
    heartbeatTimer = window.setInterval(() => {
      if (current.readyState === WebSocket.OPEN)
        current.send(JSON.stringify({ type: "ping" }));
    }, 20_000);
  };
  current.onmessage = (event) => {
    if (socket !== current) return;
    const message = JSON.parse(event.data);
    if (message.type === "hello") {
      clientID = message.clientID || "";
      controller.value = Boolean(clientID && message.controller === clientID);
      sendTerminalTheme();
      replaying = true;
      terminal?.reset();
      const finishReplay = () => {
        if (socket !== current) return;
        if (message.truncated)
          terminal?.writeln("\r\n\x1b[33m[部分历史输出已省略]\x1b[0m");
        replaying = false;
        resumeOutputFollow();
        resize();
      };
      if (message.data)
        writeTerminal(base64ToBytes(message.data), finishReplay);
      else finishReplay();
    } else if (message.type === "output" && message.data) {
      writeTerminal(base64ToBytes(message.data));
      scheduleQuietNotification();
    } else if (message.type === "control_granted") {
      controller.value = true;
      sendTerminalTheme();
      ElMessage.success("已取得终端控制权");
      resize();
    } else if (message.type === "control_pending")
      ElMessage.info("已向当前控制设备发送接管请求");
    else if (message.type === "control_denied") {
      controller.value = false;
      ElMessage.warning("当前控制设备拒绝了接管请求");
    } else if (message.type === "controller")
      controller.value = message.controller === clientID;
    else if (
      message.type === "control_request" &&
      message.requester &&
      !controlPromptOpen
    ) {
      controlPromptOpen = true;
      ElMessageBox.confirm(
        "另一台设备请求接管此终端。允许后，当前设备会立即变为只读。",
        "终端控制权",
        {
          confirmButtonText: "允许接管",
          cancelButtonText: "保持控制",
          distinguishCancelAndClose: true,
          type: "warning",
        },
      )
        .then(() =>
          current.send(
            JSON.stringify({
              type: "control_response",
              requester: message.requester,
              approved: true,
            }),
          ),
        )
        .catch(() =>
          current.send(
            JSON.stringify({
              type: "control_response",
              requester: message.requester,
              approved: false,
            }),
          ),
        )
        .finally(() => {
          controlPromptOpen = false;
        });
    } else if (message.type === "status") {
      status.value = message.status;
      statusMessage.value = message.message || "";
      emit("status", props.session.id, message.status, message.message);
    }
  };
  current.onclose = () => {
    if (socket !== current) return;
    clearInterval(heartbeatTimer);
    replaying = false;
    connected.value = false;
    if (!disposed && status.value !== "ended") {
      status.value = "reconnecting";
	  scheduleReconnect();
    }
  };
}

function sendTerminalSize() {
  if (!terminal || socket?.readyState !== WebSocket.OPEN || !controller.value)
    return;
  if (terminal.cols === lastSentCols && terminal.rows === lastSentRows) return;
  socket.send(
    JSON.stringify({
      type: "resize",
      rows: terminal.rows,
      cols: terminal.cols,
    }),
  );
  lastSentCols = terminal.cols;
  lastSentRows = terminal.rows;
}
function sendTerminalTheme() {
  if (socket?.readyState !== WebSocket.OPEN || !controller.value) return;
  const theme = terminalXtermTheme(props.preferences.terminalTheme);
  socket.send(
    JSON.stringify({
      type: "terminal_theme",
      foreground: theme.foreground,
      background: theme.background,
    }),
  );
}
function fitTerminal() {
  if (!props.visible || !fit || !terminal || !container.value) return;
  try {
    const dimensions = fit.proposeDimensions();
    if (!dimensions || dimensions.cols < 2 || dimensions.rows < 1) return;
    const keepBottom = followOutput && !historyLocked.value;
    if (
      dimensions.cols === terminal.cols &&
      dimensions.rows === terminal.rows
    ) {
      if (keepBottom) scrollToBottom();
      sendTerminalSize();
      return;
    }
    terminal.resize(dimensions.cols, dimensions.rows);
    if (keepBottom) {
      terminal.scrollToBottom();
      scrollToBottom();
    }
    clearTimeout(resizeSocketTimer);
    resizeSocketTimer = window.setTimeout(sendTerminalSize, 80);
  } catch {}
}
function resize() {
  if (!props.visible || !fit || !terminal) return;
  clearTimeout(resizeSettleTimer);
  if (resizeFrame !== undefined) cancelAnimationFrame(resizeFrame);
  resizeSettleTimer = window.setTimeout(() => {
    nextTick(() => {
      resizeFrame = requestAnimationFrame(fitTerminal);
    });
  }, 90);
}
function requestControl() {
  if (socket?.readyState === WebSocket.OPEN)
    socket.send(JSON.stringify({ type: "request_control" }));
}
function toggleControl() {
  if (socket?.readyState !== WebSocket.OPEN) return;
  socket.send(
    JSON.stringify({
      type: controller.value ? "release_control" : "request_control",
    }),
  );
}
function sendKey(key: string) {
  if (!controller.value) return requestControl();
  resumeOutputFollow();
  socket?.send(JSON.stringify({ type: "input", data: bytesToBase64(key) }));
}
function modifiedKey(data: string) {
  if (!props.mobileCtrl && !props.mobileAlt) return data;
  const arrows: Record<string, string> = {
    "\x1b[A": "A",
    "\x1b[B": "B",
    "\x1b[C": "C",
    "\x1b[D": "D",
    "\x1b[H": "H",
    "\x1b[F": "F",
  };
  const suffix = arrows[data];
  let output = data;
  if (suffix) {
    const modifier = 1 + (props.mobileAlt ? 2 : 0) + (props.mobileCtrl ? 4 : 0);
    output = `\x1b[1;${modifier}${suffix}`;
  } else {
    if (props.mobileCtrl && data.length === 1) {
      const code = data.toUpperCase().charCodeAt(0);
      if (code >= 64 && code <= 95) output = String.fromCharCode(code & 31);
      else if (data === "?") output = "\x7f";
      else if (data === " ") output = "\x00";
    }
    if (props.mobileAlt) output = "\x1b" + output;
  }
  emit("modifiersUsed");
  return output;
}
function sendModifiedKey(key: string) {
  sendKey(modifiedKey(key));
}
function findNext() {
  if (searchText.value)
    search?.findNext(searchText.value, { caseSensitive: false });
}
function focus() {
  terminal?.focus();
}
function restoreTerminalFocus() {
  nextTick(() => {
    requestAnimationFrame(() => {
      if (!disposed && props.visible && container.value?.isConnected) {
        terminal?.focus();
      }
    });
  });
}
async function sendInput(data: string) {
  if (!data) return;
  if (!controller.value) {
    requestControl();
    ElMessage.info("已请求终端控制权，请再次粘贴");
    return;
  }
  if (
    props.preferences.pasteGuard &&
    (data.includes("\n") || data.includes("\r")) &&
    data.length > 2
  ) {
    try {
      await ElMessageBox.confirm(
        `即将向远程终端粘贴 ${data.split(/\r?\n/).length} 行内容。`,
        "确认多行粘贴",
        {
          confirmButtonText: "发送",
          cancelButtonText: "取消",
          type: "warning",
        },
      );
    } catch {
      return;
    } finally {
      restoreTerminalFocus();
    }
  }
  if (socket?.readyState === WebSocket.OPEN) {
    resumeOutputFollow();
    socket.send(JSON.stringify({ type: "input", data: bytesToBase64(data) }));
  }
}
async function copySelection() {
  const selected = terminal?.getSelection() || "";
  if (!selected) return ElMessage.info("请先选中终端内容");
  try {
    await navigator.clipboard.writeText(selected);
  } catch {
    const input = document.createElement("textarea");
    input.value = selected;
    input.style.position = "fixed";
    input.style.opacity = "0";
    document.body.appendChild(input);
    input.select();
    const copied = document.execCommand("copy");
    input.remove();
    if (!copied) return ElMessage.error("浏览器禁止访问剪贴板");
  }
  ElMessage.success("已复制");
}
async function pasteClipboard() {
  try {
    let text = "";
    try {
      text = await navigator.clipboard.readText();
    } catch {
      try {
        const result = await ElMessageBox.prompt(
          "浏览器无法直接读取剪贴板，请在此粘贴内容。",
          "粘贴到终端",
          {
            confirmButtonText: "发送",
            cancelButtonText: "取消",
            inputType: "textarea",
          },
        );
        text = result.value;
      } catch {
        return;
      }
    }
    await sendInput(text);
  } finally {
    restoreTerminalFocus();
  }
}
function selectAll() {
  terminal?.selectAll();
  terminal?.focus();
}
function clearTerminal() {
  terminal?.clear();
  terminal?.focus();
}
function downloadText() {
  if (!terminal) return;
  const lines: string[] = [];
  const buffer = terminal.buffer.active;
  for (let index = 0; index < buffer.length; index++)
    lines.push(buffer.getLine(index)?.translateToString(true) || "");
  const blob = new Blob([lines.join("\n")], {
    type: "text/plain;charset=utf-8",
  });
  const link = document.createElement("a");
  link.href = URL.createObjectURL(blob);
  link.download = `${props.session.name.replace(/[^a-zA-Z0-9._-]+/g, "_") || "terminal"}-${new Date().toISOString().replace(/[:.]/g, "-")}.txt`;
  link.click();
  window.setTimeout(() => URL.revokeObjectURL(link.href), 0);
}
function ringBell() {
  if (props.preferences.visualBell) {
    bellActive.value = true;
    window.setTimeout(() => (bellActive.value = false), 220);
  }
  if (props.preferences.soundBell) {
    try {
      const AudioContextClass =
        window.AudioContext || (window as any).webkitAudioContext;
      const context = new AudioContextClass(),
        oscillator = context.createOscillator(),
        gain = context.createGain();
      oscillator.frequency.value = 740;
      gain.gain.value = 0.035;
      oscillator.connect(gain);
      gain.connect(context.destination);
      oscillator.start();
      gain.gain.exponentialRampToValueAtTime(0.001, context.currentTime + 0.1);
      oscillator.stop(context.currentTime + 0.11);
    } catch {}
  }
  api("/api/notifications", {
    method: "POST",
    body: json({ sessionID: props.session.id, kind: "bell" }),
  }).catch(() => {});
  if (
    props.preferences.browserNotifications &&
    document.hidden &&
    "Notification" in window &&
    Notification.permission === "granted"
  ) {
    const notice = new Notification("Velin 终端响铃", {
      body: props.session.name,
      tag: `velin-${props.session.id}`,
    });
    notice.onclick = () => {
      window.focus();
      notice.close();
    };
  }
}
function scheduleQuietNotification() {
  if (!props.watched || replaying) return;
  clearTimeout(quietTimer);
  quietTimer = window.setTimeout(() => {
    api("/api/notifications", {
      method: "POST",
      body: json({ sessionID: props.session.id, kind: "quiet" }),
    }).catch(() => {});
    if (
      props.preferences.browserNotifications &&
      document.hidden &&
      "Notification" in window &&
      Notification.permission === "granted"
    ) {
      const notice = new Notification("Velin 任务已静默", {
        body: props.session.name,
        tag: `velin-quiet-${props.session.id}`,
      });
      notice.onclick = () => {
        window.focus();
        notice.close();
      };
    }
  }, 30_000);
}

onMounted(() => {
  terminal = new Terminal({
    cursorBlink: props.preferences.cursorBlink,
    cursorStyle: props.preferences.cursorStyle,
    fontSize: props.preferences.fontSize,
    lineHeight: props.preferences.lineHeight,
    fontFamily: terminalFontFamily,
    fontWeight: props.preferences.fontWeight,
    letterSpacing: props.preferences.letterSpacing,
    theme: terminalXtermTheme(props.preferences.terminalTheme),
    scrollback: 5000,
    scrollOnUserInput: true,
    smoothScrollDuration: 90,
    allowProposedApi: false,
  });
  fit = new FitAddon();
  search = new SearchAddon();
  terminal.loadAddon(fit);
  terminal.loadAddon(search);
  terminal.loadAddon(new WebLinksAddon());
  terminal.open(container.value!);
  viewport = container.value!.querySelector<HTMLElement>(".xterm-viewport");
  viewport?.addEventListener("pointerdown", (event) => {
    const rect = (event.currentTarget as HTMLElement).getBoundingClientRect();
    if (event.clientX >= rect.right - 14) {
      scrollbarDragging = true;
      pauseOutputFollow();
    }
  });
  window.addEventListener("pointerup", finishScrollbarDrag);
  window.addEventListener("pointercancel", finishScrollbarDrag);
  terminal.parser.registerOscHandler(7, (value) => {
    try {
      const url = new URL(value);
      if (url.protocol !== "file:") return false;
      emit("directory", props.session.id, decodeURIComponent(url.pathname));
      return true;
    } catch {
      return false;
    }
  });
  terminal.attachCustomWheelEventHandler((event) => {
    wheelScrollActive = true;
    clearTimeout(wheelScrollTimer);
    if (event.deltaY < 0) pauseOutputFollow();
    wheelScrollTimer = window.setTimeout(() => {
      wheelScrollActive = false;
      settleUserScroll();
    }, 140);
    return true;
  });
  terminal.onScroll(() => {
    if (
      restoringScroll ||
      followedWrites > 0 ||
      (!scrollbarDragging && !wheelScrollActive)
    )
      return;
    if (!isAtBottom()) pauseOutputFollow();
  });
  terminal.onWriteParsed(() => {
    if (followOutput) scrollToBottom();
  });
  terminal.onTitleChange((title) =>
    emit(
      "title",
      props.session.id,
      title.replace(/[\u0000-\u001f\u007f]/g, "").slice(0, 80),
    ),
  );
  terminal.onBell(ringBell);
  terminal.onData(async (data) => {
    if (replaying) return;
    await sendInput(modifiedKey(data));
  });
  observer = new ResizeObserver(resize);
  observer.observe(container.value!);
  ensureAttached();
});

watch(
  () => props.visible,
  (visible) => {
    if (visible) resize();
  },
);
watch(
  () => props.watched,
  (watched) => {
    if (!watched) clearTimeout(quietTimer);
  },
);
watch(
  () => props.preferences,
  (value) => {
    if (terminal) {
      terminal.options.fontSize = value.fontSize;
      terminal.options.lineHeight = value.lineHeight;
      terminal.options.fontWeight = value.fontWeight;
      terminal.options.letterSpacing = value.letterSpacing;
      terminal.options.cursorStyle = value.cursorStyle;
      terminal.options.cursorBlink = value.cursorBlink;
      terminal.options.theme = terminalXtermTheme(value.terminalTheme);
      sendTerminalTheme();
      resize();
    }
  },
  { deep: true },
);
onBeforeUnmount(() => {
  disposed = true;
  clearTimeout(reconnectTimer);
  clearTimeout(quietTimer);
  clearTimeout(resizeSettleTimer);
  clearTimeout(resizeSocketTimer);
  clearTimeout(restoringScrollTimer);
  clearTimeout(wheelScrollTimer);
  clearInterval(heartbeatTimer);
  if (resizeFrame !== undefined) cancelAnimationFrame(resizeFrame);
  if (bottomFrame !== undefined) cancelAnimationFrame(bottomFrame);
  observer?.disconnect();
  window.removeEventListener("pointerup", finishScrollbarDrag);
  window.removeEventListener("pointercancel", finishScrollbarDrag);
  socket?.close();
  terminal?.dispose();
});
defineExpose({
  resize,
  focus,
  sendKey,
  sendModifiedKey,
  sendInput,
  copySelection,
  pasteClipboard,
  selectAll,
  clearTerminal,
  downloadText,
});
</script>

<template>
  <section
    class="terminal-pane"
    :class="{ inactive: !visible, 'terminal-bell': bellActive }"
    :style="{ backgroundColor: preferences.background }"
  >
    <div class="terminal-statusbar">
      <div class="status-left">
        <span class="status-dot" :class="status"></span
        ><strong>{{ session.name }}</strong
        ><span>{{ statusLabel }}</span>
      </div>
      <div class="terminal-tools">
        <el-tooltip :content="controller ? '释放控制权' : '请求控制权'"
          ><button
            class="icon-btn"
            :class="{ active: controller }"
            @click="toggleControl"
          >
            <component
              :is="controller ? KeyboardOff : Keyboard"
              :size="15"
            /></button
        ></el-tooltip>
        <el-tooltip content="搜索终端"
          ><button class="icon-btn" @click="searchOpen = !searchOpen">
            <Search :size="15" /></button
        ></el-tooltip>
        <el-tooltip v-if="closable" content="终止并关闭"
          ><button class="icon-btn pane-close" @click="emit('close')">
            <X :size="15" /></button
        ></el-tooltip>
        <component
          :is="connected ? Wifi : Unplug"
          :size="14"
          class="connection-icon"
        />
      </div>
    </div>
    <div v-if="searchOpen" class="terminal-search">
      <el-input
        v-model="searchText"
        size="small"
        placeholder="搜索"
        @keyup.enter="findNext"
      /><el-button size="small" @click="findNext">下一个</el-button>
    </div>
    <div v-if="!controller && connected" class="readonly-banner">
      <Eye :size="14" /> 只读观看，点击键盘图标请求控制
    </div>
    <div
      ref="container"
      class="terminal-canvas"
    ></div>
    <button
      v-if="historyLocked && hasNewOutput"
      class="terminal-new-output"
      title="跳转到最新输出"
      @click="resumeOutputFollow"
    >
      <ArrowDown :size="14" />
      <span>有新消息</span>
    </button>
    <div v-if="!connected && statusMessage" class="terminal-error">
      <strong>{{ statusLabel }}</strong
      ><span>{{ statusMessage }}</span
      ><el-button size="small" @click="ensureAttached">重新连接</el-button>
    </div>
  </section>
</template>
