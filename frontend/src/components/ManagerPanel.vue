<template>
  <div class="role-dashboard">
    <el-empty
      v-if="!token"
      description="请先在上方登录管理员，再进入下属管理页面"
      :image-size="130"
    >
      <el-button type="primary" @click="$emit('logout')">返回登录区</el-button>
    </el-empty>

    <template v-else>
      <section class="panel-grid panel-grid--manager">
        <article class="panel-card panel-card--highlight">
          <div class="panel-headline">
            <h3>管理员续费</h3>
            <el-tag type="warning">续费秘钥</el-tag>
          </div>
          <el-form :model="redeemForm" inline>
            <el-form-item label="秘钥">
              <el-input v-model="redeemForm.code" placeholder="mrk_xxx" clearable />
            </el-form-item>
            <el-form-item>
              <el-button type="success" :loading="loading.redeem" @click="redeemRenewalKey">兑换续费</el-button>
            </el-form-item>
          </el-form>
          <el-alert
            type="info"
            :closable="false"
            title="续费规则：max(now, 当前到期时间) + 秘钥天数"
          />
        </article>

        <article class="panel-card panel-card--compact">
          <div class="panel-headline">
            <h3>激活码与快速建号</h3>
            <el-button text type="primary" :loading="loading.users" @click="loadUsers">刷新下属</el-button>
          </div>

          <el-form :model="activationForm" inline>
            <el-form-item label="激活天数">
              <el-input-number v-model="activationForm.duration_days" :min="1" :max="3650" />
            </el-form-item>
            <el-form-item>
              <el-button type="primary" :loading="loading.activation" @click="createActivationCode">生成激活码</el-button>
            </el-form-item>
          </el-form>
          <el-alert v-if="latestActivationCode" :closable="false" type="success" :title="`激活码：${latestActivationCode}`" />

          <el-divider />

          <el-form :model="quickForm" inline>
            <el-form-item label="建号天数">
              <el-input-number v-model="quickForm.duration_days" :min="1" :max="3650" />
            </el-form-item>
            <el-form-item>
              <el-button type="warning" :loading="loading.quickCreate" @click="quickCreateUser">快速创建下属</el-button>
            </el-form-item>
          </el-form>
          <el-alert v-if="quickCreatedAccount" :closable="false" type="info" :title="`新建账号：${quickCreatedAccount}`" />
        </article>
      </section>

      <section class="panel-card">
        <div class="panel-headline">
          <h3>下属用户列表</h3>
          <div class="row-actions">
            <el-input v-model="filters.keyword" placeholder="搜索账号" clearable style="width: 190px" />
            <el-select v-model="filters.status" clearable placeholder="状态过滤" style="width: 130px">
              <el-option label="active" value="active" />
              <el-option label="expired" value="expired" />
              <el-option label="disabled" value="disabled" />
            </el-select>
          </div>
        </div>

        <el-table
          :data="filteredUsers"
          border
          stripe
          height="290"
          row-key="id"
          empty-text="暂无下属数据"
          @row-click="selectUser"
        >
          <el-table-column prop="id" label="ID" width="80" />
          <el-table-column prop="account_no" label="账号" min-width="180" />
          <el-table-column prop="status" label="状态" width="120">
            <template #default="scope">
              <el-tag :type="statusTagType(scope.row.status)">{{ scope.row.status }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="expires_at" label="到期时间" min-width="190" />
        </el-table>
      </section>

      <section class="panel-grid panel-grid--manager-detail" v-if="selectedUserId">
        <article class="panel-card">
          <div class="panel-headline">
            <h3>用户 {{ selectedUserId }} 任务配置</h3>
            <div class="row-actions">
              <el-button plain :loading="loading.tasks" @click="loadSelectedUserTasks">加载</el-button>
              <el-button type="primary" :loading="loading.saveTasks" @click="saveSelectedUserTasks">保存</el-button>
            </div>
          </div>
          <el-input
            v-model="selectedTaskConfigRaw"
            type="textarea"
            :rows="16"
            placeholder='{"签到":{"enabled":true,"next_time":"08:30"}}'
          />
          <p class="tip-text">仅 enabled === true 会被调度器识别为启用任务。</p>
        </article>

        <article class="panel-card">
          <div class="panel-headline">
            <h3>用户 {{ selectedUserId }} 执行日志</h3>
            <div class="row-actions">
              <el-input-number v-model="logsLimit" :min="10" :max="200" size="small" />
              <el-button plain :loading="loading.logs" @click="loadSelectedUserLogs">刷新日志</el-button>
            </div>
          </div>
          <el-table :data="selectedUserLogs" border stripe height="390" empty-text="暂无日志">
            <el-table-column prop="event_at" label="时间" min-width="170" />
            <el-table-column prop="event_type" label="事件" width="130" />
            <el-table-column prop="error_code" label="错误码" width="130" />
            <el-table-column prop="message" label="消息" min-width="220" />
          </el-table>
        </article>
      </section>
    </template>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref, watch } from "vue";
