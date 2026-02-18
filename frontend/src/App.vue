<template>
  <div class="console-shell">
    <div class="decor decor--a" />
    <div class="decor decor--b" />

    <header class="console-header glass-card stagger-1">
      <div class="brand">
        <p class="eyebrow">Cloud Command Center</p>
        <h1>OAS 云控台</h1>
        <p class="subtitle">统一登录入口 + 三角色全流程操作，支持 Redis 必选架构与云端任务调度。</p>
      </div>
      <div class="status-pills">
        <el-tag :type="platform.health.ok ? 'success' : 'danger'" effect="dark">
          API {{ platform.health.ok ? '在线' : '异常' }}
        </el-tag>
        <el-tag :type="platform.scheduler.enabled ? 'warning' : 'info'" effect="dark">
          调度器 {{ platform.scheduler.enabled ? '启用' : '停用' }}
        </el-tag>
        <el-button text type="primary" @click="refreshPlatformStatus">刷新状态</el-button>
      </div>
    </header>

    <section class="auth-and-nav">
      <article class="auth-panel glass-card stagger-2">
        <div class="panel-head">
          <h2>统一登录页面</h2>
          <p>先登录角色，再进入对应功能页面。登录态会保存在浏览器本地。</p>
        </div>

        <el-tabs v-model="activeRole" class="role-tabs" stretch>
          <el-tab-pane label="超级管理员" name="super">
            <div class="auth-block">
              <div class="row-between">
                <el-button plain size="small" @click="loadBootstrapStatus">检查初始化状态</el-button>
                <el-tag :type="superAuth.bootstrapInitialized ? 'success' : 'warning'">
                  {{ superAuth.bootstrapInitialized ? '已初始化' : '未初始化' }}
                </el-tag>
              </div>
              <el-form :model="superAuth.form" label-width="90px" class="compact-form">
                <el-form-item label="账号">
                  <el-input v-model="superAuth.form.username" placeholder="super_admin" />
                </el-form-item>
                <el-form-item label="密码">
                  <el-input v-model="superAuth.form.password" type="password" show-password />
                </el-form-item>
                <el-form-item>
                  <el-button type="warning" @click="bootstrapInit">首次初始化</el-button>
                  <el-button type="primary" @click="superLogin">登录超管</el-button>
                  <el-button :disabled="!sessions.superToken" @click="logoutSuper">退出</el-button>
                </el-form-item>
              </el-form>
            </div>
          </el-tab-pane>

          <el-tab-pane label="管理员" name="manager">
            <div class="auth-block">
              <el-form :model="managerAuth.form" label-width="90px" class="compact-form">
                <el-form-item label="账号">
                  <el-input v-model="managerAuth.form.username" placeholder="manager_demo" />
                </el-form-item>
                <el-form-item label="密码">
                  <el-input v-model="managerAuth.form.password" type="password" show-password />
                </el-form-item>
                <el-form-item>
                  <el-button type="warning" @click="registerManager">公共注册</el-button>
                  <el-button type="primary" @click="loginManager">管理员登录</el-button>
                  <el-button :disabled="!sessions.managerToken" @click="logoutManager">退出</el-button>
                </el-form-item>
              </el-form>
              <el-alert type="info" :closable="false" title="管理员注册后默认过期，请在管理页兑换超管发放的续费秘钥。" />
            </div>
          </el-tab-pane>

          <el-tab-pane label="普通用户" name="user">
            <div class="auth-block">
              <el-form :model="userAuth" label-width="104px" class="compact-form">
                <el-form-item label="激活码注册">
                  <el-input v-model="userAuth.registerCode" placeholder="uac_xxx" />
                </el-form-item>
                <el-form-item>
                  <el-button type="warning" @click="registerUserByCode">注册并登录</el-button>
                </el-form-item>
                <el-divider />
                <el-form-item label="账号登录">
                  <el-input v-model="userAuth.accountNo" placeholder="U2026..." />
                </el-form-item>
                <el-form-item>
                  <el-button type="primary" @click="loginUser">用户登录</el-button>
                  <el-button :disabled="!sessions.userToken" @click="logoutUser">退出</el-button>
                </el-form-item>
              </el-form>
            </div>
          </el-tab-pane>
        </el-tabs>
      </article>

      <aside class="quick-nav glass-card stagger-3">
        <h3>角色会话总览</h3>
        <ul>
          <li>
            <span>超级管理员</span>
            <el-tag :type="sessions.superToken ? 'success' : 'info'">{{ sessions.superToken ? '已登录' : '未登录' }}</el-tag>
          </li>
          <li>
            <span>管理员</span>
            <el-tag :type="sessions.managerToken ? 'success' : 'info'">{{ sessions.managerToken ? '已登录' : '未登录' }}</el-tag>
          </li>
          <li>
            <span>普通用户</span>
            <el-tag :type="sessions.userToken ? 'success' : 'info'">{{ sessions.userToken ? '已登录' : '未登录' }}</el-tag>
          </li>
        </ul>

        <el-divider />

        <h4>工作台切换</h4>
        <el-segmented v-model="activeRole" :options="roleOptions" class="role-segment" />
        <p class="hint">可独立登录多个角色并并行运维。</p>
      </aside>
    </section>

    <section class="workspace glass-card stagger-4">
      <header class="workspace-head">
        <h2>{{ roleTitleMap[activeRole] }}</h2>
        <p>{{ roleDescMap[activeRole] }}</p>
      </header>

      <SuperPanel
        v-if="activeRole === 'super'"
        :token="sessions.superToken"
        @logout="logoutSuper"
      />
      <ManagerPanel
        v-else-if="activeRole === 'manager'"
        :token="sessions.managerToken"
        @logout="logoutManager"
      />
      <UserPanel
        v-else
        :token="sessions.userToken"
        :account-no="sessions.userAccountNo"
        @logout="logoutUser"
      />
    </section>
  </div>
