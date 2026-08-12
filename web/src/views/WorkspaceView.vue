<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Archive, ChevronRight, Columns2, Grid2X2, KeyRound, Menu, Monitor, MoreHorizontal, PanelLeftClose, Plus, Search, Server, Settings, Square, Star, TerminalSquare, Trash2, X } from '@lucide/vue'
import { ApiError, api, json } from '../api'
import { useAuthStore } from '../stores/auth'
import type { Credential, Host, Preferences, TerminalSession, WorkspaceLayout } from '../types'
import TerminalPane from '../components/TerminalPane.vue'
import HostDialog from '../components/HostDialog.vue'
import CredentialDialog from '../components/CredentialDialog.vue'
import SettingsDrawer from '../components/SettingsDrawer.vue'

const router=useRouter(),auth=useAuthStore()
const hosts=ref<Host[]>([]),credentials=ref<Credential[]>([]),sessions=ref<TerminalSession[]>([])
const search=ref(''),section=ref<'hosts'|'sessions'>('hosts'),sidebarOpen=ref(true),mobileSidebar=ref(false)
const hostDialog=ref(false),credentialDialog=ref(false),settingsOpen=ref(false),editingHost=ref<Host>()
const connecting=ref<string>(),paneRefs=ref<any[]>([]),workspaceVersion=ref(0),saveTimer=ref<number>()
const preferences=reactive<Preferences>({theme:'dark',fontSize:14,lineHeight:1.25,cursorStyle:'block',cursorBlink:true,pasteGuard:true})
const layout=reactive<WorkspaceLayout>({tabs:[],panes:[],split:'single'})

const visibleHosts=computed(()=>{const q=search.value.toLowerCase();return hosts.value.filter(h=>!q||`${h.name} ${h.address} ${h.groupName} ${h.tags}`.toLowerCase().includes(q))})
const backgroundSessions=computed(()=>sessions.value.filter(s=>!layout.tabs.includes(s.id)&&s.status!=='ended'))
const activeSessions=computed(()=>layout.panes.map(id=>sessions.value.find(s=>s.id===id)).filter(Boolean) as TerminalSession[])
const paneLimit=computed(()=>layout.split==='single'?1:layout.split==='grid'?4:2)

async function load(){
  try{
    const [h,c,s,w,p]=await Promise.all([api<Host[]>('/api/hosts'),api<Credential[]>('/api/credentials'),api<TerminalSession[]>('/api/sessions'),api<{layout:WorkspaceLayout;version:number}>('/api/workspace'),api<Partial<Preferences>>('/api/preferences')])
    hosts.value=h;credentials.value=c;sessions.value=s;workspaceVersion.value=w.version;Object.assign(preferences,p)
    const saved=w.layout||{};layout.tabs=Array.isArray(saved.tabs)?saved.tabs.filter(id=>s.some(x=>x.id===id)):[];layout.panes=Array.isArray(saved.panes)?saved.panes.filter(id=>s.some(x=>x.id===id)):[];layout.active=saved.active;layout.split=saved.split||'single'
    if(!layout.panes.length&&layout.tabs.length)layout.panes=[layout.tabs[0]]
  }catch(e){ElMessage.error(e instanceof Error?e.message:'加载工作区失败')}
}

async function connect(host:Host,trustFingerprint='',temporary?:{secret:string;passphrase:string}){
  connecting.value=host.id
  try{
    const body={hostID:host.id,credentialID:host.credentialID,secret:temporary?.secret||'',passphrase:temporary?.passphrase||'',trustFingerprint,name:host.name}
    const session=await api<TerminalSession>('/api/sessions',{method:'POST',body:json(body)});sessions.value.unshift(session);openSession(session)
  }catch(e){
    if(e instanceof ApiError&&(e.body.code==='unknown_host_key'||e.body.code==='host_key_changed')){try{await ElMessageBox.confirm(`${e.body.code==='host_key_changed'?'主机指纹发生变化':'首次连接需要信任主机指纹'}：\n${e.body.fingerprint}`,'确认主机指纹',{confirmButtonText:'信任并连接',cancelButtonText:'取消',type:'warning'});await connect(host,e.body.fingerprint||'',temporary)}catch{}}
    else if(e instanceof Error&&e.message.includes('credential required')) await promptTemporary(host)
    else ElMessage.error(e instanceof Error?e.message:'连接失败')
  }finally{connecting.value=undefined}
}