import { ElMessage } from "element-plus";
import { managerApi, parseApiError } from "../lib/http";

const props = defineProps({
  token: {
    type: String,
    default: "",
  },
});

defineEmits(["logout"]);

const loading = reactive({
  redeem: false,
  activation: false,
  quickCreate: false,
  users: false,
  tasks: false,
  saveTasks: false,
  logs: false,
});

const redeemForm = reactive({ code: "" });
const activationForm = reactive({ duration_days: 30 });
const quickForm = reactive({ duration_days: 30 });
const filters = reactive({ keyword: "", status: "" });

const latestActivationCode = ref("");
const quickCreatedAccount = ref("");
const users = ref([]);
const selectedUserId = ref(0);
const selectedTaskConfigRaw = ref("{}");
const selectedUserLogs = ref([]);
const logsLimit = ref(80);

const filteredUsers = computed(() => {
  const keyword = filters.keyword.trim().toLowerCase();
  return users.value.filter((item) => {
    const matchKeyword = !keyword || item.account_no?.toLowerCase().includes(keyword);
    const matchStatus = !filters.status || item.status === filters.status;
    return matchKeyword && matchStatus;
  });
});

watch(
  () => props.token,
  async (value) => {
    if (!value) {
      users.value = [];
      selectedUserId.value = 0;
      selectedTaskConfigRaw.value = "{}";
      selectedUserLogs.value = [];
      return;
    }
    await loadUsers();
  },
  { immediate: true },
);

onMounted(async () => {
  if (props.token) {
    await loadUsers();
  }
});

function statusTagType(status) {
  if (status === "active") return "success";
  if (status === "disabled") return "danger";
  if (status === "expired") return "warning";
  return "info";
}

async function redeemRenewalKey() {
  const code = redeemForm.code.trim();
  if (!code) {
    ElMessage.warning("请输入续费秘钥");
    return;
  }
  loading.redeem = true;
  try {
    await managerApi.redeemRenewalKey(props.token, { code });
    ElMessage.success("管理员续费成功");
    redeemForm.code = "";
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.redeem = false;
  }
}

async function createActivationCode() {
  loading.activation = true;
  try {
    const response = await managerApi.createActivationCode(props.token, {
      duration_days: activationForm.duration_days,
    });
    latestActivationCode.value = response.code || "";
    ElMessage.success("用户激活码已生成");
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.activation = false;
  }
}

async function quickCreateUser() {
  loading.quickCreate = true;
  try {
    const response = await managerApi.quickCreateUser(props.token, {
      duration_days: quickForm.duration_days,
    });
    quickCreatedAccount.value = response.account_no || "";
    ElMessage.success("下属账号创建成功");
    await loadUsers();
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.quickCreate = false;
  }
}

async function loadUsers() {
  loading.users = true;
  try {
    const response = await managerApi.listUsers(props.token);
    users.value = response.items || [];
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.users = false;
  }
}

async function selectUser(row) {
  selectedUserId.value = row.id;
  await Promise.all([loadSelectedUserTasks(), loadSelectedUserLogs()]);
}

async function loadSelectedUserTasks() {
  if (!selectedUserId.value) return;
  loading.tasks = true;
  try {
    const response = await managerApi.getUserTasks(props.token, selectedUserId.value);
    selectedTaskConfigRaw.value = JSON.stringify(response.task_config || {}, null, 2);
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.tasks = false;
  }
}

async function saveSelectedUserTasks() {
  if (!selectedUserId.value) {
    ElMessage.warning("请先选择下属账号");
    return;
  }
  let parsed = {};
  try {
    parsed = JSON.parse(selectedTaskConfigRaw.value || "{}");
  } catch (error) {
    ElMessage.error("任务配置 JSON 格式不合法");
    return;
  }

  loading.saveTasks = true;
  try {
    const response = await managerApi.putUserTasks(props.token, selectedUserId.value, {
      task_config: parsed,
    });
    selectedTaskConfigRaw.value = JSON.stringify(response.task_config || {}, null, 2);
    ElMessage.success("任务配置更新成功");
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.saveTasks = false;
  }
}

async function loadSelectedUserLogs() {
  if (!selectedUserId.value) return;
  loading.logs = true;
  try {
    const response = await managerApi.getUserLogs(props.token, selectedUserId.value, logsLimit.value);
    selectedUserLogs.value = response.items || [];
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.logs = false;
  }
}
</script>
