<script setup>
import { computed, onMounted, reactive, ref, watch } from "vue";

import { ElMessage } from "element-plus";
import { parseApiError, userApi } from "../../lib/http";
import { userTypeLabel, ensureTaskConfig, parseTaskConfigFromRaw, SPECIAL_TASK_NAMES, INTERRUPTABLE_TASK_OPTIONS, FOSTER_REWARD_OPTIONS } from "../../lib/helpers";
import { useTaskTemplates } from "../../composables/useTaskTemplates";
import LineupTab from "./LineupTab.vue";

const props = defineProps({
  token: { type: String, default: "" },
});

const loading = reactive({ tasks: false });
const taskConfigRaw = ref("{}");
const taskRows = ref([]);
const { templateCache, ensureTaskTemplates } = useTaskTemplates();

const userType = ref("daily");
const taskFilter = ref("");
const lineupDialogVisible = ref(false);

// Duiyi answer source
const duiyiSource = ref("manager");
const duiyiBloggerId = ref(null);
const duiyiBloggers = ref([]);

const filteredTaskRows = computed(() => {
  if (taskFilter.value === "enabled") return taskRows.value.filter(r => r.config.enabled === true);
  if (taskFilter.value === "disabled") return taskRows.value.filter(r => r.config.enabled !== true);
  return taskRows.value;
});

const currentTemplate = computed(() => {
  return templateCache[userType.value] || { order: [], defaultConfig: {} };
});

function buildTaskRows(config) {
  const tpl = currentTemplate.value;
  taskRows.value = tpl.order.map((name) => {
    const cfg = reactive(ensureTaskConfig(config[name]));
    return { name, config: cfg };
  });
}

function stringifyTaskConfig(config) {
  taskConfigRaw.value = JSON.stringify(config || {}, null, 2);
}

async function applyTaskConfigFromRaw() {
  const config = parseTaskConfigFromRaw(taskConfigRaw.value);
  buildTaskRows(config);
  await saveMeTasks();
  ElMessage.success("已从 JSON 同步并保存");
}

watch(
  () => props.token,
  async (value) => {
    if (!value) { taskConfigRaw.value = "{}"; taskRows.value = []; return; }
    await loadMeTasks();
  },
  { immediate: true },
);

onMounted(async () => {
  if (props.token) await loadMeTasks();
});

async function loadMeTasks() {
  loading.tasks = true;
  try {
    const response = await userApi.getMeTasks(props.token);
    const ut = response.user_type || "daily";
    userType.value = ut;
    const template = await ensureTaskTemplates(ut);
    const merged = {
      ...(template.defaultConfig || {}),
      ...(response.task_config || {}),
    };
    buildTaskRows(merged);
    stringifyTaskConfig(merged);
    if (ut === "duiyi") await loadDuiyiSources();
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.tasks = false;
  }
}

async function saveMeTasks() {
  const normalizedConfig = {};
  taskRows.value.forEach((row) => {
    normalizedConfig[row.name] = {
      ...(row.config || {}),
      enabled: row.config.enabled === true,
    };
  });

  try {
    const response = await userApi.putMeTasks(props.token, { task_config: normalizedConfig });
    userType.value = response.user_type || userType.value || "daily";
    const savedConfig = response.task_config || normalizedConfig;
    buildTaskRows(savedConfig);
    stringifyTaskConfig(savedConfig);
  } catch (error) {
    ElMessage.error(parseApiError(error));
  }
}

async function saveOneTask(row) {
  const payload = {
    task_config: {
      [row.name]: {
        ...(row.config || {}),
        enabled: row.config.enabled === true,
      },
    },
  };
  try {
    await userApi.putMeTasks(props.token, payload);
  } catch (error) {
    ElMessage.error(parseApiError(error));
  }
}
function isSpecialTask(name) {
  return SPECIAL_TASK_NAMES.has(name);
}
function tableRowClassName({ row }) {
  return isSpecialTask(row.name) ? '' : 'hide-expand';
}

async function loadDuiyiSources() {
  if (!props.token || userType.value !== "duiyi") return;
  try {
    const res = await userApi.getDuiyiAnswerSources(props.token);
    const data = res.data;
    duiyiSource.value = data.current_source || "manager";
    duiyiBloggerId.value = data.current_blogger_id || null;
    duiyiBloggers.value = data.bloggers || [];
  } catch { /* ignore */ }
}

async function saveDuiyiSource() {
  if (!props.token) return;
  const payload = { source: duiyiSource.value };
  if (duiyiSource.value === "blogger") {
    if (!duiyiBloggerId.value) { ElMessage.warning("请选择博主"); return; }
    payload.blogger_id = duiyiBloggerId.value;
  } else {
    duiyiBloggerId.value = null;
  }
  try {
    await userApi.putDuiyiAnswerSource(props.token, payload);
    ElMessage.success("答案来源已保存");
  } catch (e) {
    ElMessage.error(parseApiError(e));
  }
}
</script>