</template>

<script setup>
import { onMounted, reactive, ref } from "vue";
import { ElMessage } from "element-plus";
import SuperPanel from "./components/SuperPanel.vue";
import ManagerPanel from "./components/ManagerPanel.vue";
import UserPanel from "./components/UserPanel.vue";
import { commonApi, managerApi, parseApiError, superApi, userApi } from "./lib/http";

const STORAGE_KEYS = {
  superToken: "oas_cloud_super_token",
  managerToken: "oas_cloud_manager_token",
  userToken: "oas_cloud_user_token",
  userAccountNo: "oas_cloud_user_account_no",
};

const roleOptions = [
  { label: "超管工作台", value: "super" },
  { label: "管理员工作台", value: "manager" },
  { label: "普通用户工作台", value: "user" },
];

const roleTitleMap = {
  super: "超级管理员页面",
  manager: "管理员页面",
  user: "普通用户页面",
};

const roleDescMap = {
  super: "发放管理员续费秘钥、治理管理员状态与到期策略。",
  manager: "管理下属用户生命周期、激活码、任务配置与执行日志。",
  user: "维护个人任务开关与时间配置，追踪自己的执行日志。",
};

const activeRole = ref("super");

const sessions = reactive({
  superToken: localStorage.getItem(STORAGE_KEYS.superToken) || "",
  managerToken: localStorage.getItem(STORAGE_KEYS.managerToken) || "",
  userToken: localStorage.getItem(STORAGE_KEYS.userToken) || "",
  userAccountNo: localStorage.getItem(STORAGE_KEYS.userAccountNo) || "",
});

const platform = reactive({
  health: { ok: false },
  scheduler: { enabled: false },
});

const superAuth = reactive({
  bootstrapInitialized: false,
  form: {
    username: "",
    password: "",
  },
});

const managerAuth = reactive({
  form: {
    username: "",
    password: "",
  },
});

const userAuth = reactive({
  registerCode: "",
  accountNo: sessions.userAccountNo || "",
});

onMounted(async () => {
  await refreshPlatformStatus();
  await loadBootstrapStatus();
});

async function refreshPlatformStatus() {
  try {
    const [health, scheduler] = await Promise.all([
      commonApi.health(),
      commonApi.schedulerStatus(),
    ]);
    platform.health.ok = health?.status === "ok";
    platform.scheduler.enabled = !!scheduler?.enabled;
  } catch (error) {
    platform.health.ok = false;
    platform.scheduler.enabled = false;
    ElMessage.warning(parseApiError(error));
  }
}

