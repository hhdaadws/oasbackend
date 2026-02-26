<script setup>
import { onMounted, reactive, ref } from "vue";
import { ElMessage, ElMessageBox } from "element-plus";
import { parseApiError, userApi } from "../../lib/http";

const props = defineProps({
  token: { type: String, default: "" },
});

const loading = reactive({ friends: false, requests: false, action: false });

const friends = ref([]);
const friendRequests = ref([]);
const showAddDialog = ref(false);
const addUsername = ref("");

async function loadFriends() {
  if (!props.token) return;
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

async function loadFriendRequests() {
  if (!props.token) return;
  loading.requests = true;
  try {
    const res = await userApi.getFriendRequests(props.token);
    friendRequests.value = res.requests || [];
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.requests = false;
  }
}

function openAddDialog() {
  addUsername.value = "";
  showAddDialog.value = true;
}

async function sendRequest() {
  const name = addUsername.value.trim();
  if (!name) {
    ElMessage.warning("请输入用户名");
    return;
  }
  loading.action = true;
  try {
    await userApi.sendFriendRequest(props.token, { friend_username: name });
    ElMessage.success("好友请求已发送");
    showAddDialog.value = false;
    await Promise.all([loadFriends(), loadFriendRequests()]);
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.action = false;
  }
}

async function acceptRequest(req) {
  loading.action = true;
  try {
    await userApi.acceptFriendRequest(props.token, req.id);
    ElMessage.success("已接受好友请求");
    await Promise.all([loadFriends(), loadFriendRequests()]);
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.action = false;
  }
}

async function rejectRequest(req) {
  loading.action = true;
  try {
    await userApi.rejectFriendRequest(props.token, req.id);
    ElMessage.success("已拒绝好友请求");
    await loadFriendRequests();
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.action = false;
  }
}

async function deleteFriend(friend) {
  try {
    await ElMessageBox.confirm(
      `确定要删除好友 "${friend.account_no || friend.username || friend.friend_id}" 吗？`,
      "确认删除",
      { confirmButtonText: "确定", cancelButtonText: "取消", type: "warning" },
    );
  } catch {
    return;
  }
  loading.action = true;
  try {
    await userApi.deleteFriend(props.token, friend.id);
    ElMessage.success("好友已删除");
    await loadFriends();
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.action = false;
  }
}

function statusLabel(status) {
  const map = { pending: "待处理", accepted: "已接受", rejected: "已拒绝" };
  return map[status] || status || "-";
}

function statusTagType(status) {
  const map = { pending: "warning", accepted: "success", rejected: "danger" };
  return map[status] || "info";
}

onMounted(async () => {
  if (props.token) {
    await Promise.all([loadFriends(), loadFriendRequests()]);
  }
});
</script>

<template>
  <section class="panel-card">
    <div class="panel-headline">
      <h3>我的好友</h3>
      <div class="row-actions">
        <el-button plain :loading="loading.friends" @click="loadFriends">刷新</el-button>
        <el-button type="primary" @click="openAddDialog">添加好友</el-button>
      </div>
    </div>

    <div class="data-table-wrapper">
      <el-table v-loading="loading.friends" :data="friends" border stripe empty-text="暂无好友">
        <el-table-column prop="account_no" label="账号" min-width="180" />
        <el-table-column prop="username" label="用户名" min-width="120" />
        <el-table-column prop="server" label="服务器" min-width="120" />
        <el-table-column label="操作" width="100">
          <template #default="scope">
            <el-button type="danger" plain size="small" :loading="loading.action" @click="deleteFriend(scope.row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>
  </section>

  <section class="panel-card">
    <div class="panel-headline">
      <h3>好友请求</h3>
      <div class="row-actions">
        <el-button plain :loading="loading.requests" @click="loadFriendRequests">刷新</el-button>
      </div>
    </div>

    <div class="data-table-wrapper">
      <el-table v-loading="loading.requests" :data="friendRequests" border stripe empty-text="暂无待处理请求">
        <el-table-column prop="from_account_no" label="来自" min-width="180" />
        <el-table-column prop="from_username" label="用户名" min-width="120" />
        <el-table-column label="状态" width="100">
          <template #default="scope">
            <el-tag :type="statusTagType(scope.row.status)">{{ statusLabel(scope.row.status) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="160">
          <template #default="scope">
            <template v-if="scope.row.status === 'pending'">
              <el-button type="success" plain size="small" :loading="loading.action" @click="acceptRequest(scope.row)">接受</el-button>
              <el-button type="danger" plain size="small" :loading="loading.action" @click="rejectRequest(scope.row)">拒绝</el-button>
            </template>
            <span v-else class="muted">{{ statusLabel(scope.row.status) }}</span>
          </template>
        </el-table-column>
      </el-table>
    </div>
  </section>

  <el-dialog v-model="showAddDialog" title="添加好友" class="dialog-sm" append-to-body>
    <el-form @submit.prevent="sendRequest">
      <el-form-item label="用户名">
        <el-input v-model="addUsername" placeholder="请输入好友的用户名" clearable @keyup.enter="sendRequest" />
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="showAddDialog = false">取消</el-button>
      <el-button type="primary" :loading="loading.action" @click="sendRequest">发送请求</el-button>
    </template>
  </el-dialog>
</template>
