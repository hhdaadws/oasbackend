<template>
  <div class="role-dashboard">
    <section class="panel-card">
      <div class="panel-headline">
        <h3>对弈竞猜答案配置</h3>
        <el-tag v-if="currentWindow" type="warning" size="small" style="margin-left: 8px">当前窗口: {{ currentWindow }}</el-tag>
        <el-tag v-else type="info" size="small" style="margin-left: 8px">非竞猜时段</el-tag>
      </div>

      <el-alert type="info" :closable="false" show-icon style="margin-bottom: 16px">
        <template #title>
          只能配置当前时间窗口的答案（左/右）。已过窗口不可修改，未来窗口需等待时间到达。
        </template>
      </el-alert>

      <!-- 我的答案 -->
      <h4 style="margin-bottom: 12px">我的答案</h4>
      <el-form label-width="100px" style="max-width: 520px; margin-bottom: 24px">
        <el-form-item v-for="w in windows" :key="'my-' + w" :label="w">
          <el-radio-group
            v-model="myForm[w]"
            :disabled="w !== currentWindow"
            @change="saveMyAnswer(w)"
          >
            <el-radio value="左">左</el-radio>
            <el-radio value="右">右</el-radio>
          </el-radio-group>
          <el-tag v-if="w === currentWindow" type="success" size="small" style="margin-left: 8px">当前</el-tag>
          <el-tag v-else-if="isPastWindow(w)" type="info" size="small" style="margin-left: 8px">已过</el-tag>
          <el-tag v-else type="info" size="small" style="margin-left: 8px">未到</el-tag>
        </el-form-item>
      </el-form>

      <!-- 博主答案配置 -->
      <el-divider />
      <h4 style="margin-bottom: 12px">博主答案配置</h4>

      <div v-if="bloggers.length === 0" style="color: #909399; margin-bottom: 16px">暂无博主，请联系超级管理员添加。</div>

      <template v-else>
        <el-form label-width="100px" style="max-width: 520px">
          <el-form-item label="选择博主">
            <el-select v-model="selectedBloggerId" placeholder="选择博主" @change="loadBloggerAnswers">
              <el-option v-for="b in bloggers" :key="b.id" :label="b.name" :value="b.id" />
            </el-select>
          </el-form-item>
        </el-form>

        <el-form v-if="selectedBloggerId" label-width="100px" style="max-width: 520px">
          <el-form-item v-for="w in windows" :key="'blogger-' + w" :label="w">
            <el-radio-group
              v-model="bloggerForm[w]"
              :disabled="w !== currentWindow"
              @change="saveBloggerAnswer(w)"
            >
              <el-radio value="左">左</el-radio>
              <el-radio value="右">右</el-radio>
            </el-radio-group>
            <el-tag v-if="w === currentWindow" type="success" size="small" style="margin-left: 8px">当前</el-tag>
            <el-tag v-else-if="isPastWindow(w)" type="info" size="small" style="margin-left: 8px">已过</el-tag>
            <el-tag v-else type="info" size="small" style="margin-left: 8px">未到</el-tag>
          </el-form-item>
        </el-form>
      </template>

      <el-button style="margin-top: 12px" @click="refreshAll" :loading="loading">刷新</el-button>
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

const myForm = reactive({});
const bloggerForm = reactive({});
windows.forEach((w) => { myForm[w] = ""; bloggerForm[w] = ""; });

const currentWindow = ref("");
const loading = ref(false);
const bloggers = ref([]);
const selectedBloggerId = ref(null);

function isPastWindow(w) {
  if (!currentWindow.value) return false;
  return w < currentWindow.value;
}

async function loadMyAnswers() {
  if (!props.token) return;
  try {
    const res = await managerApi.getDuiyiAnswers(props.token);
    const data = res.data;
    currentWindow.value = data.current_window || "";
    for (const w of windows) {
      myForm[w] = data.answers[w] || "";
    }
  } catch (e) {
    ElMessage.error(parseApiError(e));
  }
}

async function saveMyAnswer(w) {
  if (!props.token || !myForm[w]) return;
  try {
    const res = await managerApi.putDuiyiAnswers(props.token, { window: w, answer: myForm[w] });
    const data = res.data;
    currentWindow.value = data.current_window || currentWindow.value;
    for (const win of windows) {
      myForm[win] = data.answers[win] || "";
    }
    ElMessage.success("我的答案已保存");
  } catch (e) {
    ElMessage.error(parseApiError(e));
  }
}

async function loadBloggers() {
  if (!props.token) return;
  try {
    const res = await managerApi.listBloggers(props.token);
    bloggers.value = res.data || [];
  } catch (e) {
    ElMessage.error(parseApiError(e));
  }
}

async function loadBloggerAnswers() {
  if (!props.token || !selectedBloggerId.value) return;
  try {
    const res = await managerApi.getBloggerAnswers(props.token, selectedBloggerId.value);
    const data = res.data;
    currentWindow.value = data.current_window || currentWindow.value;
    for (const w of windows) {
      bloggerForm[w] = data.answers[w] || "";
    }
  } catch (e) {
    ElMessage.error(parseApiError(e));
  }
}

async function saveBloggerAnswer(w) {
  if (!props.token || !selectedBloggerId.value || !bloggerForm[w]) return;
  try {
    const res = await managerApi.putBloggerAnswer(props.token, selectedBloggerId.value, {
      window: w,
      answer: bloggerForm[w],
    });
    const data = res.data;
    currentWindow.value = data.current_window || currentWindow.value;
    for (const win of windows) {
      bloggerForm[win] = data.answers[win] || "";
    }
    ElMessage.success("博主答案已保存");
  } catch (e) {
    ElMessage.error(parseApiError(e));
  }
}

async function refreshAll() {
  loading.value = true;
  await Promise.all([loadMyAnswers(), loadBloggers()]);
  if (selectedBloggerId.value) await loadBloggerAnswers();
  loading.value = false;
}

onMounted(async () => {
  if (props.token) {
    await Promise.all([loadMyAnswers(), loadBloggers()]);
  }
});
</script>
