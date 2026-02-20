<script setup>
import { reactive, ref, watch } from "vue";
import { ElMessage } from "element-plus";
import { parseApiError, userApi } from "../../lib/http";

const props = defineProps({
  token: { type: String, default: "" },
  visible: { type: Boolean, default: false },
});
const emit = defineEmits(["update:visible"]);

const loading = reactive({ fetch: false });

const LINEUP_TASKS = ["逢魔", "地鬼", "探索", "结界突破", "道馆", "秘闻", "御魂"];

const lineupTableData = ref(
  LINEUP_TASKS.map((task) => ({ task, group: 0, position: 0 }))
);

async function loadLineup() {
  if (!props.token) return;
  loading.fetch = true;
  try {
    const res = await userApi.getMeLineup(props.token);
    const config = res.lineup_config || {};
    lineupTableData.value = LINEUP_TASKS.map((task) => ({
      task,
      group: config[task]?.group ?? 0,
      position: config[task]?.position ?? 0,
    }));
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.fetch = false;
  }
}

async function saveOneLineup(row) {
  try {
    await userApi.putMeLineup(props.token, {
      lineup_config: {
        [row.task]: { group: row.group, position: row.position },
      },
    });
  } catch (error) {
    ElMessage.error(parseApiError(error));
  }
}

/* 抽屉打开时加载数据 */
watch(
  () => props.visible,
  (val) => {
    if (val && props.token) loadLineup();
  },
);
</script>

<template>
  <Teleport to="body">
    <el-drawer
      title="阵容配置"
      :model-value="visible"
      size="520px"
      :teleported="false"
      @update:model-value="emit('update:visible', $event)"
    >
    <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px;">
      <p class="tip-text" style="margin: 0;">为每个任务配置切换的分组和阵容预设。设为"未配置"表示执行时不切换阵容。</p>
      <el-button plain size="small" :loading="loading.fetch" @click="loadLineup">刷新</el-button>
    </div>
    <el-table :data="lineupTableData" border stripe style="width: 100%">
      <el-table-column prop="task" label="任务" width="100" />
      <el-table-column label="分组" min-width="150">
        <template #default="{ row }">
          <el-select v-model="row.group" size="small" style="width: 100%" @change="saveOneLineup(row)">
            <el-option label="未配置" :value="0" />
            <el-option v-for="n in 7" :key="n" :label="'分组' + n" :value="n" />
          </el-select>
        </template>
      </el-table-column>
      <el-table-column label="阵容" min-width="150">
        <template #default="{ row }">
          <el-select v-model="row.position" size="small" style="width: 100%" @change="saveOneLineup(row)">
            <el-option label="未配置" :value="0" />
            <el-option v-for="n in 7" :key="n" :label="'阵容' + n" :value="n" />
          </el-select>
        </template>
      </el-table-column>
    </el-table>
    </el-drawer>
  </Teleport>
</template>
