<script setup lang="ts">
import { computed, defineAsyncComponent, ref, watch } from "vue";
import {
  Download,
  File,
  FilePenLine,
  Folder,
  FolderPlus,
  MoreVertical,
  Pencil,
  RefreshCw,
  Trash2,
  Upload,
} from "@lucide/vue";
import { ElMessage, ElMessageBox } from "element-plus";
import { api, json } from "../api";
import type { Host } from "../types";

const TextFileEditorDialog = defineAsyncComponent(
  () => import("./TextFileEditorDialog.vue"),
);

interface FileEntry {
  name: string;
  path: string;
  size: number;
  mode: string;
  directory: boolean;
  symlink: boolean;
  modifiedAt: string;
}

const props = defineProps<{
  modelValue: boolean;
  host?: Host;
  sessionId?: string;
  initialPath?: string;
}>();
const emit = defineEmits<{ "update:modelValue": [boolean] }>();
const currentPath = ref(".");
const entries = ref<FileEntry[]>([]);
const showHidden = ref(false);
const loading = ref(false);
const fileInput = ref<HTMLInputElement>();
const editorOpen = ref(false);
const editingFile = ref<FileEntry>();
const sessionQuery = computed(() =>
  props.sessionId ? `&session=${encodeURIComponent(props.sessionId)}` : "",
);

const shownEntries = computed(() =>
  entries.value
    .filter((item) => showHidden.value || !item.name.startsWith("."))
    .sort(
      (a, b) =>
        Number(b.directory) - Number(a.directory) ||
        a.name.localeCompare(b.name),
    ),
);

watch(
  () => props.modelValue,
  (open) => {
    if (!open) return;
    currentPath.value =
      props.initialPath?.trim() || props.host?.initialDirectory || ".";
    entries.value = [];
    listFiles(currentPath.value);
  },
);

async function listFiles(next = currentPath.value) {
  if (!props.host) return;
  loading.value = true;
  try {
    const result = await api<{ path: string; entries: FileEntry[] }>(
      `/api/sftp/${props.host.id}/list?path=${encodeURIComponent(next)}${sessionQuery.value}`,
    );
    currentPath.value = result.path;
    entries.value = result.entries;
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : "目录读取失败");
  } finally {
    loading.value = false;
  }
}

function parentPath() {
  const parts = currentPath.value.replace(/\/+$/, "").split("/");
  parts.pop();
  return parts.join("/") || "/";
}

async function mkdir() {
  if (!props.host) return;
  try {
    const { value } = await ElMessageBox.prompt("输入新目录名称", "新建目录", {
      inputValidator: (name) =>
        (Boolean(name) && !name.includes("/")) || "名称不能为空且不能包含 /",
    });
    await api(
      `/api/sftp/${props.host.id}/mkdir?session=${encodeURIComponent(props.sessionId || "")}`,
      {
        method: "POST",
        body: json({ path: `${currentPath.value}/${value}` }),
      },
    );
    await listFiles();
  } catch {}
}

async function renameEntry(entry: FileEntry) {
  if (!props.host) return;
  try {
    const { value } = await ElMessageBox.prompt("输入新名称", "重命名", {
      inputValue: entry.name,
      inputValidator: (name) =>
        (Boolean(name) && !name.includes("/")) || "名称不能为空且不能包含 /",
    });
    await api(
      `/api/sftp/${props.host.id}/rename?session=${encodeURIComponent(props.sessionId || "")}`,
      {
        method: "POST",
        body: json({
          source: entry.path,
          destination: `${currentPath.value}/${value}`,
        }),
      },
    );
    await listFiles();
  } catch {}
}

async function deleteEntry(entry: FileEntry) {
  if (!props.host) return;
  try {
    await ElMessageBox.confirm(
      `删除“${entry.name}”${entry.directory ? "及其全部内容" : ""}？`,
      "删除远程文件",
      { type: "warning" },
    );
    await api(
      `/api/sftp/${props.host.id}/delete?session=${encodeURIComponent(props.sessionId || "")}`,
      {
        method: "POST",
        body: json({ path: entry.path, recursive: entry.directory }),
      },
    );
    await listFiles();
  } catch (error: any) {
    if (error !== "cancel" && error !== "close")
      ElMessage.error(error instanceof Error ? error.message : "删除失败");
  }
}

function downloadEntry(entry: FileEntry) {
  if (!props.host) return;
  const link = document.createElement("a");
  link.href = `/api/sftp/${props.host.id}/download?path=${encodeURIComponent(entry.path)}${sessionQuery.value}`;
  link.click();
}

const editableExtensions = new Set([
  "txt", "text", "md", "markdown", "json", "jsonc", "ini", "env",
  "properties", "conf", "cfg", "yaml", "yml", "xml", "toml", "csv",
  "log", "sh", "bash", "zsh", "js", "mjs", "cjs", "ts", "tsx", "jsx",
  "css", "scss", "less", "html", "htm", "vue", "go", "py", "rb", "php",
  "java", "c", "h", "cpp", "hpp", "rs", "sql",
]);

function isEditable(entry: FileEntry) {
  if (entry.directory || entry.symlink || entry.size > 2 * 1024 * 1024) return false;
  const lower = entry.name.toLowerCase();
  if (lower === ".env" || lower.startsWith(".env.")) return true;
  const dot = lower.lastIndexOf(".");
  return dot >= 0 && editableExtensions.has(lower.slice(dot + 1));
}

