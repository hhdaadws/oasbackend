<template>
  <div class="role-dashboard">
    <el-empty
      v-if="!token"
      description="请先登录管理员，再进入下属管理页面"
      :image-size="130"
    >
      <el-button type="primary" @click="$emit('logout')">返回登录区</el-button>
    </el-empty>

    <template v-else>
      <section class="panel-card">
        <div class="panel-headline">
          <h3>运营总览</h3>
          <el-button text type="primary" :loading="loading.overview" @click="loadOverview">刷新总览</el-button>
        </div>
        <div class="stats-grid">
          <div class="stat-item">
            <span class="stat-label">下属总数</span>
            <strong class="stat-value">{{ overview.user_stats.total }}</strong>
          </div>
          <div class="stat-item">
            <span class="stat-label">活跃下属</span>
            <strong class="stat-value">{{ overview.user_stats.active }}</strong>
          </div>
          <div class="stat-item">
            <span class="stat-label">待执行任务</span>
            <strong class="stat-value">{{ overview.job_stats.pending }}</strong>
          </div>
          <div class="stat-item">
            <span class="stat-label">运行中任务</span>
            <strong class="stat-value">{{ overview.job_stats.running }}</strong>
          </div>
          <div class="stat-item">
            <span class="stat-label">24h 失败数</span>
            <strong class="stat-value">{{ overview.recent_failures_24h }}</strong>
          </div>
          <div class="stat-item">
            <span class="stat-label">最近刷新</span>
            <strong class="stat-value stat-time">{{ overview.generated_at || "-" }}</strong>
          </div>
        </div>
      </section>

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
            <el-form-item label="类型">
              <el-select v-model="activationForm.user_type" style="width: 130px">
                <el-option v-for="item in userTypeOptions" :key="item.value" :label="item.label" :value="item.value" />
              </el-select>
            </el-form-item>
            <el-form-item>
              <el-button type="primary" :loading="loading.activation" @click="createActivationCode">生成激活码</el-button>
            </el-form-item>
          </el-form>
          <el-alert v-if="latestActivationCode" :closable="false" type="success" :title="`激活码：${latestActivationCode}（${userTypeLabel(latestActivationType)}）`" />

          <el-divider />

          <el-form :model="quickForm" inline>
            <el-form-item label="建号天数">
              <el-input-number v-model="quickForm.duration_days" :min="1" :max="3650" />
            </el-form-item>
            <el-form-item label="类型">
              <el-select v-model="quickForm.user_type" style="width: 130px">
                <el-option v-for="item in userTypeOptions" :key="item.value" :label="item.label" :value="item.value" />
              </el-select>
            </el-form-item>
            <el-form-item>
              <el-button type="warning" :loading="loading.quickCreate" @click="quickCreateUser">快速创建下属</el-button>
            </el-form-item>
          </el-form>
          <el-alert v-if="quickCreatedAccount" :closable="false" type="info" :title="`新建账号：${quickCreatedAccount}（${userTypeLabel(quickCreatedUserType)}）`" />
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
          <el-table-column prop="user_type" label="类型" width="120">
            <template #default="scope">
              <el-tag type="info">{{ userTypeLabel(scope.row.user_type) }}</el-tag>
            </template>
          </el-table-column>
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
            <h3>用户 {{ selectedUserId }} 生命周期与资产（{{ userTypeLabel(selectedUserType) }}）</h3>
            <el-button plain :loading="loading.userAssets" @click="loadSelectedUserAssets">刷新</el-button>
          </div>

          <el-form :model="lifecycleForm" inline>
            <el-form-item label="延长天数">
              <el-input-number v-model="lifecycleForm.extend_days" :min="0" :max="3650" />
            </el-form-item>
            <el-form-item label="到期时间">
              <el-input v-model="lifecycleForm.expires_at" placeholder="2026-12-31 23:59" style="width: 190px" />
            </el-form-item>
            <el-form-item label="状态">
              <el-select v-model="lifecycleForm.status" clearable placeholder="自动判定" style="width: 120px">
                <el-option label="active" value="active" />
                <el-option label="expired" value="expired" />
                <el-option label="disabled" value="disabled" />
              </el-select>
            </el-form-item>
            <el-form-item>
              <el-button type="primary" :loading="loading.lifecycle" @click="saveUserLifecycle">更新生命周期</el-button>
            </el-form-item>
          </el-form>

          <div class="stats-grid">
            <div v-for="asset in assetFields" :key="asset.key" class="stat-item">
              <span class="stat-label">{{ asset.label }}</span>
              <el-input-number v-model="selectedUserAssets[asset.key]" :min="0" :max="99999999" controls-position="right" />
            </div>
          </div>
          <div class="row-actions" style="margin-top: 10px;">
            <el-button type="success" :loading="loading.saveAssets" @click="saveSelectedUserAssets">保存资产</el-button>
          </div>
        </article>

        <article class="panel-card">
          <div class="panel-headline">
            <h3>用户 {{ selectedUserId }} 全任务配置（{{ userTypeLabel(selectedUserType) }}）</h3>
            <div class="row-actions">
              <el-button plain :loading="loading.tasks" @click="loadSelectedUserTasks">加载</el-button>
              <el-button type="primary" :loading="loading.saveTasks" @click="saveSelectedUserTasks">保存</el-button>
            </div>
          </div>

          <el-table :data="taskRows" border stripe height="360" empty-text="暂无任务模板">
            <el-table-column prop="name" label="任务类型" min-width="180" />
            <el-table-column label="启用" width="100">
              <template #default="scope">
                <el-switch v-model="scope.row.config.enabled" />
              </template>
            </el-table-column>
            <el-table-column label="next_time" min-width="170">
              <template #default="scope">
                <el-input v-model="scope.row.config.next_time" placeholder="YYYY-MM-DD HH:mm" size="small" />
              </template>
            </el-table-column>
            <el-table-column label="fail_delay" width="130">
              <template #default="scope">
                <el-input-number v-model="scope.row.config.fail_delay" :min="0" :max="100000" size="small" />
              </template>
            </el-table-column>
          </el-table>

          <el-collapse style="margin-top: 10px;">
            <el-collapse-item title="高级 JSON 编辑（完整字段）" name="json">
              <el-input
                v-model="selectedTaskConfigRaw"
                type="textarea"
                :rows="12"
                placeholder='{"签到":{"enabled":true,"next_time":"08:30"}}'
              />
            </el-collapse-item>
          </el-collapse>
        </article>
      </section>

      <section class="panel-card" v-if="selectedUserId">
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
      </section>
    </template>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref, watch } from "vue";
