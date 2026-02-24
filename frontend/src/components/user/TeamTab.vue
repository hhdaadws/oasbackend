<script setup>
import { onMounted, reactive, ref } from "vue";
import { ElMessage, ElMessageBox } from "element-plus";
import { parseApiError, userApi } from "../../lib/http";
import { formatTime } from "../../lib/helpers";

const props = defineProps({
  token: { type: String, default: "" },
});

const loading = reactive({ requests: false, friends: false, action: false, lineup: false });

const teamRequests = ref([]);
const friends = ref([]);
const bookedSlots = ref([]);
const showCreateDialog = ref(false);
const showAcceptDialog = ref(false);
const acceptingRequest = ref(null);

// 阵容配置（在 tab 中直接维护）
const driverLineup = reactive({ group: 0, position: 0 });
const attackerLineup = reactive({ group: 0, position: 0 });

const createForm = reactive({
  friend_id: null,
  scheduled_at: "",
  role: "driver",
});

const acceptForm = reactive({
  role: "attacker",
});

// ── 阵容加载/保存 ────────────────────────────────────────

async function loadLineup() {
  loading.lineup = true;
  try {
    const res = await userApi.getMeLineup(props.token);
    const data = res.lineup || {};
    const driver = data["组队御魂_司机"] || {};
    const attacker = data["组队御魂_打手"] || {};
    driverLineup.group = driver.group || 0;
    driverLineup.position = driver.position || 0;
    attackerLineup.group = attacker.group || 0;
    attackerLineup.position = attacker.position || 0;
  } catch {
    // 首次无记录时静默忽略
  } finally {
    loading.lineup = false;
  }
}

async function saveLineup() {
  loading.lineup = true;
  try {
    await userApi.putMeLineup(props.token, {
      lineup: {
        "组队御魂_司机": { group: driverLineup.group, position: driverLineup.position },
        "组队御魂_打手": { group: attackerLineup.group, position: attackerLineup.position },
      },
    });
    ElMessage.success("阵容配置已保存");
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.lineup = false;
  }
}

// ── 组队请求加载 ──────────────────────────────────────────

async function loadRequests() {
  if (!props.token) return;
  loading.requests = true;
  try {
    const res = await userApi.getTeamYuhunRequests(props.token);
    // 后端返回 {"data": [...]}，res 即为该对象
    teamRequests.value = res.data || [];
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.requests = false;
  }
}

async function loadFriends() {
  loading.friends = true;
  try {
    const res = await userApi.getFriends(props.token);
    friends.value = res.friends || [];
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.friends = false;
  }
}

async function loadBookedSlots() {
  try {
    const res = await userApi.getTeamYuhunBookedSlots(props.token);
    bookedSlots.value = (res.data && res.data.booked_slots) || [];
  } catch {
    bookedSlots.value = [];
  }
}

// ── 辅助：当前用户是否为发起方 ────────────────────────────

function isRequester(row) {
  return row.direction === "sent";
}

function myRole(row) {
  return isRequester(row) ? row.requester?.role : row.receiver?.role;
}

function myLineup(row) {
  return isRequester(row) ? row.requester?.lineup : row.receiver?.lineup;
}

function partnerAccountNo(row) {
  return isRequester(row)
    ? (row.receiver?.username || row.receiver?.account_no || "-")
    : (row.requester?.username || row.requester?.account_no || "-");
}

// ── 发起组队 ─────────────────────────────────────────────

async function openCreateDialog() {
  createForm.friend_id = null;
  createForm.scheduled_at = "";
  createForm.role = "driver";
  showCreateDialog.value = true;
  await Promise.all([loadFriends(), loadBookedSlots()]);
}

// 检查选择时间是否冲突（±30分钟）
function isTimeConflict(val) {
  if (!val) return false;
  const selected = new Date(val).getTime();
  return bookedSlots.value.some((s) => Math.abs(new Date(s).getTime() - selected) < 30 * 60 * 1000);
}