async function promptTemporary(host:Host){try{const {value}=await ElMessageBox.prompt(`输入 ${host.username}@${host.address} 的 SSH 密码`,'临时凭据',{confirmButtonText:'连接',cancelButtonText:'取消',inputType:'password',inputValidator:v=>Boolean(v)||'请输入密码'});await connect(host,'',{secret:value,passphrase:''})}catch{}}
function openSession(s:TerminalSession){if(!layout.tabs.includes(s.id))layout.tabs.push(s.id);layout.active=s.id;if(!layout.panes.includes(s.id)){if(layout.panes.length<paneLimit.value)layout.panes.push(s.id);else layout.panes.splice(0,1,s.id)};mobileSidebar.value=false;scheduleSave()}
function activate(id:string){layout.active=id;if(!layout.panes.includes(id)){if(layout.split==='single')layout.panes=[id];else layout.panes.splice(0,1,id)};scheduleSave()}
function moveBackground(id:string){layout.tabs=layout.tabs.filter(x=>x!==id);layout.panes=layout.panes.filter(x=>x!==id);if(layout.active===id)layout.active=layout.tabs[0];fillPanes();scheduleSave()}
async function closeTab(session:TerminalSession){
  if(session.status==='ended'){removeLocal(session.id);return}
  try{await ElMessageBox.confirm(`关闭将终止 ${session.name} 的远程 tmux 会话及其中任务。`,'终止并关闭终端',{confirmButtonText:'终止并关闭',cancelButtonText:'取消',distinguishCancelAndClose:true,type:'warning',showClose:false});await api(`/api/sessions/${session.id}`,{method:'DELETE',body:json({})});session.status='ended';removeLocal(session.id);ElMessage.success('远程会话已终止')}
  catch(e:any){
    if(e==='cancel'||e==='close')return
    if(e instanceof Error&&e.message.toLowerCase().includes('credential required')){
      try{const {value}=await ElMessageBox.prompt('请输入创建该 tmux 会话时使用的 SSH 密码。','认证后终止',{confirmButtonText:'终止并关闭',cancelButtonText:'取消',inputType:'password',inputValidator:v=>Boolean(v)||'请输入密码'});await api(`/api/sessions/${session.id}`,{method:'DELETE',body:json({secret:value})});session.status='ended';removeLocal(session.id);ElMessage.success('远程会话已终止');return}catch(inner:any){if(inner==='cancel'||inner==='close')return;ElMessage.error(inner instanceof Error?inner.message:'终止失败，标签已保留');return}
    }
    ElMessage.error(e instanceof Error?e.message:'终止失败，标签已保留')
  }
}
function removeLocal(id:string){layout.tabs=layout.tabs.filter(x=>x!==id);layout.panes=layout.panes.filter(x=>x!==id);if(layout.active===id)layout.active=layout.tabs[0];fillPanes();scheduleSave()}
function fillPanes(){while(layout.panes.length<paneLimit.value){const next=layout.tabs.find(id=>!layout.panes.includes(id));if(!next)break;layout.panes.push(next)}layout.panes=layout.panes.slice(0,paneLimit.value)}
function setSplit(split:WorkspaceLayout['split']){layout.split=split;fillPanes();nextTick(()=>paneRefs.value.forEach(p=>p?.resize()));scheduleSave()}
function updateStatus(id:string,status:string,message?:string){const s=sessions.value.find(x=>x.id===id);if(s){s.status=status as any;s.lastError=message||''}}
function scheduleSave(){clearTimeout(saveTimer.value);saveTimer.value=window.setTimeout(saveWorkspace,500)}
async function saveWorkspace(){try{const result=await api<{version:number}>('/api/workspace',{method:'PUT',body:json({layout:{tabs:layout.tabs,panes:layout.panes,active:layout.active,split:layout.split},version:workspaceVersion.value})});workspaceVersion.value=result.version}catch(e){if(e instanceof ApiError&&e.body.code==='workspace_conflict'){const w=await api<{version:number}>('/api/workspace');workspaceVersion.value=w.version;scheduleSave()}}}
function editHost(host?:Host){editingHost.value=host?{...host}:undefined;hostDialog.value=true}
async function deleteHost(host:Host){try{await ElMessageBox.confirm(`删除主机“${host.name}”？`,'删除主机',{confirmButtonText:'删除',cancelButtonText:'取消',type:'warning'});await api(`/api/hosts/${host.id}`,{method:'DELETE'});hosts.value=hosts.value.filter(h=>h.id!==host.id)}catch(e:any){if(e!=='cancel'&&e!=='close')ElMessage.error(e instanceof Error?e.message:'删除失败')}}
function hostSaved(host:Host){const i=hosts.value.findIndex(h=>h.id===host.id);if(i>=0)hosts.value[i]=host;else hosts.value.push(host)}
function sessionHost(s:TerminalSession){return hosts.value.find(h=>h.id===s.hostID)}
function statusColor(status:string){return ['attached'].includes(status)?'online':['background'].includes(status)?'idle':['ended'].includes(status)?'off':'warning'}
async function logout(){await auth.logout();router.replace('/login')}
watch(()=>layout.split,fillPanes)
onMounted(load)
</script>

