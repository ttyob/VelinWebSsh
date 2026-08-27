<script setup lang="ts">
import { computed, ref } from "vue";
import {
  CheckSquare,
  FolderInput,
  Plus,
  Server,
  Tags,
  Trash2,
  X,
} from "@lucide/vue";
import { ElMessage, ElMessageBox } from "element-plus";
import { api, json } from "../api";
import type { Host } from "../types";
import HostGroupNode, {
  type HostDropTarget,
  type HostTreeGroup,
} from "./HostGroupNode.vue";

const props = defineProps<{
  hosts: Host[];
  testing?: string;
}>();
const emit = defineEmits<{
  connect: [Host];
  web: [Host];
  test: [Host];
  edit: [Host];
  duplicate: [Host];
  delete: [Host];
  add: [];
  refresh: [];
  reorder: [{ hostID: string; target: HostDropTarget }];
}>();
const selecting = ref(false),
  selected = ref(new Set<string>()),
  collapsed = ref(new Set<string>()),
  busy = ref(false),
  draggingHostID = ref(""),
  dropTarget = ref<HostDropTarget | null>(null);
const groups = computed(() => buildGroups(props.hosts));

function buildGroups(hosts: Host[]): HostTreeGroup[] {
  const roots: HostTreeGroup[] = [];
  const groupsByPath = new Map<string, HostTreeGroup>();
  const ungrouped: Host[] = [];
  for (const host of hosts) {
    const parts = host.groupName
      .split("/")
      .map((part) => part.trim())
      .filter(Boolean);
    if (!parts.length) {
      ungrouped.push(host);
      continue;
    }
    let path = "";
    let parent: HostTreeGroup | undefined;
    let group: HostTreeGroup | undefined;
    for (const name of parts) {
      path = path ? `${path}/${name}` : name;
      group = groupsByPath.get(path);
      if (!group) {
        group = { name, path, hosts: [], children: [] };
        groupsByPath.set(path, group);
        if (parent) parent.children.push(group);
        else roots.push(group);
      }
      parent = group;
    }
    group?.hosts.push(host);
  }
  const sortGroups = (items: HostTreeGroup[]): HostTreeGroup[] =>
    items
      .map((item) => ({
        ...item,
        hosts: [...item.hosts].sort(compareHosts),
        children: sortGroups(item.children),
      }))
      .sort((a, b) => a.name.localeCompare(b.name));
  const result = sortGroups(roots);
  if (ungrouped.length)
    result.push({
      name: "未分组",
      path: "__ungrouped__",
      hosts: [...ungrouped].sort(compareHosts),
      children: [],
    });
  return result;
}
function compareHosts(a: Host, b: Host) {
  return (
    Number(a.sortOrder ?? 0) - Number(b.sortOrder ?? 0) ||
    a.name.localeCompare(b.name)
  );
}
function toggleGroup(path: string) {
  const next = new Set(collapsed.value);
  if (next.has(path)) next.delete(path);
  else next.add(path);
  collapsed.value = next;
}
function toggle(id: string, value: boolean) {
  const next = new Set(selected.value);
  if (value) next.add(id);
  else next.delete(id);
  selected.value = next;
}
function toggleAll() {
  selected.value =
    selected.value.size === props.hosts.length
      ? new Set()
      : new Set(props.hosts.map((host) => host.id));
}
function stopSelecting() {
  selecting.value = false;
  selected.value = new Set();
}
function beginDrag(host: Host) {
  if (selecting.value) return;
  draggingHostID.value = host.id;
}
function clearDrag() {
  draggingHostID.value = "";
  dropTarget.value = null;
}
function updateDropTarget(target: HostDropTarget) {
  dropTarget.value = target;
}
function completeDrop(target: HostDropTarget) {
  const hostID = draggingHostID.value;
  if (!hostID) return clearDrag();
  emit("reorder", { hostID, target });
  clearDrag();
}
async function batch(action: "group" | "tags" | "delete") {
  if (!selected.value.size) return ElMessage.warning("请先选择主机");
  try {
    let body: any = { ids: [...selected.value], action };
    if (action === "group") {
      const result = await ElMessageBox.prompt(
        "输入目标分组名称，留空可移出当前分组。",
        `移动 ${selected.value.size} 台主机`,
        { confirmButtonText: "移动", cancelButtonText: "取消" },
      );
      body.groupName = result.value.trim();
    }
    if (action === "tags") {
      const result = await ElMessageBox.prompt(
        "输入要添加的标签，多个标签使用逗号分隔。",
        `标记 ${selected.value.size} 台主机`,
        {
          confirmButtonText: "添加",
          cancelButtonText: "取消",
          inputValidator: (v) => Boolean(v.trim()) || "请输入标签",
        },
      );
      body.tags = result.value.trim();
    }
    if (action === "delete")
      await ElMessageBox.confirm(
        `删除选中的 ${selected.value.size} 台主机？若任一主机存在活动会话，整批操作都会取消。`,
        "批量删除",
        {
          confirmButtonText: "全部删除",
          cancelButtonText: "取消",
          type: "warning",
        },
      );
    busy.value = true;
    await api("/api/hosts/batch", { method: "POST", body: json(body) });
    ElMessage.success(`已更新 ${selected.value.size} 台主机`);
    stopSelecting();
    emit("refresh");
  } catch (e: any) {
    if (e !== "cancel" && e !== "close")
      ElMessage.error(e instanceof Error ? e.message : "批量操作失败");
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <div class="host-list-tools">
    <template v-if="selecting"
      ><el-checkbox
        :model-value="selected.size === hosts.length && hosts.length > 0"
        :indeterminate="selected.size > 0 && selected.size < hosts.length"
        @change="toggleAll"
        >已选 {{ selected.size }}</el-checkbox
      >
      <div class="host-batch-actions">
        <el-tooltip content="移动分组"
          ><button class="icon-btn" :disabled="busy" @click="batch('group')">
            <FolderInput :size="15" /></button></el-tooltip
        ><el-tooltip content="添加标签"
          ><button class="icon-btn" :disabled="busy" @click="batch('tags')">
            <Tags :size="15" /></button></el-tooltip
        ><el-tooltip content="批量删除"
          ><button
            class="icon-btn danger"
            :disabled="busy"
            @click="batch('delete')"
          >
            <Trash2 :size="15" /></button></el-tooltip
        ><button class="icon-btn" title="退出选择" @click="stopSelecting">
          <X :size="15" />
        </button></div
    ></template>
    <template v-else
      ><span>{{ hosts.length }} 台主机</span>
      <div>
        <button class="icon-btn" title="批量选择" @click="selecting = true">
          <CheckSquare :size="15" /></button
        ><button class="icon-btn" title="新增主机" @click="emit('add')">
          <Plus :size="15" />
        </button></div
    ></template>
  </div>
  <div class="resource-list host-resource-list">
    <div v-if="!hosts.length" class="empty-small">
      <Server :size="28" /><span>暂无主机</span>
    </div>
    <HostGroupNode
      v-for="group in groups"
      :key="group.path"
      :group="group"
      :depth="0"
      :selecting="selecting"
      :selected="selected"
      :collapsed="collapsed"
      :testing="testing"
      :dragging-host-id="draggingHostID"
      :drop-target="dropTarget"
      @connect="emit('connect', $event)"
      @web="emit('web', $event)"
      @test="emit('test', $event)"
      @edit="emit('edit', $event)"
      @duplicate="emit('duplicate', $event)"
      @delete="emit('delete', $event)"
      @toggle-selection="toggle"
      @toggle-group="toggleGroup"
      @drag-start="beginDrag"
      @drag-end="clearDrag"
      @drag-over="updateDropTarget"
      @drop="completeDrop"
    />
  </div>
</template>
