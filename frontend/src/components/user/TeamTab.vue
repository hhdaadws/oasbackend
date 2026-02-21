<script setup>
import { onMounted, reactive, ref, computed } from "vue";
import { ElMessage, ElMessageBox } from "element-plus";
import { parseApiError, userApi } from "../../lib/http";
import { formatTime } from "../../lib/helpers";

const props = defineProps({
  token: { type: String, default: "" },
});

const loading = reactive({ requests: false, friends: false, action: false });

const teamRequests = ref([]);
const friends = ref([]);
const showCreateDialog = ref(false);
const showAcceptDialog = ref(false);
const acceptingRequest = ref(null);

const createForm = reactive({
  friend_id: null,
  scheduled_at: "",
  role: "driver",
  lineup: { group: 0, position: 0 },
});

const acceptForm = reactive({
  role: "attacker",
  lineup: { group: 0, position: 0 },
});

async function loadRequests() {
  if (!props.token) return;
  loading.requests = true;
  try {
    const res = await userApi.getTeamYuhunRequests(props.token);
    teamRequests.value = res.requests || [];
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

async function openCreateDialog() {
  showCreateDialog.value = true;
  createForm.friend_id = null;
  createForm.scheduled_at = "";
  createForm.role = "driver";
  createForm.lineup = { group: 0, position: 0 };
  await loadFriends();
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
  loading.action = true;
  try {
    await userApi.sendTeamYuhunRequest(props.token, {
      friend_id: createForm.friend_id,
      scheduled_at: createForm.scheduled_at,
      role: createForm.role,
      lineup: createForm.lineup,
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

function openAcceptDialog(req) {
  acceptingRequest.value = req;
  acceptForm.role = req.requester_role === "driver" ? "attacker" : "driver";
  acceptForm.lineup = { group: 0, position: 0 };
  showAcceptDialog.value = true;
}

async function submitAccept() {
  if (!acceptingRequest.value) return;
  loading.action = true;
  try {
    await userApi.acceptTeamYuhunRequest(props.token, acceptingRequest.value.id, {
      role: acceptForm.role,
      lineup: acceptForm.lineup,
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
    await ElMessageBox.confirm("确定要取消该组队请求吗？", "确认取消", {
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

function roleLabel(role) {
  const map = { driver: "司机", attacker: "打手" };
  return map[role] || role || "-";
}

function requestStatusLabel(status) {
  const map = { pending: "待接受", accepted: "已接受", rejected: "已拒绝", completed: "已完成", expired: "已过期" };
  return map[status] || status || "-";
}

function requestStatusTagType(status) {
  const map = { pending: "warning", accepted: "success", rejected: "danger", completed: "info", expired: "info" };
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
  if (props.token) await loadRequests();
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

    <div class="data-table-wrapper">
      <el-table v-loading="loading.requests" :data="teamRequests" border stripe empty-text="暂无组队请求">
        <el-table-column label="方向" width="80">
          <template #default="scope">
            <el-tag :type="scope.row.is_requester ? '' : 'warning'" size="small">{{ scope.row.is_requester ? '发出' : '收到' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="partner_account_no" label="对方账号" min-width="160" />
        <el-table-column label="预约时间" min-width="170">
          <template #default="scope">{{ formatTime(scope.row.scheduled_at) }}</template>
        </el-table-column>
        <el-table-column label="我的角色" width="100">
          <template #default="scope">
            {{ scope.row.is_requester ? roleLabel(scope.row.requester_role) : roleLabel(scope.row.receiver_role) }}
          </template>
        </el-table-column>
        <el-table-column label="我的阵容" width="140">
          <template #default="scope">
            {{ scope.row.is_requester ? lineupText(scope.row.requester_lineup) : lineupText(scope.row.receiver_lineup) }}
          </template>
        </el-table-column>
        <el-table-column label="状态" width="100">
          <template #default="scope">
            <el-tag :type="requestStatusTagType(scope.row.status)">{{ requestStatusLabel(scope.row.status) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="160">
          <template #default="scope">
            <template v-if="scope.row.status === 'pending'">
              <template v-if="scope.row.is_requester">
                <el-button type="danger" plain size="small" :loading="loading.action" @click="cancelRequest(scope.row)">取消</el-button>
              </template>
              <template v-else>
                <el-button type="success" plain size="small" :loading="loading.action" @click="openAcceptDialog(scope.row)">接受</el-button>
                <el-button type="danger" plain size="small" :loading="loading.action" @click="rejectRequest(scope.row)">拒绝</el-button>
              </template>
            </template>
            <span v-else class="muted">-</span>
          </template>
        </el-table-column>
      </el-table>
    </div>
  </section>

  <!-- Create Dialog -->
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
      </el-form-item>
      <el-form-item label="角色">
        <el-radio-group v-model="createForm.role">
          <el-radio value="driver">司机</el-radio>
          <el-radio value="attacker">打手</el-radio>
        </el-radio-group>
      </el-form-item>
      <el-form-item label="分组">
        <el-select v-model="createForm.lineup.group" class="w-full">
          <el-option label="未配置" :value="0" />
          <el-option v-for="n in 7" :key="n" :label="'分组' + n" :value="n" />
        </el-select>
      </el-form-item>
      <el-form-item label="阵容">
        <el-select v-model="createForm.lineup.position" class="w-full">
          <el-option label="未配置" :value="0" />
          <el-option v-for="n in 7" :key="n" :label="'阵容' + n" :value="n" />
        </el-select>
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="showCreateDialog = false">取消</el-button>
      <el-button type="primary" :loading="loading.action" @click="submitCreate">发送请求</el-button>
    </template>
  </el-dialog>

  <!-- Accept Dialog -->
  <el-dialog v-model="showAcceptDialog" title="接受组队请求" class="dialog-sm" append-to-body>
    <div v-if="acceptingRequest" class="mb-12">
      <p>对方角色：<el-tag>{{ roleLabel(acceptingRequest.requester_role) }}</el-tag></p>
      <p>预约时间：{{ formatTime(acceptingRequest.scheduled_at) }}</p>
    </div>
    <el-form :model="acceptForm" label-width="100px">
      <el-form-item label="我的角色">
        <el-radio-group v-model="acceptForm.role">
          <el-radio value="driver">司机</el-radio>
          <el-radio value="attacker">打手</el-radio>
        </el-radio-group>
      </el-form-item>
      <el-form-item label="分组">
        <el-select v-model="acceptForm.lineup.group" class="w-full">
          <el-option label="未配置" :value="0" />
          <el-option v-for="n in 7" :key="n" :label="'分组' + n" :value="n" />
        </el-select>
      </el-form-item>
      <el-form-item label="阵容">
        <el-select v-model="acceptForm.lineup.position" class="w-full">
          <el-option label="未配置" :value="0" />
          <el-option v-for="n in 7" :key="n" :label="'阵容' + n" :value="n" />
        </el-select>
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="showAcceptDialog = false">取消</el-button>
      <el-button type="primary" :loading="loading.action" @click="submitAccept">确认接受</el-button>
    </template>
  </el-dialog>
</template>