async function submitCreate() {
  if (!createForm.friend_id) {
    ElMessage.warning("请选择好友");
    return;
  }
  if (!createForm.scheduled_at) {
    ElMessage.warning("请选择预约时间");
    return;
  }
  if (isTimeConflict(createForm.scheduled_at)) {
    ElMessage.warning("该时间段已有其他用户预约（±30分钟内），请选择其他时间");
    return;
  }
  // 根据角色取对应阵容
  const lineup = createForm.role === "driver"
    ? { group: driverLineup.group, position: driverLineup.position }
    : { group: attackerLineup.group, position: attackerLineup.position };
  loading.action = true;
  try {
    await userApi.sendTeamYuhunRequest(props.token, {
      friend_id: createForm.friend_id,
      scheduled_at: createForm.scheduled_at,
      role: createForm.role,
      lineup,
    });
    ElMessage.success("组队请求已发送");
    showCreateDialog.value = false;
    await loadRequests();
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.action = false;
  }
}

// ── 接受组队 ─────────────────────────────────────────────

function openAcceptDialog(req) {
  acceptingRequest.value = req;
  // 自动反转角色（对方是司机则我是打手，反之亦然）
  const requesterRole = req.requester?.role;
  acceptForm.role = requesterRole === "driver" ? "attacker" : "driver";
  showAcceptDialog.value = true;
}

async function submitAccept() {
  if (!acceptingRequest.value) return;
  // 根据角色取对应阵容
  const lineup = acceptForm.role === "driver"
    ? { group: driverLineup.group, position: driverLineup.position }
    : { group: attackerLineup.group, position: attackerLineup.position };
  loading.action = true;
  try {
    await userApi.acceptTeamYuhunRequest(props.token, acceptingRequest.value.id, {
      role: acceptForm.role,
      lineup,
    });
    ElMessage.success("已接受组队请求");
    showAcceptDialog.value = false;
    await loadRequests();
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.action = false;
  }
}

// ── 拒绝/取消 ────────────────────────────────────────────

async function rejectRequest(req) {
  loading.action = true;
  try {
    await userApi.rejectTeamYuhunRequest(props.token, req.id);
    ElMessage.success("已拒绝组队请求");
    await loadRequests();
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.action = false;
  }
}

async function cancelRequest(req) {
  try {
    await ElMessageBox.confirm('确定要取消该组队请求吗？对方将看到"已取消"状态。', "确认取消", {
      confirmButtonText: "确定",
      cancelButtonText: "取消",
      type: "warning",
    });
  } catch {
    return;
  }
  loading.action = true;
  try {
    await userApi.cancelTeamYuhunRequest(props.token, req.id);
    ElMessage.success("组队请求已取消");
    await loadRequests();
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.action = false;
  }
}

// ── 辅助函数 ─────────────────────────────────────────────

function roleLabel(role) {
  const map = { driver: "司机", attacker: "打手" };
  return map[role] || role || "-";
}

function requestStatusLabel(status) {
  const map = {
    pending: "待接受",
    accepted: "已接受",
    rejected: "已拒绝",
    cancelled: "已取消",
    completed: "已完成",
    expired: "已过期",
  };
  return map[status] || status || "-";
}

function requestStatusTagType(status) {
  const map = {
    pending: "warning",
    accepted: "success",
    rejected: "danger",
    cancelled: "info",
    completed: "info",
    expired: "info",
  };
  return map[status] || "info";
}

function lineupText(lineup) {
  if (!lineup || (!lineup.group && !lineup.position)) return "未配置";
  const parts = [];
  if (lineup.group) parts.push("分组" + lineup.group);
  if (lineup.position) parts.push("阵容" + lineup.position);
  return parts.join(" / ");
}

onMounted(async () => {
  if (props.token) {
    await Promise.all([loadRequests(), loadLineup()]);
  }
});
</script>