function editEntry(entry: FileEntry) {
  editingFile.value = entry;
  editorOpen.value = true;
}

function activateEntry(entry: FileEntry) {
  if (entry.directory) listFiles(entry.path);
  else if (isEditable(entry)) editEntry(entry);
}

function activateRow(entry: FileEntry, event: MouseEvent, doubleClick = false) {
  if ((event.target as HTMLElement).closest(".file-row-action-zone")) return;
  if (entry.directory && !doubleClick) listFiles(entry.path);
  else if (!entry.directory && doubleClick) activateEntry(entry);
}

function formatBytes(size: number) {
  if (size < 1024) return `${size} B`;
  const units = ["KB", "MB", "GB"];
  let value = size / 1024;
  let unit = units[0];
  for (let i = 1; value >= 1024 && i < units.length; i += 1) {
    value /= 1024;
    unit = units[i];
  }
  return `${value >= 10 ? value.toFixed(0) : value.toFixed(1)} ${unit}`;
}

async function uploadFile(event: Event) {
  if (!props.host) return;
  const file = (event.target as HTMLInputElement).files?.[0];
  if (!file) return;
  const target = `${currentPath.value}/${file.name}`;
  let overwrite = false;
  try {
    if (entries.value.some((item) => item.name === file.name)) {
      await ElMessageBox.confirm("同名远程文件已存在，是否覆盖？", "覆盖文件", {
        type: "warning",
      });
      overwrite = true;
    }
    const response = await fetch(
      `/api/sftp/${props.host.id}/upload?path=${encodeURIComponent(target)}&overwrite=${overwrite}${sessionQuery.value}`,
      { method: "PUT", body: file, credentials: "same-origin" },
    );
    if (!response.ok) {
      const body = await response.json();
      throw new Error(body.message || "上传失败");
    }
    ElMessage.success("上传完成");
    await listFiles();
  } catch (error: any) {
    if (error !== "cancel" && error !== "close")
      ElMessage.error(error instanceof Error ? error.message : "上传失败");
  } finally {
    if (fileInput.value) fileInput.value.value = "";
  }
}
</script>

<template>
  <el-dialog
    :model-value="modelValue"
    class="file-manager-dialog"
    :title="`文件管理 · ${host?.name || '当前终端'}`"
    width="min(940px, calc(100vw - 28px))"
    append-to-body
    @update:model-value="emit('update:modelValue', $event)"
  >
    <div class="file-manager-toolbar">
      <el-input v-model="currentPath" @keyup.enter="listFiles()" />
      <el-tooltip content="刷新">
        <el-button
          class="file-refresh"
          :icon="RefreshCw"
          :loading="loading"
          @click="listFiles()"
        />
      </el-tooltip>
      <el-button class="file-mkdir" :icon="FolderPlus" @click="mkdir"
        >新建目录</el-button
      >
      <el-button class="file-upload" :icon="Upload" @click="fileInput?.click()"
        >上传</el-button
      >
      <input ref="fileInput" hidden type="file" @change="uploadFile" />
    </div>
    <div class="file-options">
      <el-checkbox v-model="showHidden">显示隐藏文件</el-checkbox>
      <button @click="listFiles(parentPath())">返回上级</button>
    </div>
    <div v-loading="loading" class="file-manager-list">
      <div v-if="!loading && !shownEntries.length" class="empty-small">
        <Folder :size="26" /><span>当前目录为空</span>
      </div>
      <div
        v-for="entry in shownEntries"
        :key="entry.path"
        class="file-row"
        :class="{ 'is-directory': entry.directory }"
        @click="activateRow(entry, $event)"
        @dblclick="activateRow(entry, $event, true)"
      >
        <span class="file-kind-icon" :class="entry.directory ? 'directory' : 'regular'">
          <component :is="entry.directory ? Folder : File" :size="21" />
        </span>
        <div class="file-row-copy">
          <strong>{{ entry.name }}</strong>
          <small>
            {{ entry.mode }} ·
            {{ entry.directory ? "目录" : formatBytes(entry.size) }} ·
            {{ new Date(entry.modifiedAt).toLocaleString() }}
          </small>
        </div>
        <div
          class="file-row-action-zone"
          @pointerdown.stop
          @click.stop
          @dblclick.stop
        >
          <el-dropdown trigger="click" placement="bottom-end">
            <button class="icon-btn file-row-menu" aria-label="文件操作" title="文件操作">
              <MoreVertical :size="18" />
            </button>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item v-if="entry.directory" :icon="Folder" @click="listFiles(entry.path)">打开</el-dropdown-item>
                <el-dropdown-item v-if="isEditable(entry)" :icon="FilePenLine" @click="editEntry(entry)">在线编辑</el-dropdown-item>
                <el-dropdown-item v-if="!entry.directory" :icon="Download" @click="downloadEntry(entry)">下载</el-dropdown-item>
                <el-dropdown-item :icon="Pencil" @click="renameEntry(entry)">重命名</el-dropdown-item>
                <el-dropdown-item divided :icon="Trash2" @click="deleteEntry(entry)">删除</el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
        </div>
      </div>
    </div>
    <TextFileEditorDialog
      v-model="editorOpen"
      :host="host"
      :session-id="sessionId"
      :file="editingFile"
      @saved="listFiles()"
    />
  </el-dialog>
</template>
