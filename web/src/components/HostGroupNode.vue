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

const props = defineProps<{
  group: HostTreeGroup;
  depth: number;
  selecting: boolean;
  selected: Set<string>;
  collapsed: Set<string>;
  testing?: string;
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
      color: "#6fba82",
    }
  );
}
function systemLabel(host: Host) {
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
</script>

<template>
  <section class="host-tree-group" :style="{ '--host-group-depth': depth }">
    <button
      class="host-group-heading"
      :class="{ collapsed: collapsed.has(group.path) }"
      :title="group.path"
      @click="emit('toggleGroup', group.path)"
    >
      <ChevronRight :size="14" />
      <span>{{ group.name }}</span>
      <small>{{ count }}</small>
    </button>
    <div v-show="!collapsed.has(group.path)" class="host-group-content">
      <article
        v-for="host in group.hosts"
        :key="host.id"
        class="host-row"
        :class="{ selected: selected.has(host.id) }"
        @dblclick="!selecting && emit('connect', host)"
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
            detected: Boolean(host.platform),
            distribution: Boolean(host.distribution),
          }"
          :title="systemLabel(host)"
          :style="
            host.distribution
              ? {
                  color: distributionBadge(host.distribution).color,
                  borderColor: `${distributionBadge(host.distribution).color}66`,
                  backgroundColor: `${distributionBadge(host.distribution).color}18`,
                }
              : undefined
          "
        >
          <svg
            v-if="host.distribution && distributionLogos[host.distribution]"
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
        </div>
        <span v-if="!selecting" class="host-row-actions"
          ><el-dropdown trigger="click" @click.stop
            ><button class="icon-btn row-menu">
              <MoreHorizontal :size="16" /></button
            ><template #dropdown
              ><el-dropdown-menu
                ><el-dropdown-item @click="emit('connect', host)"
                  >连接</el-dropdown-item
                ><el-dropdown-item
                  :disabled="!host.credentialID"
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
        @connect="emit('connect', $event)"
        @web="emit('web', $event)"
        @test="emit('test', $event)"
        @edit="emit('edit', $event)"
        @duplicate="emit('duplicate', $event)"
        @delete="emit('delete', $event)"
        @toggle-selection="forwardSelection"
        @toggle-group="emit('toggleGroup', $event)"
      />
    </div>
  </section>
</template>