<template>
  <article class="panel-card">
    <div class="panel-headline">
      <h3>我的任务配置（{{ userTypeLabel(userType) }}）</h3>
      <div class="row-actions">
        <el-radio-group v-model="taskFilter" size="small" class="mr-12">
          <el-radio-button value="">全部</el-radio-button>
          <el-radio-button value="enabled">已启用</el-radio-button>
          <el-radio-button value="disabled">未启用</el-radio-button>
        </el-radio-group>
        <el-button plain :loading="loading.tasks" @click="loadMeTasks">刷新</el-button>
        <el-button type="primary" plain @click="lineupDialogVisible = true">阵容配置</el-button>
      </div>
    </div>
    <div class="data-table-wrapper">
      <el-table :data="filteredTaskRows" border stripe empty-text="暂无可配置任务" :row-class-name="tableRowClassName">
        <el-table-column type="expand">
          <template #default="scope">
            <div v-if="isSpecialTask(scope.row.name)" class="expand-config" @click.stop>
              <!-- 探索突破 -->
              <template v-if="scope.row.name === '探索突破'">
                <div class="expand-row">
                  <el-checkbox v-model="scope.row.config.sub_explore" @change="saveOneTask(scope.row)">探索</el-checkbox>
                  <el-checkbox v-model="scope.row.config.sub_tupo" @change="saveOneTask(scope.row)">突破</el-checkbox>
                  <span class="expand-label">体力阈值:</span>
                  <el-input-number v-model="scope.row.config.stamina_threshold" :min="0" :max="99999" size="small" class="w-120" @change="saveOneTask(scope.row)" />
                </div>
                <div class="expand-row" style="margin-top: 8px;">
                  <span class="expand-label">中断白名单:</span>
                  <el-select
                    v-model="scope.row.config.allowed_interrupts"
                    multiple collapse-tags collapse-tags-tooltip
                    placeholder="允许中断的任务" size="small"
                    style="width: 320px"
                    @change="saveOneTask(scope.row)"
                  >
                    <el-option v-for="t in INTERRUPTABLE_TASK_OPTIONS" :key="t" :label="t" :value="t" />
                  </el-select>
                </div>
              </template>
              <!-- 结界卡合成 -->
              <template v-else-if="scope.row.name === '结界卡合成'">
                <div class="expand-row">
                  <span class="expand-label">探索计数:</span>
                  <el-input-number v-model="scope.row.config.explore_count" :min="0" size="small" class="w-120" @change="saveOneTask(scope.row)" />
                </div>
              </template>
              <!-- 寮商店 -->
              <template v-else-if="scope.row.name === '寮商店'">
                <div class="expand-row">
                  <el-checkbox v-model="scope.row.config.buy_heisui" @change="saveOneTask(scope.row)">购买黑碎</el-checkbox>
                  <el-checkbox v-model="scope.row.config.buy_lanpiao" @change="saveOneTask(scope.row)">购买蓝票</el-checkbox>
                </div>
              </template>
              <!-- 每周商店 -->
              <template v-else-if="scope.row.name === '每周商店'">
                <div class="expand-row">
                  <el-checkbox v-model="scope.row.config.buy_lanpiao" @change="saveOneTask(scope.row)">购买蓝票</el-checkbox>
                  <el-checkbox v-model="scope.row.config.buy_heidan" @change="saveOneTask(scope.row)">购买黑蛋</el-checkbox>
                  <el-checkbox v-model="scope.row.config.buy_tili" @change="saveOneTask(scope.row)">购买体力</el-checkbox>
                </div>
              </template>
              <!-- 御魂 -->
              <template v-else-if="scope.row.name === '御魂'">
                <div class="expand-row">
                  <span class="expand-label">运行次数:</span>
                  <el-input-number v-model="scope.row.config.run_count" :min="0" size="small" class="w-120" @change="saveOneTask(scope.row)" />
                  <span class="expand-label">目标层数:</span>
                  <el-input-number v-model="scope.row.config.target_level" :min="1" :max="20" size="small" class="w-120" @change="saveOneTask(scope.row)" />
                </div>
              </template>
              <!-- 斗技 -->
              <template v-else-if="scope.row.name === '斗技'">
                <div class="expand-row">
                  <span class="expand-label">开始:</span>
                  <el-select v-model="scope.row.config.start_hour" size="small" class="w-90" @change="saveOneTask(scope.row)">
                    <el-option v-for="h in 24" :key="h-1" :label="(h-1)+':00'" :value="h-1" />
                  </el-select>
                  <span class="expand-label">结束:</span>
                  <el-select v-model="scope.row.config.end_hour" size="small" class="w-90" @change="saveOneTask(scope.row)">
                    <el-option v-for="h in 24" :key="h-1" :label="(h-1)+':00'" :value="h-1" />
                  </el-select>
                  <el-radio-group v-model="scope.row.config.mode" size="small" class="ml-12" @change="saveOneTask(scope.row)">
                    <el-radio value="honor">荣誉</el-radio>
                    <el-radio value="score">分数</el-radio>
                  </el-radio-group>
                  <span class="expand-label">目标分:</span>
                  <el-input-number v-model="scope.row.config.target_score" :min="0" size="small" class="w-120" @change="saveOneTask(scope.row)" />
                </div>
              </template>
              <!-- 对弈竞猜 -->
              <template v-else-if="scope.row.name === '对弈竞猜'">
                <div class="expand-row">
                  <span class="expand-label">答案来源:</span>
                  <el-radio-group v-model="duiyiSource" @change="saveDuiyiSource">
                    <el-radio value="manager">管理员答案</el-radio>
                    <el-radio value="blogger">博主答案</el-radio>
                  </el-radio-group>
                  <el-select
                    v-if="duiyiSource === 'blogger'"
                    v-model="duiyiBloggerId"
                    placeholder="选择博主"
                    size="small"
                    class="w-120"
                    @change="saveDuiyiSource"
                  >
                    <el-option v-for="b in duiyiBloggers" :key="b.id" :label="b.name" :value="b.id" />
                  </el-select>
                </div>
              </template>
              <!-- 寄养 -->
              <template v-else-if="scope.row.name === '寄养'">
                <div class="expand-row">
                  <span class="expand-label">优先级:</span>
                  <el-select v-model="scope.row.config.foster_priority" size="small" class="w-120" @change="saveOneTask(scope.row)">
                    <el-option label="勾玉优先" value="gouyu" />
                    <el-option label="体力优先" value="tili" />
                    <el-option label="自定义" value="custom" />
                  </el-select>
                </div>
                <div v-if="scope.row.config.foster_priority === 'custom'" class="expand-row" style="margin-top: 8px;">
                  <span class="expand-label">自定义顺序:</span>
                  <el-select
                    v-model="scope.row.config.custom_priority"
                    multiple
                    placeholder="选择并排序奖励优先级"
                    size="small"
                    style="width: 320px"
                    @change="saveOneTask(scope.row)"
                  >
                    <el-option v-for="r in FOSTER_REWARD_OPTIONS" :key="r.value" :label="r.label" :value="r.value" />
                  </el-select>
                </div>
              </template>
            </div>
          </template>
        </el-table-column>
        <el-table-column prop="name" label="任务类型" min-width="170" />
        <el-table-column label="启用" width="100" align="center">
          <template #default="scope">
            <el-switch v-model="scope.row.config.enabled" @change="saveOneTask(scope.row)" />
          </template>
        </el-table-column>
        <el-table-column label="执行时间" min-width="200">
          <template #default="scope">
            <el-date-picker
              v-model="scope.row.config.next_time"
              type="datetime" value-format="YYYY-MM-DD HH:mm"
              placeholder="选择执行时间" size="small" class="w-180"
              @change="saveOneTask(scope.row)"
            />
          </template>
        </el-table-column>
        <el-table-column label="失败延迟(分)" width="130" align="center">
          <template #default="scope">
            <el-input-number v-model="scope.row.config.fail_delay" :min="0" :max="100000" size="small" @change="saveOneTask(scope.row)" />
          </template>
        </el-table-column>
      </el-table>
    </div>

    <el-collapse class="collapse-section">
      <el-collapse-item title="高级配置编辑（完整字段）" name="json">
        <el-input
          v-model="taskConfigRaw"
          type="textarea"
          :rows="12"
          placeholder='{"签到":{"enabled":true,"next_time":"09:00"}}'
        />
        <el-button type="primary" plain class="mt-8" @click="applyTaskConfigFromRaw">应用配置</el-button>
      </el-collapse-item>
    </el-collapse>
    <p class="tip-text">系统会按用户类型过滤任务，并按"默认 + 现有 + 提交"合并更新。</p>
    <LineupTab :token="token" v-model:visible="lineupDialogVisible" />
  </article>
</template>

<style scoped>
.expand-config {
  padding: 8px 16px;
}
.expand-row {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}
.expand-label {
  font-size: 13px;
  color: var(--el-text-color-secondary, #909399);
  white-space: nowrap;
}
:deep(.el-table__row.hide-expand .el-table__expand-icon) {
  visibility: hidden;
}
</style>
