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
});

function isValidEmail(email) {
  if (!email) return false;
  return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email);
}

const emailError = ref("");

function validateEmail() {
  if (form.email_enabled && form.email && !isValidEmail(form.email)) {
    emailError.value = "邮箱格式不正确";
  } else {
    emailError.value = "";
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
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.fetch = false;
  }
}

async function saveNotifyConfig() {
  validateEmail();
  if (emailError.value) {
    ElMessage.warning(emailError.value);
    return;
  }
  loading.save = true;
  try {
    await userApi.putMeProfile(props.token, {
      notify_config: {
        email_enabled: form.email_enabled,
        email: form.email,
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
      <h3>邮件通知</h3>
      <div class="row-actions">
        <el-button plain :loading="loading.fetch" @click="loadNotifyConfig">刷新</el-button>
        <el-button type="primary" :loading="loading.save" @click="saveNotifyConfig">保存</el-button>
      </div>
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
      <h3>微信公众号通知</h3>
    </div>
    <el-empty description="即将推出，敬请期待" :image-size="60" />
  </section>
</template>
