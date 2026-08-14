<script setup lang="ts">
import { computed, ref } from "vue";
import {
  Check,
  Copy,
  PackageOpen,
  RefreshCw,
  SquareTerminal,
} from "@lucide/vue";
import { ElMessage } from "element-plus";
import type { Host } from "../types";
import { tmuxInstallGuide } from "../tmuxInstall";

const props = defineProps<{
  modelValue: boolean;
  host?: Host;
}>();
const emit = defineEmits<{
  "update:modelValue": [boolean];
  retry: [];
  fallback: [];
}>();
const copied = ref(false);
const guide = computed(() =>
  tmuxInstallGuide(props.host?.platform, props.host?.distribution),
);

async function copyCommand() {
  try {
    await navigator.clipboard.writeText(guide.value.command);
    copied.value = true;
    ElMessage.success("安装命令已复制");
    window.setTimeout(() => (copied.value = false), 1600);
  } catch {
    ElMessage.warning("浏览器未允许复制，请手动选择命令");
  }
}
</script>

<template>
  <el-dialog
    :model-value="modelValue"
    class="tmux-install-dialog"
    title="需要安装 tmux"
    width="min(620px, calc(100vw - 28px))"
    append-to-body
    @update:model-value="emit('update:modelValue', $event)"
  >
    <div class="tmux-install-intro">
      <span class="tmux-install-icon"><PackageOpen :size="24" /></span>
      <div>
        <strong>{{ host?.name || "远程主机" }} 尚未安装 tmux</strong>
        <p>当前主机使用 tmux 持久模式，SSH 连接本身已经成功。</p>
      </div>
    </div>

    <div class="tmux-system-row">
      <span>检测到的系统</span>
      <strong>{{ guide.systemLabel }}</strong>
    </div>

    <template v-if="guide.supported">
      <p class="tmux-install-step">
        使用其他 SSH 客户端登录该主机，执行以下命令：
      </p>
      <div class="tmux-command-block">
        <code>{{ guide.command }}</code>
        <button
          type="button"
          class="icon-btn"
          :title="copied ? '已复制' : '复制安装命令'"
          @click="copyCommand"
        >
          <Check v-if="copied" :size="17" />
          <Copy v-else :size="17" />
        </button>
      </div>
      <p v-if="guide.notice" class="tmux-install-note">{{ guide.notice }}</p>
    </template>
    <el-alert
      v-else
      :title="guide.notice"
      type="warning"
      :closable="false"
      show-icon
    />

    <template #footer>
      <el-button @click="emit('update:modelValue', false)">稍后处理</el-button>
      <el-button
        :type="guide.supported ? 'default' : 'primary'"
        :icon="SquareTerminal"
        @click="emit('fallback')"
      >
        使用普通模式
      </el-button>
      <el-button
        v-if="guide.supported"
        type="primary"
        :icon="RefreshCw"
        @click="emit('retry')"
      >
        安装完成，重新连接
      </el-button>
    </template>
  </el-dialog>
</template>
