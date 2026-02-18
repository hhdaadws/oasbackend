<template>
  <div class="app-shell">
    <header class="hero">
      <div class="hero__left">
        <h1>OAS Cloud Console</h1>
        <p>Vue 3 + Vite + Element Plus</p>
      </div>
      <div class="hero__right">
        <el-tag effect="dark" type="success">PostgreSQL</el-tag>
        <el-tag effect="dark" type="warning">Redis Required</el-tag>
        <el-tag effect="dark" type="info">Agent Pull + Lease</el-tag>
      </div>
    </header>

    <section class="content">
      <el-tabs v-model="activeTab" class="role-tabs">
        <el-tab-pane label="超级管理员" name="super">
          <SuperPanel />
        </el-tab-pane>
        <el-tab-pane label="管理员" name="manager">
          <ManagerPanel />
        </el-tab-pane>
        <el-tab-pane label="普通用户" name="user">
          <UserPanel />
        </el-tab-pane>
      </el-tabs>
    </section>
  </div>
</template>

<script setup>
import { ref } from "vue";
import SuperPanel from "./components/SuperPanel.vue";
import ManagerPanel from "./components/ManagerPanel.vue";
import UserPanel from "./components/UserPanel.vue";

const activeTab = ref("super");
</script>
