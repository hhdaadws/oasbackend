<script setup>
import { onMounted, reactive, watch } from "vue";
import { ElMessage } from "element-plus";
import { parseApiError, userApi } from "../../lib/http";
import { userTypeLabel, formatTime } from "../../lib/helpers";

const props = defineProps({
  token: { type: String, default: "" },
  accountNo: { type: String, default: "" },
});

const emit = defineEmits(["logout"]);

const loading = reactive({ profile: false });

const profile = reactive({
  account_no: "", user_type: "daily", status: "", expires_at: "",
  server: "", username: "", manager_id: "",
});

function userStatusLabel(status) {
  return status === "active" ? "未过期" : "已过期";
}

function userStatusTagType(status) {
  return status === "active" ? "success" : "danger";
}

watch(
  () => props.token,
  async (value) => {
    if (!value) { profile.user_type = "daily"; return; }
    await loadMeProfile();
  },
  { immediate: true },
);

onMounted(async () => {
  if (props.token) await loadMeProfile();
});

async function loadMeProfile() {
  loading.profile = true;
  try {
    const response = await userApi.getMeProfile(props.token);
    profile.account_no = response.account_no || "";
    profile.user_type = response.user_type || "daily";
    profile.status = response.status || "";
    profile.expires_at = response.expires_at || "";
    profile.server = response.server || "";
    profile.username = response.username || "";
    profile.manager_id = response.manager_id || "";
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.profile = false;
  }
}
</script>

<template>
  <section class="panel-card panel-card--highlight">
    <div class="panel-headline">
      <h3>用户会话信息</h3>
      <div class="row-actions">
        <el-tag :type="userStatusTagType(profile.status)">{{ userStatusLabel(profile.status) }}</el-tag>
        <el-button type="danger" plain @click="$emit('logout')">退出登录</el-button>
      </div>
    </div>
    <div class="stats-grid">
      <div class="stat-item">
        <span class="stat-label">账号</span>
        <strong class="stat-value">{{ accountNo || "未记录" }}</strong>
      </div>
      <div class="stat-item">
        <span class="stat-label">到期时间</span>
        <strong class="stat-value stat-time">{{ formatTime(profile.expires_at) }}</strong>
      </div>
    </div>
  </section>

  <section class="panel-card">
    <div class="panel-headline">
      <h3>我的账号资料</h3>
      <el-button plain :loading="loading.profile" @click="loadMeProfile">刷新资料</el-button>
    </div>
    <div class="stats-grid">
      <div class="stat-item">
        <span class="stat-label">服务器</span>
        <strong class="stat-value">{{ profile.server || "-" }}</strong>
      </div>
      <div class="stat-item">
        <span class="stat-label">用户名</span>
        <strong class="stat-value">{{ profile.username || "-" }}</strong>
      </div>
      <div class="stat-item">
        <span class="stat-label">类型</span>
        <strong class="stat-value">{{ userTypeLabel(profile.user_type) }}</strong>
      </div>
      <div class="stat-item">
        <span class="stat-label">状态</span>
        <el-tag :type="userStatusTagType(profile.status)">{{ userStatusLabel(profile.status) }}</el-tag>
      </div>
      <div class="stat-item">
        <span class="stat-label">到期时间</span>
        <strong class="stat-value stat-time">{{ formatTime(profile.expires_at) }}</strong>
      </div>
      <div class="stat-item">
        <span class="stat-label">上级管理员ID</span>
        <strong class="stat-value">{{ profile.manager_id || "-" }}</strong>
      </div>
    </div>
  </section>
</template>