<template>
  <section class="panel-card">
    <div class="panel-headline">
      <h3>组队御魂</h3>
      <div class="row-actions">
        <el-button plain :loading="loading.requests" @click="loadRequests">刷新</el-button>
        <el-button type="primary" @click="openCreateDialog">发起组队</el-button>
      </div>
    </div>

    <!-- 阵容配置区块 -->
    <div class="lineup-config-block">
      <div class="lineup-config-title">我的阵容配置</div>
      <div class="lineup-config-row">
        <span class="lineup-role-label">司机阵容：</span>
        <span class="lineup-sub-label">分组</span>
        <el-select v-model="driverLineup.group" size="small" class="w-100">
          <el-option label="未配置" :value="0" />
          <el-option v-for="n in 7" :key="n" :label="'分组' + n" :value="n" />
        </el-select>
        <span class="lineup-sub-label">阵容</span>
        <el-select v-model="driverLineup.position" size="small" class="w-100">
          <el-option label="未配置" :value="0" />
          <el-option v-for="n in 7" :key="n" :label="'阵容' + n" :value="n" />
        </el-select>
      </div>
      <div class="lineup-config-row">
        <span class="lineup-role-label">打手阵容：</span>
        <span class="lineup-sub-label">分组</span>
        <el-select v-model="attackerLineup.group" size="small" class="w-100">
          <el-option label="未配置" :value="0" />
          <el-option v-for="n in 7" :key="n" :label="'分组' + n" :value="n" />
        </el-select>
        <span class="lineup-sub-label">阵容</span>
        <el-select v-model="attackerLineup.position" size="small" class="w-100">
          <el-option label="未配置" :value="0" />
          <el-option v-for="n in 7" :key="n" :label="'阵容' + n" :value="n" />
        </el-select>
        <el-button size="small" type="primary" plain :loading="loading.lineup" @click="saveLineup">保存阵容</el-button>
      </div>
    </div>

    <div class="data-table-wrapper">
      <el-table v-loading="loading.requests" :data="teamRequests" border stripe empty-text="暂无组队请求">
        <el-table-column label="方向" width="80">
          <template #default="scope">
            <el-tag :type="isRequester(scope.row) ? '' : 'warning'" size="small">
              {{ isRequester(scope.row) ? '发出' : '收到' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="对方账号" min-width="160">
          <template #default="scope">{{ partnerAccountNo(scope.row) }}</template>
        </el-table-column>
        <el-table-column label="预约时间" min-width="170">
          <template #default="scope">{{ formatTime(scope.row.scheduled_at) }}</template>
        </el-table-column>
        <el-table-column label="我的角色" width="100">
          <template #default="scope">{{ roleLabel(myRole(scope.row)) }}</template>
        </el-table-column>
        <el-table-column label="我的阵容" width="140">
          <template #default="scope">{{ lineupText(myLineup(scope.row)) }}</template>
        </el-table-column>
        <el-table-column label="状态" width="100">
          <template #default="scope">
            <el-tag :type="requestStatusTagType(scope.row.status)">{{ requestStatusLabel(scope.row.status) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="180">
          <template #default="scope">
            <!-- pending 状态 -->
            <template v-if="scope.row.status === 'pending'">
              <template v-if="isRequester(scope.row)">
                <el-button type="danger" plain size="small" :loading="loading.action" @click="cancelRequest(scope.row)">取消</el-button>
              </template>
              <template v-else>
                <el-button type="success" plain size="small" :loading="loading.action" @click="openAcceptDialog(scope.row)">接受</el-button>
                <el-button type="danger" plain size="small" :loading="loading.action" @click="rejectRequest(scope.row)">拒绝</el-button>
              </template>
            </template>
            <!-- accepted 状态：双方均可取消 -->
            <template v-else-if="scope.row.status === 'accepted'">
              <el-button type="danger" plain size="small" :loading="loading.action" @click="cancelRequest(scope.row)">取消</el-button>
            </template>
            <span v-else class="muted">-</span>
          </template>
        </el-table-column>
      </el-table>
    </div>
  </section>

  <!-- Create Dialog：仅好友、角色、预约时间 -->
  <el-dialog v-model="showCreateDialog" title="发起组队御魂" class="dialog-sm" append-to-body>
    <el-form :model="createForm" label-width="100px">
      <el-form-item label="选择好友">
        <el-select v-model="createForm.friend_id" :loading="loading.friends" placeholder="请选择好友" class="w-full">
          <el-option
            v-for="f in friends"
            :key="f.friend_id"
            :label="(f.username || f.account_no || '') + (f.server ? ' (' + f.server + ')' : '')"
            :value="f.friend_id"
          />
        </el-select>
      </el-form-item>
      <el-form-item label="预约时间">
        <el-date-picker
          v-model="createForm.scheduled_at"
          type="datetime"
          value-format="YYYY-MM-DDTHH:mm:ssZ"
          placeholder="选择预约执行时间"
          class="w-full"
        />
        <div v-if="createForm.scheduled_at && isTimeConflict(createForm.scheduled_at)" class="time-conflict-tip">
          该时间段已有其他用户预约（±30分钟），请选择其他时间
        </div>
      </el-form-item>
      <el-form-item label="我的角色">
        <el-radio-group v-model="createForm.role">
          <el-radio value="driver">司机（阵容：{{ lineupText(driverLineup) }}）</el-radio>
          <el-radio value="attacker">打手（阵容：{{ lineupText(attackerLineup) }}）</el-radio>
        </el-radio-group>
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="showCreateDialog = false">取消</el-button>
      <el-button type="primary" :loading="loading.action" @click="submitCreate">发送请求</el-button>
    </template>
  </el-dialog>

  <!-- Accept Dialog：仅显示信息 + 确认 -->
  <el-dialog v-model="showAcceptDialog" title="接受组队请求" class="dialog-sm" append-to-body>
    <div v-if="acceptingRequest" class="accept-info">
      <p>对方角色：<el-tag>{{ roleLabel(acceptingRequest.requester?.role) }}</el-tag></p>
      <p>预约时间：{{ formatTime(acceptingRequest.scheduled_at) }}</p>
      <p>
        我的角色：<el-tag type="success">{{ roleLabel(acceptForm.role) }}</el-tag>
        &nbsp;使用阵容：{{ acceptForm.role === 'driver' ? lineupText(driverLineup) : lineupText(attackerLineup) }}
      </p>
      <p class="tip-text">阵容配置在组队标签页上方修改。</p>
    </div>
    <template #footer>
      <el-button @click="showAcceptDialog = false">取消</el-button>
      <el-button type="primary" :loading="loading.action" @click="submitAccept">确认接受</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.lineup-config-block {
  background: var(--el-fill-color-light, #f5f7fa);
  border: 1px solid var(--el-border-color-lighter, #ebeef5);
  border-radius: 6px;
  padding: 12px 16px;
  margin-bottom: 16px;
}
.lineup-config-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-primary, #303133);
  margin-bottom: 10px;
}
.lineup-config-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}
.lineup-config-row:last-child {
  margin-bottom: 0;
}
.lineup-role-label {
  font-size: 13px;
  color: var(--el-text-color-regular, #606266);
  width: 68px;
  flex-shrink: 0;
}
.lineup-sub-label {
  font-size: 12px;
  color: var(--el-text-color-secondary, #909399);
}
.accept-info p {
  margin: 6px 0;
  font-size: 14px;
}
.time-conflict-tip {
  color: var(--el-color-danger, #f56c6c);
  font-size: 12px;
  margin-top: 4px;
}
.muted {
  color: var(--el-text-color-placeholder, #c0c4cc);
}
.w-100 {
  width: 100px;
}
</style>
