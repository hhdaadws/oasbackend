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

const loading = reactive({ profile: false, saveProfile: false });

const profile = reactive({
  account_no: "", user_type: "daily", status: "", archive_status: "normal",
  expires_at: "", server: "", username: "", manager_id: "",
});

const editForm = reactive({ server: "", username: "" });

function archiveStatusLabel(s) {
  return s === "normal" ? "正常" : "失效";
}

function archiveStatusTagType(s) {
  return s === "normal" ? "success" : "danger";
}

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
    profile.archive_status = response.archive_status || "normal";
    profile.expires_at = response.expires_at || "";
    profile.server = response.server || "";
    profile.username = response.username || "";
    profile.manager_id = response.manager_id || "";
    editForm.server = profile.server;
    editForm.username = profile.username;
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.profile = false;
  }
}

async function saveMeProfile() {
  loading.saveProfile = true;
  try {
    await userApi.putMeProfile(props.token, {
      server: editForm.server,
      username: editForm.username,
    });
    profile.server = editForm.server;
    profile.username = editForm.username;
    ElMessage.success("保存成功");
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.saveProfile = false;
  }
}
</script>

<template>
  <section class="panel-card panel-card--highlight">
    <div class="panel-headline">
      <h3>用户信息</h3>
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
      <div class="row-actions">
        <el-button plain :loading="loading.profile" @click="loadMeProfile">刷新</el-button>
        <el-button type="primary" :loading="loading.saveProfile" @click="saveMeProfile">保存</el-button>
      </div>
    </div>
    <el-form label-width="100px" style="margin-top:12px">
      <el-form-item label="服务器">
        <el-input v-model="editForm.server" placeholder="请输入服务器" clearable />
      </el-form-item>
      <el-form-item label="用户名">
        <el-input v-model="editForm.username" placeholder="请输入用户名" clearable />
      </el-form-item>
    </el-form>
    <div class="stats-grid" style="margin-top:12px">
      <div class="stat-item">
        <span class="stat-label">存档状态</span>
        <el-tag :type="archiveStatusTagType(profile.archive_status)">{{ archiveStatusLabel(profile.archive_status) }}</el-tag>
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
