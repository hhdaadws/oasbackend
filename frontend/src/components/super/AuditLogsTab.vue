<script setup>
import { onMounted, reactive, ref, watch } from "vue";
import { ElMessage } from "element-plus";
import { parseApiError, superApi } from "../../lib/http";
import {
  formatTime,
  keyStatusLabel,
  auditActionLabel,
  actorTypeLabel,
  auditActionTagType,
  AUDIT_ACTION_OPTIONS,
} from "../../lib/helpers";
import { usePagination } from "../../composables/usePagination";
import { useDebouncedFilter } from "../../composables/useDebouncedFilter";
import TableSkeleton from "../shared/TableSkeleton.vue";

const props = defineProps({
  token: { type: String, default: "" },
});

const loading = reactive({ list: false });
const filters = reactive({ action: "", keyword: "" });
const logs = ref([]);

const { pagination, updateTotal, resetPage, paginationParams } = usePagination();

useDebouncedFilter(() => filters, () => { resetPage(); loadLogs(); });

watch(
  () => props.token,
  async (value) => {
    if (!value) { logs.value = []; return; }
    await loadLogs();
  },
  { immediate: true },
);

onMounted(async () => {
  if (props.token) await loadLogs();
});

async function loadLogs() {
  if (!props.token) return;
  loading.list = true;
  try {
    const response = await superApi.listAuditLogs(props.token, {
      action: filters.action || undefined,
      keyword: filters.keyword || undefined,
      ...paginationParams(),
    });
    logs.value = response.items || [];
    updateTotal(response.total || 0);
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.list = false;
  }
}

function formatDetail(action, detail) {
  if (!detail || typeof detail !== "object" || Object.keys(detail).length === 0) return "-";
  switch (action) {
    case "create_manager_renewal_key":
      return `续期天数: ${detail.duration_days}天`;
    case "patch_manager_renewal_key_status":
      return `状态→${keyStatusLabel(detail.status || "")}`;
    case "batch_revoke_renewal_keys":
      return `撤销 ${detail.revoked || 0} 个密钥`;
    case "delete_manager_renewal_key":
      return `密钥: ${detail.code || "-"}, 状态: ${keyStatusLabel(detail.status || "")}`;
    case "batch_delete_renewal_keys":
      return `删除 ${detail.deleted || 0} 个密钥`;
    case "patch_manager_lifecycle": {
      const parts = [];
      if (detail.extend_days) parts.push(`延长${detail.extend_days}天`);
      if (detail.expires_at) parts.push(`到期时间→${formatTime(detail.expires_at)}`);
      return parts.join(", ") || "-";
    }
    case "reset_manager_password":
      return `管理员: ${detail.manager_username || "-"}`;
    case "batch_manager_lifecycle": {
      const parts = [];
      if (detail.extend_days) parts.push(`延长${detail.extend_days}天`);
      if (detail.expires_at) parts.push(`到期→${formatTime(detail.expires_at)}`);
      parts.push(`更新 ${detail.updated || 0} 个`);
      return parts.join(", ");
    }
    case "redeem_manager_renewal_key":
      return `密钥: ${detail.code || "-"}`;
    default:
      return JSON.stringify(detail);
  }
}
</script>

<template>
  <section class="panel-card panel-card--compact">
    <div class="panel-headline">
      <h3>日志筛选</h3>
      <el-button text type="primary" :loading="loading.list" @click="loadLogs">刷新</el-button>
    </div>
    <el-form label-width="100px" class="compact-form">
      <el-form-item label="操作类型">
        <el-select v-model="filters.action" clearable placeholder="全部操作">
          <el-option
            v-for="opt in AUDIT_ACTION_OPTIONS"
            :key="opt.value"
            :label="opt.label"
            :value="opt.value"
          />
        </el-select>
      </el-form-item>
      <el-form-item label="操作者">
        <el-input v-model="filters.keyword" placeholder="搜索用户名" clearable />
      </el-form-item>
    </el-form>
  </section>

  <section class="panel-card">
    <div class="panel-headline">
      <h3>操作日志</h3>
      <span class="muted">共 {{ pagination.total }} 条</span>
    </div>

    <TableSkeleton v-if="loading.list && logs.length === 0" :rows="5" :columns="7" />
    <div v-else class="data-table-wrapper">
      <el-table :data="logs" border stripe empty-text="暂无日志数据" v-loading="loading.list">
        <el-table-column prop="id" label="ID" width="80" sortable />
        <el-table-column label="操作时间" width="180" sortable>
          <template #default="scope">{{ formatTime(scope.row.created_at) }}</template>
        </el-table-column>
        <el-table-column label="操作者类型" width="130">
          <template #default="scope">
            <el-tag size="small">{{ actorTypeLabel(scope.row.actor_type) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="actor_name" label="操作者" width="150" />
        <el-table-column label="操作" width="180">
          <template #default="scope">
            <el-tag :type="auditActionTagType(scope.row.action)" size="small">
              {{ auditActionLabel(scope.row.action) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="详情" min-width="250">
          <template #default="scope">
            {{ formatDetail(scope.row.action, scope.row.detail) }}
          </template>
        </el-table-column>
        <el-table-column prop="ip" label="IP" width="140" />
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
        @size-change="() => { resetPage(); loadLogs(); }"
        @current-change="loadLogs"
      />
    </div>
  </section>
</template>
