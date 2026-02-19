<template>
  <div class="page-shell page-shell--login">
    <header class="console-header glass-card stagger-1">
      <div class="brand">
        <p class="eyebrow">云端指令中心</p>
        <h1>OAS 云端登录中心</h1>
      </div>
    </header>

    <section class="login-center-wrap">
      <section class="auth-card glass-card stagger-2 login-center-card">
      <el-tabs v-model="activeAuthTab" class="module-tabs">
        <el-tab-pane label="管理员登录" name="manager">
          <div class="panel-headline">
            <h3>管理员登录</h3>
            <el-tag type="success" v-if="session.managerToken">已登录</el-tag>
          </div>

          <el-form :model="managerForm" label-width="100px" class="compact-form">
            <el-form-item label="账号">
              <el-input v-model="managerForm.username" class="auth-input" placeholder="账号示例：mgr_001" clearable />
            </el-form-item>
            <el-form-item label="密码">
              <el-input v-model="managerForm.password" class="auth-input" type="password" show-password />
            </el-form-item>
            <el-form-item>
              <el-button type="warning" :loading="loading.managerRegister" @click="registerManager">公共注册</el-button>
              <el-button type="primary" :loading="loading.managerLogin" @click="loginManager">登录并进入</el-button>
            </el-form-item>
          </el-form>
          <el-alert
            type="info"
            :closable="false"
            title="管理员注册后默认过期，请登录后在管理员页面兑换续费秘钥。"
          />
        </el-tab-pane>

        <el-tab-pane label="普通用户登录" name="user">
          <div class="panel-headline">
            <h3>普通用户登录</h3>
            <el-tag type="success" v-if="session.userToken">已登录</el-tag>
          </div>

          <el-form :model="userForm" label-width="100px" class="compact-form">
            <div v-if="savedAccounts.length" class="saved-accounts-section">
              <div class="saved-accounts-title">已保存的账号</div>
              <div
                v-for="account in savedAccounts"
                :key="account.account_no"
                class="saved-account-item"
                @click="loginWithSaved(account)"
              >
                <div class="saved-account-info">
                  <div class="saved-account-name">
                    {{ account.login_id || account.account_no }}
                  </div>
                  <div class="saved-account-sub" v-if="account.login_id">
                    {{ account.account_no }}
                  </div>
                </div>
                <el-tag :type="statusTagType(account.status)" size="small" class="tag-gap">
                  {{ statusLabel(account.status) }}
                </el-tag>
                <el-tag :type="account.archive_status === 'normal' ? 'success' : 'danger'" size="small" class="tag-gap">
                  {{ account.archive_status === 'normal' ? '正常' : '失效' }}
                </el-tag>
                <el-button link type="danger" size="small" @click.stop="deleteSavedAccount(account.account_no)">
                  删除
                </el-button>
              </div>
              <el-divider />
            </div>
            <el-form-item label="激活码注册">
              <el-input v-model="userForm.registerCode" class="auth-input" placeholder="激活码示例：uac_xxx" clearable />
            </el-form-item>
            <el-form-item>
              <el-button type="warning" :loading="loading.userRegister" @click="registerUserByCode">
                注册并进入
              </el-button>
            </el-form-item>

            <el-divider />

            <el-form-item label="账号登录">
              <el-input v-model="userForm.accountNo" class="auth-input" placeholder="账号示例：U2026..." clearable />
            </el-form-item>
            <el-form-item>
              <el-button type="primary" :loading="loading.userLogin" @click="loginUser">登录并进入</el-button>
            </el-form-item>
          </el-form>
        </el-tab-pane>
      </el-tabs>
      </section>

      <section class="session-card glass-card stagger-4 login-session-card">
        <div class="panel-headline">
          <h3>当前会话</h3>
        </div>
        <div class="session-actions">
          <el-button
            type="primary"
            plain
            :disabled="!session.managerToken"
            @click="$emit('navigate', '/manager')"
          >
            进入管理员页面
          </el-button>
          <el-button
            type="success"
            plain
            :disabled="!session.userToken"
            @click="$emit('navigate', '/user')"
          >
            进入普通用户页面
          </el-button>
        </div>
      </section>
    </section>

    <el-dialog
      v-model="showAccountSaveDialog"
      title="注册成功 — 请保存您的账号"
      class="dialog-sm"
      :close-on-click-modal="false"
      :close-on-press-escape="false"
      :show-close="false"
      append-to-body
    >
      <el-alert
        type="warning"
        :closable="false"
        show-icon
        title="请务必保存以下账号信息，这是您登录的唯一凭证！丢失后无法找回。"
        class="mb-20"
      />
      <div class="account-display-center">
        <span class="account-no-highlight">
          {{ registeredAccountNo }}
        </span>
      </div>
      <div class="dialog-actions-center">
        <el-button type="primary" @click="copyAccountNo">复制账号</el-button>
        <el-button type="success" @click="downloadAccountInfo">下载账号信息</el-button>
      </div>
      <template #footer>
        <el-button
          type="warning"
          :disabled="!accountSaved"
          @click="confirmAccountSaved"
        >
          我已保存，进入系统
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { reactive, ref } from "vue";
import { ElMessage } from "element-plus";
import { managerApi, parseApiError, userApi } from "../lib/http";
import { setManagerToken, setUserSession, getSavedAccounts, upsertSavedAccount, removeSavedAccount } from "../lib/session";
import { copyToClipboard, statusTagType, statusLabel } from "../lib/helpers";

