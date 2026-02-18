<template>
  <div class="page-shell">
    <header class="console-header glass-card stagger-1">
      <div class="brand">
        <p class="eyebrow">Super Admin Workspace</p>
        <h1>超级管理员页面</h1>
        <p class="subtitle">
          此页面默认不在登录页展示入口，需要手动输入 URL `/super-admin` 访问。
        </p>
      </div>
      <div class="status-pills">
        <el-button plain @click="$emit('navigate', '/login')">返回登录页</el-button>
        <el-button type="danger" :disabled="!session.superToken" @click="logout">退出超管</el-button>
      </div>
    </header>

    <section class="panel-grid panel-grid--super-auth">
      <article class="panel-card glass-card stagger-2">
        <div class="panel-headline">
          <h3>初始化与登录</h3>
          <el-tag :type="bootstrapInitialized ? 'success' : 'warning'">
            {{ bootstrapInitialized ? "已初始化" : "未初始化" }}
          </el-tag>
        </div>
        <el-form :model="superForm" label-width="90px" class="compact-form">
          <el-form-item label="账号">
            <el-input v-model="superForm.username" placeholder="super_admin" />
          </el-form-item>
          <el-form-item label="密码">
            <el-input v-model="superForm.password" type="password" show-password />
          </el-form-item>
          <el-form-item>
            <el-button plain :loading="loading.status" @click="loadBootstrapStatus">检查初始化</el-button>
            <el-button type="warning" :loading="loading.init" @click="bootstrapInit">首次初始化</el-button>
            <el-button type="primary" :loading="loading.login" @click="superLogin">登录超管</el-button>
          </el-form-item>
        </el-form>
      </article>
    </section>

    <section class="workspace glass-card stagger-3">
      <SuperPanel :token="session.superToken" @logout="logout" />
    </section>
  </div>
</template>

<script setup>
import { onMounted, reactive, ref } from "vue";
import { ElMessage } from "element-plus";
import SuperPanel from "../components/SuperPanel.vue";
import { parseApiError, superApi } from "../lib/http";
import { clearSuperToken, setSuperToken } from "../lib/session";

defineProps({
  session: {
    type: Object,
    required: true,
  },
});

const emit = defineEmits(["navigate", "session-updated"]);

const bootstrapInitialized = ref(false);
const superForm = reactive({
  username: "",
  password: "",
});

const loading = reactive({
  status: false,
  init: false,
  login: false,
});

onMounted(async () => {
  await loadBootstrapStatus();
});

async function loadBootstrapStatus() {
  loading.status = true;
  try {
    const response = await superApi.bootstrapStatus();
    bootstrapInitialized.value = !!response.initialized;
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.status = false;
  }
}

async function bootstrapInit() {
  const username = superForm.username.trim();
  const password = superForm.password;
  if (!username || !password) {
    ElMessage.warning("请输入超管账号和密码");
    return;
  }
  loading.init = true;
  try {
    await superApi.bootstrapInit({ username, password });
    ElMessage.success("超管初始化成功");
    await loadBootstrapStatus();
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.init = false;
  }
}

async function superLogin() {
  const username = superForm.username.trim();
  const password = superForm.password;
  if (!username || !password) {
    ElMessage.warning("请输入超管账号和密码");
    return;
  }
  loading.login = true;
  try {
    const response = await superApi.login({ username, password });
    setSuperToken(response.token || "");
    emit("session-updated");
    ElMessage.success("超管登录成功");
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.login = false;
  }
}

function logout() {
  clearSuperToken();
  emit("session-updated");
  ElMessage.success("已退出超管会话");
}
</script>
