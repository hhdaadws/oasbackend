<script setup>
import { onMounted, reactive, watch } from "vue";
import { ElMessage } from "element-plus";
import { parseApiError, userApi } from "../../lib/http";
import { ASSET_FIELDS } from "../../lib/helpers";

const props = defineProps({
  token: { type: String, default: "" },
});

const loading = reactive({ assets: false });
const assetFields = ASSET_FIELDS;

const meAssets = reactive({
  stamina: 0, gouyu: 0, lanpiao: 0, gold: 0,
  gongxun: 0, xunzhang: 0, tupo_ticket: 0,
  fanhe_level: 1, jiuhu_level: 1, liao_level: 0,
});

function syncAssets(assets) {
  const incoming = assets || {};
  Object.keys(meAssets).forEach((key) => {
    meAssets[key] = Number(incoming[key] ?? meAssets[key] ?? 0);
  });
}

async function loadMeAssets() {
  if (!props.token) return;
  loading.assets = true;
  try {
    const response = await userApi.getMeAssets(props.token);
    syncAssets(response.assets || {});
  } catch (error) {
    ElMessage.error(parseApiError(error));
  } finally {
    loading.assets = false;
  }
}

watch(
  () => props.token,
  async (value) => {
    if (!value) return;
    await loadMeAssets();
  },
  { immediate: true },
);

onMounted(async () => {
  if (props.token) await loadMeAssets();
});
</script>

<template>
  <section class="panel-card">
    <div class="panel-headline">
      <h3>我的资产</h3>
      <el-button plain :loading="loading.assets" @click="loadMeAssets">刷新资产</el-button>
    </div>
    <div class="stats-grid">
      <div v-for="asset in assetFields" :key="asset.key" class="stat-item">
        <span class="stat-label">{{ asset.label }}</span>
        <strong class="stat-value">{{ meAssets[asset.key] ?? 0 }}</strong>
      </div>
    </div>
  </section>
</template>
