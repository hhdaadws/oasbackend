<script setup>
defineProps({
  token: { type: String, default: "" },
});
</script>

<template>
  <section class="panel-card">
    <div class="panel-headline">
      <h3>通知管理</h3>
    </div>
    <el-empty description="暂无可配置的通知项" :image-size="80" />
  </section>
</template>
