<script setup lang="ts">
import { computed } from "vue";
import { ChevronRight, MoreHorizontal } from "@lucide/vue";
import type { Host } from "../types";

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
        <div class="host-copy">
          <strong>{{ host.name }}</strong
          ><small
            >{{ host.username }}@{{ host.address }}:{{ host.port
            }}<template v-if="host.lastStatus">
              ·
              {{
                host.lastStatus === "online"
                  ? `${host.lastLatencyMs || 0} ms`
                  : "离线"
              }}</template
            ></small
          >
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
