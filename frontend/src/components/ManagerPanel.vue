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
      <el-tabs v-model="activeTab" class="module-tabs">
        <el-tab-pane label="总览" name="overview">
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
            <h3>管理员账号状态</h3>
            <el-tag :type="statusTagType(managerProfile.status)">{{ managerProfile.status || "-" }}</el-tag>
          </div>
          <div class="stats-grid">
            <div class="stat-item">
              <span class="stat-label">账号</span>
              <strong class="stat-value">{{ managerProfile.username || "-" }}</strong>
            </div>
            <div class="stat-item">
              <span class="stat-label">到期时间</span>
              <strong class="stat-value stat-time">{{ managerProfile.expires_at || "-" }}</strong>
            </div>
            <div class="stat-item">
              <span class="stat-label">是否过期</span>
              <strong class="stat-value">{{ managerProfile.expired ? "是" : "否" }}</strong>
            </div>
          </div>
          <div class="row-actions" style="margin-top: 10px;">
            <el-button plain :loading="loading.profile" @click="loadManagerProfile">刷新账号状态</el-button>
          </div>
        </article>

        <article class="panel-card panel-card--compact">
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
          <p class="tip-text">激活码发放请到“激活码管理”，快速建号请到“下属配置”。</p>
        </article>
      </section>

      </el-tab-pane>

      <el-tab-pane label="激活码管理" name="codes">
      <section class="panel-card panel-card--highlight">
        <div class="panel-headline">
          <h3>生成激活码</h3>
          <el-tag type="warning">按类型发码</el-tag>
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
      </section>

      <section class="panel-card panel-card--compact">
        <div class="panel-headline">
          <h3>激活码管理</h3>
          <el-button text type="primary" :loading="loading.activationCodes" @click="loadActivationCodes">刷新</el-button>
        </div>
        <el-form label-width="90px" class="compact-form">
          <el-form-item label="关键词">
            <el-input v-model="activationCodeFilters.keyword" placeholder="按激活码检索" clearable />
          </el-form-item>
          <el-form-item label="状态">
            <el-select v-model="activationCodeFilters.status" clearable placeholder="全部状态">
              <el-option label="unused" value="unused" />
              <el-option label="used" value="used" />
              <el-option label="revoked" value="revoked" />
            </el-select>
          </el-form-item>
          <el-form-item label="类型">
            <el-select v-model="activationCodeFilters.user_type" clearable placeholder="全部类型">
              <el-option v-for="item in userTypeOptions" :key="item.value" :label="item.label" :value="item.value" />
            </el-select>
          </el-form-item>
          <el-form-item label="数量">
            <el-input-number v-model="activationCodeFilters.limit" :min="20" :max="2000" :step="20" />
          </el-form-item>
          <el-form-item>
            <el-button type="primary" plain @click="loadActivationCodes">应用筛选</el-button>
          </el-form-item>
        </el-form>

        <div class="stats-grid">
          <div class="stat-item">
            <span class="stat-label">总数</span>
            <strong class="stat-value">{{ activationSummary.total }}</strong>
          </div>
          <div class="stat-item">
            <span class="stat-label">unused</span>
            <strong class="stat-value">{{ activationSummary.unused }}</strong>
          </div>
          <div class="stat-item">
            <span class="stat-label">used</span>
            <strong class="stat-value">{{ activationSummary.used }}</strong>
          </div>
          <div class="stat-item">
            <span class="stat-label">revoked</span>
            <strong class="stat-value">{{ activationSummary.revoked }}</strong>
          </div>
        </div>
      </section>

      <section class="panel-card">
        <div class="panel-headline">
          <h3>激活码列表</h3>
          <span class="muted">当前列表 {{ activationCodes.length }} 条</span>
        </div>
        <el-table :data="activationCodes" border stripe height="280" empty-text="暂无激活码数据">
          <el-table-column prop="id" label="ID" width="80" />
          <el-table-column prop="code" label="激活码" min-width="220" />
          <el-table-column prop="user_type" label="类型" width="120">
            <template #default="scope">
              <el-tag type="info">{{ userTypeLabel(scope.row.user_type) }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="duration_days" label="天数" width="100" />
          <el-table-column prop="status" label="状态" width="120">
            <template #default="scope">
              <el-tag :type="keyStatusTagType(scope.row.status)">{{ scope.row.status }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="used_by_account_no" label="使用账号" min-width="170" />
          <el-table-column prop="used_at" label="使用时间" min-width="170" />
          <el-table-column prop="created_at" label="创建时间" min-width="170" />
          <el-table-column label="操作" width="120">
            <template #default="scope">
              <el-button
                v-if="scope.row.status === 'unused'"
                type="danger"
                plain
                size="small"
                :loading="scope.row._revoking"
                @click="revokeActivationCode(scope.row)"
              >
                撤销
              </el-button>
              <span v-else class="muted">-</span>
            </template>
          </el-table-column>
        </el-table>
      </section>

      </el-tab-pane>

      <el-tab-pane label="下属配置" name="users">
      <section class="panel-card panel-card--highlight">
        <div class="panel-headline">
          <h3>快速创建下属账号</h3>
          <el-tag type="warning">独立模块</el-tag>
        </div>
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
        <el-alert
          v-if="quickCreatedAccount"
          :closable="false"
          type="info"
          :title="`新建账号：${quickCreatedAccount}（${userTypeLabel(quickCreatedUserType)}）`"
        />
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
            <el-select v-model="filters.userType" clearable placeholder="类型过滤" style="width: 130px">
              <el-option v-for="item in userTypeOptions" :key="item.value" :label="item.label" :value="item.value" />
            </el-select>
          </div>
        </div>

        <div class="stats-grid" style="margin-bottom: 10px;">
          <div class="stat-item">
            <span class="stat-label">总数</span>
            <strong class="stat-value">{{ userSummary.total }}</strong>
          </div>
          <div class="stat-item">
            <span class="stat-label">active</span>
            <strong class="stat-value">{{ userSummary.active }}</strong>
          </div>
          <div class="stat-item">
            <span class="stat-label">expired</span>
            <strong class="stat-value">{{ userSummary.expired }}</strong>
          </div>
          <div class="stat-item">
            <span class="stat-label">disabled</span>
            <strong class="stat-value">{{ userSummary.disabled }}</strong>
          </div>
          <div class="stat-item">
            <span class="stat-label">日常</span>
            <strong class="stat-value">{{ userSummary.daily }}</strong>
          </div>
          <div class="stat-item">
            <span class="stat-label">对弈竞猜</span>
            <strong class="stat-value">{{ userSummary.duiyi }}</strong>
          </div>
          <div class="stat-item">
            <span class="stat-label">刷卡</span>
            <strong class="stat-value">{{ userSummary.shuaka }}</strong>
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

      </el-tab-pane>

      <el-tab-pane label="执行日志" name="logs">
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
      <el-empty
        v-else
        description="请先在“下属配置”中选择用户后再查看日志"
        :image-size="120"
      />
      </el-tab-pane>
      </el-tabs>
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
  profile: false,
  redeem: false,
  activation: false,
  quickCreate: false,
  activationCodes: false,
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
const filters = reactive({ keyword: "", status: "", userType: "" });
const activationCodeFilters = reactive({
  keyword: "",
  status: "",
  user_type: "",
  limit: 200,
});

const latestActivationCode = ref("");
const latestActivationType = ref("daily");
const quickCreatedAccount = ref("");
const quickCreatedUserType = ref("daily");
const activeTab = ref("overview");
const users = ref([]);
const activationCodes = ref([]);
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

const activationSummary = reactive({
  total: 0,
  unused: 0,
  used: 0,
  revoked: 0,
});

const managerProfile = reactive({
  id: 0,
  username: "",
  status: "",
  expires_at: "",
  expired: false,
});

const userSummary = reactive({
  total: 0,
  active: 0,
  expired: 0,
  disabled: 0,
  daily: 0,
  duiyi: 0,
  shuaka: 0,
});

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
    const matchType = !filters.userType || item.user_type === filters.userType;
    return matchKeyword && matchStatus && matchType;
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
      activationCodes.value = [];
      selectedUserId.value = 0;
      selectedUserType.value = "daily";
      selectedTaskConfigRaw.value = "{}";
      selectedUserLogs.value = [];
      managerProfile.id = 0;
      managerProfile.username = "";
      managerProfile.status = "";
      managerProfile.expires_at = "";
      managerProfile.expired = false;
      return;
    }
    await ensureTaskTemplates("daily");
    await Promise.all([loadManagerProfile(), loadUsers(), loadOverview(), loadActivationCodes()]);
  },
  { immediate: true },
);

