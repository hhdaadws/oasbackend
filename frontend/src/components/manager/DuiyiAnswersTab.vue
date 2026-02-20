<template>
  <div class="role-dashboard">
    <section class="panel-card">
      <div class="panel-headline">
        <h3>对弈竞猜答案配置</h3>
        <el-tag v-if="dateLabel" type="success" size="small" style="margin-left: 8px">{{ dateLabel }}</el-tag>
        <el-tag v-else type="info" size="small" style="margin-left: 8px">今日未配置</el-tag>
      </div>

      <el-alert type="info" :closable="false" show-icon style="margin-bottom: 16px">
        <template #title>
          配置今日每个时间窗口的答案（左/右）。未配置答案的窗口不会为下属用户生成任务。每日答案独立，次日自动失效。
        </template>
      </el-alert>

      <el-form label-width="100px" style="max-width: 420px">
        <el-form-item v-for="w in windows" :key="w" :label="w">
          <el-radio-group v-model="form[w]">
            <el-radio :value="''">不配置</el-radio>
            <el-radio value="左">左</el-radio>
            <el-radio value="右">右</el-radio>
          </el-radio-group>
        </el-form-item>

        <el-form-item>
          <el-button @click="load" :loading="loading">刷新</el-button>
          <el-button type="primary" @click="save" :loading="saving">保存</el-button>
        </el-form-item>
      </el-form>
    </section>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from "vue";
import { ElMessage } from "element-plus";
import { managerApi, parseApiError } from "../../lib/http";

const props = defineProps({
  token: { type: String, default: "" },
});

const windows = ["10:00", "12:00", "14:00", "16:00", "18:00", "20:00", "22:00"];

const form = reactive({});
windows.forEach((w) => (form[w] = ""));

const dateLabel = ref(null);
const loading = ref(false);
const saving = ref(false);

async function load() {
  if (!props.token) return;
  loading.value = true;
  try {
    const res = await managerApi.getDuiyiAnswers(props.token);
    const data = res.data;
    dateLabel.value = data.date || null;
    for (const w of windows) {
      form[w] = data.answers[w] || "";
    }
  } catch (e) {
    ElMessage.error(parseApiError(e));
  } finally {
    loading.value = false;
  }
}

async function save() {
  if (!props.token) return;
  saving.value = true;
  try {
    const answers = {};
    for (const w of windows) {
      answers[w] = form[w] || null;
    }
    const res = await managerApi.putDuiyiAnswers(props.token, { answers });
    const data = res.data;
    dateLabel.value = data.date || null;
    for (const w of windows) {
      form[w] = data.answers[w] || "";
    }
    ElMessage.success("保存成功");
  } catch (e) {
    ElMessage.error(parseApiError(e));
  } finally {
    saving.value = false;
  }
}

onMounted(load);
</script>