import { ElMessage } from "element-plus";
import { commonApi, managerApi, parseApiError } from "../lib/http";

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
  overview: false,
  users: false,
  tasks: false,
  saveTasks: false,
  logs: false,
  lifecycle: false,
  userAssets: false,
  saveAssets: false,
  templates: false,
});

const redeemForm = reactive({ code: "" });
const activationForm = reactive({ duration_days: 30, user_type: "daily" });
const quickForm = reactive({ duration_days: 30, user_type: "daily" });
const filters = reactive({ keyword: "", status: "" });

const latestActivationCode = ref("");
const latestActivationType = ref("daily");
const quickCreatedAccount = ref("");
const quickCreatedUserType = ref("daily");
const users = ref([]);
const selectedUserId = ref(0);
const selectedUserType = ref("daily");
const selectedTaskConfigRaw = ref("{}");
const selectedUserLogs = ref([]);
const logsLimit = ref(80);
const templateCache = reactive({});
const userTypeOptions = ref([
  { value: "daily", label: "日常" },
  { value: "duiyi", label: "对弈竞猜" },
  { value: "shuaka", label: "刷卡" },
]);

const lifecycleForm = reactive({
  extend_days: 0,
  expires_at: "",
  status: "",
});

const selectedUserAssets = reactive({
  level: 1,
  stamina: 0,
  gouyu: 0,
  lanpiao: 0,
  gold: 0,
  gongxun: 0,
  xunzhang: 0,
  tupo_ticket: 0,
  fanhe_level: 1,
  jiuhu_level: 1,
  liao_level: 0,
});

