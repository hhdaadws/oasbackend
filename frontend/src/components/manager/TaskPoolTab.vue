<script setup>
import { onMounted, reactive, ref, watch } from "vue";
import { ElMessage } from "element-plus";
import { managerApi, parseApiError } from "../../lib/http";
import { formatTime, jobStatusTagType, jobStatusLabel, userTypeLabel } from "../../lib/helpers";
import { usePagination } from "../../composables/usePagination";

const props = defineProps({
  token: { type: String, default: "" },
});

const loading = ref(false);
const items = ref([]);
const summary = reactive({ pending: 0, leased: 0, running: 0 });
const filters = reactive({ status: "" });
const { pagination, updateTotal, resetPage, paginationParams } = usePagination();

async function loadTaskPool() {
  if (!props.token) return;
  loading.value = true;
  try {
    const response = await managerApi.taskPool(props.token, {
      status: filters.status || undefined,
      ...paginationParams(),
    });
    items.value = response.items || [];
    const s = response.summary || {};
    summary.pending = Number(s.pending ?? 0);
    summary.leased = Number(s.leased ?? 0);
    summary.running = Number(s.running ?? 0);
    updateTotal(response.total || 0);
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.value = false;
  }
}

watch(
  () => props.token,
  async (value) => {
    if (!value) { items.value = []; return; }
    await loadTaskPool();
  },
  { immediate: true },
);

onMounted(async () => {
  if (props.token) await loadTaskPool();
});
</script>

<template>
  <section class="panel-card">
    <div class="panel-headline">
      <h3>任务池预览</h3>
      <div class="row-actions">
        <el-select v-model="filters.status" clearable placeholder="活跃任务" size="small" class="w-130" @change="() => { resetPage(); loadTaskPool(); }">
          <el-option label="待执行" value="pending" />
          <el-option label="已分配" value="leased" />
          <el-option label="运行中" value="running" />
          <el-option label="成功" value="success" />
          <el-option label="失败" value="failed" />
        </el-select>
        <el-button text type="primary" :loading="loading" @click="loadTaskPool">刷新</el-button>
      </div>
    </div>
    <div class="stats-grid">
      <div class="stat-item">
        <span class="stat-label">待执行</span>
        <strong class="stat-value">{{ summary.pending }}</strong>
      </div>
      <div class="stat-item">
        <span class="stat-label">已分配</span>
        <strong class="stat-value">{{ summary.leased }}</strong>
      </div>
      <div class="stat-item">
        <span class="stat-label">运行中</span>
        <strong class="stat-value">{{ summary.running }}</strong>
      </div>
    </div>

    <div class="data-table-wrapper">
      <el-table :data="items" border stripe empty-text="暂无任务" v-loading="loading">
        <el-table-column prop="id" label="ID" width="80" sortable />
        <el-table-column prop="login_id" label="登录ID" min-width="100" />
        <el-table-column label="类型" width="100">
          <template #default="scope">
            <el-tag size="small">{{ userTypeLabel(scope.row.user_type) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="task_type" label="任务" min-width="140" />
        <el-table-column label="状态" width="110" sortable>
          <template #default="scope">
            <el-tag :type="jobStatusTagType(scope.row.status)" size="small">{{ jobStatusLabel(scope.row.status) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="priority" label="优先级" width="90" sortable />
        <el-table-column label="计划时间" min-width="170">
          <template #default="scope">{{ formatTime(scope.row.scheduled_at) }}</template>
        </el-table-column>
        <el-table-column prop="leased_by_node" label="执行节点" min-width="140">
          <template #default="scope">{{ scope.row.leased_by_node || "-" }}</template>
        </el-table-column>
        <el-table-column label="重试" width="80">
          <template #default="scope">{{ scope.row.attempts }}/{{ scope.row.max_attempts }}</template>
        </el-table-column>
      </el-table>
    </div>

    <div class="pagination-wrapper">
      <el-pagination
        v-if="pagination.total > pagination.pageSize"
        v-model:current-page="pagination.page"
        v-model:page-size="pagination.pageSize"
        :page-sizes="[20, 50, 100]"
        :total="pagination.total"
        layout="total, sizes, prev, pager, next, jumper"
        @size-change="() => { resetPage(); loadTaskPool(); }"
        @current-change="loadTaskPool"
      />
    </div>
  </section>
</template>
