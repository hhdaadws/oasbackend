<script setup>
import { reactive } from "vue";
import { ElMessage } from "element-plus";
import { parseApiError, userApi } from "../../lib/http";
import { useTaskTemplates } from "../../composables/useTaskTemplates";

const props = defineProps({
  token: { type: String, default: "" },
});

const loading = reactive({ redeem: false });
const redeemForm = reactive({ code: "" });
const { ensureTaskTemplates } = useTaskTemplates();

async function redeemCode() {
  const code = redeemForm.code.trim();
  if (!code) { ElMessage.warning("请输入激活码"); return; }
  loading.redeem = true;
  try {
    const response = await userApi.redeemCode(props.token, { code });
    await ensureTaskTemplates(response.user_type || "daily");
    ElMessage.success("续费兑换成功");
    redeemForm.code = "";
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.redeem = false;
  }
}
</script>

<template>
  <section class="panel-card">
    <div class="panel-headline">
      <h3>激活码续费</h3>
      <el-tag type="warning">一次性激活码</el-tag>
    </div>
    <el-form :model="redeemForm" inline>
      <el-form-item label="激活码">
        <el-input v-model="redeemForm.code" placeholder="激活码示例：uac_xxx" clearable />
      </el-form-item>
      <el-form-item>
        <el-button type="success" :loading="loading.redeem" @click="redeemCode">兑换续费</el-button>
      </el-form-item>
    </el-form>
  </section>
</template>