const assetFields = [
  { key: "level", label: "等级" },
  { key: "stamina", label: "体力" },
  { key: "gouyu", label: "勾玉" },
  { key: "lanpiao", label: "蓝票" },
  { key: "gold", label: "金币" },
  { key: "gongxun", label: "功勋" },
  { key: "xunzhang", label: "勋章" },
  { key: "tupo_ticket", label: "突破票" },
  { key: "fanhe_level", label: "饭盒等级" },
  { key: "jiuhu_level", label: "酒壶等级" },
  { key: "liao_level", label: "寮等级" },
];

const overview = reactive({
  user_stats: {
    total: 0,
    active: 0,
    expired: 0,
    disabled: 0,
  },
  job_stats: {
    pending: 0,
    leased: 0,
    running: 0,
    success: 0,
    failed: 0,
  },
  recent_failures_24h: 0,
  generated_at: "",
});

const filteredUsers = computed(() => {
  const keyword = filters.keyword.trim().toLowerCase();
  return users.value.filter((item) => {
    const matchKeyword = !keyword || item.account_no?.toLowerCase().includes(keyword);
    const matchStatus = !filters.status || item.status === filters.status;
    return matchKeyword && matchStatus;
  });
});

const currentTemplate = computed(() => {
  return (
    templateCache[selectedUserType.value] || {
      order: [],
      defaultConfig: {},
    }
  );
});

const taskRows = computed(() => {
  const config = parseTaskConfigFromRaw();
  return currentTemplate.value.order.map((name) => {
    const taskCfg = ensureTaskConfig(config[name]);
    config[name] = taskCfg;
    return {
      name,
      config: taskCfg,
    };
  });
});

watch(
  () => props.token,
  async (value) => {
    if (!value) {
      users.value = [];
      selectedUserId.value = 0;
      selectedUserType.value = "daily";
      selectedTaskConfigRaw.value = "{}";
      selectedUserLogs.value = [];
      return;
    }
    await ensureTaskTemplates("daily");
    await loadUsers();
    await loadOverview();
  },
  { immediate: true },
);

onMounted(async () => {
  if (props.token) {
    await ensureTaskTemplates("daily");
    await loadUsers();
    await loadOverview();
  }
});

function statusTagType(status) {
  if (status === "active") return "success";
  if (status === "disabled") return "danger";
  if (status === "expired") return "warning";
  return "info";
}

function userTypeLabel(userType) {
  if (userType === "duiyi") return "对弈竞猜";
  if (userType === "shuaka") return "刷卡";
  return "日常";
}

function ensureTaskConfig(taskCfg) {
  const merged = {
    enabled: false,
    next_time: "",
    fail_delay: 30,
    ...(taskCfg || {}),
  };
  if (typeof merged.enabled !== "boolean") merged.enabled = false;
  if (typeof merged.next_time !== "string") merged.next_time = "";
  if (typeof merged.fail_delay !== "number") merged.fail_delay = Number(merged.fail_delay || 0);
  return merged;
}

function parseTaskConfigFromRaw() {
  try {
    const parsed = JSON.parse(selectedTaskConfigRaw.value || "{}");
    return parsed && typeof parsed === "object" ? parsed : {};
  } catch {
    return {};
  }
}

function stringifyTaskConfig(config) {
  selectedTaskConfigRaw.value = JSON.stringify(config || {}, null, 2);
}

async function ensureTaskTemplates(userType) {
  const normalizedType = userType || "daily";
  if (templateCache[normalizedType]) return templateCache[normalizedType];
  loading.templates = true;
  try {
    const response = await commonApi.taskTemplates(normalizedType);
    templateCache[normalizedType] = {
      order: response.order || [],
      defaultConfig: response.default_config || {},
    };
    if (Array.isArray(response.supported_user_types) && response.supported_user_types.length > 0) {
      userTypeOptions.value = response.supported_user_types.map((item) => ({
        value: item,
        label: userTypeLabel(item),
      }));
    }
    return templateCache[normalizedType];
  } catch (error) {
    ElMessage.error(parseApiError(error));
    return {
      order: [],
      defaultConfig: {},
    };
  } finally {
    loading.templates = false;
  }
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
      user_type: activationForm.user_type,
    });
    latestActivationCode.value = response.code || "";
    latestActivationType.value = response.user_type || activationForm.user_type;
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
      user_type: quickForm.user_type,
    });
    quickCreatedAccount.value = response.account_no || "";
    quickCreatedUserType.value = response.user_type || quickForm.user_type;
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