<template>
  <main class="workspace-shell">
    <aside class="sidebar" :class="{collapsed:!sidebarOpen,'mobile-open':mobileSidebar}">
      <header class="sidebar-header"><div class="brand-mini"><span class="brand-mark"><TerminalSquare :size="20"/></span><strong>Velin</strong></div><button class="icon-btn desktop-only" @click="sidebarOpen=!sidebarOpen"><PanelLeftClose :size="18"/></button><button class="icon-btn mobile-only" @click="mobileSidebar=false"><X :size="18"/></button></header>
      <template v-if="sidebarOpen||mobileSidebar"><div class="resource-switch"><button :class="{active:section==='hosts'}" @click="section='hosts'"><Server :size="16"/>主机</button><button :class="{active:section==='sessions'}" @click="section='sessions'"><Monitor :size="16"/>会话<span v-if="backgroundSessions.length">{{backgroundSessions.length}}</span></button></div><div class="sidebar-search"><Search :size="15"/><input v-model="search" placeholder="搜索主机"/><button class="icon-btn" @click="editHost()"><Plus :size="17"/></button></div><div v-if="section==='hosts'" class="resource-list"><div v-if="!visibleHosts.length" class="empty-small"><Server :size="28"/><span>暂无主机</span><button @click="editHost()">新增主机</button></div><article v-for="host in visibleHosts" :key="host.id" class="host-row" @dblclick="connect(host)"><span class="host-icon">{{host.name.slice(0,1).toUpperCase()}}</span><div class="host-copy"><strong>{{host.name}}</strong><small>{{host.username}}@{{host.address}}:{{host.port}}</small></div><Star v-if="host.favorite" :size="13" class="favorite"/><el-dropdown trigger="click" @click.stop><button class="icon-btn row-menu"><MoreHorizontal :size="16"/></button><template #dropdown><el-dropdown-menu><el-dropdown-item @click="connect(host)">连接</el-dropdown-item><el-dropdown-item @click="editHost(host)">编辑</el-dropdown-item><el-dropdown-item divided @click="deleteHost(host)">删除</el-dropdown-item></el-dropdown-menu></template></el-dropdown></article></div><div v-else class="resource-list"><article v-for="session in sessions" :key="session.id" class="session-row" @click="openSession(session)"><span class="status-dot" :class="statusColor(session.status)"></span><div class="host-copy"><strong>{{session.name}}</strong><small>{{session.status}}<template v-if="sessionHost(session)"> · {{sessionHost(session)?.address}}</template></small></div><Archive v-if="!layout.tabs.includes(session.id)&&session.status!=='ended'" :size="14"/></article></div><footer class="sidebar-footer"><button @click="credentialDialog=true"><KeyRound :size="16"/><span>凭据</span></button><button @click="settingsOpen=true"><Settings :size="16"/><span>设置</span></button><span class="user-avatar">{{auth.user?.username.slice(0,1).toUpperCase()}}</span></footer></template><button v-else class="collapsed-open" @click="sidebarOpen=true"><ChevronRight :size="18"/></button>
    </aside>
    <section class="workspace-main">
      <header class="tabbar"><button class="icon-btn mobile-only" @click="mobileSidebar=true"><Menu :size="18"/></button><div class="tabs-scroll"><div v-for="id in layout.tabs" :key="id" class="terminal-tab" :class="{active:layout.active===id}" @click="activate(id)"><span class="status-dot" :class="statusColor(sessions.find(s=>s.id===id)?.status||'ended')"></span><span>{{sessions.find(s=>s.id===id)?.name||'已结束会话'}}</span><el-dropdown trigger="click" @click.stop><button class="tab-close" title="会话操作"><MoreHorizontal :size="13"/></button><template #dropdown><el-dropdown-menu><el-dropdown-item :icon="Archive" @click="moveBackground(id)">移到后台</el-dropdown-item><el-dropdown-item divided :icon="Trash2" @click="sessions.find(s=>s.id===id)&&closeTab(sessions.find(s=>s.id===id)!)">终止并关闭</el-dropdown-item></el-dropdown-menu></template></el-dropdown><button class="tab-close danger" title="终止并关闭" @click.stop="sessions.find(s=>s.id===id)&&closeTab(sessions.find(s=>s.id===id)!)"><X :size="13"/></button></div><button class="new-tab" title="新增主机" @click="editHost()"><Plus :size="16"/></button></div><div class="layout-controls"><el-tooltip content="单窗口"><button class="icon-btn" :class="{active:layout.split==='single'}" @click="setSplit('single')"><Square :size="16"/></button></el-tooltip><el-tooltip content="双窗口"><button class="icon-btn" :class="{active:['horizontal','vertical'].includes(layout.split||'')}" @click="setSplit(layout.split==='horizontal'?'vertical':'horizontal')"><Columns2 :size="17"/></button></el-tooltip><el-tooltip content="四窗口"><button class="icon-btn" :class="{active:layout.split==='grid'}" @click="setSplit('grid')"><Grid2X2 :size="17"/></button></el-tooltip><button class="icon-btn" @click="settingsOpen=true"><Settings :size="17"/></button></div></header>
      <div v-if="activeSessions.length" class="terminal-grid" :class="`layout-${layout.split}`"><TerminalPane v-for="(session,index) in activeSessions" :key="session.id" ref="paneRefs" :session="session" :preferences="preferences" :visible="layout.panes.includes(session.id)" @status="updateStatus"/><div v-for="n in Math.max(0,paneLimit-activeSessions.length)" :key="`empty-${n}`" class="empty-pane"><TerminalSquare :size="25"/><span>从标签或主机列表选择会话</span></div></div>
      <div v-else class="workspace-empty"><div class="empty-terminal-mark"><TerminalSquare :size="34"/></div><h2>没有打开的终端</h2><p>从左侧主机列表双击连接，或新建一台主机。</p><el-button type="primary" :icon="Plus" @click="editHost()">新增主机</el-button></div>
      <div v-if="activeSessions.length" class="mobile-keybar"><button @click="paneRefs[0]?.sendKey('\x1b')">Esc</button><button @click="paneRefs[0]?.sendKey('\t')">Tab</button><button @click="paneRefs[0]?.sendKey('\x03')">Ctrl+C</button><button @click="paneRefs[0]?.sendKey('\x04')">Ctrl+D</button><button @click="paneRefs[0]?.sendKey('\x1b[A')">↑</button><button @click="paneRefs[0]?.sendKey('\x1b[B')">↓</button><button @click="paneRefs[0]?.sendKey('\x1b[D')">←</button><button @click="paneRefs[0]?.sendKey('\x1b[C')">→</button></div>
    </section>
    <div v-if="mobileSidebar" class="mobile-overlay" @click="mobileSidebar=false"></div>
    <HostDialog v-model="hostDialog" :host="editingHost" :credentials="credentials" @saved="hostSaved"/>
    <CredentialDialog v-model="credentialDialog" @saved="credentials.push($event)"/>
    <SettingsDrawer v-model="settingsOpen" :preferences="preferences" @preferences="Object.assign(preferences,$event)" @logout="logout"/>
  </main>
</template>
