<script setup lang="ts">
import { reactive, ref, watch } from "vue";
import { ElMessage } from "element-plus";
import { api, json } from "../api";
import type { Credential, Host } from "../types";

const props = defineProps<{
  modelValue: boolean;
  host?: Host;
  credentials: Credential[];
}>();
const emit = defineEmits<{
  "update:modelValue": [value: boolean];
  saved: [host: Host];
}>();
const defaults = (): Host => ({
  id: "",
  name: "",
  address: "",
  port: 22,
  username: "root",
  credentialID: "",
  groupName: "",
  tags: "",
  notes: "",
  initialDirectory: "",
  connectTimeout: 12,
  keepaliveInterval: 30,
  maxRetries: 5,
  terminalType: "xterm-256color",
});
const form = reactive<Host>(defaults());
const advancedOpen = ref<string[]>([]),
  authMode = ref<"password" | "credential" | "prompt">("password"),
  password = ref("");
watch(
  () => props.modelValue,
  (open) => {
    if (open) {
      Object.assign(form, defaults(), props.host || {});
      authMode.value = props.host
        ? props.host.credentialID
          ? "credential"
          : "prompt"
        : "password";
      password.value = "";
      advancedOpen.value = [];
    }
  },
);
async function save() {
  try {
    if (authMode.value === "password" && !password.value)
      return ElMessage.warning("请输入 SSH 密码");
    if (authMode.value === "credential" && !form.credentialID)
      return ElMessage.warning("请选择凭据");
    const method = form.id ? "PUT" : "POST";
    const path = form.id ? `/api/hosts/${form.id}` : "/api/hosts";
    const saved = await api<Host>(path, {
      method,
      body: json({
        ...form,
        credentialID: authMode.value === "credential" ? form.credentialID : "",
        password: authMode.value === "password" ? password.value : "",
      }),
    });
    emit("saved", saved);
    emit("update:modelValue", false);
    ElMessage.success("主机已保存");
  } catch (e) {
    ElMessage.error(e instanceof Error ? e.message : "保存失败");
  }
}
</script>
<template>
  <el-dialog
    :model-value="modelValue"
    class="host-dialog"
    :title="form.id ? '编辑主机' : '新增主机'"
    width="min(660px, calc(100vw - 28px))"
    @update:model-value="emit('update:modelValue', $event)"
  >
    <el-form label-position="top">
      <section class="host-form-section">
        <h3>基础设置</h3>
        <div class="form-grid">
          <el-form-item label="名称"
            ><el-input v-model="form.name"
          /></el-form-item>
          <el-form-item label="分组"
            ><el-input
              v-model="form.groupName"
              placeholder="例如 生产/华东/数据库"
          /></el-form-item>
          <el-form-item label="主机地址" class="span-2"
            ><el-input v-model="form.address" placeholder="server.example.com"
          /></el-form-item>
          <el-form-item label="端口"
            ><el-input-number
              v-model="form.port"
              :min="1"
              :max="65535"
              controls-position="right"
          /></el-form-item>
          <el-form-item label="用户名"
            ><el-input v-model="form.username"
          /></el-form-item>
          <el-form-item label="认证方式" class="span-2">
            <el-segmented
              v-model="authMode"
              :options="[
                { label: '直接输入密码', value: 'password' },
                { label: '已保存凭据', value: 'credential' },
                { label: '连接时输入', value: 'prompt' },
              ]"
            />
          </el-form-item>
          <el-form-item
            v-if="authMode === 'password'"
            label="SSH 密码"
            class="span-2"
          >
            <el-input
              v-model="password"
              type="password"
              show-password
              autocomplete="new-password"
              placeholder="保存后将加密存储"
            />
          </el-form-item>
          <el-form-item
            v-else-if="authMode === 'credential'"
            label="凭据"
            class="span-2"
            ><el-select v-model="form.credentialID" placeholder="选择已保存凭据"
              ><el-option
                v-for="credential in credentials"
                :key="credential.id"
                :label="credential.name"
                :value="credential.id" /></el-select
          ></el-form-item>
          <el-form-item label="标签" class="span-2"
            ><el-input v-model="form.tags" placeholder="逗号分隔"
          /></el-form-item>
          <el-form-item label="备注" class="span-2"
            ><el-input v-model="form.notes" type="textarea" :rows="2"
          /></el-form-item>
        </div>
      </section>
      <el-collapse v-model="advancedOpen" class="host-advanced">
        <el-collapse-item title="高级设置" name="advanced">
          <div class="form-grid">
            <el-form-item label="启动目录" class="span-2"
              ><el-input
                v-model="form.initialDirectory"
                placeholder="例如 /srv/app，留空使用远程默认目录"
            /></el-form-item>
            <el-form-item label="连接超时（秒）"
              ><el-input-number
                v-model="form.connectTimeout"
                :min="3"
                :max="120"
                controls-position="right"
            /></el-form-item>
            <el-form-item label="Keepalive（秒）"
              ><el-input-number
                v-model="form.keepaliveInterval"
                :min="0"
                :max="300"
                controls-position="right"
            /></el-form-item>
            <el-form-item label="最大重试次数"
              ><el-input-number
                v-model="form.maxRetries"
                :min="0"
                :max="20"
                controls-position="right"
            /></el-form-item>
            <el-form-item label="终端类型"
              ><el-select v-model="form.terminalType"
                ><el-option
                  label="xterm-256color"
                  value="xterm-256color" /><el-option
                  label="xterm"
                  value="xterm" /><el-option
                  label="screen-256color"
                  value="screen-256color" /></el-select
            ></el-form-item>
          </div>
        </el-collapse-item>
      </el-collapse>
    </el-form>
    <template #footer>
      <el-button @click="emit('update:modelValue', false)">取消</el-button>
      <el-button type="primary" @click="save">保存</el-button>
    </template>
  </el-dialog>
</template>
