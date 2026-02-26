<template>
  <div class="role-dashboard">
    <section class="panel-card">
      <div class="panel-headline">
        <h3>下属用户列表</h3>
        <div class="row-actions">
          <template v-if="hasSelection">
            <span class="selection-count">已选 {{ selectedCount }} 项</span>
            <el-button type="primary" size="small" @click="showBatchLifecycleDialog = true">批量生命周期</el-button>
            <el-button type="success" size="small" @click="showBatchAssetsDialog = true">批量资产设置</el-button>
            <el-button type="danger" size="small" :loading="loading.batchDelete" @click="batchDeleteUsers">批量删除</el-button>
            <el-button size="small" plain @click="doClearSelection">取消</el-button>
          </template>
          <el-button v-else type="warning" size="small" @click="showQuickCreateDialog = true">创建用户</el-button>
        </div>
      </div>
      <div class="filter-row filter-row-spaced">
        <el-input v-model="filters.keyword" placeholder="搜索账号" clearable />
        <el-select v-model="filters.status" clearable placeholder="状态过滤">
          <el-option label="未过期" value="active" />
          <el-option label="已过期" value="expired" />
        </el-select>
        <el-select v-model="filters.userType" clearable placeholder="类型过滤">
          <el-option v-for="item in userTypeOptions" :key="item.value" :label="item.label" :value="item.value" />
        </el-select>
      </div>

      <div class="stats-grid">
        <div class="stat-item">
          <span class="stat-label">总数</span>
          <strong class="stat-value">{{ userSummary.total }}</strong>
        </div>
        <div class="stat-item">
          <span class="stat-label">未过期</span>
          <strong class="stat-value">{{ userSummary.active }}</strong>
        </div>
        <div class="stat-item">
          <span class="stat-label">已过期</span>
          <strong class="stat-value">{{ userSummary.expired }}</strong>
        </div>
        <div class="stat-item">
          <span class="stat-label">日常</span>
          <strong class="stat-value">{{ userSummary.daily }}</strong>
        </div>
        <div class="stat-item">
          <span class="stat-label">对弈竞猜</span>
          <strong class="stat-value">{{ userSummary.duiyi }}</strong>
        </div>
        <div class="stat-item">
          <span class="stat-label">刷卡</span>
          <strong class="stat-value">{{ userSummary.shuaka }}</strong>
        </div>
        <div class="stat-item">
          <span class="stat-label">寄养</span>
          <strong class="stat-value">{{ userSummary.foster }}</strong>
        </div>
        <div class="stat-item">
          <span class="stat-label">精致日常</span>
          <strong class="stat-value">{{ userSummary.jingzhi }}</strong>
        </div>
      </div>

      <TableSkeleton v-if="users.length === 0 && loading.users" :rows="5" :columns="5" />

      <div v-else class="data-table-wrapper">
        <el-table
          ref="userTableRef"
          v-loading="loading.users"
          :data="users"
          border
          stripe
          row-key="id"
          empty-text="暂无下属数据"
          @selection-change="onSelectionChange"
        >
          <el-table-column type="selection" width="45" />
          <el-table-column prop="id" label="ID" width="80" sortable />
          <el-table-column prop="login_id" label="登录ID" width="100" sortable />
          <el-table-column label="账号" min-width="180">
            <template #default="scope">
              <span class="clickable-cell" @click="copyAccountNo(scope.row.account_no)" title="点击复制">{{ scope.row.account_no }}</span>
            </template>
          </el-table-column>
          <el-table-column prop="user_type" label="类型" width="120">
            <template #default="scope">
              <el-tag type="info">{{ userTypeLabel(scope.row.user_type) }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="server" label="服务器" min-width="120" />
          <el-table-column prop="username" label="用户名" min-width="120" />
          <el-table-column prop="status" label="状态" width="120" sortable>
            <template #default="scope">
              <el-tag :type="statusTagType(scope.row.status)">{{ statusLabel(scope.row.status) }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="存档状态" width="120">
            <template #default="scope">
              <el-tag :type="scope.row.archive_status === 'normal' ? 'success' : 'danger'">{{ scope.row.archive_status === 'normal' ? '正常' : '失效' }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="到期时间" min-width="190" sortable>
            <template #default="scope">{{ formatTime(scope.row.expires_at) }}</template>
          </el-table-column>
          <el-table-column label="查看日志" width="100" align="center">
            <template #default="scope">
              <el-switch v-model="scope.row.can_view_logs" @change="toggleCanViewLogs(scope.row)" />
            </template>
          </el-table-column>
          <el-table-column label="操作" width="160">
            <template #default="scope">
              <el-button type="primary" plain size="small" @click="selectUser(scope.row)">详情</el-button>
              <el-button type="danger" plain size="small" :loading="scope.row._deleting" @click="deleteUser(scope.row)">删除</el-button>
            </template>
          </el-table-column>
        </el-table>
      </div>

      <div class="pagination-wrapper">
        <el-pagination
          v-model:current-page="pagination.page"
          v-model:page-size="pagination.pageSize"
          :total="pagination.total"
          :page-sizes="[50, 100, 200, 500]"
          layout="total, sizes, prev, pager, next"
          @current-change="loadUsers"
          @size-change="loadUsers"
        />
      </div>
    </section>

    <!-- Quick Create Dialog -->
    <el-dialog v-model="showQuickCreateDialog" title="创建下属账号" class="dialog-sm" append-to-body @close="createdAccounts.value = []">
      <el-form :model="quickForm" label-width="100px">
        <el-form-item label="建号天数">
          <el-input-number v-model="quickForm.duration_days" :min="1" :max="3650" class="w-full" />
        </el-form-item>
        <el-form-item label="类型">
          <el-select v-model="quickForm.user_type" class="w-full">
            <el-option v-for="item in userTypeOptions" :key="item.value" :label="item.label" :value="item.value" />
          </el-select>
        </el-form-item>
        <el-form-item label="数量">
          <el-input-number v-model="quickForm.count" :min="1" :max="20" class="w-full" />
        </el-form-item>
      </el-form>
      <template v-if="createdAccounts.length > 0">
        <div class="generated-list-header">
          <span class="muted">已创建 {{ createdAccounts.length }} 个账号</span>
          <el-button size="small" type="primary" plain @click="copyAllAccounts">全部复制</el-button>
        </div>
        <div class="generated-list-body">
          <div v-for="(item, idx) in createdAccounts" :key="idx" class="generated-list-item">
            <el-tag type="info" size="small">{{ userTypeLabel(item.user_type) }}</el-tag>
            <code>{{ item.login_id }} - {{ item.account_no }}</code>
            <el-button size="small" plain @click="copyAccountNo(item.account_no)">复制</el-button>
          </div>
        </div>
      </template>
      <template #footer>
        <el-button @click="showQuickCreateDialog = false">关闭</el-button>
        <el-button type="warning" :loading="loading.quickCreate" @click="quickCreateUser">{{ quickForm.count > 1 ? `批量创建 (${quickForm.count}个)` : '创建账号' }}</el-button>
      </template>
    </el-dialog>

    <!-- User Detail Dialog -->
    <el-dialog v-model="showUserDetailDialog" title="下属账号详情" class="dialog-lg" append-to-body :close-on-click-modal="false">
      <div class="dialog-header-bar">
        <strong>{{ props.selectedUserAccountNo || '-' }}</strong>
        <el-tag :type="statusTagType(props.selectedUserStatus)">{{ statusLabel(props.selectedUserStatus) }}</el-tag>
        <el-tag type="info">{{ userTypeLabel(props.selectedUserType) }}</el-tag>
        <span class="muted">到期：{{ formatTime(props.selectedUserExpiresAt) }}</span>
      </div>

      <el-tabs>
        <el-tab-pane label="过期时间">
          <el-form :model="lifecycleForm" label-width="100px" class="mt-12">
            <el-form-item label="延长天数">
              <el-input-number v-model="lifecycleForm.extend_days" :min="0" :max="3650" />
            </el-form-item>
            <el-form-item label="到期时间">
              <el-date-picker v-model="lifecycleForm.expires_at" type="datetime"
                value-format="YYYY-MM-DD HH:mm" placeholder="选择到期日期时间" class="w-210" />
            </el-form-item>
            <el-form-item label="状态">
              <el-tag :type="statusTagType(props.selectedUserStatus)">{{ statusLabel(props.selectedUserStatus) }}</el-tag>
              <span style="margin-left: 8px; color: #909399; font-size: 12px;">（自动判定）</span>
            </el-form-item>
            <el-form-item label="存档状态">
              <el-select v-model="lifecycleForm.archive_status" class="w-120">
                <el-option label="正常" value="normal" />
                <el-option label="失效" value="invalid" />
              </el-select>
            </el-form-item>
            <el-form-item label="查看日志">
              <el-switch v-model="detailCanViewLogs" @change="toggleDetailCanViewLogs" />
              <span style="margin-left: 8px; color: #909399; font-size: 12px;">（允许用户查看执行日志）</span>
            </el-form-item>
            <el-form-item>
              <el-button type="primary" :loading="loading.lifecycle" @click="saveUserLifecycle">保存</el-button>
            </el-form-item>
          </el-form>
        </el-tab-pane>

        <el-tab-pane label="资产">
          <div class="stats-grid mt-12">
            <div v-for="asset in assetFields" :key="asset.key" class="stat-item">
              <span class="stat-label">{{ asset.label }}</span>
              <el-input-number v-model="selectedUserAssets[asset.key]" :min="0" :max="99999999" controls-position="right" />
            </div>
          </div>
          <div class="row-actions mt-12">
            <el-button type="success" :loading="loading.saveAssets" @click="saveSelectedUserAssets">保存资产</el-button>
            <el-button plain :loading="loading.userAssets" @click="loadSelectedUserAssets">刷新</el-button>
          </div>
        </el-tab-pane>

        <el-tab-pane label="任务配置">
          <div class="mt-12">
            <div class="mb-12">
              <el-radio-group v-model="taskFilter" size="small">
                <el-radio-button value="">全部</el-radio-button>
                <el-radio-button value="enabled">已启用</el-radio-button>
                <el-radio-button value="disabled">未启用</el-radio-button>
              </el-radio-group>
            </div>
            <div class="data-table-wrapper">
              <el-table :data="filteredTaskRows" border stripe empty-text="暂无任务模板" :row-class-name="tableRowClassName">
                <el-table-column type="expand">
                  <template #default="scope">
                    <div v-if="isSpecialTask(scope.row.name)" class="expand-config">
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
                        <div class="expand-row" style="margin-top: 8px;">
                          <el-checkbox v-model="scope.row.config.auto_accept_friend" @change="saveOneTask(scope.row)">自动同意好友申请</el-checkbox>
                          <el-checkbox v-model="scope.row.config.collect_fanhe" @change="saveOneTask(scope.row)">领取饭盒</el-checkbox>
                        </div>
                      </template>
                    </div>
                  </template>
                </el-table-column>
                <el-table-column prop="name" label="任务类型" min-width="180" />
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
                <el-input v-model="selectedTaskConfigRaw" type="textarea" :rows="10"
                  placeholder='{"签到":{"enabled":true,"next_time":"08:30"}}' />
                <el-button type="primary" plain class="mt-8" @click="applyTaskConfigFromRaw">应用配置</el-button>
              </el-collapse-item>
            </el-collapse>

            <div class="row-actions mt-12">
              <el-button plain :loading="loading.tasks" @click="loadSelectedUserTasks">刷新</el-button>
            </div>
          </div>
        </el-tab-pane>

        <el-tab-pane label="执行日志">
          <UserLogsTab
            :token="props.token"
            :selected-user-id="props.selectedUserId"
            :selected-user-account-no="props.selectedUserAccountNo"
          />
        </el-tab-pane>
      </el-tabs>

      <template #footer>
        <el-button @click="showUserDetailDialog = false">关闭</el-button>
      </template>
    </el-dialog>

    <!-- Batch Lifecycle Dialog -->
    <el-dialog v-model="showBatchLifecycleDialog" title="批量生命周期操作" class="dialog-sm" append-to-body>
      <el-form :model="batchLifecycleForm" label-width="100px">
        <el-form-item label="延长天数">
          <el-input-number v-model="batchLifecycleForm.extend_days" :min="0" :max="3650" />
        </el-form-item>
        <el-form-item label="到期时间">
          <el-date-picker
            v-model="batchLifecycleForm.expires_at"
            type="datetime"
            value-format="YYYY-MM-DD HH:mm"
            placeholder="选择到期日期时间"
            class="w-full"
          />
        </el-form-item>
        <el-form-item label="状态">
          <el-select v-model="batchLifecycleForm.status" clearable placeholder="不改变" class="w-full">
            <el-option label="活跃" value="active" />
            <el-option label="已过期" value="expired" />
            <el-option label="已禁用" value="disabled" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showBatchLifecycleDialog = false">取消</el-button>
        <el-button type="primary" :loading="loading.batchLifecycle" @click="batchExtendUsers">确认执行</el-button>
      </template>
    </el-dialog>

    <!-- Batch Assets Dialog -->
    <el-dialog v-model="showBatchAssetsDialog" title="批量资产设置" class="dialog-md" append-to-body>
      <el-form label-width="100px">
        <el-form-item v-for="asset in assetFields" :key="asset.key" :label="asset.label">
          <el-input-number v-model="batchAssetsForm[asset.key]" :min="0" :max="99999999" controls-position="right" class="w-full" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showBatchAssetsDialog = false">取消</el-button>
        <el-button type="success" :loading="loading.batchAssets" @click="batchSetUserAssets">确认设置</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref, watch } from "vue";
import { ElMessage, ElMessageBox } from "element-plus";
import { managerApi, parseApiError } from "../../lib/http";
import {
  statusTagType,
  statusLabel,
  userTypeLabel,
  formatTime,
  patchSummary,
  ensureTaskConfig,
  parseTaskConfigFromRaw,
  ASSET_FIELDS,
  USER_TYPE_OPTIONS,
  SPECIAL_TASK_NAMES,
  INTERRUPTABLE_TASK_OPTIONS,
  FOSTER_REWARD_OPTIONS,
  copyToClipboard,
} from "../../lib/helpers";
import { usePagination } from "../../composables/usePagination";
import { useBatchSelection } from "../../composables/useBatchSelection";
import { useDebouncedFilter } from "../../composables/useDebouncedFilter";
import TableSkeleton from "../shared/TableSkeleton.vue";
import UserLogsTab from "./UserLogsTab.vue";

const props = defineProps({
  token: { type: String, default: "" },
  selectedUserId: { type: Number, default: 0 },
  selectedUserType: { type: String, default: "daily" },
  selectedUserAccountNo: { type: String, default: "" },
  selectedUserStatus: { type: String, default: "" },
  selectedUserExpiresAt: { type: String, default: "" },
  templateCache: { type: Object, default: () => ({}) },
  ensureTaskTemplates: { type: Function, required: true },
});

const emit = defineEmits(["user-selected"]);

const loading = reactive({
  quickCreate: false,
  users: false,
  tasks: false,
  saveTasks: false,
  lifecycle: false,
  userAssets: false,
  saveAssets: false,
  batchLifecycle: false,
  batchAssets: false,
  batchDelete: false,
});

const quickForm = reactive({ duration_days: 30, user_type: "daily", count: 1 });
const filters = reactive({ keyword: "", status: "", userType: "" });
const quickCreatedAccount = ref("");
const quickCreatedUserType = ref("daily");
const createdAccounts = ref([]);
const users = ref([]);
const userTableRef = ref(null);
const selectedTaskConfigRaw = ref("{}");
const taskRows = ref([]);
const taskFilter = ref("");

const filteredTaskRows = computed(() => {
  if (taskFilter.value === "enabled") return taskRows.value.filter(r => r.config.enabled === true);
  if (taskFilter.value === "disabled") return taskRows.value.filter(r => r.config.enabled !== true);
  return taskRows.value;
});
const userTypeOptions = ref([...USER_TYPE_OPTIONS]);
const assetFields = ASSET_FIELDS;

const showUserDetailDialog = ref(false);
const showQuickCreateDialog = ref(false);
const showBatchLifecycleDialog = ref(false);
const showBatchAssetsDialog = ref(false);

const batchLifecycleForm = reactive({
  extend_days: 0,
  expires_at: "",
  status: "",
});

const batchAssetsForm = reactive({
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

const lifecycleForm = reactive({
  extend_days: 0,
  expires_at: "",
  archive_status: "",
});

const detailCanViewLogs = ref(false);

const selectedUserAssets = reactive({
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

const userSummary = reactive({
  total: 0,
  active: 0,
  expired: 0,
  disabled: 0,
  daily: 0,
  duiyi: 0,
  shuaka: 0,
  foster: 0,
  jingzhi: 0,
});

const { pagination, updateTotal, resetPage, paginationParams } = usePagination({ defaultPageSize: 50 });
const { selectedIds, selectedCount, hasSelection, onSelectionChange, clearSelection } = useBatchSelection();

useDebouncedFilter(() => filters, loadUsers, { delay: 400, resetPage });

const currentTemplate = computed(() => {
  return (
    props.templateCache[props.selectedUserType] || {
      order: [],
      defaultConfig: {},
    }
  );
});

function doClearSelection() {
  clearSelection();
  if (userTableRef.value) {
    userTableRef.value.clearSelection();
  }
}

function buildTaskRows(config) {
  const tpl = currentTemplate.value;
  taskRows.value = tpl.order.map((name) => {
    const cfg = reactive(ensureTaskConfig(config[name]));
    return { name, config: cfg };
  });
}

function isSpecialTask(name) {
  return SPECIAL_TASK_NAMES.has(name);
}

function tableRowClassName({ row }) {
  return isSpecialTask(row.name) ? '' : 'hide-expand';
}

function stringifyTaskConfig(config) {
  selectedTaskConfigRaw.value = JSON.stringify(config || {}, null, 2);
}

async function applyTaskConfigFromRaw() {
  const config = parseTaskConfigFromRaw(selectedTaskConfigRaw.value);
  buildTaskRows(config);
  await saveSelectedUserTasks();
  ElMessage.success("已从 JSON 同步并保存");
}

async function saveOneTask(row) {
  if (!props.selectedUserId) return;
  const payload = {
    task_config: {
      [row.name]: {
        ...(row.config || {}),
        enabled: row.config.enabled === true,
      },
    },
  };
  try {
    await managerApi.putUserTasks(props.token, props.selectedUserId, payload);
  } catch (error) {
    ElMessage.error(parseApiError(error));
  }
}

async function quickCreateUser() {
  loading.quickCreate = true;
  createdAccounts.value = [];
  try {
    const total = Math.max(1, Math.min(20, quickForm.count || 1));
    for (let i = 0; i < total; i++) {
      const response = await managerApi.quickCreateUser(props.token, {
        duration_days: quickForm.duration_days,
        user_type: quickForm.user_type,
      });
      createdAccounts.value.push({ account_no: response.account_no || "", login_id: response.login_id || "", user_type: response.user_type || quickForm.user_type });
    }
    quickCreatedAccount.value = createdAccounts.value[0]?.account_no || "";
    quickCreatedUserType.value = createdAccounts.value[0]?.user_type || quickForm.user_type;
    ElMessage.success(total > 1 ? `已创建 ${total} 个账号` : "下属账号创建成功");
    await loadUsers();
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.quickCreate = false;
  }
}

async function copyAccountNo(accountNo) {
  try {
    await copyToClipboard(accountNo);
    ElMessage.success("已复制到剪贴板");
  } catch {
    ElMessage.warning("复制失败，请手动选择");
  }
}

async function copyAllAccounts() {
  const text = createdAccounts.value.map((item) => item.account_no).join("\n");
  try {
    await copyToClipboard(text);
    ElMessage.success(`已复制 ${createdAccounts.value.length} 个账号`);
  } catch {
    ElMessage.warning("复制失败，请手动选择");
  }
}

async function loadUsers() {
  if (!props.token) return;
  loading.users = true;
  try {
    const params = {
      ...paginationParams(),
      keyword: filters.keyword || undefined,
      status: filters.status || undefined,
      user_type: filters.userType || undefined,
    };
    const response = await managerApi.listUsers(props.token, params);
    users.value = (response.items || []).map((item) => ({ ...item, _deleting: false }));
    patchSummary(userSummary, response.summary, ["total", "active", "expired", "disabled", "daily", "duiyi", "shuaka", "foster", "jingzhi"]);
    updateTotal(response.total || response.summary?.total || users.value.length);
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.users = false;
  }
}

async function selectUser(row) {
  emit("user-selected", row);
  lifecycleForm.expires_at = row.expires_at || "";
  lifecycleForm.extend_days = 0;
  lifecycleForm.archive_status = row.archive_status || "normal";
  detailCanViewLogs.value = !!row.can_view_logs;
  showUserDetailDialog.value = true;
  await props.ensureTaskTemplates(row.user_type || "daily", userTypeOptions);
  await Promise.all([loadSelectedUserTasks(), loadSelectedUserAssets()]);
}

async function loadSelectedUserTasks() {
  if (!props.selectedUserId) return;
  loading.tasks = true;
  try {
    const response = await managerApi.getUserTasks(props.token, props.selectedUserId);
    const userType = response.user_type || props.selectedUserType || "daily";
    const template = await props.ensureTaskTemplates(userType, userTypeOptions);
    const merged = {
      ...(template.defaultConfig || {}),
      ...(response.task_config || {}),
    };
    buildTaskRows(merged);
    stringifyTaskConfig(merged);
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.tasks = false;
  }
}

async function saveSelectedUserTasks() {
  if (!props.selectedUserId) {
    ElMessage.warning("请先选择下属账号");
    return;
  }
  const normalizedConfig = {};
  const rows = taskRows.value;
  rows.forEach((row) => {
    normalizedConfig[row.name] = { ...row.config };
  });
  loading.saveTasks = true;
  try {
    const response = await managerApi.putUserTasks(props.token, props.selectedUserId, {
      task_config: normalizedConfig,
    });
    const savedConfig = response.task_config || normalizedConfig;
    buildTaskRows(savedConfig);
    stringifyTaskConfig(savedConfig);
    ElMessage.success("任务配置更新成功");
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.saveTasks = false;
  }
}

async function loadSelectedUserAssets() {
  if (!props.selectedUserId) return;
  loading.userAssets = true;
  try {
    const response = await managerApi.getUserAssets(props.token, props.selectedUserId);
    const incoming = response.assets || {};
    Object.keys(selectedUserAssets).forEach((key) => {
      selectedUserAssets[key] = Number(incoming[key] ?? selectedUserAssets[key] ?? 0);
    });
    lifecycleForm.expires_at = response.expires_at || lifecycleForm.expires_at;
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.userAssets = false;
  }
}

async function saveSelectedUserAssets() {
  if (!props.selectedUserId) return;
  loading.saveAssets = true;
  try {
    await managerApi.putUserAssets(props.token, props.selectedUserId, {
      assets: { ...selectedUserAssets },
    });
    ElMessage.success("用户资产已更新");
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.saveAssets = false;
  }
}

async function saveUserLifecycle() {
  if (!props.selectedUserId) return;
  const payload = {};
  if (lifecycleForm.extend_days > 0) payload.extend_days = lifecycleForm.extend_days;
  const trimmedExpires = (lifecycleForm.expires_at || "").trim();
  if (trimmedExpires) payload.expires_at = trimmedExpires;
  if (lifecycleForm.archive_status) payload.archive_status = lifecycleForm.archive_status;
  if (Object.keys(payload).length === 0) {
    ElMessage.warning("请填写至少一个字段");
    return;
  }
  loading.lifecycle = true;
  try {
    await managerApi.patchUserLifecycle(props.token, props.selectedUserId, payload);
    ElMessage.success("用户过期时间/状态更新成功");
    lifecycleForm.extend_days = 0;
    await loadUsers();
    const updatedUser = users.value.find((u) => u.id === props.selectedUserId);
    if (updatedUser) {
      emit("user-selected", updatedUser);
      lifecycleForm.expires_at = updatedUser.expires_at || "";
      lifecycleForm.archive_status = updatedUser.archive_status || "normal";
    }
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.lifecycle = false;
  }
}

async function batchExtendUsers() {
  if (!hasSelection.value) return;
  const ids = [...selectedIds.value];
  const payload = { user_ids: ids };
  if (batchLifecycleForm.extend_days > 0) payload.extend_days = batchLifecycleForm.extend_days;
  const trimmedExpires = (batchLifecycleForm.expires_at || "").trim();
  if (trimmedExpires) payload.expires_at = trimmedExpires;
  if (batchLifecycleForm.status) payload.status = batchLifecycleForm.status;
  if (!payload.extend_days && !payload.expires_at && !payload.status) {
    ElMessage.warning("请填写至少一个生命周期字段");
    return;
  }
  loading.batchLifecycle = true;
  try {
    await managerApi.batchUserLifecycle(props.token, payload);
    ElMessage.success(`已批量更新 ${ids.length} 个用户的生命周期`);
    showBatchLifecycleDialog.value = false;
    batchLifecycleForm.extend_days = 0;
    batchLifecycleForm.expires_at = "";
    batchLifecycleForm.status = "";
    doClearSelection();
    await loadUsers();
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.batchLifecycle = false;
  }
}

async function batchSetUserAssets() {
  if (!hasSelection.value) return;
  const ids = [...selectedIds.value];
  loading.batchAssets = true;
  try {
    await managerApi.batchUserAssets(props.token, {
      user_ids: ids,
      assets: { ...batchAssetsForm },
    });
    ElMessage.success(`已批量设置 ${ids.length} 个用户的资产`);
    showBatchAssetsDialog.value = false;
    doClearSelection();
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.batchAssets = false;
  }
}

async function toggleCanViewLogs(row) {
  try {
    await managerApi.patchUserSettings(props.token, row.id, {
      can_view_logs: !!row.can_view_logs,
    });
    ElMessage.success(row.can_view_logs ? "已允许查看日志" : "已禁止查看日志");
  } catch (error) {
    row.can_view_logs = !row.can_view_logs;
    ElMessage.error(parseApiError(error));
  }
}

async function toggleDetailCanViewLogs(val) {
  if (!props.selectedUserId) return;
  try {
    await managerApi.patchUserSettings(props.token, props.selectedUserId, {
      can_view_logs: !!val,
    });
    const row = users.value.find((u) => u.id === props.selectedUserId);
    if (row) row.can_view_logs = !!val;
    ElMessage.success(val ? "已允许查看日志" : "已禁止查看日志");
  } catch (error) {
    detailCanViewLogs.value = !val;
    ElMessage.error(parseApiError(error));
  }
}

async function deleteUser(row) {
  if (!row?.id) {
    ElMessage.warning("无效用户记录");
    return;
  }
  try {
    await ElMessageBox.confirm(
      `确定要删除用户 "${row.account_no}" 吗？删除后该用户的所有数据（任务、日志、Token等）将被永久清除，无法恢复。`,
      "确认删除",
      { confirmButtonText: "确定删除", cancelButtonText: "取消", type: "warning" },
    );
  } catch {
    return;
  }
  row._deleting = true;
  try {
    await managerApi.deleteUser(props.token, row.id);
    ElMessage.success("用户已删除");
    await loadUsers();
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    row._deleting = false;
  }
}

async function batchDeleteUsers() {
  if (!hasSelection.value) return;
  const ids = [...selectedIds.value];
  try {
    await ElMessageBox.confirm(
      `确定要批量删除 ${ids.length} 个用户吗？删除后所有相关数据将被永久清除，无法恢复。`,
      "批量删除确认",
      { confirmButtonText: "确定删除", cancelButtonText: "取消", type: "warning" },
    );
  } catch {
    return;
  }
  loading.batchDelete = true;
  try {
    await managerApi.batchDeleteUsers(props.token, { user_ids: ids });
    ElMessage.success(`已批量删除 ${ids.length} 个用户`);
    doClearSelection();
    await loadUsers();
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.batchDelete = false;
  }
}

watch(
  () => props.token,
  async (value) => {
    if (!value) {
      users.value = [];
      taskRows.value = [];
      selectedTaskConfigRaw.value = "{}";
      return;
    }
    await props.ensureTaskTemplates("daily", userTypeOptions);
    await loadUsers();
  },
  { immediate: true },
);

onMounted(async () => {
  if (props.token) {
    await props.ensureTaskTemplates("daily", userTypeOptions);
    await loadUsers();
  }
});
</script>

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
