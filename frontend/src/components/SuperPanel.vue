<template>
  <div class="role-dashboard">
    <el-empty
      v-if="!token"
      description="请先登录超级管理员，再进入治理页面"
      :image-size="130"
    >
      <el-button type="primary" @click="$emit('logout')">返回登录区</el-button>
    </el-empty>

    <template v-else>
      <el-tabs v-model="activeTab" class="module-tabs">
        <el-tab-pane label="秘钥中心" name="keys">
          <section class="panel-grid panel-grid--super">
            <article class="panel-card panel-card--highlight">
              <div class="panel-headline">
                <h3>管理员续费秘钥发放</h3>
                <el-tag type="warning">一次性秘钥</el-tag>
              </div>
              <p class="tip-text">秘钥可用于管理员激活/续期，建议按周期批量发放。</p>

              <el-form :model="renewalForm" inline>
                <el-form-item label="续期天数">
                  <el-input-number v-model="renewalForm.duration_days" :min="1" :max="3650" />
                </el-form-item>
                <el-form-item>
                  <el-button type="primary" :loading="loading.createKey" @click="createRenewalKey">生成秘钥</el-button>
                </el-form-item>
              </el-form>

              <el-alert
                v-if="latestRenewal.code"
                type="success"
                :closable="false"
                :title="`最新秘钥：${latestRenewal.code}`"
                :description="`续期天数：${latestRenewal.duration_days} 天`"
              />
            </article>

            <article class="panel-card panel-card--compact">
              <div class="panel-headline">
                <h3>秘钥筛选</h3>
                <el-button text type="primary" :loading="loading.keys" @click="loadRenewalKeys">刷新</el-button>
              </div>
              <el-form label-width="90px" class="compact-form">
                <el-form-item label="关键词">
                  <el-input v-model="keyFilters.keyword" placeholder="按秘钥检索" clearable />
                </el-form-item>
                <el-form-item label="状态">
                  <el-select v-model="keyFilters.status" clearable placeholder="全部状态">
                    <el-option label="unused" value="unused" />
                    <el-option label="used" value="used" />
                    <el-option label="revoked" value="revoked" />
                  </el-select>
                </el-form-item>
                <el-form-item label="数量">
                  <el-input-number v-model="keyFilters.limit" :min="20" :max="2000" :step="20" />
                </el-form-item>
                <el-form-item>
                  <el-button type="primary" plain @click="loadRenewalKeys">应用筛选</el-button>
                </el-form-item>
              </el-form>
            </article>
          </section>

          <section class="panel-card panel-card--compact">
            <div class="panel-headline">
              <h3>秘钥状态统计</h3>
              <span class="muted">全量维度</span>
            </div>
            <div class="stats-grid">
              <div class="stat-item">
                <span class="stat-label">总数</span>
                <strong class="stat-value">{{ keySummary.total }}</strong>
              </div>
              <div class="stat-item">
                <span class="stat-label">unused</span>
                <strong class="stat-value">{{ keySummary.unused }}</strong>
              </div>
              <div class="stat-item">
                <span class="stat-label">used</span>
                <strong class="stat-value">{{ keySummary.used }}</strong>
              </div>
              <div class="stat-item">
                <span class="stat-label">revoked</span>
                <strong class="stat-value">{{ keySummary.revoked }}</strong>
              </div>
            </div>
          </section>

          <section class="panel-card">
            <div class="panel-headline">
              <h3>秘钥状态总表</h3>
              <span class="muted">当前列表 {{ renewalKeys.length }} 条</span>
            </div>

            <el-table :data="renewalKeys" border stripe height="420" empty-text="暂无秘钥数据">
              <el-table-column prop="id" label="ID" width="80" />
              <el-table-column prop="code" label="秘钥" min-width="220" />
              <el-table-column prop="duration_days" label="天数" width="100" />
              <el-table-column prop="status" label="状态" width="120">
                <template #default="scope">
                  <el-tag :type="keyStatusTagType(scope.row.status)">{{ scope.row.status }}</el-tag>
                </template>
              </el-table-column>
              <el-table-column label="使用者" min-width="180">
                <template #default="scope">
                  <span>{{ scope.row.used_by_manager_username || scope.row.used_by_manager_id || "-" }}</span>
                </template>
              </el-table-column>
              <el-table-column prop="used_at" label="使用时间" min-width="180" />
              <el-table-column prop="created_at" label="创建时间" min-width="180" />
              <el-table-column label="操作" width="120">
                <template #default="scope">
                  <el-button
                    v-if="scope.row.status === 'unused'"
                    type="danger"
                    plain
                    size="small"
                    :loading="scope.row._revoking"
                    @click="revokeRenewalKey(scope.row)"
                  >
                    撤销
                  </el-button>
                  <span v-else class="muted">-</span>
                </template>
              </el-table-column>
            </el-table>
          </section>
        </el-tab-pane>

        <el-tab-pane label="管理员管理" name="managers">
          <section class="panel-card panel-card--compact">
            <div class="panel-headline">
              <h3>管理员筛选</h3>
              <el-button text type="primary" :loading="loading.list" @click="loadManagers">刷新</el-button>
            </div>
            <el-form label-width="90px" class="compact-form">
              <el-form-item label="关键词">
                <el-input v-model="filters.keyword" placeholder="按账号过滤" clearable />
              </el-form-item>
              <el-form-item label="状态">
                <el-select v-model="filters.status" clearable placeholder="全部状态">
                  <el-option label="active" value="active" />
                  <el-option label="expired" value="expired" />
                  <el-option label="disabled" value="disabled" />
                </el-select>
              </el-form-item>
              <el-form-item label="数量">
                <el-input-number v-model="filters.limit" :min="20" :max="2000" :step="20" />
              </el-form-item>
              <el-form-item>
                <el-button type="primary" plain @click="loadManagers">应用筛选</el-button>
              </el-form-item>
            </el-form>
          </section>

          <section class="panel-card panel-card--compact">
            <div class="panel-headline">
              <h3>管理员状态统计</h3>
              <span class="muted">当前列表维度</span>
            </div>
            <div class="stats-grid">
              <div class="stat-item">
                <span class="stat-label">总数</span>
                <strong class="stat-value">{{ managerSummary.total }}</strong>
              </div>
              <div class="stat-item">
                <span class="stat-label">active</span>
                <strong class="stat-value">{{ managerSummary.active }}</strong>
              </div>
              <div class="stat-item">
                <span class="stat-label">expired</span>
                <strong class="stat-value">{{ managerSummary.expired }}</strong>
              </div>
              <div class="stat-item">
                <span class="stat-label">disabled</span>
                <strong class="stat-value">{{ managerSummary.disabled }}</strong>
              </div>
              <div class="stat-item">
                <span class="stat-label">7天内到期</span>
                <strong class="stat-value">{{ managerSummary.expiring_7d }}</strong>
              </div>
            </div>
          </section>

          <section class="panel-card">
            <div class="panel-headline">
              <h3>管理员生命周期管理</h3>
              <span class="muted">当前列表 {{ managers.length }} 条</span>
            </div>

            <el-table :data="managers" border stripe height="450" empty-text="暂无管理员数据">
              <el-table-column prop="id" label="ID" width="80" />
              <el-table-column prop="username" label="账号" min-width="180" />
              <el-table-column prop="status" label="状态" width="130">
                <template #default="scope">
                  <el-tag :type="statusTagType(scope.row.status)">{{ scope.row.status }}</el-tag>
                </template>
              </el-table-column>
              <el-table-column label="到期标记" width="130">
                <template #default="scope">
                  <el-tag :type="scope.row.is_expired ? 'warning' : 'success'">
                    {{ scope.row.is_expired ? "已到期" : "有效" }}
                  </el-tag>
                </template>
              </el-table-column>
              <el-table-column prop="expires_at" label="到期时间" min-width="190" />
              <el-table-column label="设置到期时间" min-width="200">
                <template #default="scope">
                  <el-input v-model="scope.row._editExpiresAt" placeholder="YYYY-MM-DD HH:mm" />
                </template>
              </el-table-column>
              <el-table-column label="延长天数" width="150">
                <template #default="scope">
                  <div class="row-actions">
                    <el-input-number v-model="scope.row._extendDays" :min="0" :max="3650" :step="1" />
                  </div>
                </template>
              </el-table-column>
              <el-table-column label="操作" width="120">
                <template #default="scope">
                  <div class="row-actions">
                    <el-button
                      type="primary"
                      :loading="scope.row._updating"
                      @click="saveManagerLifecycle(scope.row)"
                    >
                      保存
                    </el-button>
                  </div>
                </template>
              </el-table-column>
            </el-table>
          </section>
        </el-tab-pane>
      </el-tabs>
    </template>
  </div>
