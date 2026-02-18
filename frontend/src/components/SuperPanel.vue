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
          </el-form>
        </article>
      </section>

      <section class="panel-card">
        <div class="panel-headline">
          <h3>管理员生命周期管理</h3>
          <span class="muted">共 {{ filteredManagers.length }} 条</span>
        </div>

        <el-table :data="filteredManagers" border stripe height="420" empty-text="暂无管理员数据">
          <el-table-column prop="id" label="ID" width="80" />
          <el-table-column prop="username" label="账号" min-width="180" />
          <el-table-column prop="status" label="状态" width="130">
            <template #default="scope">
              <el-tag :type="statusTagType(scope.row.status)">{{ scope.row.status }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="expires_at" label="到期时间" min-width="190" />
          <el-table-column label="状态变更" width="300">
            <template #default="scope">
              <div class="row-actions">
                <el-select v-model="scope.row._targetStatus" placeholder="选择状态" style="width: 130px">
                  <el-option label="active" value="active" />
                  <el-option label="expired" value="expired" />
                  <el-option label="disabled" value="disabled" />
                </el-select>
                <el-button
                  type="primary"
                  :loading="scope.row._saving"
                  :disabled="!scope.row._targetStatus"
                  @click="patchManagerStatus(scope.row)"
                >
                  保存
                </el-button>
              </div>
            </template>
          </el-table-column>
        </el-table>
      </section>
    </template>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref, watch } from "vue";
import { ElMessage } from "element-plus";
import { parseApiError, superApi } from "../lib/http";

const props = defineProps({
  token: {
    type: String,
    default: "",
  },
});

defineEmits(["logout"]);

const loading = reactive({
  list: false,
  createKey: false,
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
});

const managers = ref([]);

const filteredManagers = computed(() => {
  const keyword = filters.keyword.trim().toLowerCase();
  return managers.value.filter((item) => {
    const matchKeyword = !keyword || item.username?.toLowerCase().includes(keyword);
    const matchStatus = !filters.status || item.status === filters.status;
    return matchKeyword && matchStatus;
  });
});

watch(
  () => props.token,
  async (value) => {
    if (!value) {
      managers.value = [];
      return;
    }
    await loadManagers();
  },
  { immediate: true },
);

onMounted(async () => {
  if (props.token) {
    await loadManagers();
  }
});

function statusTagType(status) {
  if (status === "active") return "success";
  if (status === "disabled") return "danger";
  if (status === "expired") return "warning";
  return "info";
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
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.createKey = false;
  }
}

async function loadManagers() {
  if (!props.token) return;
  loading.list = true;
  try {
    const response = await superApi.listManagers(props.token);
    managers.value = (response.items || []).map((item) => ({
      ...item,
      _targetStatus: item.status,
      _saving: false,
    }));
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.list = false;
  }
}

async function patchManagerStatus(row) {
  if (!row?._targetStatus) {
    ElMessage.warning("请选择目标状态");
    return;
  }
  row._saving = true;
  try {
    await superApi.patchManagerStatus(props.token, row.id, {
      status: row._targetStatus,
    });
    ElMessage.success(`管理员 ${row.username} 状态已更新`);
    await loadManagers();
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    row._saving = false;
  }
}
</script>
