<template>
  <div class="panel-grid">
    <section class="panel-card">
      <h3>管理员账号</h3>
      <el-form :model="authForm" label-width="88px" class="compact-form">
        <el-form-item label="账号">
          <el-input v-model="authForm.username" />
        </el-form-item>
        <el-form-item label="密码">
          <el-input v-model="authForm.password" type="password" show-password />
        </el-form-item>
        <el-form-item>
          <el-button type="warning" @click="registerManager">公共注册</el-button>
          <el-button type="primary" @click="loginManager">登录</el-button>
          <el-button @click="logout">退出</el-button>
        </el-form-item>
      </el-form>

      <el-divider />

      <el-form :model="redeemForm" inline>
        <el-form-item label="续费秘钥">
          <el-input v-model="redeemForm.code" placeholder="mrk_xxx" />
        </el-form-item>
        <el-form-item>
          <el-button type="success" :disabled="!token" @click="redeemRenewalKey">兑换续费</el-button>
        </el-form-item>
      </el-form>
    </section>

    <section class="panel-card">
      <h3>下属账号发放</h3>
      <el-form :model="activationForm" inline>
        <el-form-item label="激活天数">
          <el-input-number v-model="activationForm.duration_days" :min="1" :max="3650" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" :disabled="!token" @click="createActivationCode">生成激活码</el-button>
        </el-form-item>
      </el-form>
      <el-alert
        v-if="latestActivationCode"
        type="success"
        :closable="false"
        :title="`激活码：${latestActivationCode}`"
      />

      <el-divider />

      <el-form :model="quickForm" inline>
        <el-form-item label="建号天数">
          <el-input-number v-model="quickForm.duration_days" :min="1" :max="3650" />
        </el-form-item>
        <el-form-item>
          <el-button type="warning" :disabled="!token" @click="quickCreateUser">快速创建下属</el-button>
        </el-form-item>
      </el-form>
      <el-alert
        v-if="quickCreatedAccount"
        type="info"
        :closable="false"
        :title="`新账号：${quickCreatedAccount}`"
      />
    </section>

    <section class="panel-card span-2">
      <div class="inline-actions">
        <h3>下属用户</h3>
        <div>
          <el-button :disabled="!token" plain @click="loadUsers">刷新</el-button>
        </div>
      </div>
      <el-table :data="users" border height="260" @row-click="selectUser">
        <el-table-column prop="id" label="ID" width="72" />
        <el-table-column prop="account_no" label="账号" min-width="170" />
        <el-table-column prop="status" label="状态" width="120" />
        <el-table-column prop="expires_at" label="到期时间" min-width="180" />
      </el-table>
    </section>

    <section class="panel-card span-2" v-if="selectedUserId">
      <div class="inline-actions">
        <h3>用户 {{ selectedUserId }} 任务配置</h3>
        <div>
          <el-button type="primary" plain @click="loadSelectedUserTasks">加载</el-button>
          <el-button type="success" @click="saveSelectedUserTasks">保存</el-button>
        </div>
      </div>
      <el-input
        v-model="selectedTaskConfigRaw"
        type="textarea"
        :rows="12"
        placeholder='{"签到":{"enabled":true}}'
      />
    </section>

    <section class="panel-card span-2" v-if="selectedUserId">
      <div class="inline-actions">
        <h3>用户 {{ selectedUserId }} 日志</h3>
        <el-button plain @click="loadSelectedUserLogs">刷新日志</el-button>
      </div>
      <el-table :data="selectedUserLogs" border height="280">
        <el-table-column prop="event_at" label="时间" min-width="170" />
        <el-table-column prop="event_type" label="事件" width="150" />
        <el-table-column prop="error_code" label="错误码" width="150" />
        <el-table-column prop="message" label="消息" min-width="240" />
      </el-table>
    </section>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from "vue";
import { ElMessage } from "element-plus";
import { managerApi } from "../lib/http";

const STORAGE_KEY = "oas_cloud_manager_token";

