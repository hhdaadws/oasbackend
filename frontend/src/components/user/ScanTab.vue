<script setup>
import { ref, computed, onMounted, onBeforeUnmount } from "vue";
import { ElMessage } from "element-plus";
import { parseApiError, userApi } from "../../lib/http";

const props = defineProps({
  token: { type: String, default: "" },
});

// === State ===
const phase = ref("idle");
const scanJobId = ref(null);
const loginId = ref("");
const screenshot = ref("");
const queuePosition = ref(0);
const errorMessage = ref("");
const loading = ref(false);
const cooldownSec = ref(0);
const cooldownTimer = ref(null);

let ws = null;
let heartbeatInterval = null;
let statusPollInterval = null;

// === Cooldown steps ===
const COOLDOWN_STEPS = [0, 180, 600, 1800, 3600];

// === WebSocket ===
function connectWS() {
  if (!props.token || !scanJobId.value) return;
  const proto = location.protocol === "https:" ? "wss:" : "ws:";
  const baseUrl = import.meta.env.VITE_API_BASE || "/api/v1";
  const wsUrl = `${proto}//${location.host}${baseUrl}/user/scan/ws?token=${props.token}`;
  ws = new WebSocket(wsUrl);

  ws.onopen = () => {
    console.log("[ScanWS] connected");
  };

  ws.onmessage = (event) => {
    try {
      const msg = JSON.parse(event.data);
      handleWSMessage(msg);
    } catch (e) {
      console.error("[ScanWS] parse error", e);
    }
  };

  ws.onclose = () => {
    console.log("[ScanWS] disconnected");
    if (scanJobId.value && !["done", "failed", "idle"].includes(phase.value)) {
      setTimeout(connectWS, 3000);
    }
  };

  ws.onerror = (err) => {
    console.error("[ScanWS] error", err);
  };
}

function handleWSMessage(msg) {
  switch (msg.type) {
    case "phase_change":
      phase.value = msg.phase;
      if (msg.screenshot) screenshot.value = msg.screenshot;
      break;
    case "need_choice":
      phase.value = `choose_${msg.choice_type}`;
      if (msg.screenshot) screenshot.value = msg.screenshot;
      break;
    case "completed":
      phase.value = "done";
      break;
    case "failed":
      phase.value = "failed";
      errorMessage.value = msg.message || "扫码失败";
      break;
    case "queue_update":
      queuePosition.value = msg.position;
      break;
  }
}

function closeWS() {
  if (ws) {
    ws.onclose = null;
    ws.close();
    ws = null;
  }
}

// === Heartbeat ===
function startHeartbeat() {
  stopHeartbeat();
  heartbeatInterval = setInterval(async () => {
    if (scanJobId.value && props.token) {
      try {
        await userApi.scanHeartbeat(props.token, { scan_job_id: scanJobId.value });
      } catch (e) {
        // silent
      }
    }
  }, 10000);
}

function stopHeartbeat() {
  if (heartbeatInterval) {
    clearInterval(heartbeatInterval);
    heartbeatInterval = null;
  }
}

// === Status polling (fallback for WebSocket) ===
function startStatusPoll() {
  stopStatusPoll();
  statusPollInterval = setInterval(async () => {
    if (!scanJobId.value || !props.token) return;
    try {
      const res = await userApi.scanStatus(props.token);
      if (res.data) {
        const d = res.data;
        if (d.status === "success") {
          phase.value = "done";
        } else if (d.status === "failed" || d.status === "expired" || d.status === "cancelled") {
          phase.value = "failed";
          errorMessage.value = d.error_message || "扫码失败";
        } else if (d.phase && d.phase !== phase.value) {
          if (["choose_system", "choose_zone", "choose_role"].includes(d.phase)) {
            phase.value = d.phase;
          } else {
            phase.value = d.phase;
          }
          if (d.screenshots) {
            const keys = Object.keys(d.screenshots);
            if (keys.length > 0) {
              screenshot.value = d.screenshots[keys[keys.length - 1]];
            }
          }
        }
        if (d.queue_position !== undefined) {
          queuePosition.value = d.queue_position;
        }
        if (d.cooldown_remaining_sec !== undefined) {
          cooldownSec.value = d.cooldown_remaining_sec;
        }
      }
    } catch (e) {
      // silent
    }
  }, 3000);
}

function stopStatusPoll() {
  if (statusPollInterval) {
    clearInterval(statusPollInterval);
    statusPollInterval = null;
  }
}

// === Actions ===
async function startScan() {
  if (!loginId.value) {
    ElMessage.warning("登录ID未加载，请刷新页面重试");
    return;
  }
  loading.value = true;
  try {
    const res = await userApi.scanCreate(props.token, { login_id: loginId.value });
    scanJobId.value = res.data.scan_job_id;
    queuePosition.value = res.data.position_in_queue || 0;
    phase.value = "waiting";
    connectWS();
    startHeartbeat();
    startStatusPoll();
  } catch (e) {
    ElMessage.error(parseApiError(e));
  } finally {
    loading.value = false;
  }
}

