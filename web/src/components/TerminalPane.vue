<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { SearchAddon } from '@xterm/addon-search'
import { WebLinksAddon } from '@xterm/addon-web-links'
import { Maximize2, Search, Unplug, Wifi, Eye, Keyboard } from '@lucide/vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ApiError, api, json } from '../api'
import type { Preferences, TerminalSession } from '../types'

const props = defineProps<{ session: TerminalSession; preferences: Preferences; visible: boolean }>()
const emit = defineEmits<{ status: [id: string, status: string, message?: string] }>()
const container = ref<HTMLElement>()
const status = ref(props.session.status)
const statusMessage = ref(props.session.lastError || '')
const connected = ref(false)
const controller = ref(false)
const searchOpen = ref(false)
const searchText = ref('')
let terminal: Terminal | undefined
let fit: FitAddon | undefined
let search: SearchAddon | undefined
let socket: WebSocket | undefined
let observer: ResizeObserver | undefined
let reconnectTimer: number | undefined
let disposed = false
let clientID = ''

const statusLabel = computed(() => ({ attached:'已连接', background:'后台运行', reconnecting:'重连中', auth_required:'等待认证', unreachable:'不可达', ended:'已结束', ownership_error:'所有权异常', creating:'连接中', host_key_required:'等待确认' }[status.value] || status.value))

function bytesToBase64(data: string) {
  const bytes = new TextEncoder().encode(data)
  let binary = ''; for (const b of bytes) binary += String.fromCharCode(b)
  return btoa(binary)
}
function base64ToBytes(data: string) { const binary = atob(data); const out = new Uint8Array(binary.length); for (let i=0;i<binary.length;i++) out[i]=binary.charCodeAt(i); return out }

async function ensureAttached() {
  status.value = 'reconnecting'
  try { await api(`/api/sessions/${props.session.id}/restore`, { method: 'POST', body: json({}) }); connectSocket() }
  catch (error) {
    const message = error instanceof Error ? error.message : '恢复失败'
    if (message.toLowerCase().includes('credential required')) {
      status.value = 'auth_required'; statusMessage.value = '需要重新输入 SSH 凭据'
      try {
        const { value } = await ElMessageBox.prompt('远程 tmux 任务仍在运行，请输入同一远程账号的 SSH 密码。', '恢复会话', { confirmButtonText:'重新附着', cancelButtonText:'稍后处理', inputType:'password', inputValidator:v=>Boolean(v)||'请输入密码' })
        await restoreWith({ secret:value }); connectSocket(); return
      } catch {}
    } else if (error instanceof ApiError && (error.body.code === 'unknown_host_key' || error.body.code === 'host_key_changed')) {
      try {
        await ElMessageBox.confirm(`远程主机指纹：\n${error.body.fingerprint}`, '确认主机指纹', { confirmButtonText:'信任并恢复', cancelButtonText:'取消', type:'warning' })
        await restoreWith({ trustFingerprint:error.body.fingerprint || '' }); connectSocket(); return
      } catch {}
    } else { status.value = props.session.status === 'auth_required' ? 'auth_required' : 'unreachable'; statusMessage.value = message }
    emit('status', props.session.id, status.value, statusMessage.value)
  }
}
async function restoreWith(extra: {secret?:string;trustFingerprint?:string}) { await api(`/api/sessions/${props.session.id}/restore`, { method:'POST', body:json({ secret:extra.secret||'', trustFingerprint:extra.trustFingerprint||'' }) }) }

function connectSocket() {
  if (disposed) return
  socket?.close()
  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:'
  socket = new WebSocket(`${protocol}//${location.host}/ws/sessions/${props.session.id}`)
  socket.onopen = () => { connected.value = true; status.value = 'attached'; resize(); emit('status', props.session.id, 'attached') }
  socket.onmessage = (event) => {
    const message = JSON.parse(event.data)
    if (message.type === 'hello') {
      clientID = message.clientID || ''
      controller.value = Boolean(clientID && message.controller === clientID)
      if (message.data) terminal?.write(base64ToBytes(message.data))
      if (message.truncated) terminal?.writeln('\r\n\x1b[33m[部分历史输出已省略]\x1b[0m')
      resize()
    } else if (message.type === 'output' && message.data) terminal?.write(base64ToBytes(message.data))
    else if (message.type === 'control_granted') { controller.value = true; ElMessage.success('已取得终端控制权'); resize() }
    else if (message.type === 'control_denied') { controller.value = false; ElMessage.warning('当前终端正由另一设备控制') }
    else if (message.type === 'controller') controller.value = message.controller === clientID
    else if (message.type === 'status') { status.value = message.status; statusMessage.value = message.message || ''; emit('status', props.session.id, message.status, message.message) }
  }
  socket.onclose = () => { connected.value = false; if (!disposed && status.value !== 'ended') { status.value = 'reconnecting'; reconnectTimer = window.setTimeout(ensureAttached, 1800) } }
}

