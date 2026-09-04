<script setup lang="ts">
import { computed } from "vue";
import {
  Apple,
  ChevronRight,
  Monitor,
  MoreHorizontal,
  Server,
  SquareTerminal,
} from "@lucide/vue";
import type { Host } from "../types";
import { distributionLogos } from "../distributionLogos";

defineOptions({ name: "HostGroupNode" });

export interface HostTreeGroup {
  name: string;
  path: string;
  hosts: Host[];
  children: HostTreeGroup[];
}
export type HostDropTarget =
  | { kind: "host"; groupPath: string; hostID: string; after: boolean }
  | { kind: "group"; groupPath: string };

const props = defineProps<{
  group: HostTreeGroup;
  depth: number;
  selecting: boolean;
  selected: Set<string>;
  collapsed: Set<string>;
  testing?: string;
  draggingHostID?: string;
  dropTarget?: HostDropTarget | null;
}>();
const emit = defineEmits<{
  connect: [Host];
  web: [Host];
  test: [Host];
  edit: [Host];
  duplicate: [Host];
  delete: [Host];
  toggleSelection: [string, boolean];
  toggleGroup: [string];
  dragStart: [Host];
  dragEnd: [];
  dragOver: [HostDropTarget];
  drop: [HostDropTarget];
}>();

const count = computed(() => countHosts(props.group));
const platformIcons = {
  linux: SquareTerminal,
  windows: Monitor,
  macos: Apple,
  bsd: Server,
  unix: SquareTerminal,
};
const distributionBadges: Record<
  string,
  { label: string; color: string }
> = {
  ubuntu: { label: "Ubuntu", color: "#e95420" },
  debian: { label: "Debian", color: "#d70a53" },
  linuxmint: { label: "Linux Mint", color: "#87cf3e" },
  pop: { label: "Pop!_OS", color: "#48b9c7" },
  elementary: { label: "elementary OS", color: "#64baff" },
  rhel: { label: "Red Hat Enterprise Linux", color: "#ee0000" },
  centos: { label: "CentOS", color: "#a14f8c" },
  rocky: { label: "Rocky Linux", color: "#10b981" },
  almalinux: { label: "AlmaLinux", color: "#4f6bff" },
  fedora: { label: "Fedora", color: "#51a2da" },
  arch: { label: "Arch Linux", color: "#1793d1" },
  manjaro: { label: "Manjaro", color: "#35bf5c" },
  endeavouros: { label: "EndeavourOS", color: "#7f5af0" },
  alpine: { label: "Alpine Linux", color: "#0d597f" },
  opensuse: { label: "openSUSE", color: "#73ba25" },
  gentoo: { label: "Gentoo", color: "#8b7bb5" },
  kali: { label: "Kali Linux", color: "#557c94" },
  raspbian: { label: "Raspberry Pi OS", color: "#c51a4a" },
  nixos: { label: "NixOS", color: "#5277c3" },
  void: { label: "Void Linux", color: "#478061" },
  amazon: { label: "Amazon Linux", color: "#ff9900" },
  oracle: { label: "Oracle Linux", color: "#c74634" },
  proxmox: { label: "Proxmox VE", color: "#e57000" },
};
function distributionBadge(value?: string) {
  const id = value?.trim().toLowerCase() || "";
  return (
    distributionBadges[id] || {
      label: id || "Linux",
      color: "#6f9cff",
    }
  );
}
function systemLabel(host: Host) {
  if (host.protocol === "vnc") return "VNC 桌面";
  if (host.protocol === "rdp") return "RDP 桌面";
  if (host.distribution) return distributionBadge(host.distribution).label;
  const labels: Record<string, string> = {
    linux: "Linux",
    windows: "Windows",
    macos: "macOS",
    bsd: "BSD",
    unix: "Unix",
  };
  return labels[host.platform || ""] || "尚未识别系统";
}
function hostInitial(name: string) {
  return Array.from(name.trim())[0]?.toUpperCase() || "?";
}
function countHosts(group: HostTreeGroup): number {
  return (
    group.hosts.length +
    group.children.reduce((total, child) => total + countHosts(child), 0)
  );
}
function forwardSelection(id: string, value: boolean) {
  emit("toggleSelection", id, value);
}
function openHostMenu(event: MouseEvent) {
  if (props.selecting) return;
  const row = event.currentTarget;
  if (row instanceof HTMLElement)
    row.querySelector<HTMLButtonElement>(".row-menu")?.click();
}
function dragStart(event: DragEvent, host: Host) {
  if (props.selecting) return;
  event.dataTransfer?.setData("text/plain", host.id);
  if (event.dataTransfer) event.dataTransfer.effectAllowed = "move";
  emit("dragStart", host);
}
function dragOverHost(event: DragEvent, host: Host) {
  event.preventDefault();
  event.stopPropagation();
  const row = event.currentTarget;
  const after =
    row instanceof HTMLElement &&
    event.clientY > row.getBoundingClientRect().top + row.offsetHeight / 2;
  emit("dragOver", {
    kind: "host",
    groupPath: props.group.path,
    hostID: host.id,
    after,
  });
}
function dragOverGroup(event: DragEvent) {
  event.preventDefault();
  event.stopPropagation();
  emit("dragOver", { kind: "group", groupPath: props.group.path });
}
function emitDrop(event: DragEvent, target: HostDropTarget) {
  event.preventDefault();
  event.stopPropagation();
  emit("drop", target);
}
function dropHost(event: DragEvent, host: Host) {
  const row = event.currentTarget;
  const after =
    row instanceof HTMLElement &&
    event.clientY > row.getBoundingClientRect().top + row.offsetHeight / 2;
  emitDrop(event, {
    kind: "host",
    groupPath: props.group.path,
    hostID: host.id,
    after,
  });
}
</script>

