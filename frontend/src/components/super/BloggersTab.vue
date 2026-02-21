<script setup>
import { onMounted, reactive, ref, watch } from "vue";
import { ElMessage, ElMessageBox } from "element-plus";
import { parseApiError, superApi } from "../../lib/http";
import { formatTime } from "../../lib/helpers";

const props = defineProps({
  token: { type: String, default: "" },
});

const loading = reactive({ list: false, create: false });
const bloggers = ref([]);
const newName = ref("");

watch(
  () => props.token,
  async (value) => {
    if (!value) { bloggers.value = []; return; }
    await loadBloggers();
  },
  { immediate: true },
);

onMounted(async () => {
  if (props.token) await loadBloggers();
});

async function loadBloggers() {
  loading.list = true;
  try {
    const res = await superApi.listBloggers(props.token);
    bloggers.value = res.data || [];
  } catch (e) {
    ElMessage.error(parseApiError(e));
  } finally {
    loading.list = false;
  }
}

async function createBlogger() {
  const name = newName.value.trim();
  if (!name) { ElMessage.warning("请输入博主名称"); return; }
  loading.create = true;
  try {
    await superApi.createBlogger(props.token, { name });
    ElMessage.success("博主创建成功");
    newName.value = "";
    await loadBloggers();
  } catch (e) {
    ElMessage.error(parseApiError(e));
  } finally {
    loading.create = false;
  }
}

async function deleteBlogger(blogger) {
  try {
    await ElMessageBox.confirm(
      `确认删除博主「${blogger.name}」？已选择该博主的用户将自动切回管理员答案。`,
      "删除确认",
      { confirmButtonText: "确认删除", cancelButtonText: "取消", type: "warning" },
    );
  } catch { return; }
  try {
    await superApi.deleteBlogger(props.token, blogger.id);
    ElMessage.success("删除成功");
    await loadBloggers();
  } catch (e) {
    ElMessage.error(parseApiError(e));
  }
}
</script>

<template>
  <div class="role-dashboard">
    <section class="panel-card">
      <div class="panel-headline">
        <h3>博主管理</h3>
        <el-tag type="info" size="small" style="margin-left: 8px">共 {{ bloggers.length }} 个</el-tag>
      </div>

      <el-alert type="info" :closable="false" show-icon style="margin-bottom: 16px">
        <template #title>
          管理对弈竞猜答案的博主。普通用户可选择跟随某个博主的答案，管理员可为博主配置答案。
        </template>
      </el-alert>

      <div style="display: flex; gap: 8px; margin-bottom: 16px; max-width: 400px">
        <el-input v-model="newName" placeholder="博主名称" size="default" @keyup.enter="createBlogger" />
        <el-button type="primary" :loading="loading.create" @click="createBlogger">添加博主</el-button>
      </div>

      <el-table :data="bloggers" border stripe empty-text="暂无博主" v-loading="loading.list">
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="name" label="名称" min-width="180" />
        <el-table-column label="创建时间" min-width="180">
          <template #default="scope">{{ formatTime(scope.row.created_at) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="120" align="center">
          <template #default="scope">
            <el-button type="danger" link size="small" @click="deleteBlogger(scope.row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </section>
  </div>
</template>