</template>

<script setup>
import { onMounted, reactive, ref, watch } from "vue";
import { ElMessage } from "element-plus";
import { parseApiError, superApi } from "../lib/http";

const props = defineProps({
  token: {
    type: String,
    default: "",
  },
});

defineEmits(["logout"]);

const activeTab = ref("keys");

const loading = reactive({
  list: false,
  createKey: false,
  keys: false,
});

const renewalForm = reactive({
  duration_days: 30,
});

const latestRenewal = reactive({
  code: "",
  duration_days: 0,
});

const filters = reactive({
  keyword: "",
  status: "",
  limit: 500,
});

const keyFilters = reactive({
  keyword: "",
  status: "",
  limit: 200,
});

const keySummary = reactive({
  total: 0,
  unused: 0,
  used: 0,
  revoked: 0,
});

const managerSummary = reactive({
  total: 0,
  active: 0,
  expired: 0,
  disabled: 0,
  expiring_7d: 0,
});

const managers = ref([]);
const renewalKeys = ref([]);

watch(
  () => props.token,
  async (value) => {
    if (!value) {
      managers.value = [];
      renewalKeys.value = [];
      return;
    }
    await Promise.all([loadManagers(), loadRenewalKeys()]);
  },
  { immediate: true },
);

onMounted(async () => {
  if (props.token) {
    await Promise.all([loadManagers(), loadRenewalKeys()]);
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

async function createRenewalKey() {
  if (!props.token) {
    ElMessage.warning("请先登录超级管理员");
    return;
  }
  loading.createKey = true;
  try {
    const response = await superApi.createManagerRenewalKey(props.token, {
      duration_days: renewalForm.duration_days,
    });
    latestRenewal.code = response.code || "";
    latestRenewal.duration_days = response.duration_days || renewalForm.duration_days;
    ElMessage.success("管理员续费秘钥已生成");
    await loadRenewalKeys();
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.createKey = false;
  }
}

async function revokeRenewalKey(row) {
  if (!row?.id) {
    ElMessage.warning("无效秘钥记录");
    return;
  }
  row._revoking = true;
  try {
    await superApi.patchManagerRenewalKeyStatus(props.token, row.id, { status: "revoked" });
    ElMessage.success("续费秘钥已撤销");
    await loadRenewalKeys();
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    row._revoking = false;
  }
}

async function loadRenewalKeys() {
  if (!props.token) return;
  loading.keys = true;
  try {
    const response = await superApi.listManagerRenewalKeys(props.token, {
      keyword: keyFilters.keyword || undefined,
      status: keyFilters.status || undefined,
      limit: keyFilters.limit,
    });
    renewalKeys.value = (response.items || []).map((item) => ({
      ...item,
      _revoking: false,
    }));
    patchSummary(keySummary, response.summary, ["total", "unused", "used", "revoked"]);
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.keys = false;
  }
}

async function loadManagers() {
  if (!props.token) return;
  loading.list = true;
  try {
    const response = await superApi.listManagers(props.token, {
      keyword: filters.keyword || undefined,
      status: filters.status || undefined,
      limit: filters.limit,
    });
    managers.value = (response.items || []).map((item) => ({
      ...item,
      _updating: false,
      _editExpiresAt: item.expires_at || "",
      _extendDays: 0,
    }));
    patchSummary(managerSummary, response.summary, ["total", "active", "expired", "disabled", "expiring_7d"]);
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.list = false;
  }
}

async function saveManagerLifecycle(row) {
  if (!row?.id) {
    ElMessage.warning("无效管理员记录");
    return;
  }
  const expiresAt = String(row._editExpiresAt || "").trim();
  const extendDays = Number(row._extendDays || 0);
  if (!expiresAt && extendDays <= 0) {
    ElMessage.warning("请填写到期时间或延长天数");
    return;
  }
  row._updating = true;
  const payload = {};
  if (expiresAt) payload.expires_at = expiresAt;
  if (extendDays > 0) payload.extend_days = extendDays;
  try {
    await superApi.patchManagerLifecycle(props.token, row.id, payload);
    ElMessage.success(`管理员 ${row.username} 生命周期已更新`);
    await loadManagers();
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    row._updating = false;
  }
}
</script>
