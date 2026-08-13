import { nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { ElMessage } from "element-plus";
import { ApiError, api, json } from "../api";
import type { Preferences } from "../types";
import type { useAuthStore } from "../stores/auth";

const lockStorageKey = "velin-workspace-locked";
const lockSignalKey = "velin-workspace-lock-signal";

export function isLockShortcut(event: KeyboardEvent, enabled: boolean) {
  return enabled && (event.metaKey || event.getModifierState("Meta")) && event.code === "KeyL";
}

export function autoLockDelay(minutes: number) {
  return Math.max(0, minutes) * 60_000;
}

export function useWorkspaceLock(options: {
  preferences: Preferences;
  auth: ReturnType<typeof useAuthStore>;
  closeOverlays: () => void;
  reload: () => Promise<void>;
  logout: () => Promise<void>;
}) {
  const locked = ref(Boolean(options.auth.user?.sessionLocked));
  const lockPIN = ref("");
  const unlocking = ref(false);
  const lockError = ref("");
  const lockPINInput = ref<any>();
  let idleTimer: number | undefined;
  let locking = false;
  try {
    locked.value ||= sessionStorage.getItem(lockStorageKey) === "1";
  } catch {}

  function clearStoredLock() {
    try {
      sessionStorage.removeItem(lockStorageKey);
    } catch {}
  }

  function showLockScreen() {
    locked.value = true;
    lockPIN.value = "";
    lockError.value = "";
    options.closeOverlays();
    (document.activeElement as HTMLElement | null)?.blur?.();
    clearTimeout(idleTimer);
    try {
      sessionStorage.setItem(lockStorageKey, "1");
    } catch {}
    nextTick(() => lockPINInput.value?.focus?.());
  }

  function broadcast(value: "locked" | "unlocked") {
    try {
      localStorage.setItem(lockSignalKey, JSON.stringify({ value, at: Date.now() }));
    } catch {}
  }

  function resetIdleTimer() {
    clearTimeout(idleTimer);
    const delay = autoLockDelay(options.preferences.autoLockMinutes);
    if (locked.value || !options.preferences.lockEnabled || delay <= 0) return;
    idleTimer = window.setTimeout(() => void lockWorkspace("idle"), delay);
  }

  async function lockWorkspace(reason: "manual" | "idle" | "shortcut") {
    if (!options.preferences.lockEnabled || locked.value || locking) return;
    locking = true;
    showLockScreen();
    broadcast("locked");
    try {
      await api("/api/auth/lock", { method: "POST", body: json({ reason }) });
      if (options.auth.user) options.auth.user.sessionLocked = true;
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) await options.logout();
      else {
        locked.value = false;
        clearStoredLock();
        broadcast("unlocked");
        ElMessage.error(error instanceof Error ? error.message : "锁屏失败");
        resetIdleTimer();
      }
    } finally {
      locking = false;
    }
  }

  async function unlockWorkspace() {
    if (!/^\d{6}$/.test(lockPIN.value) || unlocking.value) return;
    unlocking.value = true;
    lockError.value = "";
    try {
      await api("/api/auth/unlock", { method: "POST", body: json({ pin: lockPIN.value }) });
      locked.value = false;
      lockPIN.value = "";
      if (options.auth.user) options.auth.user.sessionLocked = false;
      clearStoredLock();
      broadcast("unlocked");
      await options.reload();
      resetIdleTimer();
    } catch (error) {
      lockError.value = error instanceof Error ? error.message : "解锁失败";
      await nextTick();
      lockPINInput.value?.focus?.();
    } finally {
      unlocking.value = false;
    }
  }

  function handleSignal(event: StorageEvent) {
    if (event.key !== lockSignalKey || !event.newValue) return;
    try {
      const signal = JSON.parse(event.newValue);
      if (signal.value === "locked") showLockScreen();
      else if (signal.value === "unlocked") {
        locked.value = false;
        lockPIN.value = "";
        lockError.value = "";
        if (options.auth.user) options.auth.user.sessionLocked = false;
        clearStoredLock();
        void options.reload();
        resetIdleTimer();
      }
    } catch {}
  }

  function recordActivity(event?: Event) {
    if (locked.value) return;
    if (event instanceof KeyboardEvent && isLockShortcut(event, options.preferences.lockOnShortcut)) {
      void lockWorkspace("shortcut");
      return;
    }
    resetIdleTimer();
  }

  onMounted(() => {
    window.addEventListener("keydown", recordActivity, true);
    for (const name of ["pointerdown", "wheel", "touchstart"] as const)
      window.addEventListener(name, recordActivity, { capture: true, passive: true });
    window.addEventListener("storage", handleSignal);
    window.addEventListener("velin:session-locked", showLockScreen);
    resetIdleTimer();
    if (locked.value) nextTick(() => lockPINInput.value?.focus?.());
  });
  onBeforeUnmount(() => {
    clearTimeout(idleTimer);
    window.removeEventListener("keydown", recordActivity, true);
    for (const name of ["pointerdown", "wheel", "touchstart"] as const)
      window.removeEventListener(name, recordActivity, true);
    window.removeEventListener("storage", handleSignal);
    window.removeEventListener("velin:session-locked", showLockScreen);
  });
  watch(() => [options.preferences.lockEnabled, options.preferences.autoLockMinutes], resetIdleTimer);

  return { locked, lockPIN, unlocking, lockError, lockPINInput, lockWorkspace, unlockWorkspace, clearStoredLock };
}