const props = defineProps({
  session: {
    type: Object,
    required: true,
  },
});

const emit = defineEmits(["navigate", "session-updated"]);
const activeAuthTab = ref("manager");

const managerForm = reactive({
  username: "",
  password: "",
});

const userForm = reactive({
  registerCode: "",
  accountNo: props.session.userAccountNo || "",
});

const showAccountSaveDialog = ref(false);
const registeredAccountNo = ref("");
const accountSaved = ref(false);
const savedAccounts = ref(getSavedAccounts());

const loading = reactive({
  managerRegister: false,
  managerLogin: false,
  userRegister: false,
  userLogin: false,
});

async function registerManager() {
  const username = managerForm.username.trim();
  const password = managerForm.password;
  if (!username || !password) {
    ElMessage.warning("请输入管理员账号和密码");
    return;
  }
  loading.managerRegister = true;
  try {
    await managerApi.register({ username, password });
    ElMessage.success("管理员注册成功，请先续费后登录");
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.managerRegister = false;
  }
}

async function loginManager() {
  const username = managerForm.username.trim();
  const password = managerForm.password;
  if (!username || !password) {
    ElMessage.warning("请输入管理员账号和密码");
    return;
  }
  loading.managerLogin = true;
  try {
    const response = await managerApi.login({ username, password });
    setManagerToken(response.token || "");
    emit("session-updated");
    ElMessage.success("管理员登录成功");
    emit("navigate", "/manager");
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.managerLogin = false;
  }
}

async function registerUserByCode() {
  const code = userForm.registerCode.trim();
  if (!code) {
    ElMessage.warning("请输入用户激活码");
    return;
  }
  loading.userRegister = true;
  try {
    const response = await userApi.registerByCode({ code });
    setUserSession(response.token || "", response.account_no || "");
    userForm.accountNo = response.account_no || "";
    emit("session-updated");
    try {
      const profile = await userApi.getMeProfile(response.token);
      upsertSavedAccount({
        account_no: response.account_no,
        login_id: profile.login_id,
        username: profile.username,
        status: profile.status,
        user_type: response.user_type,
        archive_status: profile.archive_status,
      });
    } catch {
      upsertSavedAccount({ account_no: response.account_no, user_type: response.user_type });
    }
    savedAccounts.value = getSavedAccounts();
    registeredAccountNo.value = response.account_no || "";
    accountSaved.value = false;
    showAccountSaveDialog.value = true;
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.userRegister = false;
  }
}

async function copyAccountNo() {
  await copyToClipboard(registeredAccountNo.value);
  ElMessage.success("账号已复制到剪贴板");
  accountSaved.value = true;
}

function downloadAccountInfo() {
  const content = `OAS 云端账号信息\n\n账号: ${registeredAccountNo.value}\n\n请妥善保管此账号，这是您登录的唯一凭证，丢失后无法找回！`;
  const blob = new Blob([content], { type: "text/plain;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = `OAS账号_${registeredAccountNo.value}.txt`;
  a.click();
  URL.revokeObjectURL(url);
  accountSaved.value = true;
}

function confirmAccountSaved() {
  showAccountSaveDialog.value = false;
  ElMessage.success("普通用户注册并登录成功");
  emit("navigate", "/user");
}

async function loginUser() {
  const accountNo = userForm.accountNo.trim();
  if (!accountNo) {
    ElMessage.warning("请输入普通用户账号");
    return;
  }
  loading.userLogin = true;
  try {
    const response = await userApi.login({ account_no: accountNo });
    setUserSession(response.token || "", response.account_no || accountNo);
    emit("session-updated");
    try {
      const profile = await userApi.getMeProfile(response.token);
      upsertSavedAccount({
        account_no: response.account_no || accountNo,
        login_id: profile.login_id,
        username: profile.username,
        status: profile.status,
        user_type: profile.user_type,
        archive_status: profile.archive_status,
      });
    } catch {
      upsertSavedAccount({ account_no: response.account_no || accountNo });
    }
    savedAccounts.value = getSavedAccounts();
    ElMessage.success("普通用户登录成功");
    emit("navigate", "/user");
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.userLogin = false;
  }
}

function loginWithSaved(account) {
  userForm.accountNo = account.account_no;
  loginUser();
}

function deleteSavedAccount(accountNo) {
  removeSavedAccount(accountNo);
  savedAccounts.value = getSavedAccounts();
}
</script>
