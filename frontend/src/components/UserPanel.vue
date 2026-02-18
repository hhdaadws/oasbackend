<template>
  <div class="panel-grid">
    <section class="panel-card">
      <h3>普通用户注册与登录</h3>
      <el-form :model="registerForm" label-width="98px" class="compact-form">
        <el-form-item label="激活码注册">
          <el-input v-model="registerForm.code" placeholder="uac_xxx" />
        </el-form-item>
        <el-form-item>
          <el-button type="warning" @click="registerByCode">注册并登录</el-button>
        </el-form-item>
      </el-form>
      <el-alert
        v-if="registeredAccountNo"
        :closable="false"
        type="success"
        :title="`账号已生成：${registeredAccountNo}`"
      />

      <el-divider />

      <el-form :model="loginForm" label-width="98px" class="compact-form">
        <el-form-item label="账号登录">
          <el-input v-model="loginForm.account_no" placeholder="输入账号号" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="userLogin">登录</el-button>
          <el-button @click="logout">退出</el-button>
        </el-form-item>
      </el-form>
    </section>

    <section class="panel-card">
      <h3>续费兑换</h3>
      <el-form :model="redeemForm" inline>
        <el-form-item label="激活码">
          <el-input v-model="redeemForm.code" placeholder="uac_xxx" />
        </el-form-item>
        <el-form-item>
          <el-button type="success" :disabled="!token" @click="redeemCode">兑换续费</el-button>
        </el-form-item>
      </el-form>
      <el-alert
        type="info"
        :closable="false"
        title="续费规则：未过期按剩余时间顺延，已过期从当前时间延长"
      />
    </section>

    <section class="panel-card span-2">
      <div class="inline-actions">
        <h3>我的任务配置</h3>
        <div>
          <el-button plain :disabled="!token" @click="loadMeTasks">加载</el-button>
          <el-button type="primary" :disabled="!token" @click="saveMeTasks">保存</el-button>
        </div>
      </div>
      <el-input
        v-model="taskConfigRaw"
        type="textarea"
        :rows="14"
        placeholder='{"签到":{"enabled":true}}'
      />
    </section>

    <section class="panel-card span-2">
      <div class="inline-actions">
        <h3>我的日志</h3>
        <el-button plain :disabled="!token" @click="loadMeLogs">刷新日志</el-button>
      </div>
      <el-table :data="logs" border height="320">
        <el-table-column prop="event_at" label="时间" min-width="170" />
        <el-table-column prop="event_type" label="事件" width="140" />
        <el-table-column prop="error_code" label="错误码" width="140" />
        <el-table-column prop="message" label="消息" min-width="280" />
      </el-table>
    </section>
  </div>
</template>

<script setup>
import { reactive, ref } from "vue";
import { ElMessage } from "element-plus";
import { userApi } from "../lib/http";

const STORAGE_KEY = "oas_cloud_user_token";
const ACCOUNT_KEY = "oas_cloud_user_account_no";

const token = ref(localStorage.getItem(STORAGE_KEY) || "");
const registeredAccountNo = ref(localStorage.getItem(ACCOUNT_KEY) || "");
const taskConfigRaw = ref("{}");
const logs = ref([]);

const registerForm = reactive({
  code: "",
});

const loginForm = reactive({
  account_no: registeredAccountNo.value || "",
});

const redeemForm = reactive({
  code: "",
});

async function registerByCode() {
  try {
    const res = await userApi.registerByCode({ code: registerForm.code.trim() });
    registeredAccountNo.value = res.account_no || "";
    loginForm.account_no = registeredAccountNo.value;
    token.value = res.token || "";
    localStorage.setItem(STORAGE_KEY, token.value);
    localStorage.setItem(ACCOUNT_KEY, registeredAccountNo.value);
    ElMessage.success("注册成功并已登录");
    await loadMeTasks();
    await loadMeLogs();
  } catch (error) {
    handleError(error);
  }
}

async function userLogin() {
  try {
    const res = await userApi.login({ account_no: loginForm.account_no.trim() });
    token.value = res.token || "";
    registeredAccountNo.value = res.account_no || loginForm.account_no.trim();
    localStorage.setItem(STORAGE_KEY, token.value);
    localStorage.setItem(ACCOUNT_KEY, registeredAccountNo.value);
    ElMessage.success("登录成功");
    await loadMeTasks();
    await loadMeLogs();
  } catch (error) {
    handleError(error);
  }
}

function logout() {
  token.value = "";
  localStorage.removeItem(STORAGE_KEY);
  ElMessage.success("已退出普通用户会话");
}

async function redeemCode() {
  if (!token.value) {
    ElMessage.warning("请先登录");
    return;
  }
  try {
    await userApi.redeemCode(token.value, { code: redeemForm.code.trim() });
    ElMessage.success("续费兑换成功");
  } catch (error) {
    handleError(error);
  }
}

async function loadMeTasks() {
  if (!token.value) return;
  try {
    const res = await userApi.getMeTasks(token.value);
    taskConfigRaw.value = JSON.stringify(res.task_config || {}, null, 2);
  } catch (error) {
    handleError(error);
  }
}

async function saveMeTasks() {
  if (!token.value) return;
  let parsed = {};
  try {
    parsed = JSON.parse(taskConfigRaw.value || "{}");
  } catch (error) {
    ElMessage.error("任务配置 JSON 格式不合法");
    return;
  }
  try {
    const res = await userApi.putMeTasks(token.value, { task_config: parsed });
    taskConfigRaw.value = JSON.stringify(res.task_config || {}, null, 2);
    ElMessage.success("任务配置已保存");
  } catch (error) {
    handleError(error);
  }
}

async function loadMeLogs() {
  if (!token.value) return;
  try {
    const res = await userApi.getMeLogs(token.value, 80);
    logs.value = res.items || [];
  } catch (error) {
    handleError(error);
  }
}

function handleError(error) {
  const detail = error?.response?.data?.detail || error?.message || "请求失败";
  ElMessage.error(detail);
}
</script>