async function loadBootstrapStatus() {
  try {
    const response = await superApi.bootstrapStatus();
    superAuth.bootstrapInitialized = !!response.initialized;
  } catch (error) {
    ElMessage.error(parseApiError(error));
  }
}

async function bootstrapInit() {
  const username = superAuth.form.username.trim();
  const password = superAuth.form.password;
  if (!username || !password) {
    ElMessage.warning("请输入超管账号和密码");
    return;
  }
  try {
    await superApi.bootstrapInit({ username, password });
    ElMessage.success("超管初始化成功");
    await loadBootstrapStatus();
  } catch (error) {
    ElMessage.error(parseApiError(error));
  }
}

async function superLogin() {
  const username = superAuth.form.username.trim();
  const password = superAuth.form.password;
  if (!username || !password) {
    ElMessage.warning("请输入超管账号和密码");
    return;
  }
  try {
    const response = await superApi.login({ username, password });
    sessions.superToken = response.token || "";
    localStorage.setItem(STORAGE_KEYS.superToken, sessions.superToken);
    activeRole.value = "super";
    ElMessage.success("超级管理员登录成功");
  } catch (error) {
    ElMessage.error(parseApiError(error));
  }
}

async function registerManager() {
  const username = managerAuth.form.username.trim();
  const password = managerAuth.form.password;
  if (!username || !password) {
    ElMessage.warning("请输入管理员账号和密码");
    return;
  }
  try {
    await managerApi.register({ username, password });
    ElMessage.success("管理员注册成功，请先兑换续费秘钥再登录");
  } catch (error) {
    ElMessage.error(parseApiError(error));
  }
}

async function loginManager() {
  const username = managerAuth.form.username.trim();
  const password = managerAuth.form.password;
  if (!username || !password) {
    ElMessage.warning("请输入管理员账号和密码");
    return;
  }
  try {
    const response = await managerApi.login({ username, password });
    sessions.managerToken = response.token || "";
    localStorage.setItem(STORAGE_KEYS.managerToken, sessions.managerToken);
    activeRole.value = "manager";
    ElMessage.success("管理员登录成功");
  } catch (error) {
    ElMessage.error(parseApiError(error));
  }
}

async function registerUserByCode() {
  const code = userAuth.registerCode.trim();
  if (!code) {
    ElMessage.warning("请输入用户激活码");
    return;
  }
  try {
    const response = await userApi.registerByCode({ code });
    sessions.userToken = response.token || "";
    sessions.userAccountNo = response.account_no || "";
    userAuth.accountNo = sessions.userAccountNo;
    localStorage.setItem(STORAGE_KEYS.userToken, sessions.userToken);
    localStorage.setItem(STORAGE_KEYS.userAccountNo, sessions.userAccountNo);
    activeRole.value = "user";
    ElMessage.success("普通用户注册并登录成功");
  } catch (error) {
    ElMessage.error(parseApiError(error));
  }
}

async function loginUser() {
  const accountNo = userAuth.accountNo.trim();
  if (!accountNo) {
    ElMessage.warning("请输入普通用户账号");
    return;
  }
  try {
    const response = await userApi.login({ account_no: accountNo });
    sessions.userToken = response.token || "";
    sessions.userAccountNo = response.account_no || accountNo;
    localStorage.setItem(STORAGE_KEYS.userToken, sessions.userToken);
    localStorage.setItem(STORAGE_KEYS.userAccountNo, sessions.userAccountNo);
    activeRole.value = "user";
    ElMessage.success("普通用户登录成功");
  } catch (error) {
    ElMessage.error(parseApiError(error));
  }
}

function logoutSuper() {
  sessions.superToken = "";
  localStorage.removeItem(STORAGE_KEYS.superToken);
  ElMessage.success("已退出超管会话");
}

function logoutManager() {
  sessions.managerToken = "";
  localStorage.removeItem(STORAGE_KEYS.managerToken);
  ElMessage.success("已退出管理员会话");
}

function logoutUser() {
  sessions.userToken = "";
  localStorage.removeItem(STORAGE_KEYS.userToken);
  ElMessage.success("已退出普通用户会话");
}
</script>