const token = ref(localStorage.getItem(STORAGE_KEY) || "");
const users = ref([]);
const selectedUserId = ref(0);
const selectedTaskConfigRaw = ref("{}");
const selectedUserLogs = ref([]);
const latestActivationCode = ref("");
const quickCreatedAccount = ref("");

const authForm = reactive({
  username: "",
  password: "",
});

const redeemForm = reactive({
  code: "",
});

const activationForm = reactive({
  duration_days: 30,
});

const quickForm = reactive({
  duration_days: 30,
});

onMounted(() => {
  if (token.value) {
    loadUsers();
  }
});

async function registerManager() {
  try {
    await managerApi.register({
      username: authForm.username.trim(),
      password: authForm.password,
    });
    ElMessage.success("注册成功，请先兑换续费秘钥后登录");
  } catch (error) {
    handleError(error);
  }
}

async function loginManager() {
  try {
    const res = await managerApi.login({
      username: authForm.username.trim(),
      password: authForm.password,
    });
    token.value = res.token || "";
    localStorage.setItem(STORAGE_KEY, token.value);
    ElMessage.success("管理员登录成功");
    await loadUsers();
  } catch (error) {
    handleError(error);
  }
}

function logout() {
  token.value = "";
  localStorage.removeItem(STORAGE_KEY);
  users.value = [];
  selectedUserId.value = 0;
  selectedTaskConfigRaw.value = "{}";
  selectedUserLogs.value = [];
  ElMessage.success("已退出管理员会话");
}

async function redeemRenewalKey() {
  if (!token.value) {
    ElMessage.warning("请先登录");
    return;
  }
  try {
    await managerApi.redeemRenewalKey(token.value, { code: redeemForm.code.trim() });
    ElMessage.success("兑换成功");
  } catch (error) {
    handleError(error);
  }
}

async function createActivationCode() {
  try {
    const res = await managerApi.createActivationCode(token.value, {
      duration_days: activationForm.duration_days,
    });
    latestActivationCode.value = res.code || "";
    ElMessage.success("激活码已生成");
  } catch (error) {
    handleError(error);
  }
}

async function quickCreateUser() {
  try {
    const res = await managerApi.quickCreateUser(token.value, {
      duration_days: quickForm.duration_days,
    });
    quickCreatedAccount.value = res.account_no || "";
    ElMessage.success("下属账号创建成功");
    await loadUsers();
  } catch (error) {
    handleError(error);
  }
}

async function loadUsers() {
  if (!token.value) return;
  try {
    const res = await managerApi.listUsers(token.value);
    users.value = res.items || [];
  } catch (error) {
    handleError(error);
  }
}

async function selectUser(row) {
  selectedUserId.value = row.id;
  await loadSelectedUserTasks();
  await loadSelectedUserLogs();
}

async function loadSelectedUserTasks() {
  if (!selectedUserId.value) return;
  try {
    const res = await managerApi.getUserTasks(token.value, selectedUserId.value);
    selectedTaskConfigRaw.value = JSON.stringify(res.task_config || {}, null, 2);
  } catch (error) {
    handleError(error);
  }
}

async function saveSelectedUserTasks() {
  if (!selectedUserId.value) return;
  let parsed = {};
  try {
    parsed = JSON.parse(selectedTaskConfigRaw.value || "{}");
  } catch (error) {
    ElMessage.error("任务配置 JSON 格式不合法");
    return;
  }
  try {
    const res = await managerApi.putUserTasks(token.value, selectedUserId.value, {
      task_config: parsed,
    });
    selectedTaskConfigRaw.value = JSON.stringify(res.task_config || {}, null, 2);
    ElMessage.success("任务配置已保存");
  } catch (error) {
    handleError(error);
  }
}

async function loadSelectedUserLogs() {
  if (!selectedUserId.value) return;
  try {
    const res = await managerApi.getUserLogs(token.value, selectedUserId.value, 80);
    selectedUserLogs.value = res.items || [];
  } catch (error) {
    handleError(error);
  }
}

function handleError(error) {
  const detail = error?.response?.data?.detail || error?.message || "请求失败";
  ElMessage.error(detail);
}
</script>
