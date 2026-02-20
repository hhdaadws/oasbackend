<script setup>
import { onMounted, reactive, ref, watch } from "vue";
import { ElMessage } from "element-plus";
import { parseApiError, userApi } from "../../lib/http";

const props = defineProps({
  token: { type: String, default: "" },
});

const loading = reactive({ fetch: false, save: false });

const form = reactive({
  email_enabled: false,
  email: "",
  wechat_enabled: false,
  wechat_miao_code: "",
});

function isValidEmail(email) {
  if (!email) return false;
  return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email);
}

const emailError = ref("");
const miaoCodeError = ref("");

function validateEmail() {
  if (form.email_enabled && form.email && !isValidEmail(form.email)) {
    emailError.value = "邮箱格式不正确";
  } else {
    emailError.value = "";
  }
}

function validateMiaoCode() {
  if (form.wechat_enabled && !form.wechat_miao_code.trim()) {
    miaoCodeError.value = "请填写喵码";
  } else if (form.wechat_miao_code && !/^[a-zA-Z0-9]+$/.test(form.wechat_miao_code)) {
    miaoCodeError.value = "喵码只能包含字母和数字";
  } else {
    miaoCodeError.value = "";
  }
}

async function loadNotifyConfig() {
  if (!props.token) return;
  loading.fetch = true;
  try {
    const res = await userApi.getMeProfile(props.token);
    const nc = res.notify_config || {};
    form.email_enabled = !!nc.email_enabled;
    form.email = nc.email || "";
    form.wechat_enabled = !!nc.wechat_enabled;
    form.wechat_miao_code = nc.wechat_miao_code || "";
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.fetch = false;
  }
}

async function saveNotifyConfig() {
  validateEmail();
  validateMiaoCode();
  if (emailError.value || miaoCodeError.value) {
    ElMessage.warning(emailError.value || miaoCodeError.value);
    return;
  }
  loading.save = true;
  try {
    await userApi.putMeProfile(props.token, {
      notify_config: {
        email_enabled: form.email_enabled,
        email: form.email,
        wechat_enabled: form.wechat_enabled,
        wechat_miao_code: form.wechat_miao_code,
      },
    });
    ElMessage.success("通知设置已保存");
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.save = false;
  }
}

watch(
  () => props.token,
  async (value) => {
    if (!value) return;
    await loadNotifyConfig();
  },
  { immediate: true },
);

onMounted(async () => {
  if (props.token) await loadNotifyConfig();
});
</script>

<template>
  <section class="panel-card">
    <div class="panel-headline">
      <h3>通知设置</h3>
      <div class="row-actions">
        <el-button plain :loading="loading.fetch" @click="loadNotifyConfig">刷新</el-button>
        <el-button type="primary" :loading="loading.save" @click="saveNotifyConfig">保存</el-button>
      </div>
    </div>
  </section>

  <section class="panel-card">
    <div class="panel-headline">
      <h3>邮件通知</h3>
    </div>
    <el-form label-width="120px" class="mt-12">
      <el-form-item label="启用邮件通知">
        <el-switch v-model="form.email_enabled" />
      </el-form-item>
      <el-form-item label="邮箱地址" :error="emailError">
        <el-input
          v-model="form.email"
          placeholder="请输入邮箱地址"
          clearable
          :disabled="!form.email_enabled"
          @blur="validateEmail"
        />
      </el-form-item>
    </el-form>
  </section>

  <section class="panel-card">
    <div class="panel-headline">
      <h3>微信通知（喵提醒）</h3>
    </div>
    <el-form label-width="120px" class="mt-12">
      <el-form-item label="启用微信通知">
        <el-switch v-model="form.wechat_enabled" />
      </el-form-item>
      <el-form-item label="喵码" :error="miaoCodeError">
        <el-input
          v-model="form.wechat_miao_code"
          placeholder="请输入喵提醒的喵码"
          clearable
          :disabled="!form.wechat_enabled"
          @blur="validateMiaoCode"
        />
      </el-form-item>
      <el-form-item>
        <el-text type="info" size="small">
          请在微信搜索并关注「喵提醒」公众号，创建提醒后获取专属喵码填入此处。任务完成或失败时将收到微信推送通知。
        </el-text>
      </el-form-item>
    </el-form>
  </section>
</template>