onMounted(async () => {
  if (props.token) {
    await ensureTaskTemplates("daily");
    await Promise.all([loadManagerProfile(), loadUsers(), loadOverview(), loadActivationCodes()]);
  }
});

function statusTagType(status) {
  if (status === "active") return "success";
  if (status === "disabled") return "danger";
  if (status === "expired") return "warning";
  return "info";
}

function keyStatusTagType(status) {
  if (status === "unused") return "success";
  if (status === "used") return "warning";
  if (status === "revoked") return "danger";
  return "info";
}

function patchSummary(target, source, keys) {
  keys.forEach((key) => {
    const value = Number(source?.[key] ?? 0);
    target[key] = Number.isFinite(value) ? value : 0;
  });
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

async function loadManagerProfile() {
  loading.profile = true;
  try {
    const response = await managerApi.me(props.token);
    managerProfile.id = response.id || 0;
    managerProfile.username = response.username || "";
    managerProfile.status = response.status || "";
    managerProfile.expires_at = response.expires_at || "";
    managerProfile.expired = response.expired === true;
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.profile = false;
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
    await loadManagerProfile();
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
    await loadActivationCodes();
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

async function loadActivationCodes() {
  loading.activationCodes = true;
  try {
    const response = await managerApi.listActivationCodes(props.token, {
      keyword: activationCodeFilters.keyword || undefined,
      status: activationCodeFilters.status || undefined,
      user_type: activationCodeFilters.user_type || undefined,
      limit: activationCodeFilters.limit,
    });
    activationCodes.value = (response.items || []).map((item) => ({
      ...item,
      _revoking: false,
    }));
    patchSummary(activationSummary, response.summary, ["total", "unused", "used", "revoked"]);
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.activationCodes = false;
  }
}

async function revokeActivationCode(row) {
  if (!row?.id) {
    ElMessage.warning("无效激活码记录");
    return;
  }
  row._revoking = true;
  try {
    await managerApi.patchActivationCodeStatus(props.token, row.id, { status: "revoked" });
    ElMessage.success("激活码已撤销");
    await loadActivationCodes();
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    row._revoking = false;
  }
}

async function loadUsers() {
  loading.users = true;
  try {
    const response = await managerApi.listUsers(props.token, { limit: 2000 });
    users.value = response.items || [];
    patchSummary(userSummary, response.summary, ["total", "active", "expired", "disabled", "daily", "duiyi", "shuaka"]);
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
  activeTab.value = "users";
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
