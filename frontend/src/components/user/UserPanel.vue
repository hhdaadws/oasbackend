<script setup>
import { ref } from "vue";
import AccountTab from "./AccountTab.vue";
import RedeemTab from "./RedeemTab.vue";
import AssetsTab from "./AssetsTab.vue";
import TasksTab from "./TasksTab.vue";
import UserLogsTab from "./UserLogsTab.vue";
import NotifyTab from "./NotifyTab.vue";
import ScanTab from "./ScanTab.vue";
import FriendsTab from "./FriendsTab.vue";
import TeamTab from "./TeamTab.vue";

const props = defineProps({
  token: { type: String, default: "" },
  accountNo: { type: String, default: "" },
});
defineEmits(["logout"]);

const activeTab = ref("account");
const canViewLogs = ref(false);
const userType = ref("daily");

function onCanViewLogs(val) {
  canViewLogs.value = val;
}

function onUserType(val) {
  userType.value = val || "daily";
}
</script>

<template>
  <div class="role-dashboard">
    <el-empty
      v-if="!token"
      class="empty-center"
      description="请先登录普通用户，再进入个人中心"
      :image-size="100"
    >
      <el-button type="primary" @click="$emit('logout')">返回登录区</el-button>
    </el-empty>

    <template v-else>
      <el-tabs v-model="activeTab" class="module-tabs">
        <el-tab-pane label="账号信息" name="account">
          <AccountTab :token="token" :account-no="accountNo" @logout="$emit('logout')" @can-view-logs="onCanViewLogs" @user-type="onUserType" />
        </el-tab-pane>
        <el-tab-pane label="续费" name="redeem">
          <RedeemTab :token="token" />
        </el-tab-pane>
        <el-tab-pane label="我的资产" name="assets">
          <AssetsTab :token="token" />
        </el-tab-pane>
        <el-tab-pane label="任务配置" name="tasks">
          <TasksTab :token="token" />
        </el-tab-pane>
        <el-tab-pane v-if="canViewLogs" label="执行日志" name="logs">
          <UserLogsTab :token="token" />
        </el-tab-pane>
        <el-tab-pane label="通知管理" name="notify">
          <NotifyTab :token="token" />
        </el-tab-pane>
        <el-tab-pane label="自助扫码" name="scan">
          <ScanTab :token="token" />
        </el-tab-pane>
        <el-tab-pane v-if="userType === 'jingzhi'" label="好友" name="friends">
          <FriendsTab :token="token" />
        </el-tab-pane>
        <el-tab-pane v-if="userType === 'jingzhi'" label="组队" name="team">
          <TeamTab :token="token" />
        </el-tab-pane>
      </el-tabs>
    </template>
  </div>
</template>