<template>
  <section class="host-tree-group" :style="{ '--host-group-depth': depth }">
    <button
      class="host-group-heading"
      :title="group.path"
      @dragover="dragOverGroup"
      @drop="emitDrop($event, { kind: 'group', groupPath: group.path })"
      :class="{
        collapsed: collapsed.has(group.path),
        'drop-target':
          dropTarget?.kind === 'group' && dropTarget.groupPath === group.path,
      }"
      @click="emit('toggleGroup', group.path)"
    >
      <ChevronRight :size="14" />
      <span>{{ group.name }}</span>
      <small>{{ count }}</small>
    </button>
    <div
      v-show="!collapsed.has(group.path)"
      class="host-group-content"
      :class="{
        'drop-target':
          dropTarget?.kind === 'group' && dropTarget.groupPath === group.path,
      }"
      @dragover="dragOverGroup"
      @drop="emitDrop($event, { kind: 'group', groupPath: group.path })"
    >
      <article
        v-for="host in group.hosts"
        :key="host.id"
        class="host-row"
        :class="{
          selected: selected.has(host.id),
          dragging: draggingHostID === host.id,
          'drop-target':
            dropTarget?.kind === 'host' && dropTarget.hostID === host.id,
        }"
        :draggable="!selecting"
        @dragstart="dragStart($event, host)"
        @dragend="emit('dragEnd')"
        @dragover="dragOverHost($event, host)"
        @drop="dropHost($event, host)"
        @dblclick="!selecting && emit('connect', host)"
        @contextmenu.prevent="openHostMenu"
      >
        <el-checkbox
          v-if="selecting"
          :model-value="selected.has(host.id)"
          @change="
            (value: boolean | string | number) =>
              emit('toggleSelection', host.id, Boolean(value))
          "
          @click.stop
        />
        <span
          class="host-icon"
          :class="{
            detected:
              (host.protocol || 'ssh') === 'ssh' && Boolean(host.platform),
            distribution:
              (host.protocol || 'ssh') === 'ssh' && Boolean(host.distribution),
          }"
          :title="systemLabel(host)"
          :style="
            (host.protocol || 'ssh') === 'ssh' && host.distribution
              ? {
                  color: distributionBadge(host.distribution).color,
                  borderColor: `${distributionBadge(host.distribution).color}66`,
                  backgroundColor: `${distributionBadge(host.distribution).color}18`,
                }
              : undefined
          "
        >
          <Monitor
            v-if="host.protocol === 'vnc' || host.protocol === 'rdp'"
            :size="17"
            :stroke-width="1.8"
          />
          <svg
            v-else-if="host.distribution && distributionLogos[host.distribution]"
            class="distribution-logo"
            viewBox="0 0 24 24"
            aria-hidden="true"
          >
            <path :d="distributionLogos[host.distribution]" />
          </svg>
          <component
            :is="platformIcons[host.platform as keyof typeof platformIcons]"
            v-else-if="host.platform && platformIcons[host.platform as keyof typeof platformIcons]"
            :size="17"
            :stroke-width="1.8"
          />
          <span v-else>{{ hostInitial(host.name) }}</span>
        </span>
        <div class="host-copy">
          <strong>{{ host.name }}</strong>
          <small>
            {{ (host.protocol || "ssh").toUpperCase() }} · {{ host.address }}:{{ host.port }}
          </small>
        </div>
        <span v-if="!selecting" class="host-row-actions"
          ><el-dropdown
            trigger="click"
            @click.stop
            ><button class="icon-btn row-menu">
              <MoreHorizontal :size="16" /></button
            ><template #dropdown
              ><el-dropdown-menu
                ><el-dropdown-item @click="emit('connect', host)"
                  >连接</el-dropdown-item
                ><el-dropdown-item
                  :disabled="
                    (host.protocol || 'ssh') !== 'ssh' || !host.credentialID
                  "
                  @click="emit('web', host)"
                  >访问内网 Web</el-dropdown-item
                ><el-dropdown-item
                  :disabled="testing === host.id"
                  @click="emit('test', host)"
                  >{{
                    testing === host.id ? "正在测试…" : "测试连接"
                  }}</el-dropdown-item
                ><el-dropdown-item @click="emit('edit', host)"
                  >编辑</el-dropdown-item
                ><el-dropdown-item @click="emit('duplicate', host)"
                  >复制主机</el-dropdown-item
                ><el-dropdown-item divided @click="emit('delete', host)"
                  >删除</el-dropdown-item
                ></el-dropdown-menu
              ></template
            ></el-dropdown
          ></span
        >
      </article>
      <HostGroupNode
        v-for="child in group.children"
        :key="child.path"
        :group="child"
        :depth="depth + 1"
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
        @toggle-selection="forwardSelection"
        @toggle-group="emit('toggleGroup', $event)"
        @drag-start="emit('dragStart', $event)"
        @drag-end="emit('dragEnd')"
        @drag-over="emit('dragOver', $event)"
        @drop="emit('drop', $event)"
      />
    </div>
  </section>
</template>
