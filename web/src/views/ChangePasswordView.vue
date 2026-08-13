<script setup lang="ts">
import { ref } from "vue";
import { useRouter } from "vue-router";
import { KeyRound, LockKeyhole, Save } from "@lucide/vue";
import { ElMessage } from "element-plus";
import { api, json } from "../api";
import { useAuthStore } from "../stores/auth";

const auth = useAuthStore();
const router = useRouter();
const currentPassword = ref("");
const newPassword = ref("");
const confirmPassword = ref("");
const loading = ref(false);

async function submit() {
  if (newPassword.value !== confirmPassword.value)
    return ElMessage.warning("两次输入的新密码不一致");
  loading.value = true;
  try {
    await api("/api/auth/password", {
      method: "POST",
      body: json({
        currentPassword: currentPassword.value,
        newPassword: newPassword.value,
      }),
    });
    if (auth.user) auth.user.forcePasswordChange = false;
    ElMessage.success("密码已修改");
    await router.replace("/workspace");
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : "修改失败");
  } finally {
    loading.value = false;
  }
}
</script>

<template>
  <main class="login-page">
    <section class="login-panel password-panel">
      <div class="brand-lockup">
        <span class="brand-mark"><KeyRound :size="24" /></span>
        <div>
          <h1>修改初始密码</h1>
          <p>继续使用前需要设置自己的密码</p>
        </div>
      </div>
      <form @submit.prevent="submit">
        <label for="current-password">当前密码</label>
        <el-input
          id="current-password"
          v-model="currentPassword"
          size="large"
          type="password"
          show-password
          autocomplete="current-password"
        >
          <template #prefix><LockKeyhole :size="17" /></template>
        </el-input>
        <label for="new-password">新密码</label>
        <el-input
          id="new-password"
          v-model="newPassword"
          size="large"
          type="password"
          show-password
          autocomplete="new-password"
        />
        <label for="confirm-password">确认新密码</label>
        <el-input
          id="confirm-password"
          v-model="confirmPassword"
          size="large"
          type="password"
          show-password
          autocomplete="new-password"
        />
        <el-button
          native-type="submit"
          type="primary"
          size="large"
          :icon="Save"
          :loading="loading"
        >
          修改并进入工作区
        </el-button>
      </form>
    </section>
  </main>
</template>
