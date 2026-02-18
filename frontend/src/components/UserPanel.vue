<template>
  <div class="role-dashboard">
    <el-empty
      v-if="!token"
      description="请先登录普通用户，再进入个人中心"
      :image-size="130"
    >
      <el-button type="primary" @click="$emit('logout')">返回登录区</el-button>
    </el-empty>

    <template v-else>
      <el-tabs v-model="activeTab" class="module-tabs">
        <el-tab-pane label="账号与续费" name="account">
          <section class="panel-grid panel-grid--user">
        <article class="panel-card panel-card--highlight">
          <div class="panel-headline">
            <h3>用户会话信息</h3>
            <el-tag type="success">已登录</el-tag>
          </div>
          <p class="tip-text">账号：{{ accountNo || '未记录' }}</p>
          <el-button type="danger" plain @click="$emit('logout')">退出当前用户会话</el-button>
        </article>

        <article class="panel-card panel-card--compact">
          <div class="panel-headline">
            <h3>激活码续费</h3>
            <el-tag type="warning">一次性激活码</el-tag>
          </div>
          <el-form :model="redeemForm" inline>
            <el-form-item label="激活码">
              <el-input v-model="redeemForm.code" placeholder="uac_xxx" clearable />
            </el-form-item>
            <el-form-item>
              <el-button type="success" :loading="loading.redeem" @click="redeemCode">兑换续费</el-button>
            </el-form-item>
          </el-form>
          <el-alert
            type="info"
            :closable="false"
            title="续费规则：未过期顺延，已过期从当前时刻重算"
          />
        </article>
          </section>

          <section class="panel-card">
        <div class="panel-headline">
          <h3>我的账号资料</h3>
          <div class="row-actions">
            <el-switch
              v-model="logoutAll"
              active-text="退出全部设备"
              inactive-text="仅退出当前设备"
            />
            <el-button plain :loading="loading.profile" @click="loadMeProfile">刷新资料</el-button>
            <el-button type="danger" :loading="loading.logout" @click="logoutWithServer">安全退出</el-button>
          </div>
        </div>
        <div class="stats-grid">
          <div class="stat-item">
            <span class="stat-label">账号</span>
            <strong class="stat-value">{{ profile.account_no || accountNo || "-" }}</strong>
          </div>
          <div class="stat-item">
            <span class="stat-label">类型</span>
            <strong class="stat-value">{{ userTypeLabel(profile.user_type) }}</strong>
          </div>
          <div class="stat-item">
            <span class="stat-label">状态</span>
            <strong class="stat-value">{{ profile.status || "-" }}</strong>
          </div>
          <div class="stat-item">
            <span class="stat-label">到期时间</span>
            <strong class="stat-value stat-time">{{ profile.expires_at || "-" }}</strong>
          </div>
          <div class="stat-item">
            <span class="stat-label">Token 到期</span>
            <strong class="stat-value stat-time">{{ profile.token_exp || "-" }}</strong>
          </div>
          <div class="stat-item">
            <span class="stat-label">最近活跃</span>
            <strong class="stat-value stat-time">{{ profile.last_used_at || "-" }}</strong>
          </div>
          <div class="stat-item">
            <span class="stat-label">上级管理员ID</span>
            <strong class="stat-value">{{ profile.manager_id || "-" }}</strong>
          </div>
        </div>
          </section>

          <section class="panel-card">
        <div class="panel-headline">
          <h3>我的资产</h3>
          <el-button plain :loading="loading.assets" @click="loadMeAssets">刷新资产</el-button>
        </div>
        <div class="stats-grid">
          <div v-for="asset in assetFields" :key="asset.key" class="stat-item">
            <span class="stat-label">{{ asset.label }}</span>
            <strong class="stat-value">{{ meAssets[asset.key] ?? 0 }}</strong>
          </div>
        </div>
          </section>
        </el-tab-pane>

        <el-tab-pane label="任务配置" name="tasks">
          <section class="panel-card">
            <article class="panel-card">
          <div class="panel-headline">
            <h3>我的任务配置（{{ userTypeLabel(profile.user_type) }}）</h3>
            <div class="row-actions">
              <el-button plain :loading="loading.tasks" @click="loadMeTasks">加载</el-button>
              <el-button type="primary" :loading="loading.saveTasks" @click="saveMeTasks">保存</el-button>
            </div>
          </div>
          <el-table :data="taskRows" border stripe height="390" empty-text="暂无可配置任务">
            <el-table-column prop="name" label="任务类型" min-width="170" />
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
                v-model="taskConfigRaw"
                type="textarea"
                :rows="12"
                placeholder='{"签到":{"enabled":true,"next_time":"09:00"}}'
              />
            </el-collapse-item>
          </el-collapse>
          <p class="tip-text">系统会按用户类型过滤任务，并按“默认 + 现有 + 提交”合并更新。</p>
            </article>
          </section>
        </el-tab-pane>

        <el-tab-pane label="执行日志" name="logs">
          <section class="panel-card">
            <article class="panel-card">
          <div class="panel-headline">
            <h3>我的执行日志</h3>
            <div class="row-actions">
              <el-input-number v-model="logsLimit" :min="10" :max="200" size="small" />
              <el-button plain :loading="loading.logs" @click="loadMeLogs">刷新日志</el-button>
            </div>
          </div>
          <el-table :data="logs" border stripe height="420" empty-text="暂无日志">
            <el-table-column prop="event_at" label="时间" min-width="170" />
            <el-table-column prop="event_type" label="事件" width="130" />
            <el-table-column prop="error_code" label="错误码" width="130" />
            <el-table-column prop="message" label="消息" min-width="220" />
          </el-table>
            </article>
          </section>
        </el-tab-pane>
      </el-tabs>
    </template>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref, watch } from "vue";