async function submitChoice(choiceType, value) {
  loading.value = true;
  try {
    await userApi.scanChoice(props.token, {
      scan_job_id: scanJobId.value,
      choice_type: choiceType,
      value: String(value),
    });
    phase.value = "entering";
  } catch (e) {
    ElMessage.error(parseApiError(e));
  } finally {
    loading.value = false;
  }
}

async function cancelScan() {
  if (!scanJobId.value) return;
  try {
    await userApi.scanCancel(props.token, { scan_job_id: scanJobId.value });
    cleanup();
    ElMessage.info("已取消扫码");
  } catch (e) {
    ElMessage.error(parseApiError(e));
  }
}

function cleanup() {
  closeWS();
  stopHeartbeat();
  stopStatusPoll();
  scanJobId.value = null;
  screenshot.value = "";
  errorMessage.value = "";
  phase.value = "idle";
}

// === Cooldown timer ===
function startCooldownTimer() {
  if (cooldownTimer.value) clearInterval(cooldownTimer.value);
  cooldownTimer.value = setInterval(() => {
    if (cooldownSec.value > 0) {
      cooldownSec.value--;
    } else {
      clearInterval(cooldownTimer.value);
      cooldownTimer.value = null;
    }
  }, 1000);
}

// === Initialization ===
async function loadMyLoginId() {
  if (!props.token) return;
  try {
    const res = await userApi.getMeProfile(props.token);
    if (res.login_id) {
      loginId.value = res.login_id;
    }
  } catch (e) {
    // silent
  }
}

async function checkExistingScan() {
  if (!props.token) return;
  try {
    const res = await userApi.scanStatus(props.token);
    if (res.data && res.data.scan_job_id) {
      scanJobId.value = res.data.scan_job_id;
      phase.value = res.data.phase || "waiting";
      if (res.data.login_id) loginId.value = res.data.login_id;
      if (res.data.screenshots) {
        const keys = Object.keys(res.data.screenshots);
        if (keys.length > 0) screenshot.value = res.data.screenshots[keys[keys.length - 1]];
      }
      connectWS();
      startHeartbeat();
      startStatusPoll();
    }
    if (res.data && res.data.cooldown_remaining_sec > 0) {
      cooldownSec.value = res.data.cooldown_remaining_sec;
      startCooldownTimer();
    }
  } catch (e) {
    // silent
  }
}

// === Visibility change handler ===
function handleVisibilityChange() {
  if (document.hidden && scanJobId.value && !["done", "failed", "idle"].includes(phase.value)) {
    stopHeartbeat();
  } else if (!document.hidden && scanJobId.value) {
    startHeartbeat();
  }
}

onMounted(() => {
  loadMyLoginId();
  checkExistingScan();
  document.addEventListener("visibilitychange", handleVisibilityChange);
});

onBeforeUnmount(() => {
  if (scanJobId.value && !["done", "failed", "idle"].includes(phase.value)) {
    userApi.scanCancel(props.token, { scan_job_id: scanJobId.value }).catch(() => {});
  }
  cleanup();
  if (cooldownTimer.value) clearInterval(cooldownTimer.value);
  document.removeEventListener("visibilitychange", handleVisibilityChange);
});

const cooldownDisplay = computed(() => {
  if (cooldownSec.value <= 0) return "";
  const min = Math.floor(cooldownSec.value / 60);
  const sec = cooldownSec.value % 60;
  return min > 0 ? `${min}分${sec}秒` : `${sec}秒`;
});

const canStartScan = computed(() => {
  return phase.value === "idle" && cooldownSec.value <= 0 && !loading.value && !!loginId.value;
});
</script>