function resize() { if (!props.visible || !fit || !terminal) return; nextTick(() => { try { fit?.fit(); if (socket?.readyState === WebSocket.OPEN && controller.value) socket.send(JSON.stringify({ type:'resize', rows:terminal?.rows, cols:terminal?.cols })) } catch {} }) }
function requestControl() { if (socket?.readyState === WebSocket.OPEN) socket.send(JSON.stringify({ type:'request_control' })) }
function sendKey(key: string) { if (!controller.value) return requestControl(); socket?.send(JSON.stringify({ type:'input', data:bytesToBase64(key) })) }
function findNext() { if (searchText.value) search?.findNext(searchText.value, { caseSensitive:false }) }

onMounted(() => {
  terminal = new Terminal({ cursorBlink:props.preferences.cursorBlink, cursorStyle:props.preferences.cursorStyle, fontSize:props.preferences.fontSize, lineHeight:props.preferences.lineHeight, fontFamily:'"JetBrains Mono", "Cascadia Code", Menlo, Consolas, monospace', theme:{ background:'#111416', foreground:'#d8ded9', cursor:'#8fd6a7', selectionBackground:'#405347', black:'#202523', red:'#e8776f', green:'#8fd6a7', yellow:'#d6b56f', blue:'#75a9d6', magenta:'#b99bd8', cyan:'#78c5c4', white:'#d8ded9', brightBlack:'#66706a' }, scrollback:5000, allowProposedApi:false })
  fit = new FitAddon(); search = new SearchAddon(); terminal.loadAddon(fit); terminal.loadAddon(search); terminal.loadAddon(new WebLinksAddon()); terminal.open(container.value!)
  terminal.onData(async data => {
    if (!controller.value) return requestControl()
    if (props.preferences.pasteGuard && (data.includes('\n') || data.includes('\r')) && data.length > 2) {
      try { await ElMessageBox.confirm(`即将向远程终端粘贴 ${data.split(/\r?\n/).length} 行内容。`, '确认多行粘贴', { confirmButtonText:'发送', cancelButtonText:'取消', type:'warning' }) } catch { return }
    }
    if (socket?.readyState === WebSocket.OPEN) socket.send(JSON.stringify({ type:'input', data:bytesToBase64(data) }))
  })
  observer = new ResizeObserver(resize); observer.observe(container.value!); ensureAttached()
})

watch(() => props.visible, visible => { if (visible) resize() })
watch(() => props.preferences, value => { if (terminal) { terminal.options.fontSize=value.fontSize; terminal.options.lineHeight=value.lineHeight; terminal.options.cursorStyle=value.cursorStyle; terminal.options.cursorBlink=value.cursorBlink; resize() } }, { deep:true })
onBeforeUnmount(() => { disposed=true; clearTimeout(reconnectTimer); observer?.disconnect(); socket?.close(); terminal?.dispose() })
defineExpose({ resize, sendKey })
</script>

<template>
  <section class="terminal-pane" :class="{ inactive: !visible }">
    <div class="terminal-statusbar">
      <div class="status-left"><span class="status-dot" :class="status"></span><strong>{{ session.name }}</strong><span>{{ statusLabel }}</span></div>
      <div class="terminal-tools">
        <el-tooltip :content="controller ? '当前设备拥有输入权' : '请求控制权'"><button class="icon-btn" :class="{ active: controller }" @click="requestControl"><Keyboard :size="15" /></button></el-tooltip>
        <el-tooltip content="搜索终端"><button class="icon-btn" @click="searchOpen=!searchOpen"><Search :size="15" /></button></el-tooltip>
        <el-tooltip content="适应窗口"><button class="icon-btn" @click="resize"><Maximize2 :size="15" /></button></el-tooltip>
        <component :is="connected ? Wifi : Unplug" :size="14" class="connection-icon" />
      </div>
    </div>
    <div v-if="searchOpen" class="terminal-search"><el-input v-model="searchText" size="small" placeholder="搜索" @keyup.enter="findNext" /><el-button size="small" @click="findNext">下一个</el-button></div>
    <div v-if="!controller && connected" class="readonly-banner"><Eye :size="14" /> 只读观看，点击键盘图标请求控制</div>
    <div ref="container" class="terminal-canvas"></div>
    <div v-if="!connected && statusMessage" class="terminal-error"><strong>{{ statusLabel }}</strong><span>{{ statusMessage }}</span><el-button size="small" @click="ensureAttached">重新连接</el-button></div>
  </section>
</template>
