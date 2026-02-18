<script setup>
import { onMounted, reactive, ref, watch } from "vue";
import { ElMessage } from "element-plus";
import { parseApiError, userApi } from "../../lib/http";
import { formatTime } from "../../lib/helpers";
import { usePagination } from "../../composables/usePagination";
import TableSkeleton from "../shared/TableSkeleton.vue";

const props = defineProps({
  token: { type: String, default: "" },
});

const loading = reactive({ logs: false });
const logs = ref([]);

const { pagination, updateTotal, resetPage, paginationParams } = usePagination();

watch(
  () => props.token,
  async (value) => {
    if (!value) { logs.value = []; return; }
    await loadMeLogs();
  },
  { immediate: true },
);

onMounted(async () => {
  if (props.token) await loadMeLogs();
});

async function loadMeLogs() {
  loading.logs = true;
  try {
    const response = await userApi.getMeLogs(props.token, paginationParams());
    logs.value = response.items || [];
    updateTotal(response.total || 0);
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.logs = false;
  }
}
</script>

<template>
  <article class="panel-card">
    <div class="panel-headline">
      <h3>我的执行日志</h3>
      <el-button plain :loading="loading.logs" @click="loadMeLogs">刷新日志</el-button>
    </div>

    <TableSkeleton v-if="loading.logs && logs.length === 0" :rows="5" :columns="4" />
    <div v-else class="data-table-wrapper">
      <el-table :data="logs" border stripe empty-text="暂无日志" v-loading="loading.logs">
        <el-table-column label="时间" min-width="170" sortable>
          <template #default="scope">{{ formatTime(scope.row.event_at) }}</template>
        </el-table-column>
        <el-table-column prop="event_type" label="事件" width="130" sortable />
        <el-table-column prop="error_code" label="错误码" width="130" />
        <el-table-column prop="message" label="消息" min-width="220" />
      </el-table>
    </div>

    <div class="pagination-wrapper">
      <el-pagination
        v-if="pagination.total > pagination.pageSize"
        v-model:current-page="pagination.page"
        v-model:page-size="pagination.pageSize"
        :page-sizes="[20, 50, 100, 200]"
        :total="pagination.total"
        layout="total, sizes, prev, pager, next, jumper"
        @size-change="() => { resetPage(); loadMeLogs(); }"
        @current-change="loadMeLogs"
      />
    </div>
  </article>
</template>