<template>
  <div class="scan-tab">
    <!-- Idle -->
    <div v-if="phase === 'idle'" class="scan-idle">
      <el-alert type="info" :closable="false" show-icon style="margin-bottom: 16px">
        <template #title>自助扫码登录</template>
        <p>点击开始扫码，系统将自动启动模拟器并生成二维码供您扫码登录。</p>
      </el-alert>
      <div style="max-width: 400px">
        <p v-if="loginId" style="margin-bottom: 12px; color: #606266">当前登录ID：<strong>{{ loginId }}</strong></p>
        <el-button type="primary" :disabled="!canStartScan" :loading="loading" @click="startScan">
          {{ cooldownSec > 0 ? `冷却中 (${cooldownDisplay})` : '开始扫码' }}
        </el-button>
      </div>
    </div>

    <!-- Waiting -->
    <div v-else-if="phase === 'waiting'" class="scan-status">
      <el-result icon="info" title="等待中" :sub-title="`排队位置: ${queuePosition > 0 ? '第' + queuePosition + '位' : '即将开始'}`">
        <template #extra>
          <el-button @click="cancelScan">取消</el-button>
        </template>
      </el-result>
    </div>

    <!-- Launching -->
    <div v-else-if="phase === 'launching'" class="scan-status">
      <el-result icon="info" title="正在启动游戏..." sub-title="请稍候，模拟器正在启动并加载游戏">
        <template #extra>
          <el-button @click="cancelScan">取消</el-button>
        </template>
      </el-result>
    </div>

    <!-- QR Code Ready -->
    <div v-else-if="phase === 'qrcode_ready'" class="scan-qrcode">
      <el-alert type="success" :closable="false" show-icon title="请用手机扫描下方二维码" style="margin-bottom: 16px" />
      <div v-if="screenshot" class="screenshot-container">
        <el-image :src="screenshot.startsWith('data:') ? screenshot : 'data:image/png;base64,' + screenshot" alt="二维码" class="screenshot-img" :preview-src-list="[screenshot.startsWith('data:') ? screenshot : 'data:image/png;base64,' + screenshot]" fit="contain" />
      </div>
      <el-button style="margin-top: 12px" @click="cancelScan">取消</el-button>
    </div>

    <!-- Choose System -->
    <div v-else-if="phase === 'choose_system'" class="scan-choice">
      <el-alert type="warning" :closable="false" show-icon title="请选择您的系统" style="margin-bottom: 16px" />
      <div class="choice-buttons">
        <el-button type="primary" size="large" :loading="loading" @click="submitChoice('system', 'ios')">
          iOS (苹果)
        </el-button>
        <el-button type="success" size="large" :loading="loading" @click="submitChoice('system', 'android')">
          Android (安卓)
        </el-button>
      </div>
      <el-button style="margin-top: 12px" @click="cancelScan">取消</el-button>
    </div>

    <!-- Choose Zone -->
    <div v-else-if="phase === 'choose_zone'" class="scan-choice">
      <el-alert type="warning" :closable="false" show-icon title="请选择您的大区" style="margin-bottom: 16px" />
      <div v-if="screenshot" class="screenshot-container" style="margin-bottom: 16px">
        <el-image :src="screenshot.startsWith('data:') ? screenshot : 'data:image/png;base64,' + screenshot" alt="选区" class="screenshot-img" :preview-src-list="[screenshot.startsWith('data:') ? screenshot : 'data:image/png;base64,' + screenshot]" fit="contain" />
      </div>
      <div class="choice-buttons">
        <el-button v-for="n in 4" :key="n" type="primary" size="large" :loading="loading" @click="submitChoice('zone', n)">
          第 {{ n }} 区
        </el-button>
      </div>
      <el-button style="margin-top: 12px" @click="cancelScan">取消</el-button>
    </div>

    <!-- Choose Role -->
    <div v-else-if="phase === 'choose_role'" class="scan-choice">
      <el-alert type="warning" :closable="false" show-icon title="请选择您的角色" style="margin-bottom: 16px" />
      <div v-if="screenshot" class="screenshot-container" style="margin-bottom: 16px">
        <el-image :src="screenshot.startsWith('data:') ? screenshot : 'data:image/png;base64,' + screenshot" alt="选角色" class="screenshot-img" :preview-src-list="[screenshot.startsWith('data:') ? screenshot : 'data:image/png;base64,' + screenshot]" fit="contain" />
      </div>
      <div class="choice-buttons">
        <el-button v-for="n in 4" :key="n" type="primary" size="large" :loading="loading" @click="submitChoice('role', n)">
          角色 {{ n }}
        </el-button>
      </div>
      <el-button style="margin-top: 12px" @click="cancelScan">取消</el-button>
    </div>

    <!-- Entering Game -->
    <div v-else-if="phase === 'entering'" class="scan-status">
      <el-result icon="info" title="正在进入游戏..." sub-title="请耐心等待">
        <template #extra>
          <el-button @click="cancelScan">取消</el-button>
        </template>
      </el-result>
    </div>

    <!-- Pulling Data -->
    <div v-else-if="phase === 'pulling_data'" class="scan-status">
      <el-result icon="info" title="正在抓取账号数据..." sub-title="即将完成" />
    </div>

    <!-- Done -->
    <div v-else-if="phase === 'done'" class="scan-status">
      <el-result icon="success" title="扫码完成！" :sub-title="`账号 ${loginId} 已成功绑定`">
        <template #extra>
          <el-button type="primary" @click="cleanup">完成</el-button>
        </template>
      </el-result>
    </div>

    <!-- Failed -->
    <div v-else-if="phase === 'failed'" class="scan-status">
      <el-result icon="error" title="扫码失败" :sub-title="errorMessage || '未知错误'">
        <template #extra>
          <el-button type="primary" :disabled="cooldownSec > 0" @click="cleanup">
            {{ cooldownSec > 0 ? `重试 (${cooldownDisplay})` : '重新扫码' }}
          </el-button>
        </template>
      </el-result>
    </div>
  </div>
</template>

<style scoped>
.scan-tab {
  padding: 16px 0;
}
.scan-idle {
  max-width: 500px;
}
.scan-status {
  display: flex;
  justify-content: center;
}
.scan-qrcode {
  text-align: center;
}
.scan-choice {
  text-align: center;
}
.screenshot-container {
  display: inline-block;
  border: 2px solid #e4e7ed;
  border-radius: 8px;
  overflow: hidden;
  max-width: 480px;
}
.screenshot-img {
  width: 100%;
  display: block;
  cursor: pointer;
}
.choice-buttons {
  display: flex;
  gap: 16px;
  justify-content: center;
  flex-wrap: wrap;
}
</style>
