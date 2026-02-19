<script setup>
import { onMounted, reactive, ref, watch } from "vue";
import { ElMessage, ElMessageBox } from "element-plus";
import { parseApiError, superApi } from "../../lib/http";
import { formatTime, patchSummary } from "../../lib/helpers";
import { usePagination } from "../../composables/usePagination";
import { useBatchSelection } from "../../composables/useBatchSelection";
import { useDebouncedFilter } from "../../composables/useDebouncedFilter";
import TableSkeleton from "../shared/TableSkeleton.vue";
import BatchActionBar from "../shared/BatchActionBar.vue";

const props = defineProps({
  token: { type: String, default: "" },
});

const loading = reactive({ list: false, batch: false });
const filters = reactive({ keyword: "" });
const managerSummary = reactive({ total: 0, active: 0, expired: 0, expiring_7d: 0 });
const managers = ref([]);
const tableRef = ref(null);

const batchForm = reactive({ extend_days: 30 });

const showEditDialog = ref(false);
const editingManager = ref(null);
const editForm = reactive({ expires_at: "", extend_days: 0 });

const showPasswordDialog = ref(false);
const passwordManager = ref(null);
const passwordForm = reactive({ new_password: "" });

function openEditDialog(row) {
  editingManager.value = row;
  editForm.expires_at = row.expires_at || "";
  editForm.extend_days = 0;
  showEditDialog.value = true;
}

function openPasswordDialog(row) {
  passwordManager.value = row;
  passwordForm.new_password = "";
  showPasswordDialog.value = true;
}

async function copyUsername(username) {
  try {
    await navigator.clipboard.writeText(username);
    ElMessage.success("已复制到剪贴板");
  } catch {
    ElMessage.warning("复制失败，请手动选择");
  }
}

const { pagination, updateTotal, resetPage, paginationParams } = usePagination();
const { selectedIds, selectedCount, hasSelection, onSelectionChange, clearSelection } = useBatchSelection();

useDebouncedFilter(() => filters, () => { resetPage(); loadManagers(); });

watch(
  () => props.token,
  async (value) => {
    if (!value) { managers.value = []; return; }
    await loadManagers();
  },
  { immediate: true },
);

onMounted(async () => {
  if (props.token) await loadManagers();
});

async function loadManagers() {
  if (!props.token) return;
  loading.list = true;
  try {
    const response = await superApi.listManagers(props.token, {
      keyword: filters.keyword || undefined,
      ...paginationParams(),
    });
    managers.value = (response.items || []).map((item) => ({
      ...item,
      _updating: false,
    }));
    patchSummary(managerSummary, response.summary, ["total", "active", "expired", "expiring_7d"]);
    updateTotal(response.total || 0);
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.list = false;
  }
}

async function saveManagerLifecycle() {
  if (!editingManager.value?.id) { ElMessage.warning("无效管理员记录"); return; }
  const expiresAt = String(editForm.expires_at || "").trim();
  const extendDays = Number(editForm.extend_days || 0);
  if (!expiresAt && extendDays <= 0) {
    ElMessage.warning("请填写到期时间或延长天数");
    return;
  }
  editingManager.value._updating = true;
  const payload = {};
  if (expiresAt) payload.expires_at = expiresAt;
  if (extendDays > 0) payload.extend_days = extendDays;
  try {
    await superApi.patchManagerLifecycle(props.token, editingManager.value.id, payload);
    ElMessage.success(`管理员 ${editingManager.value.username} 生命周期已更新`);
    showEditDialog.value = false;
    await loadManagers();
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    if (editingManager.value) editingManager.value._updating = false;
  }
}

async function batchExtendManagers() {
  if (!hasSelection.value) return;
  if (batchForm.extend_days <= 0) {
    ElMessage.warning("请输入延长天数");
    return;
  }
  try {
    await ElMessageBox.confirm(
      `确定要为 ${selectedCount.value} 个管理员延长 ${batchForm.extend_days} 天吗？`,
      "确认批量延长",
      { confirmButtonText: "确定", cancelButtonText: "取消", type: "info" },
    );
  } catch { return; }
  loading.batch = true;
  try {
    const res = await superApi.batchManagerLifecycle(props.token, {
      manager_ids: selectedIds.value,
      extend_days: batchForm.extend_days,
    });
    ElMessage.success(`已更新 ${res.updated} 个管理员`);
    clearSelection();
    if (tableRef.value) tableRef.value.clearSelection();
    await loadManagers();
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.batch = false;
  }
}

async function resetManagerPassword() {
  if (!passwordManager.value?.id) { ElMessage.warning("无效管理员记录"); return; }
  if (!passwordForm.new_password || passwordForm.new_password.length < 6) {
    ElMessage.warning("密码长度至少6位");
    return;
  }
  try {
    await ElMessageBox.confirm(
      `确定要重置管理员 "${passwordManager.value.username}" 的密码吗？`,
      "确认重置密码",
      { confirmButtonText: "确定重置", cancelButtonText: "取消", type: "warning" },
    );
  } catch { return; }
  try {
    await superApi.resetManagerPassword(props.token, passwordManager.value.id, {
      new_password: passwordForm.new_password,
    });
    ElMessage.success(`管理员 ${passwordManager.value.username} 密码已重置`);
    showPasswordDialog.value = false;
  } catch (error) {
    ElMessage.error(parseApiError(error));
  }
}

