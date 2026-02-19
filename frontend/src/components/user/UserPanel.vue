<script setup>
import { ref } from "vue";
import AccountTab from "./AccountTab.vue";
import RedeemTab from "./RedeemTab.vue";
import AssetsTab from "./AssetsTab.vue";
import TasksTab from "./TasksTab.vue";
import UserLogsTab from "./UserLogsTab.vue";
import NotifyTab from "./NotifyTab.vue";

const props = defineProps({
  token: { type: String, default: "" },
  accountNo: { type: String, default: "" },
});
defineEmits(["logout"]);

const activeTab = ref("account");
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
          <AccountTab :token="token" :account-no="accountNo" @logout="$emit('logout')" />
        </el-tab-pane>
        <el-tab-pane label="激活码续费" name="redeem">
          <RedeemTab :token="token" />
        </el-tab-pane>
        <el-tab-pane label="我的资产" name="assets">
          <AssetsTab :token="token" />
        </el-tab-pane>
        <el-tab-pane label="任务配置" name="tasks">
          <TasksTab :token="token" />
        </el-tab-pane>
        <el-tab-pane label="执行日志" name="logs">
          <UserLogsTab :token="token" />
        </el-tab-pane>
        <el-tab-pane label="通知设置" name="notify">
          <NotifyTab :token="token" />
        </el-tab-pane>
      </el-tabs>
    </template>
  </div>
</template>
