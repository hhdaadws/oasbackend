<template>
  <div class="panel-grid">
    <section class="panel-card">
      <h3>初始化与登录</h3>
      <div class="inline-actions">
        <el-button type="info" plain @click="loadBootstrapStatus">检查初始化状态</el-button>
        <el-tag :type="bootstrapInitialized ? 'success' : 'warning'">
          {{ bootstrapInitialized ? "已初始化" : "未初始化" }}
        </el-tag>
      </div>

      <el-form :model="bootstrapForm" label-width="90px" class="compact-form">
        <el-form-item label="超管账号">
          <el-input v-model="bootstrapForm.username" placeholder="首次初始化时设置" />
        </el-form-item>
        <el-form-item label="超管密码">
          <el-input v-model="bootstrapForm.password" type="password" show-password />
        </el-form-item>
        <el-form-item>
          <el-button type="warning" @click="bootstrapInit">首次初始化</el-button>
          <el-button type="primary" @click="superLogin">超管登录</el-button>
          <el-button @click="logout">退出</el-button>
        </el-form-item>
      </el-form>
    </section>

    <section class="panel-card">
      <h3>管理员续费秘钥</h3>
      <el-form :model="renewalForm" inline>
        <el-form-item label="天数">
          <el-input-number v-model="renewalForm.duration_days" :min="1" :max="3650" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" :disabled="!token" @click="createRenewalKey">生成秘钥</el-button>
        </el-form-item>
      </el-form>
      <el-alert
        v-if="latestRenewalCode"
        type="success"
        :closable="false"
        :title="`最新秘钥：${latestRenewalCode}`"
      />
    </section>

    <section class="panel-card span-2">
      <div class="inline-actions">
        <h3>管理员列表</h3>
        <el-button type="primary" plain :disabled="!token" @click="loadManagers">刷新</el-button>
      </div>
      <el-table :data="managers" border height="360">
        <el-table-column prop="id" label="ID" width="72" />
        <el-table-column prop="username" label="账号" min-width="140" />
        <el-table-column prop="status" label="状态" width="130" />
        <el-table-column prop="expires_at" label="到期时间" min-width="180" />
        <el-table-column label="操作" width="280">
          <template #default="scope">
            <el-select v-model="scope.row._targetStatus" placeholder="选择状态" style="width: 120px">
              <el-option label="active" value="active" />
              <el-option label="expired" value="expired" />
              <el-option label="disabled" value="disabled" />
            </el-select>
            <el-button
              type="primary"
              link
              :disabled="!scope.row._targetStatus || !token"
              @click="patchManagerStatus(scope.row)"
            >
              更新
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </section>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from "vue";
import { ElMessage } from "element-plus";
import { superApi } from "../lib/http";

const STORAGE_KEY = "oas_cloud_super_token";

const token = ref(localStorage.getItem(STORAGE_KEY) || "");
const bootstrapInitialized = ref(false);
const latestRenewalCode = ref("");
const managers = ref([]);

const bootstrapForm = reactive({
  username: "",
  password: "",
});

const renewalForm = reactive({
  duration_days: 30,
});

onMounted(() => {
  loadBootstrapStatus();
  if (token.value) {
    loadManagers();
  }
});

async function loadBootstrapStatus() {
  try {
    const res = await superApi.bootstrapStatus();
    bootstrapInitialized.value = !!res.initialized;
  } catch (error) {
    handleError(error);
  }
}

async function bootstrapInit() {
  try {
    await superApi.bootstrapInit({
      username: bootstrapForm.username.trim(),
      password: bootstrapForm.password,
    });
    ElMessage.success("初始化成功");
    await loadBootstrapStatus();
  } catch (error) {
    handleError(error);
  }
}

async function superLogin() {
  try {
    const res = await superApi.login({
      username: bootstrapForm.username.trim(),
      password: bootstrapForm.password,
    });
    token.value = res.token || "";
    localStorage.setItem(STORAGE_KEY, token.value);
    ElMessage.success("超管登录成功");
    await loadManagers();
  } catch (error) {
    handleError(error);
  }
}

function logout() {
  token.value = "";
  localStorage.removeItem(STORAGE_KEY);
  managers.value = [];
  ElMessage.success("已退出超管会话");
}

async function createRenewalKey() {
  if (!token.value) {
    ElMessage.warning("请先登录超管");
    return;
  }
  try {
    const res = await superApi.createManagerRenewalKey(token.value, {
      duration_days: renewalForm.duration_days,
    });
    latestRenewalCode.value = res.code || "";
    ElMessage.success("续费秘钥已生成");
  } catch (error) {
    handleError(error);
  }
}

async function loadManagers() {
  if (!token.value) return;
  try {
    const res = await superApi.listManagers(token.value);
    managers.value = (res.items || []).map((item) => ({
      ...item,
      _targetStatus: item.status,
    }));
  } catch (error) {
    handleError(error);
  }
}

async function patchManagerStatus(row) {
  try {
    await superApi.patchManagerStatus(token.value, row.id, {
      status: row._targetStatus,
    });
    ElMessage.success(`管理员 ${row.username} 状态已更新`);
    await loadManagers();
  } catch (error) {
    handleError(error);
  }
}

function handleError(error) {
  const detail = error?.response?.data?.detail || error?.message || "请求失败";
  ElMessage.error(detail);
}
</script>
