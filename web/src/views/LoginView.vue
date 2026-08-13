<script setup lang="ts">
import { ref } from "vue";
import { useRouter } from "vue-router";
import { LockKeyhole, LogIn, Server, UserRound } from "@lucide/vue";
import { ElMessage, ElMessageBox } from "element-plus";
import { useAuthStore } from "../stores/auth";
import { ApiError } from "../api";

const auth = useAuthStore();
const router = useRouter();
const username = ref("");
const password = ref("");
const remember = ref(true);
const loading = ref(false);

async function submit() {
  if (!username.value || !password.value)
    return ElMessage.warning("请输入用户名和密码");
  loading.value = true;
  try {
    await auth.login(username.value, password.value, remember.value);
    await router.replace(
      auth.user?.forcePasswordChange ? "/change-password" : "/workspace",
    );
  } catch (error) {
    if (error instanceof ApiError && error.body.code === "totp_required") {
      try {
        const { value } = await ElMessageBox.prompt(
          "输入验证器中的 6 位验证码，或一个恢复码。",
          "双因素认证",
          {
            confirmButtonText: "验证",
            cancelButtonText: "取消",
            inputValidator: (value) => Boolean(value) || "请输入验证码",
          },
        );
        await auth.login(username.value, password.value, remember.value, value);
        await router.replace(
          auth.user?.forcePasswordChange ? "/change-password" : "/workspace",
        );
      } catch (inner) {
        if (inner !== "cancel" && inner !== "close")
          ElMessage.error(inner instanceof Error ? inner.message : "验证失败");
      }
    } else ElMessage.error(error instanceof Error ? error.message : "登录失败");
  } finally {
    loading.value = false;
  }
}
</script>

<template>
  <main class="login-page">
    <section class="login-panel">
      <div class="brand-lockup">
        <span class="brand-mark"><Server :size="24" /></span>
        <div>
          <h1>Velin Web SSH</h1>
          <p>Secure terminal workspace</p>
        </div>
      </div>
      <form @submit.prevent="submit">
        <label for="velin-username">用户名</label>
        <el-input
          id="velin-username"
          v-model="username"
          size="large"
          autocomplete="username"
          autofocus
        >
          <template #prefix><UserRound :size="17" /></template>
        </el-input>
        <label for="velin-password">密码</label>
        <el-input
          id="velin-password"
          v-model="password"
          size="large"
          type="password"
          show-password
          autocomplete="current-password"
          @keyup.enter="submit"
        >
          <template #prefix><LockKeyhole :size="17" /></template>
        </el-input>
        <div class="login-options">
          <el-checkbox v-model="remember">保持登录</el-checkbox>
        </div>
        <el-button
          native-type="submit"
          type="primary"
          size="large"
          :icon="LogIn"
          :loading="loading"
        >
          登录
        </el-button>
      </form>
      <p class="login-foot">SSH 会话由远程 tmux 安全托管</p>
    </section>
  </main>
</template>