async function loadOverview() {
  loading.overview = true;
  try {
    const response = await managerApi.overview(props.token);
    overview.user_stats = {
      ...overview.user_stats,
      ...(response.user_stats || {}),
    };
    overview.job_stats = {
      ...overview.job_stats,
      ...(response.job_stats || {}),
    };
    overview.recent_failures_24h = response.recent_failures_24h || 0;
    overview.generated_at = response.generated_at || "";
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.overview = false;
  }
}

async function selectUser(row) {
  selectedUserId.value = row.id;
  selectedUserType.value = row.user_type || "daily";
  lifecycleForm.expires_at = row.expires_at || "";
  lifecycleForm.extend_days = 0;
  lifecycleForm.status = row.status || "";
  await ensureTaskTemplates(selectedUserType.value);
  await Promise.all([loadSelectedUserTasks(), loadSelectedUserAssets(), loadSelectedUserLogs()]);
}

async function loadSelectedUserTasks() {
  if (!selectedUserId.value) return;
  loading.tasks = true;
  try {
    const response = await managerApi.getUserTasks(props.token, selectedUserId.value);
    const userType = response.user_type || selectedUserType.value || "daily";
    selectedUserType.value = userType;
    const template = await ensureTaskTemplates(userType);
    const merged = {
      ...(template.defaultConfig || {}),
      ...(response.task_config || {}),
    };
    stringifyTaskConfig(merged);
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

  const normalizedConfig = {};
  const rows = taskRows.value;
  rows.forEach((row) => {
    normalizedConfig[row.name] = {
      ...(row.config || {}),
      enabled: row.config.enabled === true,
    };
  });

  loading.saveTasks = true;
  try {
    const response = await managerApi.putUserTasks(props.token, selectedUserId.value, {
      task_config: normalizedConfig,
    });
    stringifyTaskConfig(response.task_config || normalizedConfig);
    ElMessage.success("任务配置更新成功");
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.saveTasks = false;
  }
}

async function loadSelectedUserAssets() {
  if (!selectedUserId.value) return;
  loading.userAssets = true;
  try {
    const response = await managerApi.getUserAssets(props.token, selectedUserId.value);
    const incoming = response.assets || {};
    selectedUserType.value = response.user_type || selectedUserType.value || "daily";
    Object.keys(selectedUserAssets).forEach((key) => {
      selectedUserAssets[key] = Number(incoming[key] ?? selectedUserAssets[key] ?? 0);
    });
    lifecycleForm.expires_at = response.expires_at || lifecycleForm.expires_at;
    lifecycleForm.status = response.status || lifecycleForm.status;
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.userAssets = false;
  }
}

async function saveSelectedUserAssets() {
  if (!selectedUserId.value) return;
  loading.saveAssets = true;
  try {
    await managerApi.putUserAssets(props.token, selectedUserId.value, {
      assets: { ...selectedUserAssets },
    });
    ElMessage.success("用户资产已更新");
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.saveAssets = false;
  }
}

async function saveUserLifecycle() {
  if (!selectedUserId.value) return;
  loading.lifecycle = true;
  try {
    await managerApi.patchUserLifecycle(props.token, selectedUserId.value, {
      extend_days: lifecycleForm.extend_days || 0,
      expires_at: lifecycleForm.expires_at || "",
      status: lifecycleForm.status || "",
    });
    ElMessage.success("用户过期时间/状态更新成功");
    await loadUsers();
    await loadSelectedUserAssets();
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.lifecycle = false;
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
