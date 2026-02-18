<template>
  <div class="role-dashboard">
    <el-empty
      v-if="!token"
      description="请先在上方登录普通用户，再进入个人中心"
      :image-size="130"
    >
      <el-button type="primary" @click="$emit('logout')">返回登录区</el-button>
    </el-empty>

    <template v-else>
      <section class="panel-grid panel-grid--user">
        <article class="panel-card panel-card--highlight">
          <div class="panel-headline">
            <h3>用户会话信息</h3>
            <el-tag type="success">已登录</el-tag>
          </div>
          <p class="tip-text">账号：{{ accountNo || '未记录' }}</p>
          <el-button type="danger" plain @click="$emit('logout')">退出当前用户会话</el-button>
        </article>

        <article class="panel-card panel-card--compact">
          <div class="panel-headline">
            <h3>激活码续费</h3>
            <el-tag type="warning">一次性激活码</el-tag>
          </div>
          <el-form :model="redeemForm" inline>
            <el-form-item label="激活码">
              <el-input v-model="redeemForm.code" placeholder="uac_xxx" clearable />
            </el-form-item>
            <el-form-item>
              <el-button type="success" :loading="loading.redeem" @click="redeemCode">兑换续费</el-button>
            </el-form-item>
          </el-form>
          <el-alert
            type="info"
            :closable="false"
            title="续费规则：未过期顺延，已过期从当前时刻重算"
          />
        </article>
      </section>

      <section class="panel-grid panel-grid--user-detail">
        <article class="panel-card">
          <div class="panel-headline">
            <h3>我的任务配置</h3>
            <div class="row-actions">
              <el-button plain :loading="loading.tasks" @click="loadMeTasks">加载</el-button>
              <el-button type="primary" :loading="loading.saveTasks" @click="saveMeTasks">保存</el-button>
            </div>
          </div>
          <el-input
            v-model="taskConfigRaw"
            type="textarea"
            :rows="16"
            placeholder='{"签到":{"enabled":true,"next_time":"09:00"}}'
          />
          <p class="tip-text">建议仅修改已知字段，系统会按“现有配置 + 提交配置”进行合并更新。</p>
        </article>

        <article class="panel-card">
          <div class="panel-headline">
            <h3>我的执行日志</h3>
            <div class="row-actions">
              <el-input-number v-model="logsLimit" :min="10" :max="200" size="small" />
              <el-button plain :loading="loading.logs" @click="loadMeLogs">刷新日志</el-button>
            </div>
          </div>
          <el-table :data="logs" border stripe height="390" empty-text="暂无日志">
            <el-table-column prop="event_at" label="时间" min-width="170" />
            <el-table-column prop="event_type" label="事件" width="130" />
            <el-table-column prop="error_code" label="错误码" width="130" />
            <el-table-column prop="message" label="消息" min-width="220" />
          </el-table>
        </article>
      </section>
    </template>
  </div>
</template>

<script setup>
import { onMounted, reactive, ref, watch } from "vue";
import { ElMessage } from "element-plus";
import { parseApiError, userApi } from "../lib/http";

const props = defineProps({
  token: {
    type: String,
    default: "",
  },
  accountNo: {
    type: String,
    default: "",
  },
});

defineEmits(["logout"]);

const loading = reactive({
  redeem: false,
  tasks: false,
  saveTasks: false,
  logs: false,
});

const redeemForm = reactive({ code: "" });
const taskConfigRaw = ref("{}");
const logs = ref([]);
const logsLimit = ref(80);

watch(
  () => props.token,
  async (value) => {
    if (!value) {
      taskConfigRaw.value = "{}";
      logs.value = [];
      return;
    }
    await Promise.all([loadMeTasks(), loadMeLogs()]);
  },
  { immediate: true },
);

onMounted(async () => {
  if (props.token) {
    await Promise.all([loadMeTasks(), loadMeLogs()]);
  }
});

async function redeemCode() {
  const code = redeemForm.code.trim();
  if (!code) {
    ElMessage.warning("请输入激活码");
    return;
  }
  loading.redeem = true;
  try {
    await userApi.redeemCode(props.token, { code });
    ElMessage.success("续费兑换成功");
    redeemForm.code = "";
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.redeem = false;
  }
}

async function loadMeTasks() {
  loading.tasks = true;
  try {
    const response = await userApi.getMeTasks(props.token);
    taskConfigRaw.value = JSON.stringify(response.task_config || {}, null, 2);
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.tasks = false;
  }
}

async function saveMeTasks() {
  let parsed = {};
  try {
    parsed = JSON.parse(taskConfigRaw.value || "{}");
  } catch (error) {
    ElMessage.error("任务配置 JSON 格式不合法");
    return;
  }

  loading.saveTasks = true;
  try {
    const response = await userApi.putMeTasks(props.token, { task_config: parsed });
    taskConfigRaw.value = JSON.stringify(response.task_config || {}, null, 2);
    ElMessage.success("任务配置保存成功");
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.saveTasks = false;
  }
}

async function loadMeLogs() {
  loading.logs = true;
  try {
    const response = await userApi.getMeLogs(props.token, logsLimit.value);
    logs.value = response.items || [];
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.logs = false;
  }
}
</script>
