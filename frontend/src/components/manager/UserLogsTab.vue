<template>
  <div class="role-dashboard">
    <section class="panel-card" v-if="selectedUserId">
      <div class="panel-headline">
        <h3>用户 {{ selectedUserId }}（{{ selectedUserAccountNo }}）执行日志</h3>
        <div class="row-actions">
          <el-input v-model="keyword" placeholder="按任务类型、节点或说明搜索" clearable class="w-220" />
          <el-button plain :loading="loading.logs" @click="loadLogs">刷新</el-button>
          <el-button type="danger" plain :loading="loading.clear" @click="clearLogs">清空日志</el-button>
        </div>
      </div>

      <TableSkeleton v-if="logs.length === 0 && loading.logs" :rows="5" :columns="4" />

      <div v-else class="data-table-wrapper">
        <el-table
          v-loading="loading.logs"
          :data="filteredLogs"
          border
          stripe
          empty-text="暂无日志"
        >
          <el-table-column label="时间" min-width="170" sortable>
            <template #default="scope">{{ formatTime(scope.row.event_at) }}</template>
          </el-table-column>
          <el-table-column prop="task_type" label="任务类型" width="130">
            <template #default="scope">
              <span>{{ scope.row.task_type || "-" }}</span>
            </template>
          </el-table-column>
          <el-table-column prop="leased_by_node" label="执行节点" width="160">
            <template #default="scope">{{ scope.row.leased_by_node || "-" }}</template>
          </el-table-column>
          <el-table-column prop="message" label="说明" min-width="220" />
        </el-table>
      </div>

      <div class="pagination-wrapper">
        <el-pagination
          v-model:current-page="pagination.page"
          v-model:page-size="pagination.pageSize"
          :total="pagination.total"
          :page-sizes="[50, 100, 200]"
          layout="total, sizes, prev, pager, next"
          @current-change="loadLogs"
          @size-change="loadLogs"
        />
      </div>
    </section>

    <el-empty
      v-else
      class="empty-center"
      description='请先在"下属配置"中选择用户后再查看日志'
      :image-size="100"
    />
  </div>
</template>

<script setup>
import { computed, reactive, ref, watch } from "vue";
import { ElMessage, ElMessageBox } from "element-plus";
import { managerApi, parseApiError } from "../../lib/http";
import { formatTime } from "../../lib/helpers";
import { usePagination } from "../../composables/usePagination";
import TableSkeleton from "../shared/TableSkeleton.vue";

const props = defineProps({
  token: { type: String, default: "" },
  selectedUserId: { type: Number, default: 0 },
  selectedUserAccountNo: { type: String, default: "" },
});

const loading = reactive({ logs: false, clear: false });
const logs = ref([]);
const keyword = ref("");
const filteredLogs = computed(() => {
  if (!keyword.value) return logs.value;
  const q = keyword.value.toLowerCase();
  return logs.value.filter(
    (log) =>
      (log.task_type || "").toLowerCase().includes(q) ||
      (log.leased_by_node || "").toLowerCase().includes(q) ||
      (log.message || "").toLowerCase().includes(q),
  );
});

const { pagination, updateTotal, paginationParams } = usePagination({ defaultPageSize: 50 });

async function loadLogs() {
  if (!props.selectedUserId || !props.token) return;
  loading.logs = true;
  try {
    const response = await managerApi.getUserLogs(props.token, props.selectedUserId, paginationParams());
    logs.value = response.items || [];
    updateTotal(response.total || logs.value.length);
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.logs = false;
  }
}

async function clearLogs() {
  if (!props.selectedUserId || !props.token) return;
  try {
    await ElMessageBox.confirm("确定要清空该用户的所有执行日志吗？此操作不可撤销。", "确认清空", {
      confirmButtonText: "确定清空",
      cancelButtonText: "取消",
      type: "warning",
    });
  } catch {
    return;
  }
  loading.clear = true;
  try {
    await managerApi.deleteUserLogs(props.token, props.selectedUserId);
    ElMessage.success("日志已清空");
    logs.value = [];
    updateTotal(0);
    pagination.page = 1;
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.clear = false;
  }
}

watch(
  () => props.selectedUserId,
  async (newId) => {
    if (newId && props.token) {
      pagination.page = 1;
      keyword.value = "";
      await loadLogs();
    } else {
      logs.value = [];
      pagination.page = 1;
      updateTotal(0);
    }
  },
  { immediate: true },
);
</script>