</script>

<template>
  <section class="panel-card panel-card--compact">
    <div class="panel-headline">
      <h3>管理员筛选</h3>
      <el-button text type="primary" :loading="loading.list" @click="loadManagers">刷新</el-button>
    </div>
    <el-form label-width="100px" class="compact-form">
      <el-form-item label="关键词">
        <el-input v-model="filters.keyword" placeholder="按账号过滤" clearable />
      </el-form-item>
    </el-form>
  </section>

  <section class="panel-card panel-card--compact">
    <div class="panel-headline">
      <h3>管理员状态统计</h3>
      <span class="muted">全量维度</span>
    </div>
    <div class="stats-grid">
      <div class="stat-item">
        <span class="stat-label">总数</span>
        <strong class="stat-value">{{ managerSummary.total }}</strong>
      </div>
      <div class="stat-item">
        <span class="stat-label">活跃</span>
        <strong class="stat-value">{{ managerSummary.active }}</strong>
      </div>
      <div class="stat-item">
        <span class="stat-label">已过期</span>
        <strong class="stat-value">{{ managerSummary.expired }}</strong>
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
      <span class="muted">共 {{ pagination.total }} 条</span>
    </div>

    <TableSkeleton v-if="loading.list && managers.length === 0" :rows="5" :columns="10" />
    <div v-else class="data-table-wrapper">
      <el-table
        ref="tableRef"
        :data="managers"
        border
        stripe
        empty-text="暂无管理员数据"
        v-loading="loading.list"
        @selection-change="onSelectionChange"
      >
        <el-table-column type="selection" width="48" />
        <el-table-column prop="id" label="ID" width="80" sortable />
        <el-table-column label="账号" min-width="180">
          <template #default="scope">
            <span class="clickable-cell" @click="copyUsername(scope.row.username)" title="点击复制">{{ scope.row.username }}</span>
          </template>
        </el-table-column>
        <el-table-column label="到期标记" width="130" sortable>
          <template #default="scope">
            <el-tag :type="scope.row.is_expired ? 'warning' : 'success'">
              {{ scope.row.is_expired ? "已到期" : "有效" }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="到期时间" min-width="190" sortable>
          <template #default="scope">{{ formatTime(scope.row.expires_at) }}</template>
        </el-table-column>
        <el-table-column prop="total_users" label="总用户数" width="110" sortable>
          <template #default="scope">
            <span>{{ scope.row.total_users ?? 0 }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="active_users" label="活跃用户" width="110" sortable>
          <template #default="scope">
            <el-tag type="success" size="small">{{ scope.row.active_users ?? 0 }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="expired_users" label="过期用户" width="110" sortable>
          <template #default="scope">
            <el-tag type="warning" size="small">{{ scope.row.expired_users ?? 0 }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="180">
          <template #default="scope">
            <el-button type="primary" plain size="small" @click="openEditDialog(scope.row)">编辑</el-button>
            <el-button type="warning" plain size="small" @click="openPasswordDialog(scope.row)">重置密码</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <BatchActionBar :selected-count="selectedCount" @clear="() => { clearSelection(); tableRef?.clearSelection(); }">
      <el-input-number v-model="batchForm.extend_days" :min="1" :max="3650" size="small" class="w-100" />
      <el-button type="primary" :loading="loading.batch" @click="batchExtendManagers">批量延长</el-button>
    </BatchActionBar>

    <div class="pagination-wrapper">
      <el-pagination
        v-if="pagination.total > pagination.pageSize"
        v-model:current-page="pagination.page"
        v-model:page-size="pagination.pageSize"
        :page-sizes="[20, 50, 100, 200]"
        :total="pagination.total"
        layout="total, sizes, prev, pager, next, jumper"
        @size-change="() => { resetPage(); loadManagers(); }"
        @current-change="loadManagers"
      />
    </div>
  </section>

  <el-dialog v-model="showEditDialog" title="编辑管理员生命周期" class="dialog-sm" append-to-body>
    <p class="muted mb-12">管理员：{{ editingManager?.username }}</p>
    <el-form :model="editForm" label-width="100px">
      <el-form-item label="到期时间">
        <el-date-picker v-model="editForm.expires_at" type="datetime"
          value-format="YYYY-MM-DD HH:mm" placeholder="选择到期日期时间" class="w-full" />
      </el-form-item>
      <el-form-item label="延长天数">
        <el-input-number v-model="editForm.extend_days" :min="0" :max="3650" class="w-full" />
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="showEditDialog = false">取消</el-button>
      <el-button type="primary" :loading="editingManager?._updating" @click="saveManagerLifecycle">保存</el-button>
    </template>
  </el-dialog>

  <el-dialog v-model="showPasswordDialog" title="重置管理员密码" class="dialog-sm" append-to-body>
    <p class="muted mb-12">管理员：{{ passwordManager?.username }}</p>
    <el-form :model="passwordForm" label-width="100px">
      <el-form-item label="新密码">
        <el-input v-model="passwordForm.new_password" type="password"
          show-password placeholder="请输入新密码（至少6位）" maxlength="128" />
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="showPasswordDialog = false">取消</el-button>
      <el-button type="warning" @click="resetManagerPassword">确认重置</el-button>
    </template>
  </el-dialog>
</template>