import { ElMessage } from "element-plus";
import { commonApi, parseApiError, userApi } from "../lib/http";

const props = defineProps({
  token: {
    type: String,
    default: "",
  },
  accountNo: {
    type: String,
    default: "",
  },
});

const emit = defineEmits(["logout"]);

const loading = reactive({
  profile: false,
  redeem: false,
  tasks: false,
  saveTasks: false,
  logs: false,
  logout: false,
  assets: false,
  templates: false,
});

const redeemForm = reactive({ code: "" });
const taskConfigRaw = ref("{}");
const logs = ref([]);
const logsLimit = ref(80);
const logoutAll = ref(false);
const activeTab = ref("account");
const templateCache = reactive({});

const profile = reactive({
  account_no: "",
  user_type: "daily",
  status: "",
  expires_at: "",
  token_exp: "",
  last_used_at: "",
  manager_id: "",
});

const meAssets = reactive({
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

const currentUserType = computed(() => profile.user_type || "daily");

const currentTemplate = computed(() => {
  return (
    templateCache[currentUserType.value] || {
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
      taskConfigRaw.value = "{}";
      logs.value = [];
      profile.user_type = "daily";
      return;
    }
    await loadMeProfile();
    await ensureTaskTemplates(profile.user_type);
    await Promise.all([loadMeTasks(), loadMeLogs()]);
  },
  { immediate: true },
);

onMounted(async () => {
  if (props.token) {
    await loadMeProfile();
    await ensureTaskTemplates(profile.user_type);
    await Promise.all([loadMeTasks(), loadMeLogs()]);
  }
});

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
    const parsed = JSON.parse(taskConfigRaw.value || "{}");
    return parsed && typeof parsed === "object" ? parsed : {};
  } catch {
    return {};
  }
}

function stringifyTaskConfig(config) {
  taskConfigRaw.value = JSON.stringify(config || {}, null, 2);
}

function syncAssets(assets) {
  const incoming = assets || {};
  Object.keys(meAssets).forEach((key) => {
    meAssets[key] = Number(incoming[key] ?? meAssets[key] ?? 0);
  });
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

async function loadMeProfile() {
  loading.profile = true;
  try {
    const response = await userApi.getMeProfile(props.token);
    profile.account_no = response.account_no || "";
    profile.user_type = response.user_type || "daily";
    profile.status = response.status || "";
    profile.expires_at = response.expires_at || "";
    profile.token_exp = response.token_exp || "";
    profile.last_used_at = response.last_used_at || "";
    profile.manager_id = response.manager_id || "";
    syncAssets(response.assets || {});
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.profile = false;
  }
}

async function loadMeAssets() {
  loading.assets = true;
  try {
    const response = await userApi.getMeAssets(props.token);
    profile.user_type = response.user_type || profile.user_type || "daily";
    profile.expires_at = response.expires_at || profile.expires_at;
    profile.status = response.status || profile.status;
    syncAssets(response.assets || {});
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.assets = false;
  }
}

async function redeemCode() {
  const code = redeemForm.code.trim();
  if (!code) {
    ElMessage.warning("请输入激活码");
    return;
  }
  loading.redeem = true;
  try {
    await userApi.redeemCode(props.token, { code });
    await loadMeProfile();
    await ensureTaskTemplates(profile.user_type);
    await loadMeTasks();
    ElMessage.success("续费兑换成功");
    redeemForm.code = "";
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.redeem = false;
  }
}

async function loadMeTasks() {
  loading.tasks = true;
  try {
    const response = await userApi.getMeTasks(props.token);
    const userType = response.user_type || profile.user_type || "daily";
    profile.user_type = userType;
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

async function saveMeTasks() {
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
    const response = await userApi.putMeTasks(props.token, { task_config: normalizedConfig });
    profile.user_type = response.user_type || profile.user_type || "daily";
    stringifyTaskConfig(response.task_config || normalizedConfig);
    ElMessage.success("任务配置保存成功");
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.saveTasks = false;
  }
}

async function loadMeLogs() {
  loading.logs = true;
  try {
    const response = await userApi.getMeLogs(props.token, logsLimit.value);
    logs.value = response.items || [];
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.logs = false;
  }
}

async function logoutWithServer() {
  loading.logout = true;
  try {
    await userApi.logout(props.token, { all: logoutAll.value });
    ElMessage.success(logoutAll.value ? "已退出全部设备" : "已退出当前设备");
    emit("logout");
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.logout = false;
  }
}
</script>
